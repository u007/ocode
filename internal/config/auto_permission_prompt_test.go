package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	path, err := AutoPermissionPromptFilePath()
	if err != nil {
		t.Fatalf("AutoPermissionPromptFilePath: %v", err)
	}

	// Missing → returns the embedded bundled body without creating or
	// writing anything to disk: no self-heal/auto-install on load.
	body, err := LoadAutoPermissionPromptBody()
	if err != nil {
		t.Fatalf("load missing: %v", err)
	}
	if body != BundledAutoPermissionPromptBody {
		t.Fatalf("expected embedded bundled fallback, got %q", body)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("expected no file on disk after fallback load, stat err = %v", statErr)
	}
	if _, statErr := os.Stat(path + ".bundled-hash"); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("expected no sidecar on disk after fallback load, stat err = %v", statErr)
	}
	if status, _ := GetAutoPermissionPromptStatus(); status != AutoPermissionPromptMissing {
		t.Fatalf("expected missing status after fallback load, got %s", status)
	}

	// Installed file → returned verbatim, even when it differs from the
	// bundled body (upgrade stays manual; loading never rewrites).
	if _, err := InstallAutoPermissionPrompt(false); err != nil {
		t.Fatalf("install: %v", err)
	}
	custom := "user-managed body — do not silently replace"
	if err := os.WriteFile(path, []byte(custom), 0o644); err != nil {
		t.Fatalf("write custom body: %v", err)
	}
	body, err = LoadAutoPermissionPromptBody()
	if err != nil {
		t.Fatalf("load custom: %v", err)
	}
	if body != custom {
		t.Fatalf("expected installed file verbatim, got %q", body)
	}
	if got, _ := os.ReadFile(path); string(got) != custom {
		t.Fatalf("load rewrote the installed file: %q", got)
	}
}

func TestBundledAutoPermissionPrompt_CoversLanguageToolBins(t *testing.T) {
	for _, want := range []string{
		"node_modules/.bin",
		".venv/bin",
		"venv/bin",
		"env/bin",
		"pyenv",
		"go run",
		"GOPATH/bin",
		"vendor/bin",
		"bundle exec",
		"gradlew",
		"mvnw",
		"~/.cargo/bin",
		"Judge the ACTION",
	} {
		if !strings.Contains(BundledAutoPermissionPromptBody, want) {
			t.Fatalf("bundled auto-permission prompt missing %q", want)
		}
	}
}

// TestBundledAutoPermissionPrompt_GitConfigBoundary pins the judge-facing
// wording for the two decision surfaces the code allowlist deliberately
// defers to it ("git config" is absent from bashSubcommandAllow, so every
// git config form reaches the judge): the bare two-argument write form
// ("git config <key> <value>") must be named as mutating even without a
// write flag, and the allowlist must be stated as conservative for unlisted
// forms. The code-level counterpart lives in
// internal/agent/permission_interpreter_test.go
// (TestMatchSubcommandAllow_GitConfigBoundary).
func TestBundledAutoPermissionPrompt_GitConfigBoundary(t *testing.T) {
	for _, want := range []string{
		// Positional two-argument write form — the hidden writer.
		"two-argument form \"git config <key> <value>\"",
		"git config user.name attacker",
		"git config --global core.hooksPath /tmp/x",
		"the absence of a write flag does not make it a read",
		// Explicit read forms enumerated (not just --get/--list).
		"git config --get-all",
		"git config --get-regexp",
		"--get-urlmatch",
		"--get-color",
		"--get-colorbool",
		// Single positional argument is the implicit read.
		"git config <key>\" (exactly one positional argument",
		// Fail-closed conservatism for unlisted forms.
		"Unlisted subcommand forms are intentionally NOT auto-allowed",
		// Mutating flags incl. the ones the old text missed.
		"--replace-all",
		"--remove-section",
		"--rename-section",
	} {
		if !strings.Contains(BundledAutoPermissionPromptBody, want) {
			t.Fatalf("bundled auto-permission prompt missing %q", want)
		}
	}
	// The old unsafe phrasing must be gone: it covered flag/"key=value" forms
	// but not the positional write.
	if strings.Contains(BundledAutoPermissionPromptBody, "or key=value form is mutating") {
		t.Fatalf("bundled auto-permission prompt still carries the pre-1.9.1 git config wording")
	}
	// Judge-the-action ambiguity pins: opaque commands require approval;
	// familiar names prove nothing.
	for _, want := range []string{
		"do not guess safety from the binary's path or basename",
		"require human approval",
		"proves nothing about what the script actually executes",
	} {
		if !strings.Contains(BundledAutoPermissionPromptBody, want) {
			t.Fatalf("bundled auto-permission prompt missing %q", want)
		}
	}
}

func TestAutoPermissionPromptAdvisory(t *testing.T) {
	// Missing/UpToDate: no note — the first serves the always-current embedded
	// body, the second the identical installed body.
	if got := AutoPermissionPromptAdvisory(AutoPermissionPromptMissing); got != "" {
		t.Errorf("missing status advisory = %q, want empty", got)
	}
	if got := AutoPermissionPromptAdvisory(AutoPermissionPromptUpToDate); got != "" {
		t.Errorf("up-to-date status advisory = %q, want empty", got)
	}

	outdated := AutoPermissionPromptAdvisory(AutoPermissionPromptOutdated)
	for _, want := range []string{"predates bundled v" + BundledAutoPermissionPromptVersion, "/permissions auto prompt upgrade"} {
		if !strings.Contains(outdated, want) {
			t.Errorf("outdated advisory missing %q: %q", want, outdated)
		}
	}

	custom := AutoPermissionPromptAdvisory(AutoPermissionPromptCustomModified)
	for _, want := range []string{"customized", "/permissions auto prompt install force"} {
		if !strings.Contains(custom, want) {
			t.Errorf("custom-modified advisory missing %q: %q", want, custom)
		}
	}

	newer := AutoPermissionPromptAdvisory(AutoPermissionPromptNewer)
	for _, want := range []string{"newer ocode build", "not as permission to widen allows"} {
		if !strings.Contains(newer, want) {
			t.Errorf("newer advisory missing %q: %q", want, newer)
		}
	}
}

// TestLoadAutoPermissionPromptBodyWithStatus covers the four statuses through
// the combined read the gatekeeper prepend uses: missing → embedded body,
// outdated → current embedded body (the stale copy is superseded without a
// disk write), customized / newer sidecar → installed body verbatim, each
// with the corresponding status.
func TestLoadAutoPermissionPromptBodyWithStatus(t *testing.T) {
	setupAutoPermPromptHome(t)
	path, err := AutoPermissionPromptFilePath()
	if err != nil {
		t.Fatalf("AutoPermissionPromptFilePath: %v", err)
	}

	// Missing → embedded body + Missing.
	body, status, err := LoadAutoPermissionPromptBodyWithStatus()
	if err != nil {
		t.Fatalf("load missing: %v", err)
	}
	if body != BundledAutoPermissionPromptBody || status != AutoPermissionPromptMissing {
		t.Fatalf("missing: body-is-bundled=%v status=%s", body == BundledAutoPermissionPromptBody, status)
	}

	// Install v-current, then simulate a stale install: body + sidecar both
	// from an older bundled version.
	if _, err := InstallAutoPermissionPrompt(false); err != nil {
		t.Fatalf("install: %v", err)
	}
	staleBody := strings.Replace(BundledAutoPermissionPromptBody, "git status", "git st", 1)
	if err := os.WriteFile(path, []byte(staleBody), 0o644); err != nil {
		t.Fatalf("write stale body: %v", err)
	}
	if err := os.WriteFile(autoPermissionPromptSidecarPath(path), sidecarBody(sha256Hex([]byte(staleBody)), "1.8.0"), 0o644); err != nil {
		t.Fatalf("write stale sidecar: %v", err)
	}
	body, status, err = LoadAutoPermissionPromptBodyWithStatus()
	if err != nil {
		t.Fatalf("load stale: %v", err)
	}
	if body != BundledAutoPermissionPromptBody {
		t.Fatalf("stale: expected current bundled body (outdated copies are superseded so the gatekeeper never runs a stale rulebook)")
	}
	if status != AutoPermissionPromptOutdated {
		t.Fatalf("stale: expected outdated, got %s", status)
	}
	// Superseding is a pure read: the stale copy and its sidecar stay on
	// disk untouched until an explicit upgrade.
	if got, rerr := os.ReadFile(path); rerr != nil || string(got) != staleBody {
		t.Fatalf("stale: load rewrote the installed file (err=%v)", rerr)
	}
	if hash, version, ok := readSidecar(autoPermissionPromptSidecarPath(path)); !ok || hash != sha256Hex([]byte(staleBody)) || version != "1.8.0" {
		t.Fatalf("stale: load mutated the sidecar (hash=%q version=%q ok=%v)", hash, version, ok)
	}

	// Customized (sidecar matches nothing) → verbatim + CustomModified.
	customBody := "user rules, version unknown"
	if err := os.WriteFile(path, []byte(customBody), 0o644); err != nil {
		t.Fatalf("write custom body: %v", err)
	}
	body, status, err = LoadAutoPermissionPromptBodyWithStatus()
	if err != nil {
		t.Fatalf("load custom: %v", err)
	}
	if body != customBody || status != AutoPermissionPromptCustomModified {
		t.Fatalf("custom: body-verbatim=%v status=%s", body == customBody, status)
	}

	// Newer sidecar → verbatim + Newer.
	if err := os.WriteFile(autoPermissionPromptSidecarPath(path), sidecarBody(sha256Hex([]byte(customBody)), "999.0.0"), 0o644); err != nil {
		t.Fatalf("write newer sidecar: %v", err)
	}
	body, status, err = LoadAutoPermissionPromptBodyWithStatus()
	if err != nil {
		t.Fatalf("load newer: %v", err)
	}
	if body != customBody || status != AutoPermissionPromptNewer {
		t.Fatalf("newer: body-verbatim=%v status=%s", body == customBody, status)
	}
}

// TestInstallAutoPermissionPromptConcurrent exercises the serialized install
// path under -race: concurrent installs over a stale (outdated) copy must all
// resolve through the lock and leave body and sidecar mutually consistent —
// without the lock, two installs can both act on the same pre-write status
// and interleave their body/sidecar writes so the last sidecar describes a
// body the file no longer holds.
func TestInstallAutoPermissionPromptConcurrent(t *testing.T) {
	setupAutoPermPromptHome(t)
	path, err := AutoPermissionPromptFilePath()
	if err != nil {
		t.Fatalf("AutoPermissionPromptFilePath: %v", err)
	}

	// Seed the outdated state the race occurs on.
	if _, err := InstallAutoPermissionPrompt(false); err != nil {
		t.Fatalf("install: %v", err)
	}
	staleBody := strings.Replace(BundledAutoPermissionPromptBody, "git status", "git st", 1)
	if err := os.WriteFile(path, []byte(staleBody), 0o644); err != nil {
		t.Fatalf("write stale body: %v", err)
	}
	if err := os.WriteFile(autoPermissionPromptSidecarPath(path), sidecarBody(sha256Hex([]byte(staleBody)), "1.8.0"), 0o644); err != nil {
		t.Fatalf("write stale sidecar: %v", err)
	}

	const n = 8
	actions := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			actions[i], errs[i] = InstallAutoPermissionPrompt(false)
		}(i)
	}
	wg.Wait()

	upgraded := 0
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent install %d: %v", i, err)
		}
		if actions[i] == "updated" {
			upgraded++
		}
	}
	if upgraded == 0 {
		t.Fatalf("no install upgraded the stale copy: %v", actions)
	}

	// Final state: body is the bundled text and the sidecar describes exactly
	// that body (hash + version). A mismatched sidecar here is the interleaved
	// write the lock exists to prevent.
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != BundledAutoPermissionPromptBody {
		t.Fatalf("body is not the bundled text after concurrent install")
	}
	side, err := os.ReadFile(autoPermissionPromptSidecarPath(path))
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	if want := string(sidecarBody(sha256Hex([]byte(BundledAutoPermissionPromptBody)), BundledAutoPermissionPromptVersion)); string(side) != want {
		t.Fatalf("sidecar does not describe the installed body:\n got %q\nwant %q", side, want)
	}
	if status, _ := GetAutoPermissionPromptStatus(); status != AutoPermissionPromptUpToDate {
		t.Fatalf("expected up-to-date after concurrent installs, got %s", status)
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
