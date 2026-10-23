package remote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeHomeFile(t *testing.T, home, rel, body string, mode os.FileMode) {
	t.Helper()
	p := filepath.Join(home, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func homeFilesByName(t *testing.T, home string) map[string]SyncFile {
	t.Helper()
	files, err := collectHomeSyncFiles(home)
	if err != nil {
		t.Fatalf("collectHomeSyncFiles: %v", err)
	}
	m := map[string]SyncFile{}
	for _, f := range files {
		m[f.Name] = f
	}
	return m
}

func TestHomeSync_SettingsFilteredToAllowlistedKeys(t *testing.T) {
	home := t.TempDir()
	writeHomeFile(t, home, ".claude/settings.json", `{"env":{"TOKEN":"s3cret"},"apiKeyHelper":"x","permissions":{"allow":["Bash(ls:*)"]}}`, 0o644)
	f, ok := homeFilesByName(t, home)[".claude/settings.json"]
	if !ok {
		t.Fatal("settings.json not collected")
	}
	body := string(f.Content)
	if strings.Contains(body, "s3cret") || strings.Contains(body, "apiKeyHelper") || !strings.Contains(body, "permissions") {
		t.Fatalf("settings not filtered correctly: %s", body)
	}
}

func TestHomeSync_CollectsSkillsAgentsAndMarkdown(t *testing.T) {
	home := t.TempDir()
	writeHomeFile(t, home, ".claude/CLAUDE.md", "# claude", 0o644)
	writeHomeFile(t, home, ".config/opencode/AGENTS.md", "# agents", 0o644)
	writeHomeFile(t, home, ".claude/skills/foo/SKILL.md", "skill", 0o644)
	writeHomeFile(t, home, ".claude/skills/foo/run.sh", "#!/bin/sh", 0o755)
	writeHomeFile(t, home, ".claude/skills/foo/.env", "KEY=1", 0o644)
	writeHomeFile(t, home, ".claude/skills/foo/node_modules/x.js", "x", 0o644)
	writeHomeFile(t, home, ".claude/agents/a.md", "agent", 0o644)
	writeHomeFile(t, home, ".ssh/id_rsa", "private", 0o600)
	got := homeFilesByName(t, home)
	for _, want := range []string{".claude/CLAUDE.md", ".config/opencode/AGENTS.md", ".claude/skills/foo/SKILL.md", ".claude/skills/foo/run.sh", ".claude/agents/a.md"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing %s", want)
		}
	}
	for _, bad := range []string{".claude/skills/foo/.env", ".claude/skills/foo/node_modules/x.js", ".ssh/id_rsa"} {
		if _, ok := got[bad]; ok {
			t.Errorf("collected forbidden %s", bad)
		}
	}
	if homeFileMode(got[".claude/skills/foo/run.sh"].Mode) != 0o700 || homeFileMode(got[".claude/skills/foo/SKILL.md"].Mode) != 0o600 {
		t.Fatal("exec bit not preserved only for executables")
	}
}

func TestHomeSync_FollowsSymlinksAndHashesTargetContent(t *testing.T) {
	home := t.TempDir()
	writeHomeFile(t, home, ".agents/skills/bar/SKILL.md", "v1", 0o644)
	if err := os.MkdirAll(filepath.Join(home, ".claude", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	// directory symlink and a file symlink, both pointing at real content
	if err := os.Symlink(filepath.Join(home, ".agents", "skills", "bar"), filepath.Join(home, ".claude", "skills", "bar")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	writeHomeFile(t, home, "dotfiles/CLAUDE.md", "linked v1", 0o644)
	if err := os.Symlink(filepath.Join(home, "dotfiles", "CLAUDE.md"), filepath.Join(home, ".claude", "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}

	before := homeFilesByName(t, home)
	if string(before[".claude/skills/bar/SKILL.md"].Content) != "v1" || string(before[".claude/CLAUDE.md"].Content) != "linked v1" {
		t.Fatalf("symlinked content not collected: %+v", before)
	}
	h1, _ := PayloadHash(SyncPayload{Files: filesOf(before)})

	writeHomeFile(t, home, "dotfiles/CLAUDE.md", "linked v2", 0o644)
	h2, _ := PayloadHash(SyncPayload{Files: filesOf(homeFilesByName(t, home))})
	if h1 == h2 {
		t.Fatal("editing a symlink target did not change the payload hash")
	}
}

func filesOf(m map[string]SyncFile) []SyncFile {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	// sorted for a stable hash
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	out := make([]SyncFile, 0, len(names))
	for _, n := range names {
		out = append(out, m[n])
	}
	return out
}

func TestHomeSync_SymlinkIntoCredentialDirsOrOutsideHomeIsSkipped(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	writeHomeFile(t, home, ".ssh/id_rsa", "private", 0o600)
	writeHomeFile(t, outside, "evil.md", "outside", 0o644)
	if err := os.MkdirAll(filepath.Join(home, ".claude", "skills", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, ".ssh", "id_rsa"), filepath.Join(home, ".claude", "skills", "x", "key.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "evil.md"), filepath.Join(home, ".claude", "skills", "x", "out.md")); err != nil {
		t.Fatal(err)
	}
	got := homeFilesByName(t, home)
	if len(got) != 0 {
		t.Fatalf("unsafe links were collected: %v", got)
	}
}

func TestHomeSync_SymlinkCycleTerminates(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "skills", "loop")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeHomeFile(t, home, ".claude/skills/loop/SKILL.md", "s", 0o644)
	if err := os.Symlink(dir, filepath.Join(dir, "self")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	got := homeFilesByName(t, home)
	if _, ok := got[".claude/skills/loop/SKILL.md"]; !ok {
		t.Fatalf("real file missing: %v", got)
	}
}

func TestWritePayload_HomeRefusesNonAllowlistedPaths(t *testing.T) {
	dest := t.TempDir()
	prev := resolveSyncDirFn
	resolveSyncDirFn = func(SyncFileDir) (string, error) { return dest, nil }
	t.Cleanup(func() { resolveSyncDirFn = prev })

	for _, name := range []string{"../escape", ".ssh/authorized_keys", ".bashrc", "/etc/passwd", ".claude/skills/../../.bashrc", `.claude\skills\x`, ".claude/skills/.env"} {
		err := WritePayload(SyncPayload{Files: []SyncFile{{Dir: SyncDirHome, Name: name, Content: []byte("x")}}})
		if err == nil {
			t.Errorf("WritePayload accepted %q", name)
		}
	}
	ok := SyncFile{Dir: SyncDirHome, Name: ".claude/skills/foo/run.sh", Content: []byte("#!/bin/sh"), Mode: 0o755}
	if err := WritePayload(SyncPayload{Files: []SyncFile{ok}}); err != nil {
		t.Fatalf("allowlisted write failed: %v", err)
	}
	info, err := os.Stat(filepath.Join(dest, ".claude", "skills", "foo", "run.sh"))
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("written file: %v %v", info, err)
	}
}

func TestWritePayload_HomeDeletesStaleSyncedFilesOnly(t *testing.T) {
	dest := t.TempDir()
	prev := resolveSyncDirFn
	resolveSyncDirFn = func(SyncFileDir) (string, error) { return dest, nil }
	t.Cleanup(func() { resolveSyncDirFn = prev })

	file := func(name string) SyncFile { return SyncFile{Dir: SyncDirHome, Name: name, Content: []byte(name)} }
	first := SyncPayload{Files: []SyncFile{file(".claude/skills/a/SKILL.md"), file(".claude/skills/b/SKILL.md"), file(".claude/CLAUDE.md")}}
	if err := WritePayload(first); err != nil {
		t.Fatal(err)
	}
	// a file the user made by hand next to a synced one, and one outside the manifest
	hand := filepath.Join(dest, ".claude", "skills", "a", "mine.md")
	if err := os.WriteFile(hand, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}

	second := SyncPayload{Files: []SyncFile{file(".claude/skills/a/SKILL.md")}}
	if err := WritePayload(second); err != nil {
		t.Fatal(err)
	}
	exists := func(rel string) bool {
		_, err := os.Stat(filepath.Join(dest, filepath.FromSlash(rel)))
		return err == nil
	}
	if exists(".claude/skills/b/SKILL.md") || exists(".claude/skills/b") {
		t.Fatal("stale synced skill (and its empty dir) was not removed")
	}
	if exists(".claude/CLAUDE.md") {
		t.Fatal("stale synced CLAUDE.md was not removed")
	}
	if !exists(".claude/skills/a/SKILL.md") || !exists(".claude/skills/a/mine.md") {
		t.Fatal("kept file or hand-made file was removed")
	}
	if !exists(".claude/skills") {
		t.Fatal("tree root must never be pruned")
	}
}

func TestWritePayload_TamperedManifestCannotDeleteOutsideAllowlist(t *testing.T) {
	dest := t.TempDir()
	prev := resolveSyncDirFn
	resolveSyncDirFn = func(SyncFileDir) (string, error) { return dest, nil }
	t.Cleanup(func() { resolveSyncDirFn = prev })

	victim := filepath.Join(dest, ".bashrc")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, homeSyncManifestName), []byte(`[".bashrc","../x"]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WritePayload(SyncPayload{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("manifest tamper deleted a non-allowlisted file: %v", err)
	}
}
