package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

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
