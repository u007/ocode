package debugcli

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func TestRunProjectSlug(t *testing.T) {
	dir := t.TempDir()

	stdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	err = Run([]string{"project-slug", dir})
	w.Close()
	os.Stdout = stdout
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}

	var out projectSlugOutput
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("output not valid JSON: %v\n%s", err, buf.String())
	}
	if out.Slug == "" {
		t.Error("expected non-empty slug")
	}
	if out.GlobalDataDir == "" {
		t.Error("expected non-empty globalDataDir")
	}
	if out.SessionsDir == "" {
		t.Error("expected non-empty sessionsDir")
	}
}

func TestRunUnknownSubcommand(t *testing.T) {
	if err := Run([]string{"nope"}); err == nil {
		t.Error("expected error for unknown subcommand")
	}
}

func TestRunNoArgs(t *testing.T) {
	if err := Run(nil); err == nil {
		t.Error("expected error when no subcommand given")
	}
}
