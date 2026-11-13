package cloudconnect

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestS3List(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<ListBucketResult><Contents><Key>a.txt</Key><Size>10</Size><LastModified>2026-01-01T00:00:00Z</LastModified></Contents><CommonPrefixes><Prefix>folder/</Prefix></CommonPrefixes></ListBucketResult>`))
	}))
	defer srv.Close()

	c, _ := NewS3Connector("s1", "mybucket", SecretBundle{AccessKey: "AKIA", SecretKey: "secret", Region: "us-east-1", Endpoint: srv.URL})
	entries, err := c.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(entries), entries)
	}
}

func TestS3ProviderID(t *testing.T) {
	c, _ := NewS3Connector("s1", "bucket", SecretBundle{AccessKey: "a", SecretKey: "b"})
	if c.Provider() != "s3" || c.ID() != "s1" {
		t.Fatal("bad")
	}
}

func TestS3EmptyBucket(t *testing.T) {
	_, err := NewS3Connector("s1", "", SecretBundle{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestS3InvalidPath(t *testing.T) {
	c, _ := NewS3Connector("s1", "bucket", SecretBundle{AccessKey: "a", SecretKey: "b"})
	if _, err := c.Read(context.Background(), "../etc/passwd"); err == nil {
		t.Fatal("expected error")
	}
}

func TestS3Metadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "10")
		w.Header().Set("Last-Modified", "Thu, 01 Jan 2026 00:00:00 GMT")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	c, _ := NewS3Connector("s1", "mybucket", SecretBundle{AccessKey: "AKIA", SecretKey: "secret", Region: "us-east-1", Endpoint: srv.URL})
	e, err := c.Metadata(context.Background(), "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if e.Name != "a.txt" {
		t.Fatalf("bad: %+v", e)
	}
}

func TestS3Write(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(404)
			return
		}
		if r.Method == http.MethodPut {
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(405)
	}))
	defer srv.Close()

	c, _ := NewS3Connector("s1", "mybucket", SecretBundle{AccessKey: "AKIA", SecretKey: "secret", Region: "us-east-1", Endpoint: srv.URL})
	err := c.Write(context.Background(), "a.txt", strings.NewReader("hello"), 5)
	if err != nil {
		t.Fatal(err)
	}
}

func TestS3Delete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	c, _ := NewS3Connector("s1", "mybucket", SecretBundle{AccessKey: "AKIA", SecretKey: "secret", Region: "us-east-1", Endpoint: srv.URL})
	if err := c.Delete(context.Background(), "a.txt"); err != nil {
		t.Fatal(err)
	}
}
