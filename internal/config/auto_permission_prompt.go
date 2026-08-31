package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
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
const BundledAutoPermissionPromptVersion = "1.8.0"

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
- "git help", "git version", "git var" — help/version introspection; read-only.
- Commands using "git -c <key>=<value>" (including repeated "-c" and mixed with "-C <path>", "--no-pager", "--git-dir", "--work-tree") are NOT auto-allowed by the code allowlist — you must evaluate them. If the underlying subcommand is read-only (e.g. "git status", "git log", "git diff" with a safe key like "core.quotepath"), the wrapper does not make it mutating and it is safe to ALLOW. Do NOT ALLOW if the -c key is security-sensitive — "protocol.*.allow" (enables ext::), "core.sshCommand", "core.pager", "core.editor", "core.hooksPath", "filter.*", "url.*.insteadOf", "credential.helper" — even when the underlying subcommand is read-only; those require human approval. Repeated "-c" is handled the same way.
- All other git subcommands that are primarily read-only (e.g. "git ls-tree", "git ls-remote", "git reflog", "git shortlog", "git cat-file", "git check-ignore", "git grep", "git name-rev", "git for-each-ref", "git rev-list") are also safe to ALLOW when they do not carry destructive flags. Do not auto-allow mutating forms like "git branch -D", "git tag -d", "git remote remove", "git config --unset", "git worktree remove", or "git notes add".
- "curl"/"wget"/"http"/"https" targeting localhost, 127.0.0.0/8, or ::1 (any port/path, including with auth headers or request bodies) — loopback-only traffic stays on-host and cannot exfiltrate data off-machine.

Always ALLOW only these package-manager inspection commands:
- "npm --version", "npm version" (without a package argument), "npm ls", "npm list", "npm outdated", and "npm audit" — read-only dependency inspection.
- "pnpm --version", "pnpm list", and "pnpm outdated" — read-only dependency inspection.
- "bun --version" and "bun pm ls" — read-only runtime/dependency inspection.

All package-manager commands that install, remove, link, execute scripts or
binaries, run lifecycle hooks, modify package metadata, access registries, or
publish artifacts require human approval. This includes npm/pnpm/bun install,
ci, run, test, exec, dlx, add, remove, link, version with a package argument,
publish, pack, store, owner, and access commands.

Always ALLOW reading and manipulation of any OS temporary directory, including /tmp, /var/tmp, $TMPDIR, $TMP, and the platform-specific os.TempDir() (and any path beneath them). This covers listing, creating, reading, writing, modifying, moving, copying, and deleting files/directories under temp. This exception applies only to temporary directories; it does not grant unrestricted access to the rest of the filesystem or to the network.
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
	dir, err := GlobalConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve config directory: %w", err)
	}
	return filepath.Join(dir, "auto-permission-prompt.md"), nil
}

// CustomAutoPermissionPromptFilePath returns the path to the user's own
// free-form addendum, appended after the bundled prompt. This file is never
// managed, versioned, or overwritten by InstallAutoPermissionPrompt — it
// exists so people can extend the auto-permission gatekeeper without editing
// the bundled file, which would otherwise mark it CustomModified and block
// future auto-upgrades of the bundled content (see LoadAutoPermissionPromptBody).
func CustomAutoPermissionPromptFilePath() (string, error) {
	dir, err := GlobalConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve config directory: %w", err)
	}
	return filepath.Join(dir, "auto-permission-prompt.local.md"), nil
}

// customAutoPermissionPromptTemplate seeds a freshly created local addendum
// file with an explanatory comment, so opening it for the first time doesn't
// show a blank file with no indication of what it's for.
const customAutoPermissionPromptTemplate = `<!--
Your own additions to the auto-permission gatekeeper prompt. This text is
appended after the bundled addendum (auto-permission-prompt.md) and before
the per-request prompt. Edit freely — unlike the bundled file, this one is
never touched by "/permissions auto prompt install|upgrade".
-->
`

// EnsureCustomAutoPermissionPromptFile creates the custom addendum file,
// seeded with an explanatory template, if it doesn't already exist. It never
// overwrites an existing file.
func EnsureCustomAutoPermissionPromptFile() error {
	path, err := CustomAutoPermissionPromptFilePath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	return writeAutoPermissionPromptFileAtomic(path, []byte(customAutoPermissionPromptTemplate))
}

// LoadCustomAutoPermissionPromptBody returns the user's own addendum body,
// or "" (with a nil error) if no local addendum file exists yet.
func LoadCustomAutoPermissionPromptBody() (string, error) {
	path, err := CustomAutoPermissionPromptFilePath()
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
// (with a nil error) if nothing is installed yet. An installed file that is
// Outdated (unmodified since install, but the bundled body has since
// changed) is silently auto-upgraded first — this is the same "safe,
// unattended" case InstallAutoPermissionPrompt documents, so a stale
// addendum self-heals the next time it's loaded instead of requiring the
// user to run `/permissions auto prompt upgrade` by hand. A
// CustomModified file is left untouched, exactly as a manual upgrade
// without force would leave it.
func LoadAutoPermissionPromptBody() (string, error) {
	if status, err := GetAutoPermissionPromptStatus(); err == nil && status == AutoPermissionPromptOutdated {
		if _, ierr := InstallAutoPermissionPrompt(false); ierr != nil {
			return "", fmt.Errorf("auto-upgrade outdated auto-permission prompt: %w", ierr)
		}
	}

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

// maxAutoPermissionPromptBackups caps how many timestamped .bak copies of
// the auto-permission prompt are retained; older ones are pruned on every
// new backup so repeated installs/upgrades don't accumulate files forever.
const maxAutoPermissionPromptBackups = 5

func backupAutoPermissionPromptFile(src string) error {
	dir := filepath.Dir(src)
	base := filepath.Base(src)
	ts := time.Now().UTC().Format("20060102T150405Z")
	dst := filepath.Join(dir, base+".bak."+ts)
	defer pruneAutoPermissionPromptBackups(dir, base)
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

// pruneAutoPermissionPromptBackups deletes all but the newest
// maxAutoPermissionPromptBackups "<base>.bak.<ts>" files in dir. The UTC
// timestamp suffix sorts lexicographically, so name order is age order.
func pruneAutoPermissionPromptBackups(dir, base string) {
	matches, err := filepath.Glob(filepath.Join(dir, base+".bak.*"))
	if err != nil {
		// intentionally not logged: only fails on a malformed pattern,
		// which a fixed literal base cannot produce.
		return
	}
	if len(matches) <= maxAutoPermissionPromptBackups {
		return
	}
	sort.Strings(matches)
	for _, old := range matches[:len(matches)-maxAutoPermissionPromptBackups] {
		if err := os.Remove(old); err != nil {
			log.Printf("config: prune auto-permission prompt backup %s: %v", old, err)
		}
	}
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
