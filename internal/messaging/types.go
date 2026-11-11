package messaging

import "time"

// Provider identifies a chat platform.
type Provider string

const (
	ProviderTelegram Provider = "telegram"
	ProviderWhatsApp Provider = "whatsapp"
)

// Account is a configured connector account (one bot/phone number).
type Account struct {
	ID          string    `json:"id"`
	Provider    Provider  `json:"provider"`
	Label       string    `json:"label"`
	Credentials string    `json:"-"`      // JSON secret blob, never serialized to clients masked
	Status      string    `json:"status"` // disconnected, connecting, connected, error
	Error       string    `json:"error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Chat is a conversation thread on a provider.
type Chat struct {
	ID            string    `json:"id"`
	AccountID     string    `json:"account_id"`
	Title         string    `json:"title"`
	Type          string    `json:"type"` // dm, group, channel
	LastMessageAt time.Time `json:"last_message_at"`
}

// Message is a single chat message persisted to SQLite.
type Message struct {
	ID         string    `json:"id"`
	AccountID  string    `json:"account_id"`
	ChatID     string    `json:"chat_id"`
	SenderID   string    `json:"sender_id"`
	SenderName string    `json:"sender_name"`
	Direction  string    `json:"direction"` // in, out
	Text       string    `json:"text"`
	MediaURL   string    `json:"media_url,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
	Raw        string    `json:"-"` // raw provider payload
}

// InboundHandler is called by providers when a message arrives.
type InboundHandler func(msg Message)
