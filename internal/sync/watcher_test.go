package sync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
)

func TestWatcherDebouncesRapidSaves(t *testing.T) {
	withTempHome(t)

	var pushCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&pushCount, 1)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "version": 1})
	}))
	defer srv.Close()

	if err := SaveToken("tok"); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	c := &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}
	stop := StartWatcher(c, WithDebounce(20*time.Millisecond))
	defer stop()

	for i := 0; i < 5; i++ {
		config.OnConfigSaved.Fire()
		time.Sleep(5 * time.Millisecond)
	}

	time.Sleep(60 * time.Millisecond)
	if got := atomic.LoadInt32(&pushCount); got != 1 {
		t.Errorf("expected exactly one debounced push for 5 rapid saves, got %d", got)
	}
	_ = context.Background()
}
