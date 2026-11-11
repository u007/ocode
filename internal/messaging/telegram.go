package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/u007/ocode/internal/crashguard"
)

type TelegramProvider struct {
	acc    Account
	bot    *tgbotapi.BotAPI
	cancel context.CancelFunc
	status string
	mu     sync.Mutex
}

func NewTelegramProvider(acc Account) *TelegramProvider {
	return &TelegramProvider{acc: acc, status: "disconnected"}
}

func (p *TelegramProvider) Name() string { return "telegram" }

func (p *TelegramProvider) Start(ctx context.Context, acc Account, store *Store, onInbound InboundHandler) error {
	p.acc = acc
	bot, err := tgbotapi.NewBotAPI(strings.TrimSpace(acc.Credentials))
	if err != nil {
		return err
	}
	p.bot = bot
	p.mu.Lock()
	p.status = "connected"
	p.mu.Unlock()
	cctx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	crashguard.Go(func() {
		u := tgbotapi.NewUpdate(0)
		u.Timeout = 30
		updates := bot.GetUpdatesChan(u)
		for {
			select {
			case <-cctx.Done():
				return
			case upd := <-updates:
				if upd.Message == nil {
					continue
				}
				m := upd.Message
				chatID := fmt.Sprintf("%d", m.Chat.ID)
				title := m.Chat.Title
				if title == "" {
					title = m.Chat.FirstName
				}
				if title == "" {
					title = chatID
				}
				_ = store.UpsertChat(Chat{AccountID: acc.ID, ID: chatID, Title: title, Type: chatType(m.Chat), LastMessageAt: time.Now()})
				msg := Message{
					ID:         fmt.Sprintf("%d", m.MessageID),
					AccountID:  acc.ID,
					ChatID:     chatID,
					SenderID:   fmt.Sprintf("%d", m.From.ID),
					SenderName: strings.TrimSpace(m.From.FirstName + " " + m.From.LastName),
					Direction:  "in",
					Text:       m.Text,
					Timestamp:  time.Unix(int64(m.Date), 0),
				}
				if m.Photo != nil && len(m.Photo) > 0 {
					msg.MediaURL = "photo"
				}
				raw, _ := json.Marshal(m)
				msg.Raw = string(raw)
				onInbound(msg)
			}
		}
	})
	return nil
}

func (p *TelegramProvider) Stop() {
	if p.bot != nil {
		p.bot.StopReceivingUpdates()
	}
	if p.cancel != nil {
		p.cancel()
	}
	p.mu.Lock()
	p.status = "disconnected"
	p.mu.Unlock()
}

func (p *TelegramProvider) Send(ctx context.Context, chatID, text string) (string, error) {
	if p.bot == nil {
		return "", fmt.Errorf("not connected")
	}
	cid, err := parseChatID(chatID)
	if err != nil {
		return "", err
	}
	msg := tgbotapi.NewMessage(cid, text)
	sent, err := p.bot.Send(msg)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d", sent.MessageID), nil
}

func (p *TelegramProvider) Status() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

func chatType(c *tgbotapi.Chat) string {
	if c.IsPrivate() {
		return "dm"
	}
	if c.IsGroup() || c.IsSuperGroup() {
		return "group"
	}
	if c.IsChannel() {
		return "channel"
	}
	return "dm"
}

func parseChatID(s string) (int64, error) {
	var id int64
	_, err := fmt.Sscanf(s, "%d", &id)
	return id, err
}
