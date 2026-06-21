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

func TestShortDylibName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"libllama-common.0.0.9747.dylib", "libllama-common.0.dylib"},
		{"libllama.0.0.9747.dylib", "libllama.0.dylib"},
		{"libmtmd.0.0.9747.dylib", "libmtmd.0.dylib"},
		{"libggml.0.15.2.dylib", "libggml.0.dylib"},
		{"libggml-base.0.15.2.dylib", "libggml-base.0.dylib"},
		{"libggml-cpu.0.15.2.dylib", "libggml-cpu.0.dylib"},
		{"libggml-metal.0.15.2.dylib", "libggml-metal.0.dylib"},
		{"libggml-blas.0.15.2.dylib", "libggml-blas.0.dylib"},
		{"libggml-rpc.0.15.2.dylib", "libggml-rpc.0.dylib"},
		// Unversioned dylibs — no symlink needed.
		{"libllama-server-impl.dylib", ""},
		{"libllama-batched-bench-impl.dylib", ""},
		{"libllama-bench-impl.dylib", ""},
		{"libllama-cli-impl.dylib", ""},
		{"libllama-completion-impl.dylib", ""},
		{"libllama-fit-params-impl.dylib", ""},
		{"libllama-perplexity-impl.dylib", ""},
		{"libllama-quantize-impl.dylib", ""},
		// Non-dylib files — no symlink.
		{"llama-server", ""},
		{"bge-m3-q4_k_m.gguf", ""},
		{"README.md", ""},
		// Short names that already match the expected form — no symlink needed.
		{"libfoo.1.dylib", ""},
		{"libbar.0.dylib", ""},
	}

	for _, tt := range tests {
		got := shortDylibName(tt.in)
		if got != tt.want {
			t.Errorf("shortDylibName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestEnsureDylibSymlinks(t *testing.T) {
	dir := t.TempDir()

	// Create version-triple dylib files.
	files := map[string]string{
		"libllama-common.0.0.9747.dylib": "common",
		"libllama.0.0.9747.dylib":        "llama",
		"libggml.0.15.2.dylib":           "ggml",
		"libggml-cpu.0.15.2.dylib":       "ggml-cpu",
		"libllama-server-impl.dylib":     "server", // unversioned, no symlink expected
		"readme.txt":                      "text",   // not a dylib
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ensureDylibSymlinks(dir)

	// Verify symlinks.
	expectedLinks := map[string]string{
		"libllama-common.0.dylib": "libllama-common.0.0.9747.dylib",
		"libllama.0.dylib":        "libllama.0.0.9747.dylib",
		"libggml.0.dylib":         "libggml.0.15.2.dylib",
		"libggml-cpu.0.dylib":     "libggml-cpu.0.15.2.dylib",
	}
	for link, target := range expectedLinks {
		linkPath := filepath.Join(dir, link)
		got, err := os.Readlink(linkPath)
		if err != nil {
			t.Errorf("expected symlink %s, got error: %v", link, err)
			continue
		}
		if got != target {
			t.Errorf("symlink %s -> %s, want -> %s", link, got, target)
		}
	}

	// Verify unversioned dylib did NOT get a symlink.
	if _, err := os.Lstat(filepath.Join(dir, "libllama-server-impl.dylib")); err != nil {
		t.Fatal("libllama-server-impl.dylib should still exist")
	}
	// Ensure no bogus symlink was created for unversioned dylib.
	if _, err := os.Lstat(filepath.Join(dir, "libllama-server-impl.0.dylib")); !os.IsNotExist(err) {
		t.Error("unexpected symlink created for unversioned dylib")
	}

	// Ensure non-dylib files were not affected.
	if _, err := os.Lstat(filepath.Join(dir, "readme.txt")); err != nil {
		t.Errorf("readme.txt should still exist: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "readme.dylib")); !os.IsNotExist(err) {
		t.Error("unexpected .dylib file created for readme.txt")
	}
}

func TestEnsureDylibSymlinksIdempotent(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "libfoo.0.1.2.dylib"), []byte("foo"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Run twice.
	ensureDylibSymlinks(dir)
	ensureDylibSymlinks(dir)

	linkPath := filepath.Join(dir, "libfoo.0.dylib")
	got, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("expected symlink after first run: %v", err)
	}
	if got != "libfoo.0.1.2.dylib" {
		t.Fatalf("symlink points to %q, want %q", got, "libfoo.0.1.2.dylib")
	}
}

func TestEnsureDylibSymlinksRecursive(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "llama-b9747")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "libggml.0.15.2.dylib"), []byte("ggml"), 0o644); err != nil {
		t.Fatal(err)
	}

	ensureDylibSymlinks(dir)

	linkPath := filepath.Join(sub, "libggml.0.dylib")
	got, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("expected symlink in subdirectory: %v", err)
	}
	if got != "libggml.0.15.2.dylib" {
		t.Fatalf("symlink points to %q, want %q", got, "libggml.0.15.2.dylib")
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
