package messaging

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Provider drives a platform connector for a single account.
type Platform interface {
	Name() string
	Start(ctx context.Context, acc Account, store *Store, onInbound InboundHandler) error
	Stop()
	Send(ctx context.Context, chatID, text string) (string, error) // returns provider message id
	Status() string
}

// Manager owns accounts + active providers.
type Manager struct {
	store     *Store
	providers map[string]Platform // accountID -> provider
	mu        sync.RWMutex
	dataDir   string
	// OnInbound is called for every persisted inbound message (for SSE/hooks).
	OnInbound func(Message)
}

func NewManager(store *Store, dataDir string) *Manager {
	return &Manager{store: store, providers: map[string]Platform{}, dataDir: dataDir}
}

func (m *Manager) Store() *Store { return m.store }

func (m *Manager) StartAccount(ctx context.Context, acc Account) error {
	var p Platform
	switch acc.Provider {
	case ProviderTelegram:
		p = NewTelegramProvider(acc)
	case ProviderWhatsApp:
		p = NewWhatsAppProvider(acc, m.dataDir)
	default:
		return fmt.Errorf("unknown provider %s", acc.Provider)
	}
	if err := p.Start(ctx, acc, m.store, func(msg Message) {
		_ = m.store.AddMessage(msg)
		if m.OnInbound != nil {
			m.OnInbound(msg)
		}
	}); err != nil {
		acc.Status = "error"
		acc.Error = err.Error()
		_ = m.store.UpsertAccount(acc)
		return err
	}
	m.mu.Lock()
	m.providers[acc.ID] = p
	m.mu.Unlock()
	acc.Status = "connected"
	acc.Error = ""
	_ = m.store.UpsertAccount(acc)
	return nil
}

func (m *Manager) StopAccount(id string) {
	m.mu.Lock()
	p, ok := m.providers[id]
	if ok {
		delete(m.providers, id)
	}
	m.mu.Unlock()
	if ok {
		p.Stop()
	}
	if acc, err := m.store.GetAccount(id); err == nil {
		acc.Status = "disconnected"
		_ = m.store.UpsertAccount(acc)
	}
}

func (m *Manager) Send(ctx context.Context, accountID, chatID, text string) (string, error) {
	m.mu.RLock()
	p, ok := m.providers[accountID]
	m.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("account %s not connected", accountID)
	}
	mid, err := p.Send(ctx, chatID, text)
	if err != nil {
		return "", err
	}
	acc, _ := m.store.GetAccount(accountID)
	_ = m.store.AddMessage(Message{
		ID:         mid,
		AccountID:  accountID,
		ChatID:     chatID,
		SenderID:   "me",
		SenderName: acc.Label,
		Direction:  "out",
		Text:       text,
		Timestamp:  time.Now(),
	})
	return mid, nil
}

func (m *Manager) Status(id string) string {
	m.mu.RLock()
	p, ok := m.providers[id]
	m.mu.RUnlock()
	if ok {
		return p.Status()
	}
	if acc, err := m.store.GetAccount(id); err == nil {
		return acc.Status
	}
	return "unknown"
}

func (m *Manager) StopAll() {
	m.mu.RLock()
	ids := make([]string, 0, len(m.providers))
	for id := range m.providers {
		ids = append(ids, id)
	}
	m.mu.RUnlock()
	for _, id := range ids {
		m.StopAccount(id)
	}
}

// DefaultManager is the process-wide messaging manager, set by the server at boot.
var DefaultManager *Manager
