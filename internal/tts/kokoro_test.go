package tts

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

// shortTempDir returns a temp directory with a short absolute path. t.TempDir()
// on macOS resolves under /var/folders/... and can already be 150+ bytes, which
// leaves no room for a short espeak data directory under the 160-byte
// N_PATH_HOME limit.
func shortTempDir(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "ocode-kokoro-")
	if err != nil {
		t.Skipf("cannot create a short temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return root
}

func TestEspeakDataPath(t *testing.T) {
	dir := shortTempDir(t)
	short := filepath.Join(dir, "espeak-ng-data")
	if err := os.MkdirAll(short, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(short, "phontab"), []byte("phontab"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := espeakDataPath(dir, short); err != nil || got != short {
		t.Fatalf("path within the limit must be used as-is: got %q err %v", got, err)
	}

	// A bundled path deeper than espeak-ng's fixed buffer needs a short copy.
	long := filepath.Join(dir, strings.Repeat("nested/", 30), "espeak-ng-data")
	if len(long) < espeakDataPathLimit() {
		t.Fatalf("test setup: %d-byte path is not over the %d-byte limit", len(long), espeakDataPathLimit())
	}
	if err := os.MkdirAll(long, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(long, "phontab"), []byte("phontab"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := espeakDataPath(dir, long)
	if err != nil {
		t.Fatalf("espeakDataPath: %v", err)
	}
	if len(got) >= espeakDataPathLimit() {
		t.Fatalf("short data path is still %d bytes (limit %d): %q", len(got), espeakDataPathLimit(), got)
	}
	if info, err := os.Stat(filepath.Join(got, "phontab")); err != nil || info.IsDir() {
		t.Fatalf("copied data directory is incomplete: %v", err)
	}
	// The copy must be a real directory: phonemizer resolves symlinks before
	// handing the path to espeak-ng, which would expand the link again.
	if fi, err := os.Lstat(got); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("expected a real directory copy, got mode %v err %v", fi.Mode(), err)
	}
	// Second call reuses the existing copy.
	again, err := espeakDataPath(dir, long)
	if err != nil || again != got {
		t.Fatalf("second call changed the copy: got %q err %v, want %q", again, err, got)
	}
	// No short data directory exists if dir itself already exceeds the limit.
	deep := strings.Repeat("d", espeakDataPathLimit()+1)
	if _, err := espeakDataPath(deep, long); err == nil {
		t.Fatal("expected an error when no short data directory fits the limit")
	}
}

// TestKokoroSynthShortensLongEspeakDataPath is the regression guard for the
// "Error processing file '/Users/runner/work/.../phontab'" failure: the synth
// process must receive a data path short enough for espeak-ng's N_PATH_HOME
// buffer even when the venv's bundled data directory is too long.
func TestKokoroSynthShortensLongEspeakDataPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake venv script needs a POSIX shell")
	}
	root := shortTempDir(t)
	m := kokoroManifest
	m.Runtime = map[string]PythonRuntime{Host(): {Requirements: []string{"x==1"}, MinPython: [2]int{3, 11}}}
	m.VoiceFiles = nil
	dir, err := CacheDirChecked(root, m.Engine, m.Voice, m.Version)
	if err != nil {
		t.Fatal(err)
	}
	venv := filepath.Join(dir, "venv")
	if err := os.MkdirAll(filepath.Join(venv, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Model the venv's real nesting, padded so it exceeds espeak-ng's limit
	// while the `.espeak-data` copy next to the script stays short.
	long := filepath.Join(venv, "lib", "python3.13", "site-packages", strings.Repeat("nested/", 30), "espeakng_loader", "espeak-ng-data")
	if len(long) < espeakDataPathLimit() {
		t.Fatalf("test setup: bundled path %d is under the %d-byte limit", len(long), espeakDataPathLimit())
	}
	if len(filepath.Join(dir, espeakDataDirName)) >= espeakDataPathLimit() {
		t.Fatalf("test setup: %s does not fit the %d-byte limit", espeakDataDirName, espeakDataPathLimit())
	}
	if err := os.MkdirAll(long, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(long, "phontab"), []byte("phontab"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The fake "python" answers the espeak data-path query with the long path
	// and, for the synth invocation, fails unless argv[6] is a real short
	// directory holding the copied data before writing a stand-in WAV.
	fake := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "-c" ]; then
  printf '%%s\n' '%s'
  exit 0
fi
data="$6"
if [ "$data" = '%s' ]; then
  echo "data path was not shortened: $data" >&2
  exit 7
fi
if [ ! -d "$data" ]; then
  echo "data path is not a directory: $data" >&2
  exit 8
fi
if [ -L "$data" ]; then
  echo "data path must not be a symlink: $data" >&2
  exit 9
fi
if [ ! -f "$data/phontab" ]; then
  echo "copied data is missing phontab: $data" >&2
  exit 10
fi
cat >/dev/null
printf 'RIFFfakewav' > "$5"
`, long, long)
	if err := os.WriteFile(filepath.Join(venv, "bin", "python"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(dir, "out.wav")
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	if err := kokoroSynth(context.Background(), sup, root, m, "test", "hello there", outPath, m.Voice); err != nil {
		t.Fatalf("kokoroSynth: %v", err)
	}
	if b, err := os.ReadFile(outPath); err != nil || !bytes.HasPrefix(b, []byte("RIFF")) {
		t.Fatalf("no WAV produced: err %v content %q", err, b)
	}
	if info, err := os.Stat(filepath.Join(dir, espeakDataDirName, "phontab")); err != nil || info.IsDir() {
		t.Fatalf("no short espeak data copy was left behind: %v", err)
	}
}
