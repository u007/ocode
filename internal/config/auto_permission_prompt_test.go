package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupAutoPermPromptHome points HOME at a fresh temp dir so
// AutoPermissionPromptFilePath resolves inside it.
func setupAutoPermPromptHome(t *testing.T) string {
	t.Helper()
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	return tmpHome
}

func TestAutoPermissionPromptLifecycle(t *testing.T) {
	setupAutoPermPromptHome(t)
	path, err := AutoPermissionPromptFilePath()
	if err != nil {
		t.Fatalf("AutoPermissionPromptFilePath: %v", err)
	}

	// Fresh HOME → missing.
	status, err := GetAutoPermissionPromptStatus()
	if err != nil {
		t.Fatalf("status on missing file: %v", err)
	}
	if status != AutoPermissionPromptMissing {
		t.Fatalf("expected missing, got %s", status)
	}

	// Install → up-to-date; body must be the bundled one.
	action, err := InstallAutoPermissionPrompt(false)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if action != "installed" {
		t.Fatalf("expected installed, got %q", action)
	}
	status, err = GetAutoPermissionPromptStatus()
	if err != nil {
		t.Fatalf("status after install: %v", err)
	}
	if status != AutoPermissionPromptUpToDate {
		t.Fatalf("expected up-to-date, got %s", status)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read installed body: %v", err)
	}
	if string(body) != BundledAutoPermissionPromptBody {
		t.Fatalf("installed body != bundled body")
	}
	// Sidecar must exist and hold the bundled hash.
	if _, err := os.Stat(autoPermissionPromptSidecarPath(path)); err != nil {
		t.Fatalf("sidecar missing after install: %v", err)
	}

	// Re-install (up-to-date) → no-op.
	action, err = InstallAutoPermissionPrompt(false)
	if err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	if action != "up-to-date" {
		t.Fatalf("expected up-to-date, got %q", action)
	}

	// Custom modification (no sidecar match) → refused without force.
	modified := "user's custom prompt"
	if err := os.WriteFile(path, []byte(modified), 0o644); err != nil {
		t.Fatalf("write custom body: %v", err)
	}
	status, err = GetAutoPermissionPromptStatus()
	if err != nil {
		t.Fatalf("status on custom file: %v", err)
	}
	if status != AutoPermissionPromptCustomModified {
		t.Fatalf("expected custom-modified, got %s", status)
	}
	if _, err := InstallAutoPermissionPrompt(false); err == nil {
		t.Fatal("expected error installing over custom-modified without force")
	}
	// Body must be untouched.
	if got, _ := os.ReadFile(path); string(got) != modified {
		t.Fatalf("custom body was overwritten without force: %q", got)
	}

	// Force overwrites and backs up the custom file.
	action, err = InstallAutoPermissionPrompt(true)
	if err != nil {
		t.Fatalf("force install: %v", err)
	}
	if action != "updated" {
		t.Fatalf("expected updated, got %q", action)
	}
	if got, _ := os.ReadFile(path); string(got) != BundledAutoPermissionPromptBody {
		t.Fatalf("force install did not restore bundled body")
	}
	backups, err := filepath.Glob(filepath.Join(filepath.Dir(path), "auto-permission-prompt.md.bak.*"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("expected one backup file, got %v (err %v)", backups, err)
	}
	if got, _ := os.ReadFile(backups[0]); string(got) != modified {
		t.Fatalf("backup does not hold the custom body: %q", got)
	}
	status, _ = GetAutoPermissionPromptStatus()
	if status != AutoPermissionPromptUpToDate {
		t.Fatalf("expected up-to-date after force install, got %s", status)
	}
}

func TestAutoPermissionPromptOutdatedUpgrade(t *testing.T) {
	setupAutoPermPromptHome(t)
	path, err := AutoPermissionPromptFilePath()
	if err != nil {
		t.Fatalf("AutoPermissionPromptFilePath: %v", err)
	}

	// Install, then simulate a bundled upgrade: stale body + old sidecar.
	if _, err := InstallAutoPermissionPrompt(false); err != nil {
		t.Fatalf("install: %v", err)
	}
	staleBody := strings.Replace(BundledAutoPermissionPromptBody, "git status", "git st", 1)
	if err := os.WriteFile(path, []byte(staleBody), 0o644); err != nil {
		t.Fatalf("write stale body: %v", err)
	}
	// Sidecar must record the installed (stale) body's hash — an "outdated"
	// install is one where the file matches what was installed (sidecar) but
	// the bundled body has since changed. Writing the sidecar to match the
	// stale body simulates that.
	if err := os.WriteFile(autoPermissionPromptSidecarPath(path), []byte(sha256Hex([]byte(staleBody))), 0o644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
	status, err := GetAutoPermissionPromptStatus()
	if err != nil {
		t.Fatalf("status on outdated file: %v", err)
	}
	if status != AutoPermissionPromptOutdated {
		t.Fatalf("expected outdated, got %s", status)
	}

	// Untouched outdated copy upgrades without force.
	action, err := InstallAutoPermissionPrompt(false)
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if action != "updated" {
		t.Fatalf("expected updated, got %q", action)
	}
	if got, _ := os.ReadFile(path); string(got) != BundledAutoPermissionPromptBody {
		t.Fatalf("upgrade did not restore bundled body")
	}
}

func TestLoadAutoPermissionPromptBody(t *testing.T) {
	setupAutoPermPromptHome(t)

	// Missing → self-installs and returns the bundled body; a missing file
	// must never silently drop the bundled rules from the gatekeeper prompt.
	body, err := LoadAutoPermissionPromptBody()
	if err != nil {
		t.Fatalf("load missing: %v", err)
	}
	if body != BundledAutoPermissionPromptBody {
		t.Fatalf("expected bundled body after self-install, got %q", body)
	}
	if status, _ := GetAutoPermissionPromptStatus(); status != AutoPermissionPromptUpToDate {
		t.Fatalf("expected up-to-date after self-install, got %s", status)
	}

	// Installed → bundled body.
	if _, err := InstallAutoPermissionPrompt(false); err != nil {
		t.Fatalf("install: %v", err)
	}
	body, err = LoadAutoPermissionPromptBody()
	if err != nil {
		t.Fatalf("load installed: %v", err)
	}
	if body != BundledAutoPermissionPromptBody {
		t.Fatalf("expected bundled body")
	}
}

func TestAutoPermissionPromptStatusString(t *testing.T) {
	cases := map[AutoPermissionPromptStatus]string{
		AutoPermissionPromptMissing:        "missing",
		AutoPermissionPromptUpToDate:       "up-to-date",
		AutoPermissionPromptOutdated:       "outdated",
		AutoPermissionPromptCustomModified: "custom-modified",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("status %d: expected %q, got %q", int(s), want, got)
		}
	}
}

func TestPruneAutoPermissionPromptBackups(t *testing.T) {
	dir := t.TempDir()
	const base = "auto-permission-prompt.md"
	// Create more backups than the retention cap, with sortable timestamps.
	stamps := []string{
		"20260101T000000Z", "20260102T000000Z", "20260103T000000Z",
		"20260104T000000Z", "20260105T000000Z", "20260106T000000Z",
		"20260107T000000Z", "20260108T000000Z",
	}
	for _, ts := range stamps {
		p := filepath.Join(dir, base+".bak."+ts)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	pruneAutoPermissionPromptBackups(dir, base)

	left, err := filepath.Glob(filepath.Join(dir, base+".bak.*"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(left) != maxAutoPermissionPromptBackups {
		t.Fatalf("got %d backups after prune, want %d: %v", len(left), maxAutoPermissionPromptBackups, left)
	}
	// The survivors must be the NEWEST ones.
	for _, p := range left {
		if strings.HasSuffix(p, "20260101T000000Z") || strings.HasSuffix(p, "20260102T000000Z") || strings.HasSuffix(p, "20260103T000000Z") {
			t.Fatalf("old backup survived prune: %s", p)
		}
	}

	// Under-cap dir is untouched.
	pruneAutoPermissionPromptBackups(dir, base)
	left2, _ := filepath.Glob(filepath.Join(dir, base+".bak.*"))
	if len(left2) != maxAutoPermissionPromptBackups {
		t.Fatalf("second prune changed count: %d", len(left2))
	}
}

func TestBackupAutoPermissionPromptFilePrunes(t *testing.T) {
	setupAutoPermPromptHome(t)
	path, err := AutoPermissionPromptFilePath()
	if err != nil {
		t.Fatalf("AutoPermissionPromptFilePath: %v", err)
	}
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Pre-seed cap-many old backups; a new backup should push one out.
	for _, ts := range []string{"20260101T000000Z", "20260102T000000Z", "20260103T000000Z", "20260104T000000Z", "20260105T000000Z"} {
		if err := os.WriteFile(filepath.Join(dir, base+".bak."+ts), []byte("old"), 0o644); err != nil {
			t.Fatalf("seed backup: %v", err)
		}
	}
	if err := os.WriteFile(path, []byte("current"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	if err := backupAutoPermissionPromptFile(path); err != nil {
		t.Fatalf("backup: %v", err)
	}

	left, _ := filepath.Glob(filepath.Join(dir, base+".bak.*"))
	if len(left) != maxAutoPermissionPromptBackups {
		t.Fatalf("got %d backups after backup+prune, want %d: %v", len(left), maxAutoPermissionPromptBackups, left)
	}
	for _, p := range left {
		if strings.HasSuffix(p, "20260101T000000Z") {
			t.Fatalf("oldest backup should have been pruned: %v", left)
		}
	}
}

func TestAutoPermissionPromptNewerNeverDowngraded(t *testing.T) {
	setupAutoPermPromptHome(t)
	path, err := AutoPermissionPromptFilePath()
	if err != nil {
		t.Fatalf("AutoPermissionPromptFilePath: %v", err)
	}
	// Simulate a newer build having installed a different body: sidecar hash
	// matches the file, sidecar version is above ours.
	newerBody := BundledAutoPermissionPromptBody + "\nAlways ALLOW \"future-cmd\".\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(newerBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(autoPermissionPromptSidecarPath(path), sidecarBody(sha256Hex([]byte(newerBody)), "999.0.0"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, err := GetAutoPermissionPromptStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status != AutoPermissionPromptNewer {
		t.Fatalf("expected newer, got %s", status)
	}
	if action, err := InstallAutoPermissionPrompt(false); err != nil || action != "newer" {
		t.Fatalf("install without force: action=%q err=%v", action, err)
	}
	body, err := LoadAutoPermissionPromptBody()
	if err != nil {
		t.Fatal(err)
	}
	if body != newerBody {
		t.Fatalf("load downgraded a newer install")
	}
	if action, err := InstallAutoPermissionPrompt(true); err != nil || action != "updated" {
		t.Fatalf("forced install: action=%q err=%v", action, err)
	}
	if got, _ := os.ReadFile(path); string(got) != BundledAutoPermissionPromptBody {
		t.Fatalf("forced install did not write bundled body")
	}
	if _, version, ok := readSidecar(autoPermissionPromptSidecarPath(path)); !ok || version != BundledAutoPermissionPromptVersion {
		t.Fatalf("sidecar version = %q ok=%v, want %q", version, ok, BundledAutoPermissionPromptVersion)
	}
}

func TestAutoPermissionPromptLegacySidecarIsOutdated(t *testing.T) {
	setupAutoPermPromptHome(t)
	path, err := AutoPermissionPromptFilePath()
	if err != nil {
		t.Fatal(err)
	}
	staleBody := strings.Replace(BundledAutoPermissionPromptBody, "git status", "git st", 1)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(staleBody), 0o644); err != nil {
		t.Fatal(err)
	}
	// Pre-version sidecar: hash only, no trailing version line.
	if err := os.WriteFile(autoPermissionPromptSidecarPath(path), []byte(sha256Hex([]byte(staleBody))), 0o644); err != nil {
		t.Fatal(err)
	}
	if status, _ := GetAutoPermissionPromptStatus(); status != AutoPermissionPromptOutdated {
		t.Fatalf("expected outdated for legacy sidecar, got %s", status)
	}
}

func TestBackupAutoPermissionPromptFileKeepsSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "auto-permission-prompt.md")
	if err := os.WriteFile(src, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := backupAutoPermissionPromptFile(src); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("backup removed the live file: %v", err)
	}
	baks, _ := filepath.Glob(filepath.Join(dir, "auto-permission-prompt.md.bak.*"))
	if len(baks) != 1 {
		t.Fatalf("expected 1 backup, got %d", len(baks))
	}
}

func TestCompareVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{{"1.8.0", "1.8.0", 0}, {"1.9.0", "1.8.0", 1}, {"1.8", "1.8.1", -1}, {"0.0.0", "1.0.0", -1}, {"2.0.0", "1.99.99", 1}}
	for _, c := range cases {
		if got := compareVersion(c.a, c.b); got != c.want {
			t.Errorf("compareVersion(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}
