package cloudconnect

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDropboxList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"entries":[{"tag":"file","name":"a.txt","path_lower":"/a.txt","size":10,"server_modified":"2026-01-01T00:00:00Z"},{"tag":"folder","name":"sub","path_lower":"/sub"}],"cursor":"","has_more":false}`))
	}))
	defer srv.Close()
	old := dropboxAPIBase
	dropboxAPIBase = srv.URL
	defer func() { dropboxAPIBase = old }()

	c, _ := NewDropboxConnector("d1", "", SecretBundle{AccessKey: "tok"})
	entries, err := c.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2, got %d: %+v", len(entries), entries)
	}
}

func TestDropboxMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag":"file","name":"a.txt","path_lower":"/a.txt","size":10,"server_modified":"2026-01-01T00:00:00Z"}`))
	}))
	defer srv.Close()
	old := dropboxAPIBase
	dropboxAPIBase = srv.URL
	defer func() { dropboxAPIBase = old }()

	c, _ := NewDropboxConnector("d1", "", SecretBundle{AccessKey: "tok"})
	e, err := c.Metadata(context.Background(), "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if e.Name != "a.txt" || e.Size != 10 {
		t.Fatalf("bad: %+v", e)
	}
}

func TestDropboxProviderID(t *testing.T) {
	c, _ := NewDropboxConnector("d1", "root", SecretBundle{})
	if c.Provider() != "dropbox" || c.ID() != "d1" {
		t.Fatal("bad")
	}
}

func TestDropboxInvalidPath(t *testing.T) {
	c, _ := NewDropboxConnector("d1", "", SecretBundle{AccessKey: "tok"})
	if _, err := c.Read(context.Background(), "../etc/passwd"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := c.Metadata(context.Background(), "../etc/passwd"); err == nil {
		t.Fatal("expected error")
	}
}

func TestDropboxEmptyNotRequired(t *testing.T) {
	c, err := NewDropboxConnector("d1", "", SecretBundle{})
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("nil")
	}
}

func TestDropboxWrite(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/files/get_metadata" {
			w.WriteHeader(404)
			return
		}
		if r.URL.Path == "/files/upload" {
			if r.Header.Get("Dropbox-API-Arg") == "" {
				t.Error("missing Dropbox-API-Arg")
			}
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()
	oldAPI := dropboxAPIBase
	oldContent := dropboxContentBase
	dropboxAPIBase = srv.URL
	dropboxContentBase = srv.URL
	defer func() {
		dropboxAPIBase = oldAPI
		dropboxContentBase = oldContent
	}()

	c, _ := NewDropboxConnector("d1", "", SecretBundle{AccessKey: "tok"})
	err := c.Write(context.Background(), "a.txt", strings.NewReader("hello"), 5)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDropboxDelete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/files/delete_v2" {
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()
	oldAPI := dropboxAPIBase
	oldContent := dropboxContentBase
	dropboxAPIBase = srv.URL
	dropboxContentBase = srv.URL
	defer func() {
		dropboxAPIBase = oldAPI
		dropboxContentBase = oldContent
	}()

	c, _ := NewDropboxConnector("d1", "", SecretBundle{AccessKey: "tok"})
	if err := c.Delete(context.Background(), "a.txt"); err != nil {
		t.Fatal(err)
	}
}
