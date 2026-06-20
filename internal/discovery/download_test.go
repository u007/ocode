package discovery

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureArtifactVerifiesAndCaches(t *testing.T) {
	payload := []byte("model-bytes")
	sum := sha256.Sum256(payload)
	hexSum := hex.EncodeToString(sum[:])

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	dir := t.TempDir()
	art := Artifact{URL: srv.URL, SHA256: hexSum, Dest: "model.bin"}
	if err := EnsureArtifact(art, dir); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "model.bin"))
	if string(got) != string(payload) {
		t.Fatal("downloaded content mismatch")
	}
	// Second call: cached (sha matches) → no re-download.
	if err := EnsureArtifact(art, dir); err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Fatalf("expected 1 download, got %d (cache not honored)", hits)
	}
}

func TestEnsureArtifactRejectsBadSHA(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("tampered"))
	}))
	defer srv.Close()
	if err := EnsureArtifact(Artifact{URL: srv.URL, SHA256: "deadbeef", Dest: "x.bin"}, t.TempDir()); err == nil {
		t.Fatal("must reject sha mismatch")
	}
}
