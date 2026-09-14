package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenCrashLog_CreatesDirAndAppends(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	f, err := openCrashLog(dir)
	if err != nil {
		t.Fatalf("openCrashLog: %v", err)
	}
	if _, err := f.WriteString("one\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	f, err = openCrashLog(dir)
	if err != nil {
		t.Fatalf("openCrashLog second open: %v", err)
	}
	if _, err := f.WriteString("two\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	data, err := os.ReadFile(filepath.Join(dir, crashLogName))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "one\ntwo\n" {
		t.Fatalf("expected append semantics, got %q", data)
	}
}
