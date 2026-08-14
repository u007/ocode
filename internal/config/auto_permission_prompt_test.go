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

	// Missing → "" with nil error.
	body, err := LoadAutoPermissionPromptBody()
	if err != nil {
		t.Fatalf("load missing: %v", err)
	}
	if body != "" {
		t.Fatalf("expected empty body, got %q", body)
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
