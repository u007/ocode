package cloudconnect

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOneDriveList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/me/drive/root/children" {
			w.Write([]byte(`{"value":[{"id":"f1","name":"a.txt","size":10,"lastModifiedDateTime":"2026-01-01T00:00:00Z","file":{"mimeType":"text/plain"}}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	old := graphAPIBase
	graphAPIBase = srv.URL
	defer func() { graphAPIBase = old }()

	c, _ := NewOneDriveConnector("o1", "", SecretBundle{AccessKey: "tok"})
	entries, err := c.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "a.txt" || entries[0].IsDir {
		t.Fatalf("bad: %+v", entries)
	}
}

func TestOneDriveMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"f1","name":"a.txt","size":10,"lastModifiedDateTime":"2026-01-01T00:00:00Z","file":{"mimeType":"text/plain"}}`))
	}))
	defer srv.Close()
	old := graphAPIBase
	graphAPIBase = srv.URL
	defer func() { graphAPIBase = old }()

	c, _ := NewOneDriveConnector("o1", "", SecretBundle{AccessKey: "tok"})
	e, err := c.Metadata(context.Background(), "f1")
	if err != nil {
		t.Fatal(err)
	}
	if e.Name != "a.txt" || e.Size != 10 {
		t.Fatalf("bad: %+v", e)
	}
}

func TestOneDriveEmptyPath(t *testing.T) {
	c, _ := NewOneDriveConnector("o1", "", SecretBundle{AccessKey: "tok"})
	if _, err := c.Read(context.Background(), ""); err == nil {
		t.Fatal("expected error")
	}
	if _, err := c.Metadata(context.Background(), ""); err == nil {
		t.Fatal("expected error")
	}
}

func TestOneDriveProviderID(t *testing.T) {
	c, _ := NewOneDriveConnector("o1", "root", SecretBundle{})
	if c.Provider() != "onedrive" || c.ID() != "o1" {
		t.Fatal("bad")
	}
}

func TestOneDriveListFolder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/me/drive/items/folder1/children" {
			w.Write([]byte(`{"value":[{"id":"d1","name":"sub","lastModifiedDateTime":"2026-01-01T00:00:00Z","folder":{}}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	old := graphAPIBase
	graphAPIBase = srv.URL
	defer func() { graphAPIBase = old }()

	c, _ := NewOneDriveConnector("o1", "", SecretBundle{AccessKey: "tok"})
	entries, err := c.List(context.Background(), "folder1")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !entries[0].IsDir {
		t.Fatalf("bad: %+v", entries)
	}
}

func TestOneDriveListPagination(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Write([]byte(`{"value":[{"id":"f1","name":"a.txt","size":10,"lastModifiedDateTime":"2026-01-01T00:00:00Z","file":{"mimeType":"text/plain"}}],"@odata.nextLink":"` + "http://" + r.Host + `/anything"}`))
			return
		}
		w.Write([]byte(`{"value":[{"id":"f2","name":"b.txt","size":20,"lastModifiedDateTime":"2026-01-01T00:00:00Z","file":{"mimeType":"text/plain"}}]}`))
	}))
	defer srv.Close()
	old := graphAPIBase
	graphAPIBase = srv.URL
	defer func() { graphAPIBase = old }()

	c, _ := NewOneDriveConnector("o1", "", SecretBundle{AccessKey: "tok"})
	entries, err := c.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries across pages, got %d", len(entries))
	}
}

func TestOneDriveList429(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			return
		}
		w.Write([]byte(`{"value":[]}`))
	}))
	defer srv.Close()
	old := graphAPIBase
	graphAPIBase = srv.URL
	defer func() { graphAPIBase = old }()

	c, _ := NewOneDriveConnector("o1", "", SecretBundle{AccessKey: "tok"})
	entries, err := c.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0, got %d", len(entries))
	}
}

func TestOneDriveList401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()
	old := graphAPIBase
	graphAPIBase = srv.URL
	defer func() { graphAPIBase = old }()

	c, _ := NewOneDriveConnector("o1", "", SecretBundle{AccessKey: "tok"})
	_, err := c.List(context.Background(), "")
	if err == nil {
		t.Fatal("expected error on 401")
	}
}
