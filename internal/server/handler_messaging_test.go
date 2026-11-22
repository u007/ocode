package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/u007/ocode/internal/messaging"
)

func TestMessagingAccountsCRUD(t *testing.T) {
	h := NewHandler()
	body, _ := json.Marshal(map[string]string{
		"provider":    "telegram",
		"label":       "bot",
		"credentials": "tok",
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/messaging/accounts", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	h.handleMessagingAccounts(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var cr map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &cr)
	id := cr["id"]
	if id == "" {
		t.Fatal("no id")
	}

	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodGet, "/api/messaging/accounts", nil)
	h.handleMessagingAccounts(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("list: %d", w2.Code)
	}

	w3 := httptest.NewRecorder()
	r3 := httptest.NewRequest(http.MethodDelete, "/api/messaging/accounts/"+id, nil)
	h.handleMessagingAccountAction(w3, r3)
	if w3.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", w3.Code, w3.Body.String())
	}
}

// TestMessagingAccountTest covers the account-test endpoint's not-found,
// unsupported-provider and uninitialised responses.
func TestMessagingAccountTest(t *testing.T) {
	h := NewHandler()
	st, err := messaging.NewStore(filepath.Join(t.TempDir(), "messaging.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	h.messaging = messaging.NewManager(st, t.TempDir())

	if err := st.UpsertAccount(messaging.Account{
		ID:          "acct-telegram",
		Provider:    messaging.ProviderTelegram,
		Label:       "tg",
		Credentials: "123456:ABC",
		Status:      "disconnected",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := st.UpsertAccount(messaging.Account{
		ID:          "acct-whatsapp",
		Provider:    messaging.ProviderWhatsApp,
		Label:       "wa",
		Credentials: "session",
		Status:      "disconnected",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// not found
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/messaging/accounts/nope/test", nil)
	h.handleMessagingAccountTest(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("not found: %d %s", w.Code, w.Body.String())
	}

	// unsupported provider
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/api/messaging/accounts/acct-whatsapp/test", nil)
	h.handleMessagingAccountTest(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unsupported: %d %s", w.Code, w.Body.String())
	}

	// messaging not initialized
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/api/messaging/accounts/acct-telegram/test", nil)
	(&Handler{}).handleMessagingAccountTest(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("not initialized: %d %s", w.Code, w.Body.String())
	}
}

func TestTelegramTokenCheckMessage(t *testing.T) {
	const (
		reach    = "could not reach Telegram API"
		rejected = "token rejected by Telegram"
		failed   = "Telegram check failed, try again"
	)
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"unreachable", &url.Error{Op: "Post", URL: "https://api.telegram.org/botTOKEN/getMe", Err: errors.New("dial tcp: no route")}, reach},
		{"unauthorized", &tgbotapi.Error{Code: http.StatusUnauthorized, Message: "Unauthorized"}, rejected},
		{"not found", &tgbotapi.Error{Code: http.StatusNotFound, Message: "Not Found"}, rejected},
		{"rate limited", &tgbotapi.Error{Code: http.StatusTooManyRequests, Message: "Too Many Requests"}, failed},
		{"bad gateway", &tgbotapi.Error{Code: http.StatusBadGateway, Message: "Bad Gateway"}, failed},
		{"decode failure", errors.New("invalid character '<' looking for beginning of value"), failed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := telegramTokenCheckMessage(tc.err); got != tc.want {
				t.Fatalf("telegramTokenCheckMessage() = %q, want %q", got, tc.want)
			}
		})
	}
}

// The hub URL is not a client setting: an omitted url must resolve to the
// configured sync_url (the production hub when unset), so the SPA cannot point
// messaging sync at an arbitrary server. A relative/empty target would have
// produced a request path with no host.
func TestMessagingSyncResolvesSyncURLWhenURLOmitted(t *testing.T) {
	var gotPath string
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeJSON(w, http.StatusOK, map[string]int64{"cursor": 0})
	}))
	defer stub.Close()

	h := NewHandler()
	if h.cfg == nil {
		t.Fatal("NewHandler returned nil config")
	}
	h.cfg.Ocode.SyncURL = stub.URL

	st, err := messaging.NewStore(filepath.Join(t.TempDir(), "messaging.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	h.messaging = messaging.NewManager(st, t.TempDir())
	if err := st.AddMessage(messaging.Message{
		AccountID: "acct-1",
		ID:        "msg-1",
		ChatID:    "chat-1",
		Direction: "inbound",
		Text:      "hello",
		Timestamp: time.Now(),
	}); err != nil {
		t.Fatalf("add message: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/messaging/sync", bytes.NewReader([]byte(`{"dir":"push"}`)))
	r.Header.Set("Content-Type", "application/json")
	h.handleMessagingSync(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("sync: %d %s", w.Code, w.Body.String())
	}
	if gotPath != "/api/messaging/sync" {
		t.Fatalf("hub received %q, want /api/messaging/sync", gotPath)
	}
}
