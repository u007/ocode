package sync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestPullBootstrapsEmptyLocalFile(t *testing.T) {
	withTempHome(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"version": 3, "blob": `{"theme":"dark"}`})
	}))
	defer srv.Close()

	if err := SaveToken("tok"); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	c := &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}
	if err := c.Pull(context.Background(), BlobTypeConfig); err != nil {
		t.Fatalf("Pull: %v", err)
	}

	localPath, err := localConfigPathFor(BlobTypeConfig)
	if err != nil {
		t.Fatalf("localConfigPathFor: %v", err)
	}
	data, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("expected local file written by Pull: %v", err)
	}
	var out map[string]string
	json.Unmarshal(data, &out)
	if out["theme"] != "dark" {
		t.Errorf("expected theme=dark, got %v", out)
	}
}

func TestPullNoTokenIsNoop(t *testing.T) {
	withTempHome(t)

	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}
	if err := c.Pull(context.Background(), BlobTypeConfig); err != nil {
		t.Fatalf("Pull without token: %v", err)
	}
	if hit {
		t.Fatal("expected no request when not logged in")
	}
}

func TestPullEmptyBlobIsNoop(t *testing.T) {
	withTempHome(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"version": 0, "blob": ""})
	}))
	defer srv.Close()

	if err := SaveToken("tok"); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	c := &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}
	if err := c.Pull(context.Background(), BlobTypeConfig); err != nil {
		t.Fatalf("Pull with empty blob: %v", err)
	}
}
