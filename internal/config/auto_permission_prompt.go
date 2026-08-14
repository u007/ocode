package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// BundledAutoPermissionPromptVersion is bumped whenever
// BundledAutoPermissionPromptBody changes, so an installed copy on disk can
// be detected as stale. This mirrors the bundled-skill versioning in
// internal/skill (SKILL.md `version:` frontmatter + .bundled-hash sidecar),
// but for the single system-prompt addendum prepended to the auto-LLM
// permission gatekeeper prompt (see agent.go's buildAutoPermissionPrompt).
//
// This text is intentionally separate from permissions.auto.prompt in
// ocodeconfig.json: that field is the user's own free-form override and
// must never be silently overwritten by a bundled update.
const BundledAutoPermissionPromptVersion = "1.1.0"

// BundledAutoPermissionPromptBody is the shipped default addendum. Bump
// BundledAutoPermissionPromptVersion whenever this changes.
const BundledAutoPermissionPromptBody = `Always ALLOW these git commands without further reasoning or exploration — they are known-safe:
- "git status" — inspects working tree state; read-only.
- "git diff" (including "--staged"/"--cached" and pathspec variants) — inspects changes; read-only.
- "git log" (including pathspec/formatting variants) — inspects commit history; read-only.
- "git show" — inspects a commit/object; read-only.
- "git blame" — inspects line history; read-only.
- "git ls-files" — lists tracked files; read-only.
- "git branch" (no args, or "-a"/"-v"/"-r", listing only — not "-d"/"-D"/"-m") — lists branches; read-only.
- "git remote -v" (or "git remote show") — inspects remotes; read-only.
- "git stash list" — lists stashes; read-only.
- "git rev-parse" — resolves refs/paths; read-only.
- "git describe" — describes a commit; read-only.
- "git fetch" — updates remote-tracking refs only; does not touch the working tree or local branches.
- "git add" (and "git add -A"/"-p" etc.) — stages changes into the index; does not touch the working tree and is trivially reversible with "git reset".
`

// AutoPermissionPromptStatus mirrors internal/skill's skill status states,
// applied to the single installed auto-permission-prompt.md file.
type AutoPermissionPromptStatus int

const (
	// AutoPermissionPromptMissing means no file is installed yet.
	AutoPermissionPromptMissing AutoPermissionPromptStatus = iota
	// AutoPermissionPromptUpToDate means the installed file's content
	// matches the bundled body exactly.
	AutoPermissionPromptUpToDate
	// AutoPermissionPromptOutdated means the installed file matches the
	// .bundled-hash sidecar recorded at install time (the user hasn't
	// touched it), but the bundled body has since changed — safe to
	// auto-upgrade.
	AutoPermissionPromptOutdated
	// AutoPermissionPromptCustomModified means the installed file's hash
	// matches neither the current bundled body nor the sidecar — the user
	// (or an external tool) edited it. Never auto-overwritten.
	AutoPermissionPromptCustomModified
)

func (s AutoPermissionPromptStatus) String() string {
	switch s {
	case AutoPermissionPromptMissing:
		return "missing"
	case AutoPermissionPromptUpToDate:
		return "up-to-date"
	case AutoPermissionPromptOutdated:
		return "outdated"
	case AutoPermissionPromptCustomModified:
		return "custom-modified"
	default:
		return "unknown"
	}
}

// AutoPermissionPromptFilePath returns the path to the installed
// auto-permission-prompt.md override, alongside ocodeconfig.json.
func AutoPermissionPromptFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	dir := filepath.Join(home, ".config", "opencode")
	if runtime.GOOS == "windows" {
		dir = filepath.Join(os.Getenv("APPDATA"), "opencode")
	}
	return filepath.Join(dir, "auto-permission-prompt.md"), nil
}

func autoPermissionPromptSidecarPath(installedPath string) string {
	return installedPath + ".bundled-hash"
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// GetAutoPermissionPromptStatus reports whether the installed prompt file
// is missing, up-to-date, outdated, or custom-modified relative to the
// bundled body.
func GetAutoPermissionPromptStatus() (AutoPermissionPromptStatus, error) {
	path, err := AutoPermissionPromptFilePath()
	if err != nil {
		return AutoPermissionPromptMissing, err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return AutoPermissionPromptMissing, nil
		}
		return AutoPermissionPromptMissing, err
	}
	bundledHash := sha256Hex([]byte(BundledAutoPermissionPromptBody))
	installedHash := sha256Hex(body)
	if installedHash == bundledHash {
		return AutoPermissionPromptUpToDate, nil
	}
	if sidecar, serr := os.ReadFile(autoPermissionPromptSidecarPath(path)); serr == nil {
		if strings.TrimSpace(string(sidecar)) == installedHash {
			return AutoPermissionPromptOutdated, nil
		}
	}
	return AutoPermissionPromptCustomModified, nil
}

// InstallAutoPermissionPrompt writes the bundled prompt body to disk.
//   - AutoPermissionPromptMissing: always installs.
//   - AutoPermissionPromptUpToDate: no-op unless force.
//   - AutoPermissionPromptOutdated: upgrades (this is the safe, unattended case).
//   - AutoPermissionPromptCustomModified: refuses unless force (the user
//     edited the file; overwriting would silently discard that).
//
// Returns "installed", "updated", or "up-to-date".
func InstallAutoPermissionPrompt(force bool) (string, error) {
	path, err := AutoPermissionPromptFilePath()
	if err != nil {
		return "", err
	}
	status, err := GetAutoPermissionPromptStatus()
	if err != nil {
		return "", err
	}
	switch status {
	case AutoPermissionPromptUpToDate:
		if !force {
			return "up-to-date", nil
		}
	case AutoPermissionPromptCustomModified:
		if !force {
			return "", fmt.Errorf("installed auto-permission prompt at %s has been customized; re-run with force to overwrite", path)
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if existing, rerr := os.ReadFile(path); rerr == nil && len(existing) > 0 {
		if err := backupAutoPermissionPromptFile(path); err != nil {
			return "", fmt.Errorf("backup existing %s: %w", path, err)
		}
	}
	if err := writeAutoPermissionPromptFileAtomic(path, []byte(BundledAutoPermissionPromptBody)); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.WriteFile(autoPermissionPromptSidecarPath(path), []byte(sha256Hex([]byte(BundledAutoPermissionPromptBody))), 0o644); err != nil {
		return "", fmt.Errorf("write bundled-hash sidecar: %w", err)
	}

	if status == AutoPermissionPromptMissing {
		return "installed", nil
	}
	return "updated", nil
}

// LoadAutoPermissionPromptBody returns the installed prompt body, or ""
// (with a nil error) if nothing is installed yet.
func LoadAutoPermissionPromptBody() (string, error) {
	path, err := AutoPermissionPromptFilePath()
	if err != nil {
		return "", err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return string(body), nil
}

func backupAutoPermissionPromptFile(src string) error {
	dir := filepath.Dir(src)
	base := filepath.Base(src)
	ts := time.Now().UTC().Format("20060102T150405Z")
	dst := filepath.Join(dir, base+".bak."+ts)
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	body, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, body, 0o644); err != nil {
		return err
	}
	return os.Remove(src)
}

func writeAutoPermissionPromptFileAtomic(dst string, data []byte) error {
	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, ".auto-permission-prompt.md.tmp.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return err
	}
	tmpName = ""
	return nil
}
