package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
