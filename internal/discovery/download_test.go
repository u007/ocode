package discovery

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

func TestEnsureArtifactRetriesTransientDownloadError(t *testing.T) {
	payload := []byte("model-bytes")
	sum := sha256.Sum256(payload)
	hexSum := hex.EncodeToString(sum[:])

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n == 1 {
			hj, ok := w.(http.Hijacker)
			if !ok {
				panic("response writer does not support hijacking")
			}
			conn, buf, err := hj.Hijack()
			if err != nil {
				panic(err)
			}
			_, _ = fmt.Fprintf(buf, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\nContent-Type: application/octet-stream\r\n\r\n%s", len(payload), payload[:len(payload)/2])
			_ = buf.Flush()
			_ = conn.Close()
			return
		}
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	dir := t.TempDir()
	art := Artifact{URL: srv.URL, SHA256: hexSum, Dest: "model.bin"}
	if err := EnsureArtifact(art, dir); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("expected 2 download attempts, got %d", got)
	}
	got, err := os.ReadFile(filepath.Join(dir, "model.bin"))
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("downloaded content mismatch: %q", got)
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

// makeTarGZ builds a tar.gz in memory with the given (name, content) entries.
// Top-level directories are added implicitly. Used to simulate the llama.cpp
// release tarball layout ("llama-b9747/llama-server" + sibling libraries).
func makeTarGZ(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range entries {
		hdr := &tar.Header{
			Name:     name,
			Mode:     0o644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestEnsureArtifactExtractsTarGZ(t *testing.T) {
	// Layout matches the llama.cpp release: versioned top-level dir contains
	// the binary and its sibling libraries.
	payload := makeTarGZ(t, map[string]string{
		"llama-b9747/llama-server":     "fake-binary",
		"llama-b9747/libllama.0.dylib": "libllama-bytes",
		"llama-b9747/libggml.dylib":    "libggml-bytes",
		"llama-b9747/README.md":        "irrelevant text",
	})
	sum := sha256.Sum256(payload)
	hexSum := hex.EncodeToString(sum[:])

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	dir := t.TempDir()
	art := Artifact{
		URL:     srv.URL,
		SHA256:  hexSum,
		Dest:    "llama-server",
		Exec:    true,
		Archive: ArchiveGZ,
	}
	if err := EnsureArtifact(art, dir); err != nil {
		t.Fatal(err)
	}

	// Binary extracted at the versioned path the launch argv references.
	got, err := os.ReadFile(filepath.Join(dir, "llama-b9747", "llama-server"))
	if err != nil {
		t.Fatalf("binary not extracted: %v", err)
	}
	if string(got) != "fake-binary" {
		t.Fatalf("binary content mismatch: %q", got)
	}
	// Sibling libraries extracted alongside the binary (so DYLD_LIBRARY_PATH
	// can find them).
	if _, err := os.Stat(filepath.Join(dir, "llama-b9747", "libllama.0.dylib")); err != nil {
		t.Fatalf("sibling lib not extracted: %v", err)
	}
	// Exec bit set.
	info, err := os.Stat(filepath.Join(dir, "llama-b9747", "llama-server"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o100 == 0 {
		t.Fatalf("expected exec bit set, mode = %v", info.Mode())
	}
	// Marker file written next to dest so a second call skips the download.
	marker, err := os.ReadFile(filepath.Join(dir, "llama-server.extracted-from"))
	if err != nil {
		t.Fatalf("marker not written: %v", err)
	}
	if strings.TrimRight(string(marker), "\n") != hexSum {
		t.Fatalf("marker = %q, want %q", marker, hexSum)
	}
	// Second call: cache honored, no re-download, no re-extract.
	if err := EnsureArtifact(art, dir); err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Fatalf("expected 1 download, got %d (archive cache not honored)", hits)
	}
	// Tarball should be removed after extraction (don't keep hundreds of MB
	// of duplicate content on disk).
	if _, err := os.Stat(filepath.Join(dir, "llama-server.tar.gz.tmp")); err == nil {
		t.Fatal("tarball tmp file should be removed after successful extraction")
	}
}

func TestEnsureArtifactRejectsTarGZBadSHA(t *testing.T) {
	payload := makeTarGZ(t, map[string]string{
		"llama-b9747/llama-server": "fake-binary",
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()
	art := Artifact{
		URL:     srv.URL,
		SHA256:  "deadbeef",
		Dest:    "llama-server",
		Archive: ArchiveGZ,
	}
	if err := EnsureArtifact(art, t.TempDir()); err == nil {
		t.Fatal("must reject sha mismatch on archive")
	}
}

func TestEnsureArtifactArchiveInvalidatesOnSHAMismatch(t *testing.T) {
	// Two archives with different SHAs. The second call must re-download
	// even if the destination still exists from the first call.
	payload1 := makeTarGZ(t, map[string]string{
		"llama-b9747/llama-server": "v1",
	})
	sum1 := sha256.Sum256(payload1)
	payload2 := makeTarGZ(t, map[string]string{
		"llama-b9747/llama-server": "v2",
	})
	sum2 := sha256.Sum256(payload2)

	idx := 0
	payloads := [][]byte{payload1, payload2}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payloads[idx])
		idx++
	}))
	defer srv.Close()

	dir := t.TempDir()
	art1 := Artifact{URL: srv.URL, SHA256: hex.EncodeToString(sum1[:]), Dest: "llama-server", Archive: ArchiveGZ}
	if err := EnsureArtifact(art1, dir); err != nil {
		t.Fatal(err)
	}
	art2 := Artifact{URL: srv.URL, SHA256: hex.EncodeToString(sum2[:]), Dest: "llama-server", Archive: ArchiveGZ}
	if err := EnsureArtifact(art2, dir); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "llama-b9747", "llama-server"))
	if string(got) != "v2" {
		t.Fatalf("expected v2 after re-download, got %q", got)
	}
	if idx != 2 {
		t.Fatalf("expected 2 downloads, got %d (sha change did not invalidate cache)", idx)
	}
}

func TestExtractTarGZRejectsZipSlip(t *testing.T) {
	// Entry that escapes destDir via `..` must be rejected, not extracted.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: "../escape.txt", Mode: 0o644, Size: 4, Typeflag: tar.TypeReg}
	_ = tw.WriteHeader(hdr)
	_, _ = tw.Write([]byte("pwn!"))
	_ = tw.Close()
	_ = gz.Close()
	archivePath := filepath.Join(t.TempDir(), "evil.tar.gz")
	if err := os.WriteFile(archivePath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := extractTarGZ(archivePath, t.TempDir(), "escape.txt"); err == nil {
		t.Fatal("must reject archive entries that escape destDir")
	}
}

func TestFindLibDir(t *testing.T) {
	dir := t.TempDir()
	if got := findLibDir(dir); got != "" {
		t.Fatalf("empty dir: want \"\", got %q", got)
	}
	// Versioned subdir with the binary.
	sub := filepath.Join(dir, "llama-b9747")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "llama-server"), []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := findLibDir(dir); got != sub {
		t.Fatalf("versioned subdir: want %q, got %q", sub, got)
	}
	// Flat layout fallback.
	flat := t.TempDir()
	if err := os.WriteFile(filepath.Join(flat, "llama-server"), []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := findLibDir(flat); got != flat {
		t.Fatalf("flat layout: want %q, got %q", flat, got)
	}
}
