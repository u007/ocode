package messaging

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/u007/ocode/internal/crashguard"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	_ "modernc.org/sqlite"
)

type WhatsAppProvider struct {
	acc       Account
	client    *whatsmeow.Client
	container *sqlstore.Container
	cancel    context.CancelFunc
	status    string
	qr        string
	mu        sync.Mutex
	dataDir   string
}

func NewWhatsAppProvider(acc Account, dataDir string) *WhatsAppProvider {
	return &WhatsAppProvider{acc: acc, status: "disconnected", dataDir: dataDir}
}

func (p *WhatsAppProvider) Name() string { return "whatsapp" }

func (p *WhatsAppProvider) Start(ctx context.Context, acc Account, store *Store, onInbound InboundHandler) error {
	p.acc = acc
	dir := filepath.Join(p.dataDir, "whatsapp", acc.ID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	dbPath := filepath.Join(dir, "session.db")
	container, err := sqlstore.New(ctx, "sqlite", "file:"+dbPath+"?_pragma=foreign_keys(1)", waLog.Noop)
	if err != nil {
		return err
	}
	p.container = container
	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		return err
	}
	client := whatsmeow.NewClient(deviceStore, waLog.Noop)
	p.client = client

	client.AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) {
		case *events.Message:
			chatID := v.Info.Chat.String()
			sender := v.Info.Sender.String()
			name := v.Info.PushName
			if name == "" {
				name = sender
			}
			text := ""
			if v.Message != nil {
				if c := v.Message.GetConversation(); c != "" {
					text = c
				} else if e := v.Message.GetExtendedTextMessage(); e != nil {
					text = e.GetText()
				}
			}
			_ = store.UpsertChat(Chat{AccountID: acc.ID, ID: chatID, Title: chatID, Type: "dm", LastMessageAt: time.Now()})
			onInbound(Message{
				ID:         v.Info.ID,
				AccountID:  acc.ID,
				ChatID:     chatID,
				SenderID:   sender,
				SenderName: name,
				Direction:  "in",
				Text:       text,
				Timestamp:  v.Info.Timestamp,
			})
		}
	})

	if client.Store.ID == nil {
		// Need pairing; get QR
		qrChan, err := client.GetQRChannel(ctx)
		if err != nil {
			return err
		}
		if err := client.Connect(); err != nil {
			return err
		}
		crashguard.Go(func() {
			for evt := range qrChan {
				p.mu.Lock()
				if evt.Event == "code" {
					p.qr = evt.Code
					p.status = "awaiting_qr"
				} else {
					p.status = evt.Event
				}
				p.mu.Unlock()
			}
		})
		p.mu.Lock()
		p.status = "awaiting_qr"
		p.mu.Unlock()
		return nil
	}

	if err := client.Connect(); err != nil {
		return err
	}
	p.mu.Lock()
	p.status = "connected"
	p.mu.Unlock()
	return nil
}

func (p *WhatsAppProvider) Stop() {
	if p.client != nil {
		p.client.Disconnect()
	}
	if p.container != nil {
		_ = p.container.Close()
	}
	if p.cancel != nil {
		p.cancel()
	}
	p.mu.Lock()
	p.status = "disconnected"
	p.mu.Unlock()
}

func (p *WhatsAppProvider) Send(ctx context.Context, chatID, text string) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("not connected")
	}
	jid, err := types.ParseJID(chatID)
	if err != nil {
		return "", err
	}
	resp, err := p.client.SendMessage(ctx, jid, &waProto.Message{Conversation: &text})
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

func (p *WhatsAppProvider) Status() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.qr != "" {
		return "awaiting_qr:" + p.qr
	}
	return p.status
}
