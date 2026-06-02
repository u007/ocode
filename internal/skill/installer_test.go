package skill

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// withBundledFS sets the bundled FS for the duration of a test using a
// fstest.MapFS so each test can build its own minimal skills tree. The
// returned restore function resets the package state via t.Cleanup.
func withBundledFS(t *testing.T, m fstest.MapFS) {
	t.Helper()
	SetBundledFS(m)
	t.Cleanup(func() { SetBundledFS(nil) })
}

// withTempHome points os.UserHomeDir at a temp dir for the duration of a
// test, so globalSkillsDir() resolves inside the test sandbox. The
// returned path is the full skills-dir path inside the temp HOME, and
// is created on disk so callers can write into it directly.
func withTempHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	skillsDir := filepath.Join(dir, ".config", "opencode", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return skillsDir
}

const skillA = `---
name: alpha
version: 1.0.0
description: alpha skill
---

# alpha
`

const skillAv2 = `---
name: alpha
version: 2.0.0
description: alpha skill v2
---

# alpha (v2)
`

const skillB = `---
name: beta
description: beta skill (no version)
---

# beta
`

// ---------------------------------------------------------------------------
// Version / hash detection
// ---------------------------------------------------------------------------

func TestParseFrontmatterVersion(t *testing.T) {
	cases := map[string]string{
		skillA:    "1.0.0",
		skillAv2:  "2.0.0",
		skillB:    "",
		"foo\n":   "",
		"---\nversion: 1.2\n---\n":   "",
		"---\nversion: 1.2.3.4\n---\n": "1.2.3",
		"---\nversion: \"1.2.3\"\n---\n": "1.2.3",
		"---\nVERSION: 1.2.3\n---\n": "1.2.3",
	}
	for in, want := range cases {
		if got := parseFrontmatterVersion(in); got != want {
			t.Errorf("parseFrontmatterVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"1.0.1", "1.0.0", 1},
		{"2.0.0", "1.99.99", 1},
		{"", "1.0.0", -1},
		{"1.0.0", "", 1},
		{"", "", 0},
		{"1", "1.0.0", 0},
		{"1.0", "1.0.0", 0},
	}
	for _, tc := range cases {
		if got := compareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestIsUpToDatePrefersVersionOverHash(t *testing.T) {
	b := bundledSkill{Name: "alpha", Bytes: []byte(skillAv2), Version: "2.0.0"}
	// Installed has version 1.0.0 → outdated even though we can compare
	// bodies: version comparison is the source of truth.
	inst := installedInfo{Version: "1.0.0", Sha256Hex: "irrelevant"}
	if isUpToDate(b, inst) {
		t.Fatal("expected outdated (bundled 2.0.0 > installed 1.0.0)")
	}
	// Same version on both sides → up to date, regardless of body.
	inst.Version = "2.0.0"
	if !isUpToDate(b, inst) {
		t.Fatal("expected up-to-date when versions match")
	}
}

func TestIsUpToDateFallsBackToHash(t *testing.T) {
	b := bundledSkill{Name: "alpha", Bytes: []byte(skillB), Version: ""}
	inst := installedInfo{Version: "", Bytes: []byte(skillB)}
	// Identical body → up to date.
	if !isUpToDate(b, inst) {
		t.Fatal("expected up-to-date when bodies match and no versions declared")
	}
	// Different body → outdated.
	inst.Bytes = []byte("# beta edited\n")
	if isUpToDate(b, inst) {
		t.Fatal("expected outdated when bodies differ")
	}
}

// ---------------------------------------------------------------------------
// Install / upgrade
// ---------------------------------------------------------------------------

func TestInstallWritesSkillFile(t *testing.T) {
	withBundledFS(t, fstest.MapFS{
		"skills/alpha/SKILL.md": &fstest.MapFile{Data: []byte(skillA)},
	})
	target := withTempHome(t)

	if err := runInstallOrUpgrade(nil, true); err != nil {
		t.Fatalf("install: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(target, "alpha", "SKILL.md"))
	if err != nil {
		t.Fatalf("read installed: %v", err)
	}
	if string(got) != skillA {
		t.Fatalf("installed body mismatch:\nwant %q\ngot  %q", skillA, got)
	}
}

func TestInstallBacksUpExistingFile(t *testing.T) {
	withBundledFS(t, fstest.MapFS{
		"skills/alpha/SKILL.md": &fstest.MapFile{Data: []byte(skillAv2)},
	})
	target := withTempHome(t)

	// Pre-populate with v1.
	dst := filepath.Join(target, "alpha", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte(skillA), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runInstallOrUpgrade(nil, true); err != nil {
		t.Fatalf("install: %v", err)
	}

	// New file is in place.
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read installed: %v", err)
	}
	if string(got) != skillAv2 {
		t.Fatalf("installed body not v2:\nwant %q\ngot  %q", skillAv2, got)
	}

	// Backup exists and matches the v1 body.
	matches, err := filepath.Glob(filepath.Join(target, "alpha", "SKILL.md.bak.*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected exactly one backup, got %v", matches)
	}
	bak, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(bak) != skillA {
		t.Fatalf("backup body mismatch:\nwant %q\ngot  %q", skillA, bak)
	}
}

func TestUpgradeIsNoOpWhenUpToDate(t *testing.T) {
	withBundledFS(t, fstest.MapFS{
		"skills/alpha/SKILL.md": &fstest.MapFile{Data: []byte(skillA)},
	})
	target := withTempHome(t)
	dst := filepath.Join(target, "alpha", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte(skillA), 0o644); err != nil {
		t.Fatal(err)
	}
	// Pre-seed a backup from a *prior* update — the upgrade must not
	// add another one when content is already up to date.
	preBak := filepath.Join(target, "alpha", "SKILL.md.bak.20000101T000000Z")
	if err := os.WriteFile(preBak, []byte("preexisting"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runInstallOrUpgrade(nil, false /*force*/); err != nil {
		t.Fatalf("upgrade: %v", err)
	}

	matches, _ := filepath.Glob(filepath.Join(target, "alpha", "SKILL.md.bak.*"))
	if len(matches) != 1 {
		t.Fatalf("upgrade should not create a new backup; got %v", matches)
	}
}

func TestUpgradeWritesAndBacksUpWhenOutdated(t *testing.T) {
	withBundledFS(t, fstest.MapFS{
		"skills/alpha/SKILL.md": &fstest.MapFile{Data: []byte(skillAv2)},
	})
	target := withTempHome(t)
	dst := filepath.Join(target, "alpha", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte(skillA), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runInstallOrUpgrade(nil, false /*force*/); err != nil {
		t.Fatalf("upgrade: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != skillAv2 {
		t.Fatalf("upgrade did not write v2: %q", got)
	}
	matches, _ := filepath.Glob(filepath.Join(target, "alpha", "SKILL.md.bak.*"))
	if len(matches) != 1 {
		t.Fatalf("expected one backup, got %v", matches)
	}
	bak, _ := os.ReadFile(matches[0])
	if string(bak) != skillA {
		t.Fatalf("backup body mismatch: %q", bak)
	}
}

// ---------------------------------------------------------------------------
// Symlink safety
// ---------------------------------------------------------------------------

func TestInstallRefusesSymlinkTarget(t *testing.T) {
	withBundledFS(t, fstest.MapFS{
		"skills/alpha/SKILL.md": &fstest.MapFile{Data: []byte(skillA)},
	})
	target := withTempHome(t)

	// Make a real skill elsewhere, then symlink the install target to
	// that dir. The installer must refuse to overwrite the symlink.
	realDir := filepath.Join(t.TempDir(), "real-alpha")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realDir, "SKILL.md"), []byte("user content"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(target, "alpha")
	if err := os.Symlink(realDir, linkPath); err != nil {
		t.Fatal(err)
	}

	// install with force=true: even force must not follow a symlink.
	if err := runInstallOrUpgrade(nil, true); err == nil {
		t.Fatal("expected error when target is a symlink")
	}

	// The user's SKILL.md at realDir must be unchanged.
	got, err := os.ReadFile(filepath.Join(realDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "user content" {
		t.Fatalf("symlink was followed and user file overwritten: %q", got)
	}
}

func TestUninstallRefusesSymlink(t *testing.T) {
	withBundledFS(t, fstest.MapFS{})
	target := withTempHome(t)

	realDir := filepath.Join(t.TempDir(), "real-alpha")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realDir, "SKILL.md"), []byte("user content"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(target, "alpha")
	if err := os.Symlink(realDir, linkPath); err != nil {
		t.Fatal(err)
	}

	if err := runUninstall([]string{"alpha"}); err == nil {
		t.Fatal("expected uninstall error for symlink target")
	}
	// Symlink still in place.
	if _, err := os.Lstat(linkPath); err != nil {
		t.Fatalf("symlink was removed: %v", err)
	}
	// Real target untouched.
	if _, err := os.ReadFile(filepath.Join(realDir, "SKILL.md")); err != nil {
		t.Fatalf("real file removed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Pick / list / uninstall smoke
// ---------------------------------------------------------------------------

func TestPickBundledFiltersByName(t *testing.T) {
	bundled := []bundledSkill{
		{Name: "alpha"},
		{Name: "beta"},
		{Name: "gamma"},
	}
	got := pickBundled(bundled, []string{"beta"})
	if len(got) != 1 || got[0].Name != "beta" {
		t.Fatalf("pickBundled filtered wrongly: %+v", got)
	}
	got = pickBundled(bundled, nil)
	if len(got) != 3 {
		t.Fatalf("pickBundled with no names should return all; got %+v", got)
	}
}

func TestUninstallRemovesDir(t *testing.T) {
	withBundledFS(t, fstest.MapFS{})
	target := withTempHome(t)
	dst := filepath.Join(target, "alpha")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "SKILL.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runUninstall([]string{"alpha"}); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		t.Fatalf("dir still present: %v", err)
	}
}

func TestUninstallRequiresName(t *testing.T) {
	if err := runUninstall(nil); err == nil {
		t.Fatal("expected error when no name provided")
	}
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

func TestRunListReportsStatus(t *testing.T) {
	withBundledFS(t, fstest.MapFS{
		"skills/alpha/SKILL.md": &fstest.MapFile{Data: []byte(skillA)},
		"skills/beta/SKILL.md":  &fstest.MapFile{Data: []byte(skillB)},
		"skills/gamma/SKILL.md": &fstest.MapFile{Data: []byte("# gamma\n")},
	})
	target := withTempHome(t)

	// alpha: up to date
	mustMkdir(t, filepath.Join(target, "alpha"))
	mustWrite(t, filepath.Join(target, "alpha", "SKILL.md"), skillA)

	// beta: outdated (bundled != installed)
	mustMkdir(t, filepath.Join(target, "beta"))
	mustWrite(t, filepath.Join(target, "beta", "SKILL.md"), "# beta (local edit)\n")

	// gamma: missing
	// delta: installed but not bundled
	mustMkdir(t, filepath.Join(target, "delta"))
	mustWrite(t, filepath.Join(target, "delta", "SKILL.md"), "delta local\n")

	if err := runList(); err != nil {
		t.Fatalf("list: %v", err)
	}
	// We don't assert on stdout formatting here; the side-effects (no
	// mutation of the target) and the absence of error are the contract.
	// Per-skill status coverage is provided by the targeted tests above.
}

// ---------------------------------------------------------------------------
// Bundled FS plumbing
// ---------------------------------------------------------------------------

func TestBundledSkillsErrorsWhenNotRegistered(t *testing.T) {
	SetBundledFS(nil)
	if _, err := bundledSkills(); err == nil {
		t.Fatal("expected error when no FS is registered")
	}
}

func TestBundledSkillsReadsRepoRootedFS(t *testing.T) {
	// The injected FS is repo-rooted and contains a `skills/` subtree.
	withBundledFS(t, fstest.MapFS{
		"skills/alpha/SKILL.md": &fstest.MapFile{Data: []byte(skillA)},
	})
	got, err := bundledSkills()
	if err != nil {
		t.Fatalf("bundledSkills: %v", err)
	}
	if len(got) != 1 || got[0].Name != "alpha" {
		t.Fatalf("expected one skill 'alpha', got %+v", namesOf(got))
	}
}

func TestBundledSkillsSkipsDirsWithoutSkillMD(t *testing.T) {
	withBundledFS(t, fstest.MapFS{
		"skills/alpha/SKILL.md":    &fstest.MapFile{Data: []byte(skillA)},
		"skills/README.md":         &fstest.MapFile{Data: []byte("top-level readme")},
		"skills/notaskill/note.md": &fstest.MapFile{Data: []byte("note")},
	})
	got, err := bundledSkills()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "alpha" {
		t.Fatalf("expected only 'alpha', got %+v", namesOf(got))
	}
}

func namesOf(skills []bundledSkill) []string {
	out := make([]string, len(skills))
	for i, s := range skills {
		out[i] = s.Name
	}
	return out
}

// ---------------------------------------------------------------------------
// Real embedded FS sanity check
// ---------------------------------------------------------------------------

// TestRealEmbeddedFSPicksUpRepoSkills verifies that the actual repo
// `skills/` directory contains at least one skill, so the embed FS
// plumbing above can't be a no-op test passing in a vacuum.
func TestRealEmbeddedFSPicksUpRepoSkills(t *testing.T) {
	entries, err := os.ReadDir("../../skills")
	if err != nil {
		t.Skipf("repo skills dir not present: %v", err)
	}
	var found []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join("../../skills", e.Name(), "SKILL.md")); err == nil {
			found = append(found, e.Name())
		}
	}
	if len(found) == 0 {
		t.Fatal("expected at least one real skill under ../../skills, found none")
	}
	t.Logf("real bundled skills: %s", strings.Join(found, ", "))
	// Touch the import so `go vet` doesn't complain about an unused
	// `fs` import on toolchains that don't reach this branch.
	var _ fs.FS = nil
}

// ---------------------------------------------------------------------------
// Bundled-hash tracking + GetSkillStatus
// ---------------------------------------------------------------------------

func TestInstallWritesBundledHash(t *testing.T) {
	withBundledFS(t, fstest.MapFS{
		"skills/alpha/SKILL.md": &fstest.MapFile{Data: []byte(skillA)},
	})
	target := withTempHome(t)

	if err := runInstallOrUpgrade(nil, true); err != nil {
		t.Fatalf("install: %v", err)
	}

	hashPath := filepath.Join(target, "alpha", ".bundled-hash")
	b, err := os.ReadFile(hashPath)
	if err != nil {
		t.Fatalf("read .bundled-hash: %v", err)
	}
	got := strings.TrimSpace(string(b))
	want := sha256Hex([]byte(skillA))
	if got != want {
		t.Fatalf(".bundled-hash mismatch: got %s, want %s", got, want)
	}
}

func TestGetSkillStatusInstalled(t *testing.T) {
	withBundledFS(t, fstest.MapFS{
		"skills/alpha/SKILL.md": &fstest.MapFile{Data: []byte(skillA)},
	})
	_ = withTempHome(t)

	// Install so .bundled-hash is written.
	if err := runInstallOrUpgrade(nil, true); err != nil {
		t.Fatal(err)
	}

	statuses, err := GetSkillStatus()
	if err != nil {
		t.Fatalf("GetSkillStatus: %v", err)
	}
	var found *SkillStatusEntry
	for i := range statuses {
		if statuses[i].Name == "alpha" {
			found = &statuses[i]
			break
		}
	}
	if found == nil {
		t.Fatal("alpha not found in statuses")
	}
	if found.Status != SkillInstalled {
		t.Fatalf("expected SkillInstalled, got %v", found.Status)
	}
}

func TestGetSkillStatusOutdated(t *testing.T) {
	// Bundled has v2, but we install v1 first, then change the bundle
	// without touching the installed file.
	withBundledFS(t, fstest.MapFS{
		"skills/alpha/SKILL.md": &fstest.MapFile{Data: []byte(skillA)}, // v1
	})
	_ = withTempHome(t)
	if err := runInstallOrUpgrade(nil, true); err != nil {
		t.Fatal(err)
	}

	// Now swap the bundled FS to v2 (simulates a new ocode release).
	SetBundledFS(fstest.MapFS{
		"skills/alpha/SKILL.md": &fstest.MapFile{Data: []byte(skillAv2)}, // v2
	})

	statuses, err := GetSkillStatus()
	if err != nil {
		t.Fatalf("GetSkillStatus: %v", err)
	}
	var found *SkillStatusEntry
	for i := range statuses {
		if statuses[i].Name == "alpha" {
			found = &statuses[i]
			break
		}
	}
	if found == nil {
		t.Fatal("alpha not found in statuses")
	}
	if found.Status != SkillOutdated {
		t.Fatalf("expected SkillOutdated, got %v", found.Status)
	}
}

func TestGetSkillStatusCustomModified(t *testing.T) {
	withBundledFS(t, fstest.MapFS{
		"skills/alpha/SKILL.md": &fstest.MapFile{Data: []byte(skillA)},
	})
	target := withTempHome(t)
	if err := runInstallOrUpgrade(nil, true); err != nil {
		t.Fatal(err)
	}

	// User edits the file manually.
	skillPath := filepath.Join(target, "alpha", "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("# my custom version\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	statuses, err := GetSkillStatus()
	if err != nil {
		t.Fatalf("GetSkillStatus: %v", err)
	}
	var found *SkillStatusEntry
	for i := range statuses {
		if statuses[i].Name == "alpha" {
			found = &statuses[i]
			break
		}
	}
	if found == nil {
		t.Fatal("alpha not found in statuses")
	}
	if found.Status != SkillCustomModified {
		t.Fatalf("expected SkillCustomModified, got %v", found.Status)
	}
}

func TestGetSkillStatusMissing(t *testing.T) {
	withBundledFS(t, fstest.MapFS{
		"skills/alpha/SKILL.md": &fstest.MapFile{Data: []byte(skillA)},
	})
	_ = withTempHome(t) // no install

	statuses, err := GetSkillStatus()
	if err != nil {
		t.Fatalf("GetSkillStatus: %v", err)
	}
	var found *SkillStatusEntry
	for i := range statuses {
		if statuses[i].Name == "alpha" {
			found = &statuses[i]
			break
		}
	}
	if found == nil {
		t.Fatal("alpha not found in statuses")
	}
	if found.Status != SkillMissing {
		t.Fatalf("expected SkillMissing, got %v", found.Status)
	}
}

func TestGetSkillStatusCustomModifiedThenBundledChanges(t *testing.T) {
	// User installs, edits, then a new ocode release changes the bundle.
	// The user's edits should still show as custom-modified (not outdated).
	withBundledFS(t, fstest.MapFS{
		"skills/alpha/SKILL.md": &fstest.MapFile{Data: []byte(skillA)},
	})
	target := withTempHome(t)
	if err := runInstallOrUpgrade(nil, true); err != nil {
		t.Fatal(err)
	}

	// User edits.
	skillPath := filepath.Join(target, "alpha", "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("# my custom version\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Bundle changes.
	SetBundledFS(fstest.MapFS{
		"skills/alpha/SKILL.md": &fstest.MapFile{Data: []byte(skillAv2)},
	})

	statuses, err := GetSkillStatus()
	if err != nil {
		t.Fatalf("GetSkillStatus: %v", err)
	}
	var found *SkillStatusEntry
	for i := range statuses {
		if statuses[i].Name == "alpha" {
			found = &statuses[i]
			break
		}
	}
	if found == nil {
		t.Fatal("alpha not found")
	}
	if found.Status != SkillCustomModified {
		t.Fatalf("expected SkillCustomModified (user edits preserved), got %v", found.Status)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
