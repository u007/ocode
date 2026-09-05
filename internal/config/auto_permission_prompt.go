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
	"strconv"
	"strings"
	"time"

	"github.com/u007/ocode/internal/filelock"
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
const BundledAutoPermissionPromptVersion = "1.9.2"

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
- "git branch --show-current", "git branch --list", "git branch --contains"/"--merged"/"--no-merged" — listing/query forms; read-only.
- "git tag" (no args, "-l"/"--list", "-n", "--contains", "--points-at") — lists tags; read-only. Not "-a"/"-d"/"-f"/"--delete" or a bare tag name argument, which create or delete tags.
- "git worktree list" — lists worktrees; read-only.
- "git stash show" — inspects a stash entry; read-only.
- "git config --get <key>", "git config --get-all", "git config --get-regexp", "git config --get-urlmatch", "git config --get-color", "git config --get-colorbool", "git config <key>" (exactly one positional argument — implicit read, dotted keys included) — reads config; read-only. Forms that write are mutating and require human approval: "--set", "--unset"/"--unset-all", "--add", "--replace-all", "--remove-section", "--rename-section", "--edit", any "key=value" form, and the bare two-argument form "git config <key> <value>" (with or without "--global"/"--local"/"--system"/"--file") — e.g. "git config user.name attacker" or "git config --global core.hooksPath /tmp/x" writes config even though it carries no "--set" flag; the absence of a write flag does not make it a read. Unlisted subcommand forms are intentionally NOT auto-allowed — evaluate them and require human approval unless you can establish they only read. Writing any of the security-sensitive keys the "-c" rule blocks ("protocol.*.allow", "core.sshCommand", "core.pager", "core.editor", "core.hooksPath", "filter.*", "url.*.insteadOf", "credential.*", "pager.*", "gpg.program") via "git config" is exactly as dangerous as via "-c" and always requires human approval.
- "git submodule status" — inspects submodules; read-only.
- "git count-objects", "git fsck" (integrity inspection; NOT "--lost-found", which writes dangling objects into .git/lost-found) — repository inspection; read-only.
- "git verify-commit" and "git verify-tag" are NOT auto-allowed: they execute the configured gpg program ("gpg.program" in git config, which a repo can point anywhere) — require human approval unless you can establish gpg.program is the standard gpg and the signature check is expected.
- Commands using "git -c <key>=<value>" (including repeated "-c" and mixed with "-C <path>", "--no-pager", "--git-dir", "--work-tree") are NOT auto-allowed by the code allowlist — you must evaluate them. If the underlying subcommand is read-only (e.g. "git status", "git log", "git diff" with a safe key like "core.quotepath"), the wrapper does not make it mutating and it is safe to ALLOW. Do NOT ALLOW if the -c key is security-sensitive — "protocol.*.allow" (enables ext::), "core.sshCommand", "core.pager", "core.editor", "core.hooksPath", "filter.*", "url.*.insteadOf", "credential.*" (including URL-scoped forms like "credential.<url>.helper"), "pager.*" (a pager is an executed command), "gpg.program" (executed by verify-commit/verify-tag) — even when the underlying subcommand is read-only; those require human approval. Repeated "-c" is handled the same way.
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

Executing project and toolchain dependency binaries directly is normal, everyday tooling — the binary's location alone is never a reason to deny or "auto-deny" an unrecognized interpreter path. Judge the ACTION the command performs:
- Node/JS: anything under node_modules/.bin or node_modules/**/bin (e.g. node_modules/.bin/tsc, node_modules/.bin/eslint, node_modules/.bin/vite, node_modules/.bin/vitest) invoked directly.
- Python: .venv/bin, venv/bin, env/bin (e.g. .venv/bin/python, .venv/bin/pip, .venv/bin/pytest, .venv/bin/ruff) and pyenv shims.
- Go: "go run"/"go tool", go-installed tools on GOPATH/bin (e.g. ~/go/bin), and project-local bin dirs populated by build tooling (e.g. ./bin/foo, ./scripts/bin/foo).
- Other toolchains: cargo and ~/.cargo/bin, Rust target/debug|release binaries, PHP vendor/bin, Ruby "bundle exec"/gem bins, Java/Kotlin ./gradlew and ./mvnw wrappers, .NET local tools, and any other language's project-local dependency bin directory.
If the action is permissible (build, test, lint, format, codegen, local file processing), ALLOW regardless of the binary's directory. If the action itself is not (writes to sensitive paths such as .git/, data exfiltration, destructive commands), DENY regardless — a dependency-local interpreter does not sanitize the action. If you cannot establish what the command does from its arguments and the tool's known semantics (opaque flags, an unreadable script, an unfamiliar tool), do not guess safety from the binary's path or basename — require human approval. A familiar-looking name (e.g. "test", "build") inside a dependency bin dir proves nothing about what the script actually executes. This covers direct invocation only: package-manager subcommands (npm/pnpm/bun install/run/exec/dlx) still follow the package-manager rules above.

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
	// touched it), but the bundled body has since changed — upgrade only
	// via the explicit `/permissions auto prompt upgrade` command. Loading
	// never rewrites the file, and it does not serve the stale copy either:
	// the current bundled body is returned in its place so the gatekeeper
	// never runs a stale rulebook (see LoadAutoPermissionPromptBodyWithStatus).
	AutoPermissionPromptOutdated
	// AutoPermissionPromptCustomModified means the installed file's hash
	// matches neither the current bundled body nor the sidecar — the user
	// (or an external tool) edited it. Never auto-overwritten.
	AutoPermissionPromptCustomModified
	// AutoPermissionPromptNewer means the installed file was written by a
	// build whose bundled version is newer than this one (sidecar version >
	// BundledAutoPermissionPromptVersion). Never auto-downgraded: two ocode
	// builds sharing one config dir would otherwise reinstall over each
	// other on every permission prompt.
	AutoPermissionPromptNewer
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
	case AutoPermissionPromptNewer:
		return "newer"
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
// future upgrades of the bundled content (see InstallAutoPermissionPrompt).
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

// sidecarBody encodes the sidecar as "<hash>\n<version>\n". Older sidecars
// hold only the hash; readSidecar treats a missing version as "0.0.0".
func sidecarBody(hash, version string) []byte {
	return []byte(hash + "\n" + version + "\n")
}

func readSidecar(path string) (hash, version string, ok bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", "", false
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	hash = strings.TrimSpace(lines[0])
	version = "0.0.0"
	if len(lines) > 1 && strings.TrimSpace(lines[1]) != "" {
		version = strings.TrimSpace(lines[1])
	}
	return hash, version, hash != ""
}

// compareVersion compares dotted numeric versions ("1.8.0"); non-numeric
// segments compare as 0.
func compareVersion(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y int
		if i < len(as) {
			x, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			y, _ = strconv.Atoi(bs[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
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
	if hash, version, ok := readSidecar(autoPermissionPromptSidecarPath(path)); ok && hash == installedHash {
		if compareVersion(version, BundledAutoPermissionPromptVersion) > 0 {
			return AutoPermissionPromptNewer, nil
		}
		return AutoPermissionPromptOutdated, nil
	}
	return AutoPermissionPromptCustomModified, nil
}

// InstallAutoPermissionPrompt writes the bundled prompt body to disk.
//   - AutoPermissionPromptMissing: always installs.
//   - AutoPermissionPromptUpToDate: no-op unless force.
//   - AutoPermissionPromptOutdated: upgrades (this is the safe, unattended case).
//   - AutoPermissionPromptCustomModified: refuses unless force (the user
//     edited the file; overwriting would silently discard that).
//   - AutoPermissionPromptNewer: no-op unless force (never downgrade a file
//     written by a newer build sharing the same config dir).
//
// Returns "installed", "updated", "up-to-date", or "newer".
//
// The whole decide→backup→write→sidecar sequence is serialized under an
// advisory file lock and is now the ONLY writer to the installed file (the
// load path never writes), so the lock spans the status check too: without
// it, two concurrent installs can both read the same pre-write status, act
// on it, and interleave their body/sidecar writes — the last sidecar lands
// describing a body the file no longer holds.
func InstallAutoPermissionPrompt(force bool) (string, error) {
	path, err := AutoPermissionPromptFilePath()
	if err != nil {
		return "", err
	}
	// The lock file lives next to the prompt; the directory must exist before
	// the lock can be created.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	var action string
	if err := filelock.WithFileLock(autoPermissionPromptLockPath(path), func() error {
		a, err := installAutoPermissionPromptLocked(force, path)
		action = a
		return err
	}); err != nil {
		return "", err
	}
	return action, nil
}

// autoPermissionPromptLockPath is the advisory lock serializing
// InstallAutoPermissionPrompt across processes and goroutines (two ocode
// builds sharing one config dir, or the TUI command racing a background
// maintenance writer).
func autoPermissionPromptLockPath(installedPath string) string {
	return installedPath + ".lock"
}

// installAutoPermissionPromptLocked is the serialized install body; it must
// only run under autoPermissionPromptLockPath. The status check happens HERE
// — inside the lock — so the refuse/overwrite decision is made against the
// same state the writes land on, not a pre-lock snapshot.
func installAutoPermissionPromptLocked(force bool, path string) (string, error) {
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
	case AutoPermissionPromptNewer:
		if !force {
			return "newer", nil
		}
	}

	if existing, rerr := os.ReadFile(path); rerr == nil && len(existing) > 0 {
		if err := backupAutoPermissionPromptFile(path); err != nil {
			return "", fmt.Errorf("backup existing %s: %w", path, err)
		}
	}
	if err := writeAutoPermissionPromptFileAtomic(path, []byte(BundledAutoPermissionPromptBody)); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	// Sidecar is replaced atomically too: a plain os.WriteFile here leaves a
	// window where the body is the new bundled text but the sidecar still
	// records the previous hash — a reader in that window misclassifies the
	// fresh install as custom-modified.
	if err := writeAutoPermissionPromptFileAtomic(autoPermissionPromptSidecarPath(path), sidecarBody(sha256Hex([]byte(BundledAutoPermissionPromptBody)), BundledAutoPermissionPromptVersion)); err != nil {
		return "", fmt.Errorf("write bundled-hash sidecar: %w", err)
	}

	if status == AutoPermissionPromptMissing {
		return "installed", nil
	}
	return "updated", nil
}

// LoadAutoPermissionPromptBody returns the prompt body for the gatekeeper:
// the installed auto-permission-prompt.md when one exists on disk (the
// user-managed copy, written only by an explicit
// `/permissions auto prompt install|upgrade`), otherwise the embedded
// BundledAutoPermissionPromptBody. Loading never writes to disk — there is
// no self-heal/auto-install here — so a missing or stale file is never
// silently rewritten during a permission prompt: while the file is absent
// the embedded body takes over; an up-to-date, customized, or newer file is
// used verbatim, and a stale-but-unmodified file is superseded by the
// current embedded rules — still without writing to disk — until an
// explicit upgrade refreshes the copy.
func LoadAutoPermissionPromptBody() (string, error) {
	body, _, err := LoadAutoPermissionPromptBodyWithStatus()
	return body, err
}

// LoadAutoPermissionPromptBodyWithStatus is LoadAutoPermissionPromptBody with
// the installed file's status in the same read: (embedded body, Missing) when
// no file is installed, (current embedded body, Outdated) when the installed
// copy provably predates the bundled body — the sidecar hash matches, so the
// user has not touched it, and serving the shipped rules beats serving a
// stale rulebook (still no disk write: the stale copy stays on disk until an
// explicit /permissions auto prompt upgrade) — and (installed body verbatim,
// its status) for UpToDate, CustomModified, and Newer, since a customized or
// newer-build copy always wins over the bundled text. The status drives the
// gatekeeper advisory (AutoPermissionPromptAdvisory) that replaces the old
// load-time self-heal, so nothing is silently rewritten and no status is
// silently trusted.
func LoadAutoPermissionPromptBodyWithStatus() (string, AutoPermissionPromptStatus, error) {
	path, err := AutoPermissionPromptFilePath()
	if err != nil {
		return "", AutoPermissionPromptMissing, err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return BundledAutoPermissionPromptBody, AutoPermissionPromptMissing, nil
		}
		return "", AutoPermissionPromptMissing, err
	}
	status, err := GetAutoPermissionPromptStatus()
	if err != nil {
		return string(body), AutoPermissionPromptMissing, err
	}
	if status == AutoPermissionPromptOutdated {
		// The sidecar proves the installed copy is exactly what an older
		// bundle wrote (the user has not touched it), so the shipped rules
		// replace the stale copy at load time — without writing to disk.
		// The stale file keeps waiting for an explicit upgrade.
		return BundledAutoPermissionPromptBody, status, nil
	}
	return string(body), status, nil
}

// AutoPermissionPromptAdvisory returns a short note the gatekeeper appends
// after the addendum when the installed prompt file is not the current
// bundled body — the migration surface that replaced load-time self-heal.
// Loading never upgrades the file, so the status is surfaced instead: an
// outdated copy is superseded by the embedded body (the advisory says so and
// names the upgrade), while a customized or newer copy is served verbatim
// with its own guidance. Missing/UpToDate need no note: the first serves the
// embedded body (always current), the second the identical installed body.
func AutoPermissionPromptAdvisory(status AutoPermissionPromptStatus) string {
	switch status {
	case AutoPermissionPromptOutdated:
		return "\n[Note: the installed auto-permission-prompt.md predates bundled v" + BundledAutoPermissionPromptVersion +
			" and is not being served; the current bundled rules above apply. The user can update the installed copy with \"/permissions auto prompt upgrade\".]"
	case AutoPermissionPromptCustomModified:
		return "\n[Note: the installed auto-permission-prompt.md has been customized. Follow it where it speaks; where it is silent, fall back to the shipped defaults and require human approval for anything it does not clearly cover. The user can restore the shipped body with \"/permissions auto prompt install force\".]"
	case AutoPermissionPromptNewer:
		return "\n[Note: the installed auto-permission-prompt.md was written by a newer ocode build; treat absence of this build's newer rules as informational, not as permission to widen allows.]"
	default:
		return ""
	}
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
	// Copy rather than rename: the live file must never be absent between
	// backup and the atomic rewrite that follows, or a concurrent loader in
	// another ocode process sees Missing and the two race on the same path.
	body, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, body, 0o644)
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
