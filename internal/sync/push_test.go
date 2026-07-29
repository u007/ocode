package sync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestPushSendsToServerAndSavesSnapshot(t *testing.T) {
	withTempHome(t)

	var pushCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pushCount++
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "version": 1})
	}))
	defer srv.Close()

	if err := SaveToken("tok"); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	c := &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}
	err := c.Push(context.Background(), BlobTypeConfig, json.RawMessage(`{"theme":"dark"}`))
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if pushCount != 1 {
		t.Errorf("expected exactly one push attempt, got %d", pushCount)
	}

	snapPath, _ := SnapshotPath(BlobTypeConfig)
	data, err := os.ReadFile(snapPath)
	if err != nil {
		t.Fatalf("expected snapshot file: %v", err)
	}
	var snap snapshot
	json.Unmarshal(data, &snap)
	if snap.Version != 1 {
		t.Errorf("expected snapshot version 1, got %d", snap.Version)
	}
}

func TestPushRetriesOnConflictThenSucceeds(t *testing.T) {
	withTempHome(t)

	snapshotPath, _ := SnapshotPath(BlobTypeConfig)
	sd, _ := syncDir()
	os.MkdirAll(sd, 0700)
	os.WriteFile(snapshotPath, []byte(`{"version":5,"blob":{"theme":"light"}}`), 0600)

	attempt := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt == 1 {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok": false, "currentVersion": 5,
				"currentBlob": `{"theme":"light","extra":"remote"}`,
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "version": 6})
	}))
	defer srv.Close()

	if err := SaveToken("tok"); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	c := &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}
	err := c.Push(context.Background(), BlobTypeConfig, json.RawMessage(`{"theme":"dark"}`))
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if attempt != 2 {
		t.Errorf("expected retry after conflict, got %d attempts", attempt)
	}
}

func TestPushWithoutTokenIsNoop(t *testing.T) {
	withTempHome(t)

	pushed := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pushed = true
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}
	err := c.Push(context.Background(), BlobTypeConfig, json.RawMessage(`{"a":1}`))
	if err != nil {
		t.Fatalf("Push without token: %v", err)
	}
	if pushed {
		t.Fatal("expected no push when not logged in")
	}
}
