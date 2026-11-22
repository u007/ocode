package sync

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
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

// A cancelled context must end the conflict backoff at once. The hub conflicts
// on every push, so the second attempt is followed by a 500ms backoff; the
// cancel lands 20ms into it and Push has to return within 150ms of that.
func TestPushReturnsPromptlyOnCancelDuringConflictBackoff(t *testing.T) {
	withTempHome(t)

	snapshotPath, _ := SnapshotPath(BlobTypeConfig)
	sd, _ := syncDir()
	os.MkdirAll(sd, 0700)
	os.WriteFile(snapshotPath, []byte(`{"version":5,"blob":{"theme":"light"}}`), 0600)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var cancelAt time.Time
	attempt := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt == 2 {
			time.AfterFunc(20*time.Millisecond, func() {
				cancelAt = time.Now()
				cancel()
			})
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": false, "currentVersion": 5,
			"currentBlob": `{"theme":"light","extra":"remote"}`,
		})
	}))
	defer srv.Close()

	if err := SaveToken("tok"); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	c := &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}
	err := c.Push(ctx, BlobTypeConfig, json.RawMessage(`{"theme":"dark"}`))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Push error = %v, want context.Canceled", err)
	}
	if lag := time.Since(cancelAt); lag > 150*time.Millisecond {
		t.Fatalf("Push returned %v after cancel; the conflict backoff ignores ctx", lag)
	}
}
