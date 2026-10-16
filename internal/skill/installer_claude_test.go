package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// TestInstallBundledToWritesSkillFile covers `/plugin install <skill>`: the
// bundled body lands at <dir>/<name>/SKILL.md and the report names it.
func TestInstallBundledToWritesSkillFile(t *testing.T) {
	withBundledFS(t, fstest.MapFS{
		"skills/alpha/SKILL.md": &fstest.MapFile{Data: []byte(skillA)},
	})
	dir := filepath.Join(t.TempDir(), "missing", "skills")

	msg, err := installBundledTo("alpha", dir)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "alpha", "SKILL.md"))
	if err != nil {
		t.Fatalf("read installed: %v", err)
	}
	if string(got) != skillA {
		t.Fatalf("installed body mismatch:\nwant %q\ngot  %q", skillA, got)
	}
	if !strings.Contains(msg, "installed alpha") {
		t.Fatalf("report %q does not name the install", msg)
	}
}

// TestInstallBundledToSecondRunIsNoOp: an up-to-date copy is reported and not
// rewritten, so no .bak file appears.
func TestInstallBundledToSecondRunIsNoOp(t *testing.T) {
	withBundledFS(t, fstest.MapFS{
		"skills/alpha/SKILL.md": &fstest.MapFile{Data: []byte(skillA)},
	})
	dir := t.TempDir()
	if _, err := installBundledTo("alpha", dir); err != nil {
		t.Fatalf("first install: %v", err)
	}

	msg, err := installBundledTo("alpha", dir)
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if !strings.Contains(msg, "already up to date") {
		t.Fatalf("report %q should say up to date", msg)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".bak.") {
			t.Fatalf("up-to-date reinstall wrote a backup: %s", e.Name())
		}
	}
}

// TestInstallBundledToRefusesSymlinkTarget: ~/.claude/skills/<name> is often a
// symlink into a repo (cover-letter is). Installing through it would overwrite
// the repo's source, so the installer must refuse and leave the target alone.
func TestInstallBundledToRefusesSymlinkTarget(t *testing.T) {
	withBundledFS(t, fstest.MapFS{
		"skills/alpha/SKILL.md": &fstest.MapFile{Data: []byte(skillA)},
	})
	dir := t.TempDir()
	realDir := filepath.Join(t.TempDir(), "repo-alpha")
	mustMkdir(t, realDir)
	mustWrite(t, filepath.Join(realDir, "SKILL.md"), "repo source")
	if err := os.Symlink(realDir, filepath.Join(dir, "alpha")); err != nil {
		t.Fatal(err)
	}

	if _, err := installBundledTo("alpha", dir); err == nil {
		t.Fatal("expected refusal when target is a symlink")
	}
	got, err := os.ReadFile(filepath.Join(realDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "repo source" {
		t.Fatalf("symlink was followed and repo source overwritten: %q", got)
	}
}

// TestInstallBundledToUnknownNameListsBundled: a bare name that is not a
// bundled skill is an error that lists what is available. It never installs
// anything, so `/plugin install <typo>` cannot silently succeed.
func TestInstallBundledToUnknownNameListsBundled(t *testing.T) {
	withBundledFS(t, fstest.MapFS{
		"skills/alpha/SKILL.md": &fstest.MapFile{Data: []byte(skillA)},
		"skills/beta/SKILL.md":  &fstest.MapFile{Data: []byte("---\nname: beta\n---\n# beta\n")},
	})
	dir := t.TempDir()

	_, err := installBundledTo("gamma", dir)
	if err == nil {
		t.Fatal("expected error for unknown bundled skill")
	}
	if !strings.Contains(err.Error(), "bundled: alpha, beta") {
		t.Fatalf("error %q should list bundled skills", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "gamma")); statErr == nil {
		t.Fatal("unknown name must not create a target directory")
	}
}
