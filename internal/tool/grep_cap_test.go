package tool

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// A file past the per-file scan cap must be reported with a cap note, not
// silently truncated, and files below the cap must behave identically to
// before.
func TestGrepLargeFileCapped(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	oldCap := maxGrepFileBytes
	maxGrepFileBytes = 4096
	defer func() { maxGrepFileBytes = oldCap }()

	// big.log (~9KB > 4KB cap): matches INSIDE the cap (lines 1-2), then
	// noise past it; the final needle line is beyond the cap so it must NOT
	// appear, and the cap note must ride the match so the model knows the
	// file was only partially searched.
	big := "big needle one\nbig needle two\n" + strings.Repeat("noise\n", 1500) + "big needle past\n"
	if err := os.WriteFile("big.log", []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	// small.txt: normal match inside the cap.
	if err := os.WriteFile("small.txt", []byte("hello needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := GrepTool{}.Execute(json.RawMessage(`{"pattern":"needle"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res, "small.txt:1:hello needle") {
		t.Fatalf("small file match missing: %.400s", res)
	}
	if !strings.Contains(res, "big.log:1:big needle one") || !strings.Contains(res, "big.log:2:big needle two") {
		t.Fatalf("in-cap matches missing: %.600s", res)
	}
	if !strings.Contains(res, "scan cap") {
		t.Fatalf("expected cap note for big.log: %.800s", res)
	}
	if strings.Contains(res, "needle past") {
		t.Fatalf("past-cap match must not appear: %.800s", res)
	}

	// files_with_matches mode: capped file still listed (it matched) — the
	// cap note rides the content mode only.
	res2, err := GrepTool{}.Execute(json.RawMessage(`{"pattern":"needle","output_mode":"files_with_matches"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res2, "small.txt") {
		t.Fatalf("small file missing in files mode: %s", res2)
	}
}

// No matches past the cap: the file must be absent, with no error.
func TestGrepLargeFileNoMatch(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	oldCap := maxGrepFileBytes
	maxGrepFileBytes = 2048
	defer func() { maxGrepFileBytes = oldCap }()

	if err := os.WriteFile("big2.log", []byte(strings.Repeat("nothing here\n", 2000)), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := GrepTool{}.Execute(json.RawMessage(`{"pattern":"zebra-needle"}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res, "big2.log") {
		t.Fatalf("no-match capped file must not appear: %s", res)
	}
}
