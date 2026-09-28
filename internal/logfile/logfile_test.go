package logfile

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestWriterAppendsAcrossOpens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop.log")
	w, err := Open(path, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("one\n"))
	w.Close()
	w, err = Open(path, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("two\n"))
	w.Close()
	got, _ := os.ReadFile(path)
	if string(got) != "one\ntwo\n" {
		t.Fatalf("file = %q, want both lines appended", got)
	}
}

func TestWriterRotatesPastCapKeepingOneGeneration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop.log")
	w, err := Open(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for _, line := range []string{"aaaaaa\n", "bbbbbb\n", "cccccc\n"} {
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatalf("write %q: %v", line, err)
		}
	}
	cur, _ := os.ReadFile(path)
	prev, _ := os.ReadFile(path + ".1")
	if string(cur) != "cccccc\n" || string(prev) != "bbbbbb\n" {
		t.Fatalf("current = %q, .1 = %q; want newest line current and the one before in .1", cur, prev)
	}
}

func TestWriterConcurrentWritesKeepLinesWhole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop.log")
	w, err := Open(path, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.Write([]byte("0123456789\n"))
		}()
	}
	wg.Wait()
	w.Close()
	got, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSuffix(string(got), "\n"), "\n")
	if len(lines) != 50 {
		t.Fatalf("got %d lines, want 50", len(lines))
	}
	for _, l := range lines {
		if l != "0123456789" {
			t.Fatalf("torn line %q", l)
		}
	}
}
