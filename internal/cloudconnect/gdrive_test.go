package cloudconnect

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGoogleDriveList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/files") {
			w.Write([]byte(`{"files":[{"id":"f1","name":"a.txt","mimeType":"text/plain","size":"10","modifiedTime":"2026-01-01T00:00:00Z"},{"id":"d1","name":"folder","mimeType":"application/vnd.google-apps.folder","size":"","modifiedTime":"2026-01-01T00:00:00Z"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	old := driveAPIBase
	driveAPIBase = srv.URL
	defer func() { driveAPIBase = old }()

	c, _ := NewGoogleDriveConnector("g1", "", SecretBundle{AccessKey: "tok"})
	entries, err := c.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Name != "a.txt" || entries[0].IsDir {
		t.Fatalf("bad file entry: %+v", entries[0])
	}
	if !entries[1].IsDir {
		t.Fatalf("expected folder: %+v", entries[1])
	}
}

func TestGoogleDriveMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"f1","name":"a.txt","mimeType":"text/plain","size":"10","modifiedTime":"2026-01-01T00:00:00Z"}`))
	}))
	defer srv.Close()
	old := driveAPIBase
	driveAPIBase = srv.URL
	defer func() { driveAPIBase = old }()

	c, _ := NewGoogleDriveConnector("g1", "", SecretBundle{AccessKey: "tok"})
	e, err := c.Metadata(context.Background(), "f1")
	if err != nil {
		t.Fatal(err)
	}
	if e.Name != "a.txt" || e.Size != 10 {
		t.Fatalf("bad metadata: %+v", e)
	}
}

func TestReadEmptyPath(t *testing.T) {
	c, _ := NewGoogleDriveConnector("g1", "", SecretBundle{AccessKey: "tok"})
	_, err := c.Read(context.Background(), "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMetadataEmptyPath(t *testing.T) {
	c, _ := NewGoogleDriveConnector("g1", "", SecretBundle{AccessKey: "tok"})
	_, err := c.Metadata(context.Background(), "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestProviderAndID(t *testing.T) {
	c, _ := NewGoogleDriveConnector("g1", "root", SecretBundle{})
	if c.Provider() != "google-drive" || c.ID() != "g1" {
		t.Fatal("bad provider/id")
	}
}

var _ = io.ReadCloser(nil)
var _ = http.MethodGet
