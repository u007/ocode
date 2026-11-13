package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/u007/ocode/internal/cloudconnect"
)

func TestCloudConnectorsHandler(t *testing.T) {
	h := NewHandler()
	h.cloudconnect = cloudconnect.NewManager(t.TempDir(), "", "")

	body, _ := json.Marshal(map[string]interface{}{
		"id":       "g1",
		"provider": "google-drive",
		"secrets":  map[string]string{"access_key": "tok"},
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/cloud/connectors", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	h.handleCloudConnectors(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("register: %d %s", w.Code, w.Body.String())
	}

	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodGet, "/api/cloud/connectors", nil)
	h.handleCloudConnectors(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("list: %d", w2.Code)
	}
	var list []map[string]interface{}
	_ = json.Unmarshal(w2.Body.Bytes(), &list)
	if len(list) != 1 {
		t.Fatalf("expected 1 connector, got %d", len(list))
	}
}
