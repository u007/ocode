package cloudconnect

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGCSList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"items":[{"name":"a.txt","size":"10","contentType":"text/plain","updated":"2026-01-01T00:00:00Z"}],"prefixes":[]}`))
	}))
	defer srv.Close()
	old := gcsAPIBase
	gcsAPIBase = srv.URL
	defer func() { gcsAPIBase = old }()

	c, _ := NewGCSConnector("g1", "mybucket", SecretBundle{AccessKey: "tok"})
	entries, err := c.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "a.txt" {
		t.Fatalf("bad: %+v", entries)
	}
}

func TestGCSMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"name":"a.txt","size":"10","contentType":"text/plain","updated":"2026-01-01T00:00:00Z"}`))
	}))
	defer srv.Close()
	old := gcsAPIBase
	gcsAPIBase = srv.URL
	defer func() { gcsAPIBase = old }()

	c, _ := NewGCSConnector("g1", "mybucket", SecretBundle{AccessKey: "tok"})
	e, err := c.Metadata(context.Background(), "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if e.Name != "a.txt" || e.Size != 10 {
		t.Fatalf("bad: %+v", e)
	}
}

func TestGCSEmptyBucket(t *testing.T) {
	_, err := NewGCSConnector("g1", "", SecretBundle{})
	if err == nil {
		t.Fatal("expected error for empty bucket")
	}
}

func TestGCSProviderID(t *testing.T) {
	c, _ := NewGCSConnector("g1", "bucket", SecretBundle{})
	if c.Provider() != "gcs" || c.ID() != "g1" {
		t.Fatal("bad")
	}
}

func TestGCSInvalidPath(t *testing.T) {
	c, _ := NewGCSConnector("g1", "bucket", SecretBundle{AccessKey: "tok"})
	if _, err := c.Read(context.Background(), "../etc/passwd"); err == nil {
		t.Fatal("expected error")
	}
}

func TestGCSListFolders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"items":[],"prefixes":["folder/"]}`))
	}))
	defer srv.Close()
	old := gcsAPIBase
	gcsAPIBase = srv.URL
	defer func() { gcsAPIBase = old }()

	c, _ := NewGCSConnector("g1", "bucket", SecretBundle{AccessKey: "tok"})
	entries, err := c.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !entries[0].IsDir || entries[0].Name != "folder" {
		t.Fatalf("bad: %+v", entries)
	}
}

func TestGCSWrite(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(404)
			return
		}
		if r.Method == http.MethodPut {
			if r.URL.Query().Get("uploadType") != "media" {
				t.Errorf("expected uploadType=media, got %q", r.URL.RawQuery)
			}
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(405)
	}))
	defer srv.Close()
	old := gcsAPIBase
	gcsAPIBase = srv.URL
	defer func() { gcsAPIBase = old }()

	c, _ := NewGCSConnector("g1", "mybucket", SecretBundle{AccessKey: "tok"})
	err := c.Write(context.Background(), "a.txt", strings.NewReader("hello"), 5)
	if err != nil {
		t.Fatal(err)
	}
}

func TestGCSDelete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()
	old := gcsAPIBase
	gcsAPIBase = srv.URL
	defer func() { gcsAPIBase = old }()

	c, _ := NewGCSConnector("g1", "mybucket", SecretBundle{AccessKey: "tok"})
	if err := c.Delete(context.Background(), "a.txt"); err != nil {
		t.Fatal(err)
	}
}
