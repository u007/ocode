package messaging

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/u007/ocode/internal/crashguard"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var graphAPIBase = "https://graph.facebook.com/v18.0"

type messengerCreds struct {
	PageToken      string   `json:"page_token"`
	VerifyToken    string   `json:"verify_token"`
	AppSecret      string   `json:"app_secret"`
	AllowedSenders []string `json:"allowed_senders"`
}

func parseMessengerCreds(s string) messengerCreds {
	s = strings.TrimSpace(s)
	var c messengerCreds
	if err := json.Unmarshal([]byte(s), &c); err == nil && c.PageToken != "" {
		return c
	}
	c.PageToken = s
	return c
}

type MessengerProvider struct {
	acc            Account
	pageToken      string
	verifyToken    string
	appSecret      string
	allowedSenders []string
	pageID         string
	pageName       string
	cancel         context.CancelFunc
	status         string
	mu             sync.Mutex
	onInbound      InboundHandler
	store          *Store
	seenMIDs       map[string]time.Time
	seenMu         sync.Mutex
}

func NewMessengerProvider(acc Account) *MessengerProvider {
	return &MessengerProvider{acc: acc, status: "disconnected"}
}

func (p *MessengerProvider) Name() string { return string(p.acc.Provider) }

func (p *MessengerProvider) Start(ctx context.Context, acc Account, store *Store, onInbound InboundHandler) error {
	p.acc = acc
	p.store = store
	p.onInbound = onInbound
	creds := parseMessengerCreds(acc.Credentials)
	p.pageToken = creds.PageToken
	p.verifyToken = creds.VerifyToken
	p.appSecret = creds.AppSecret
	p.allowedSenders = creds.AllowedSenders
	if p.verifyToken == "" {
		p.verifyToken = acc.ID
	}
	if p.pageToken == "" {
		return fmt.Errorf("messenger: page access token required")
	}
	if p.appSecret == "" {
		return fmt.Errorf("messenger: app secret required")
	}
	pageID, name, err := messengerPageInfo(ctx, p.pageToken)
	if err != nil {
		p.mu.Lock()
		p.status = "error"
		p.mu.Unlock()
		return err
	}
	p.pageID = pageID
	if name != "" {
		p.pageName = name
	}
	cctx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	_ = cctx
	p.mu.Lock()
	p.status = "connected"
	p.seenMIDs = map[string]time.Time{}
	p.mu.Unlock()
	if err := registerMessenger(p); err != nil {
		p.mu.Lock()
		p.status = "error"
		p.mu.Unlock()
		return err
	}
	return nil
}

func (p *MessengerProvider) Stop() {
	unregisterMessenger(p)
	if p.cancel != nil {
		p.cancel()
	}
	p.mu.Lock()
	p.status = "disconnected"
	p.seenMIDs = nil
	p.mu.Unlock()
}

func (p *MessengerProvider) Send(ctx context.Context, chatID, text string) (string, error) {
	if p.pageToken == "" {
		return "", fmt.Errorf("not connected")
	}
	// Messenger caps text at 2000 chars; chunk longer replies.
	chunks := []string{text}
	if len(text) > 2000 {
		chunks = nil
		for len(text) > 0 {
			n := 2000
			if n > len(text) {
				n = len(text)
			}
			chunks = append(chunks, text[:n])
			text = text[n:]
		}
	}
	lastID := ""
	for _, c := range chunks {
		body, _ := json.Marshal(map[string]any{
			"recipient": map[string]string{"id": chatID},
			"message":   map[string]string{"text": c},
		})
		u := graphAPIBase + "/me/messages?access_token=" + url.QueryEscape(p.pageToken)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return "", fmt.Errorf("messenger send: %s: %s", resp.Status, string(raw))
		}
		var out struct {
			MessageID string `json:"message_id"`
		}
		_ = json.Unmarshal(raw, &out)
		if out.MessageID == "" {
			out.MessageID = fmt.Sprintf("msg-%d", time.Now().UnixNano())
		}
		lastID = out.MessageID
		if p.store != nil {
			acc := p.acc
			_ = p.store.AddMessage(Message{
				ID:         out.MessageID,
				AccountID:  acc.ID,
				ChatID:     chatID,
				SenderID:   "me",
				SenderName: acc.Label,
				Direction:  "out",
				Text:       text,
				Timestamp:  time.Now(),
			})
		}
	}
	return lastID, nil
}

func (p *MessengerProvider) Status() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

func (p *MessengerProvider) PageID() string      { return p.pageID }
func (p *MessengerProvider) VerifyToken() string { return p.verifyToken }

// ---------- registry ----------
var (
	messengerMu       sync.Mutex
	messengerByPage   = map[string]*MessengerProvider{}
	messengerByVerify = map[string]*MessengerProvider{}
)

func registerMessenger(p *MessengerProvider) error {
	messengerMu.Lock()
	defer messengerMu.Unlock()
	if p.pageID != "" {
		if _, ok := messengerByPage[p.pageID]; ok {
			return fmt.Errorf("messenger: page %s already registered", p.pageID)
		}
		messengerByPage[p.pageID] = p
	}
	if p.verifyToken != "" {
		if _, ok := messengerByVerify[p.verifyToken]; ok {
			return fmt.Errorf("messenger: verify token already registered")
		}
		messengerByVerify[p.verifyToken] = p
	}
	return nil
}

func unregisterMessenger(p *MessengerProvider) {
	messengerMu.Lock()
	defer messengerMu.Unlock()
	if p.pageID != "" {
		delete(messengerByPage, p.pageID)
	}
	if p.verifyToken != "" {
		delete(messengerByVerify, p.verifyToken)
	}
}

func MessengerProviderByPage(id string) *MessengerProvider {
	messengerMu.Lock()
	defer messengerMu.Unlock()
	return messengerByPage[id]
}

func MessengerProviderByVerify(token string) *MessengerProvider {
	if token == "" {
		return nil
	}
	messengerMu.Lock()
	defer messengerMu.Unlock()
	var found *MessengerProvider
	for _, p := range messengerByVerify {
		if hmac.Equal([]byte(p.verifyToken), []byte(token)) {
			found = p
			break
		}
	}
	return found
}

func messengerPageInfo(ctx context.Context, token string) (string, string, error) {
	u := graphAPIBase + "/me?access_token=" + url.QueryEscape(token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("messenger page info: %s: %s", resp.Status, string(raw))
	}
	var out struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", "", err
	}
	if out.ID == "" {
		return "", "", fmt.Errorf("messenger page info: missing id")
	}
	return out.ID, out.Name, nil
}

func verifyMessengerSignature(secret string, body []byte, header string) bool {
	header = strings.TrimSpace(header)
	if !strings.HasPrefix(header, "sha256=") {
		return false
	}
	got, err := hex.DecodeString(strings.TrimPrefix(header, "sha256="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), got)
}

// HandleMessengerWebhook serves the Facebook Messenger webhook (GET verify + POST events).
func HandleMessengerWebhook(w http.ResponseWriter, r *http.Request) {
	mgr := DefaultManager
	switch r.Method {
	case http.MethodGet:
		mode := r.URL.Query().Get("hub.mode")
		token := r.URL.Query().Get("hub.verify_token")
		challenge := r.URL.Query().Get("hub.challenge")
		if mode == "subscribe" {
			if p := MessengerProviderByVerify(token); p != nil {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(challenge))
				return
			}
		}
		http.Error(w, "forbidden", http.StatusForbidden)
	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			if _, ok := err.(*http.MaxBytesError); ok {
				http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		var payload struct {
			Object string `json:"object"`
			Entry  []struct {
				ID        string `json:"id"`
				Messaging []struct {
					Sender struct {
						ID string `json:"id"`
					} `json:"sender"`
					Recipient struct {
						ID string `json:"id"`
					} `json:"recipient"`
					Timestamp int64 `json:"timestamp"`
					Message   struct {
						MID    string `json:"mid"`
						Text   string `json:"text"`
						IsEcho bool   `json:"is_echo"`
					} `json:"message"`
				} `json:"messaging"`
			} `json:"entry"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if payload.Object != "" && payload.Object != "page" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("EVENT_RECEIVED"))
			return
		}
		// Resolve a provider for signature verification (first known page).
		var prov *MessengerProvider
		for _, e := range payload.Entry {
			if pp := MessengerProviderByPage(e.ID); pp != nil {
				prov = pp
				break
			}
			for _, m := range e.Messaging {
				if pp := MessengerProviderByPage(m.Recipient.ID); pp != nil {
					prov = pp
					break
				}
			}
			if prov != nil {
				break
			}
		}
		if prov != nil {
			if prov.appSecret == "" {
				http.Error(w, "app secret required", http.StatusForbidden)
				return
			}
			sig := r.Header.Get("X-Hub-Signature-256")
			if !verifyMessengerSignature(prov.appSecret, body, sig) {
				http.Error(w, "invalid signature", http.StatusForbidden)
				return
			}
		} else {
			// No known provider: nothing to process.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("EVENT_RECEIVED"))
			return
		}
		for _, e := range payload.Entry {
			for _, m := range e.Messaging {
				if m.Message.MID == "" && m.Message.Text == "" {
					continue
				}
				pageID := m.Recipient.ID
				if pageID == "" {
					pageID = e.ID
				}
				p := MessengerProviderByPage(pageID)
				if p == nil {
					continue
				}
				if m.Sender.ID == pageID || m.Message.IsEcho {
					continue
				}
				if len(p.allowedSenders) > 0 {
					ok := false
					for _, a := range p.allowedSenders {
						if a == m.Sender.ID {
							ok = true
							break
						}
					}
					if !ok {
						continue
					}
				}
				if m.Message.MID != "" {
					p.mu.Lock()
					if p.seenMIDs == nil {
						p.seenMIDs = map[string]time.Time{}
					}
					// TTL eviction: drop entries older than 24h
					now := time.Now()
					for k, v := range p.seenMIDs {
						if now.Sub(v) > 24*time.Hour {
							delete(p.seenMIDs, k)
						}
					}
					// size cap
					if len(p.seenMIDs) > 10000 {
						// evict oldest entries
						type kv struct {
							k string
							v time.Time
						}
						items := make([]kv, 0, len(p.seenMIDs))
						for k, v := range p.seenMIDs {
							items = append(items, kv{k, v})
						}
						// simple selection sort by time ascending
						for i := 0; i < len(items); i++ {
							for j := i + 1; j < len(items); j++ {
								if items[j].v.Before(items[i].v) {
									items[i], items[j] = items[j], items[i]
								}
							}
						}
						toDelete := len(items) - 5000
						for i := 0; i < toDelete && i < len(items); i++ {
							delete(p.seenMIDs, items[i].k)
						}
					}
					if _, ok := p.seenMIDs[m.Message.MID]; ok {
						p.mu.Unlock()
						continue
					}
					p.seenMIDs[m.Message.MID] = now
					p.mu.Unlock()
				}
				chatID := m.Sender.ID
				if chatID == "" {
					continue
				}
				if p.store != nil {
					_ = p.store.UpsertChat(Chat{AccountID: p.acc.ID, ID: chatID, Title: chatID, Type: "dm", LastMessageAt: time.Now()})
				}
				msg := Message{
					ID:         m.Message.MID,
					AccountID:  p.acc.ID,
					ChatID:     chatID,
					SenderID:   m.Sender.ID,
					SenderName: m.Sender.ID,
					Direction:  "in",
					Text:       m.Message.Text,
				}
				if msg.ID == "" {
					msg.ID = fmt.Sprintf("msg-%d", time.Now().UnixNano())
				}
				if m.Timestamp != 0 {
					msg.Timestamp = time.UnixMilli(m.Timestamp)
				} else {
					msg.Timestamp = time.Now()
				}
				if p.onInbound != nil {
					msgCopy := msg
					crashguard.Go(func() {
						p.onInbound(msgCopy)
					})
				} else if mgr != nil && mgr.Store() != nil {
					_ = mgr.Store().AddMessage(msg)
					if mgr.OnInbound != nil {
						mgr.OnInbound(msg)
					}
				}
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("EVENT_RECEIVED"))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
