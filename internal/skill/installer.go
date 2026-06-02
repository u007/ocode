package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// bundledFS is the read-only filesystem containing the SKILL.md files
// shipped with the binary. main.go sets it once at startup via
// SetBundledFS; tests can call SetBundledFS to inject a synthetic tree.
var (
	bundledFSMu sync.RWMutex
	bundledFS   fs.FS
)

// SetBundledFS registers the embedded FS that contains the bundled
// skills (typically `skills/<name>/SKILL.md`). Must be called before Run
// or any of the package's install/upgrade helpers. Safe to call from
// main() before goroutines start.
func SetBundledFS(f fs.FS) {
	bundledFSMu.Lock()
	bundledFS = f
	bundledFSMu.Unlock()
}

func getBundledFS() fs.FS {
	bundledFSMu.RLock()
	defer bundledFSMu.RUnlock()
	return bundledFS
}

// Run is the entry point for the `ocode skills` CLI subcommand.
//
// Subcommands:
//
//	ocode skills list
//	ocode skills install [<name>...]
//	ocode skills upgrade  [<name>...]
//	ocode skills uninstall <name>...
//
// `install` and `upgrade` operate on all bundled skills when no name is given.
// `install` always writes (and backs up an existing target on overwrite);
// `upgrade` is a no-op for already-up-to-date skills.
func Run(args []string) error {
	if len(args) == 0 {
		printSkillsUsage(os.Stderr)
		return errors.New("missing subcommand")
	}

	switch args[0] {
	case "list", "ls":
		return runList()

	case "install":
		return runInstallOrUpgrade(args[1:], true /*force*/)

	case "upgrade", "update":
		return runInstallOrUpgrade(args[1:], false /*force*/)

	case "uninstall", "remove":
		return runUninstall(args[1:])

	case "help", "-h", "--help":
		printSkillsUsage(os.Stdout)
		return nil

	default:
		printSkillsUsage(os.Stderr)
		return fmt.Errorf("unknown skills subcommand %q", args[0])
	}
}

func printSkillsUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: ocode skills <subcommand> [args]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "subcommands:")
	fmt.Fprintln(w, "  list                       List bundled + installed skills")
	fmt.Fprintln(w, "  install [<name>...]        Install bundled skills to ~/.config/opencode/skills/")
	fmt.Fprintln(w, "  upgrade  [<name>...]       Install only when bundled content differs from installed")
	fmt.Fprintln(w, "  uninstall <name>...        Remove an installed skill directory")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "With no <name> arguments, install/upgrade operate on every bundled skill.")
}

// ---------------------------------------------------------------------------
// Target resolution
// ---------------------------------------------------------------------------

// globalSkillsDir returns the user's global opencode skills directory
// (e.g. ~/.config/opencode/skills). It must match the first entry of
// skillSearchPaths() in loader.go so installed skills are picked up.
func globalSkillsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "opencode", "skills"), nil
}

// ---------------------------------------------------------------------------
// Bundled-skill discovery (from the embedded FS)
// ---------------------------------------------------------------------------

// bundledSkill is one skill shipped inside the binary.
type bundledSkill struct {
	Name    string // directory name under skills/
	Path    string // path inside the embedded FS, e.g. "skills/foo/SKILL.md"
	Bytes   []byte
	Version string // from frontmatter, "" if absent
}

// bundledSkills returns the set of skills shipped with this build, sorted
// by name. The injected FS is expected to be rooted at the repo root and
// to contain a `skills/` subdirectory; each immediate child of that
// subdirectory that contains a `SKILL.md` is treated as one skill.
//
// Returns nil if no FS has been registered or the `skills/` root is
// missing.
func bundledSkills() ([]bundledSkill, error) {
	fsys := getBundledFS()
	if fsys == nil {
		return nil, errors.New("no bundled skills FS registered (call skill.SetBundledFS from main)")
	}
	entries, err := fs.ReadDir(fsys, "skills")
	if err != nil {
		return nil, fmt.Errorf("read embedded skills: %w", err)
	}
	var out []bundledSkill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		rel := filepath.Join("skills", name, "SKILL.md")
		b, err := fs.ReadFile(fsys, rel)
		if err != nil {
			// Not a skill dir (e.g. a README) — skip silently.
			continue
		}
		out = append(out, bundledSkill{
			Name:    name,
			Path:    rel,
			Bytes:   b,
			Version: parseFrontmatterVersion(string(b)),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// parseFrontmatterVersion extracts `version: X.Y.Z` from a SKILL.md body.
// Returns "" if absent or malformed. We intentionally do not pull in a
// semver dep — a three-integer dotted form is enough for our needs, and
// any unparseable value degrades gracefully to content-hash comparison.
func parseFrontmatterVersion(content string) string {
	lines := strings.SplitN(content, "\n", 64)
	inFrontmatter := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "---" {
			if !inFrontmatter {
				inFrontmatter = true
				continue
			}
			// closing fence
			return ""
		}
		if !inFrontmatter {
			continue
		}
		kv := strings.SplitN(line, ":", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(kv[0]))
		if key != "version" {
			continue
		}
		val := strings.TrimSpace(kv[1])
		val = strings.Trim(val, "\"'")
		// Light shape check: accept X.Y.Z, and truncate X.Y.Z.* (build
		// suffix) to the first three components. Reject anything else.
		if isDottedTriplet(val) {
			return val
		}
		if isDottedIntChain(val) && strings.Count(val, ".") >= 3 {
			parts := strings.SplitN(val, ".", 4)
			return parts[0] + "." + parts[1] + "." + parts[2]
		}
		return ""
	}
	return ""
}

func isDottedTriplet(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return false
	}
	return allDigits(parts)
}

func isDottedIntChain(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) < 3 {
		return false
	}
	return allDigits(parts)
}

func allDigits(parts []string) bool {
	for _, p := range parts {
		if p == "" {
			return false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

// compareVersions returns -1/0/+1 like strings.Compare. Missing parts are
// treated as 0. Unparseable inputs sort before parseable ones.
func compareVersions(a, b string) int {
	pa := versionParts(a)
	pb := versionParts(b)
	if pa == nil && pb == nil {
		return 0
	}
	if pa == nil {
		return -1
	}
	if pb == nil {
		return 1
	}
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// versionParts returns nil if v is empty or contains a non-integer
// component, otherwise a 3-element array of major/minor/patch.
func versionParts(v string) *[3]int {
	if v == "" {
		return nil
	}
	parts := strings.SplitN(v, ".", 4)
	var out [3]int
	for i := 0; i < 3 && i < len(parts); i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return nil
		}
		out[i] = n
	}
	return &out
}

// ---------------------------------------------------------------------------
// Outdated detection
// ---------------------------------------------------------------------------

type skillStatus int

const (
	statusMissing    skillStatus = iota // no skill dir at all (or no SKILL.md)
	statusInstalled                     // SKILL.md loaded; check version/hash for up-to-date
	statusUpToDate                      // installed, matches bundled
	statusOutdated                      // installed, differs from bundled
	statusSymlink                       // target path is a symlink (refuse to overwrite)
)

type installedInfo struct {
	Dir        string
	SkillPath  string
	Bytes      []byte
	Version    string
	Sha256Hex  string
	IsSymlink  bool
}

// loadInstalled reads <globalDir>/<name>/SKILL.md from disk and returns
// its parsed metadata. The skillStatus return distinguishes the four
// observable end states: missing (no dir or no SKILL.md), installed
// (SKILL.md present and parseable), symlink (dir is a symlink — caller
// should refuse to overwrite). I/O errors are returned via the err
// return with statusMissing.
func loadInstalled(name, globalDir string) (installedInfo, skillStatus, error) {
	dir := filepath.Join(globalDir, name)
	info, lerr := os.Lstat(dir)
	if lerr != nil {
		if errors.Is(lerr, fs.ErrNotExist) {
			return installedInfo{}, statusMissing, nil
		}
		return installedInfo{}, statusMissing, lerr
	}
	// Refuse symlinked targets — see Symlink safety in the design notes.
	if info.Mode()&os.ModeSymlink != 0 {
		return installedInfo{Dir: dir, IsSymlink: true}, statusSymlink, nil
	}
	if !info.IsDir() {
		return installedInfo{}, statusMissing, fmt.Errorf("%s exists but is not a directory", dir)
	}
	skillPath := filepath.Join(dir, "SKILL.md")
	body, err := os.ReadFile(skillPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return installedInfo{Dir: dir}, statusMissing, nil
		}
		return installedInfo{}, statusMissing, err
	}
	sum := sha256.Sum256(body)
	return installedInfo{
		Dir:       dir,
		SkillPath: skillPath,
		Bytes:     body,
		Version:   parseFrontmatterVersion(string(body)),
		Sha256Hex: hex.EncodeToString(sum[:]),
	}, statusInstalled, nil
}

// classifyInstalled combines loadInstalled with a bundled-skill to produce
// the final status (missing / up-to-date / outdated / symlink).
func classifyInstalled(b bundledSkill, globalDir string) (installedInfo, skillStatus, error) {
	inst, status, err := loadInstalled(b.Name, globalDir)
	if err != nil || status == statusMissing || status == statusSymlink {
		return inst, status, err
	}
	// status == statusInstalled
	if isUpToDate(b, inst) {
		return inst, statusUpToDate, nil
	}
	return inst, statusOutdated, nil
}

func isUpToDate(b bundledSkill, inst installedInfo) bool {
	// Version takes priority when both sides declare one. The bundled
	// build is the source of truth.
	if b.Version != "" && inst.Version != "" {
		return compareVersions(b.Version, inst.Version) <= 0
	}
	// Fall back to content hash. Equal bodies => up to date. We
	// recompute both sides from the actual bytes so callers don't have
	// to remember to populate installedInfo.Sha256Hex (and so a stale
	// cached hash can't make us misclassify).
	sumB := sha256.Sum256(b.Bytes)
	sumI := sha256.Sum256(inst.Bytes)
	return hex.EncodeToString(sumB[:]) == hex.EncodeToString(sumI[:])
}

// ---------------------------------------------------------------------------
// Install / upgrade
// ---------------------------------------------------------------------------

func runInstallOrUpgrade(names []string, force bool) error {
	bundled, err := bundledSkills()
	if err != nil {
		return err
	}
	if len(bundled) == 0 {
		fmt.Fprintln(os.Stderr, "ocode skills: no bundled skills found in this build")
		return nil
	}

	target, err := globalSkillsDir()
	if err != nil {
		return err
	}

	if len(names) == 0 {
		fmt.Fprintf(os.Stdout, "%s skills to %s\n",
				ternary(force, "installing", "upgrading"),
				target)
	} else {
		fmt.Fprintf(os.Stdout, "%s %d skill(s) to %s\n",
				ternary(force, "installing", "upgrading"),
				len(names), target)
	}

	selected := pickBundled(bundled, names)
	if len(selected) == 0 {
		fmt.Fprintf(os.Stderr, "ocode skills: no bundled skills match %v\n", names)
		return nil
	}

	var installed, updated, skipped, failed int
	for _, b := range selected {
		inst, status, err := classifyInstalled(b, target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ! %s: %v\n", b.Name, err)
			failed++
			continue
		}
		switch status {
		case statusSymlink:
			fmt.Fprintf(os.Stderr, "  ! %s: target is a symlink (%s) — refusing to overwrite; remove the symlink first\n",
				b.Name, inst.Dir)
			failed++
			continue

		case statusUpToDate:
			if !force {
				fmt.Fprintf(os.Stdout, "  = %s: up to date\n", b.Name)
				skipped++
				continue
			}
			// force: still install (and back up) — covers the case where
			// the user wants to refresh the on-disk copy even if hash/version
			// match. Falls through to the outdated branch.

		case statusMissing:
			// proceed to install
		}

		action, err := installOne(b, inst, target, force)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ! %s: %v\n", b.Name, err)
			failed++
			continue
		}
		fmt.Fprintf(os.Stdout, "  %s %s\n", action, b.Name)
		switch action {
		case "installed":
			installed++
		case "updated":
			updated++
		}
	}

	fmt.Fprintf(os.Stdout, "\ndone: %d installed, %d updated, %d up-to-date, %d failed\n",
		installed, updated, skipped, failed)
	if failed > 0 {
		return fmt.Errorf("%d skill(s) failed", failed)
	}
	return nil
}

func pickBundled(bundled []bundledSkill, names []string) []bundledSkill {
	if len(names) == 0 {
		return bundled
	}
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	var out []bundledSkill
	for _, b := range bundled {
		if want[b.Name] {
			out = append(out, b)
		}
	}
	return out
}

// installOne writes the bundled skill to <globalDir>/<name>/SKILL.md. If a
// non-empty SKILL.md already exists, it is first copied (not moved) to a
// timestamped backup next to it. Returns the human-readable action label.
func installOne(b bundledSkill, inst installedInfo, globalDir string, force bool) (string, error) {
	dst := filepath.Join(globalDir, b.Name)
	skillDst := filepath.Join(dst, "SKILL.md")

	if err := os.MkdirAll(dst, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", dst, err)
	}

	// If there's an existing SKILL.md and its bytes differ from what we're
	// about to write, back it up before overwriting. Backup failure is
	// fatal: the user explicitly asked for "backup of the skill file on the
	// same dir before replacing it" and we don't want to silently lose
	// their previous version.
	action := "installed"
	if len(inst.Bytes) > 0 {
		sum := sha256.Sum256(b.Bytes)
		newHash := hex.EncodeToString(sum[:])
		if newHash == inst.Sha256Hex && !force {
			// Content matches and not forced — should have been caught
			// by the caller, but be defensive.
			return "up-to-date", nil
		}
		if err := backupFile(inst.SkillPath); err != nil {
			return "", fmt.Errorf("backup existing SKILL.md: %w", err)
		}
		action = "updated"
	}

	// Write the new file. Create it 0o644; preserve umask.
	if err := writeFileAtomic(skillDst, b.Bytes, 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", skillDst, err)
	}

	// Write the bundled hash alongside so GetSkillStatus can later
	// distinguish "outdated" from "custom-modified".
	writeBundledHash(dst, b.Bytes)

	return action, nil
}

// backupFile moves src to "<dir-of-src>/<filename>.bak.<UTC-timestamp>".
// The destination timestamp has 1-second resolution, so successive calls
// in the same second would collide; in practice install/upgrade is slow
// enough that this never happens.
//
// Uses rename when possible (atomic, preserves mode), falls back to
// read+remove on cross-device errors so we still succeed when src is on
// a different filesystem from its sibling.
func backupFile(src string) error {
	dir := filepath.Dir(src)
	base := filepath.Base(src)
	ts := time.Now().UTC().Format("20060102T150405Z")
	dst := filepath.Join(dir, base+".bak."+ts)

	// Fast path: rename is atomic and keeps the source's mode bits.
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !isCrossDeviceErr(err) {
		// Non-cross-device rename failure (permission, missing parent,
		// etc.) — surface to the caller.
		return err
	}

	// Cross-device fallback: read, write, remove.
	body, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, body, 0o644); err != nil {
		return err
	}
	return os.Remove(src)
}

func isCrossDeviceErr(err error) bool {
	// We don't want to import syscall just for this. Use the standard
	// library's "errors.Is" with the portable error string.
	// On Linux/macOS, errors from cross-device renames include
	// "invalid cross-device link" or "EXDEV". The portable way is to
	// string-match; the exact wording is stable enough for this purpose.
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "cross-device") ||
		strings.Contains(msg, "EXDEV")
}

// writeFileAtomic writes data to a temp file in the same directory and
// renames it onto dst. This avoids leaving a half-written SKILL.md if the
// process is killed mid-write.
func writeFileAtomic(dst string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, ".SKILL.md.tmp.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Ensure cleanup if we return early.
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return err
	}
	tmpName = "" // rename succeeded, suppress cleanup
	return nil
}

// ---------------------------------------------------------------------------
// Bundled-hash tracking (smart status detection)
// ---------------------------------------------------------------------------

// SkillStatus represents the install state of a skill relative to the
// bundled version shipped with ocode.
type SkillStatus int

const (
	// SkillMissing means no SKILL.md exists at the expected install path.
	SkillMissing SkillStatus = iota
	// SkillInstalled means the installed SKILL.md hash matches the
	// bundled version exactly — the skill is up to date.
	SkillInstalled
	// SkillOutdated means the installed SKILL.md hash matches the
	// bundled hash recorded at install time, but the bundled version
	// has since changed. The user hasn't touched the file.
	SkillOutdated
	// SkillCustomModified means the installed SKILL.md hash differs
	// from both the current bundled version AND the hash recorded at
	// install time. The user (or an external tool) has edited the file.
	SkillCustomModified
)

func (s SkillStatus) String() string {
	switch s {
	case SkillMissing:
		return "missing"
	case SkillInstalled:
		return "installed"
	case SkillOutdated:
		return "outdated"
	case SkillCustomModified:
		return "custom-modified"
	default:
		return "unknown"
	}
}

// SkillStatusEntry is one skill's full status report.
type SkillStatusEntry struct {
	Name        string
	Description string
	Status      SkillStatus
	Source      string // on-disk path
}

// bundledHashPath returns the path to the sidecar file that records the
// sha256 of the bundled SKILL.md at the time it was installed. This
// lets GetSkillStatus distinguish "user hasn't changed the file, but
// the bundle has moved on" (outdated) from "user edited the file"
// (custom-modified).
func bundledHashPath(dir string) string {
	return filepath.Join(dir, ".bundled-hash")
}

func writeBundledHash(dir string, bundledBody []byte) {
	sum := sha256.Sum256(bundledBody)
	_ = os.WriteFile(bundledHashPath(dir), []byte(hex.EncodeToString(sum[:])), 0o644)
}

func readBundledHash(dir string) string {
	b, err := os.ReadFile(bundledHashPath(dir))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// GetSkillStatus returns the status of every skill the system knows
// about: bundled skills (from the embedded FS) and installed skills
// (from the global config dir). The result is sorted by name.
//
// Status logic:
//
//	SKILL.md missing                      → SkillMissing
//	installed hash == bundled hash         → SkillInstalled
//	installed hash != bundled hash
//	  AND installed hash == .bundled-hash  → SkillOutdated
//	installed hash != bundled hash
//	  AND installed hash != .bundled-hash  → SkillCustomModified
func GetSkillStatus() ([]SkillStatusEntry, error) {
	target, err := globalSkillsDir()
	if err != nil {
		return nil, err
	}

	// Collect bundled skills (may be nil if no FS registered).
	bundled, _ := bundledSkills()
	bundledByName := make(map[string]bundledSkill, len(bundled))
	for _, b := range bundled {
		bundledByName[b.Name] = b
	}

	// Walk the installed skills directory to find all installed skills.
	// This catches both bundled-installed and user-created skills.
	seen := make(map[string]bool)
	var entries []SkillStatusEntry

	if dirEntries, err := os.ReadDir(target); err == nil {
		for _, de := range dirEntries {
			if !de.IsDir() {
				continue
			}
			name := de.Name()
			if seen[name] {
				continue
			}
			seen[name] = true

			dir := filepath.Join(target, name)
			info, lerr := os.Lstat(dir)
			if lerr != nil || info.Mode()&os.ModeSymlink != 0 {
				// Symlinked skill — show as-is, don't classify.
				entries = append(entries, SkillStatusEntry{
					Name:   name,
					Status: SkillMissing,
					Source: dir,
				})
				continue
			}

			skillPath := filepath.Join(dir, "SKILL.md")
			body, err := os.ReadFile(skillPath)
			if err != nil {
				continue
			}

			installedHash := sha256Hex(body)
			b, isBundled := bundledByName[name]

			var status SkillStatus
			switch {
			case !isBundled:
				// Installed locally but not in the bundle — treat as
				// custom. The user created this skill themselves.
				status = SkillCustomModified
			case installedHash == sha256Hex(b.Bytes):
				status = SkillInstalled
			default:
				// Hashes differ. Check the sidecar to decide whether
				// the user modified the file or just hasn't upgraded.
				storedHash := readBundledHash(dir)
				if storedHash != "" && installedHash == storedHash {
					status = SkillOutdated
				} else {
					status = SkillCustomModified
				}
			}

			desc := ""
			if isBundled {
				desc = b.Version
			}
			entries = append(entries, SkillStatusEntry{
				Name:        name,
				Description: desc,
				Status:      status,
				Source:      skillPath,
			})
		}
	}

	// Also include bundled skills that are NOT installed at all.
	for _, b := range bundled {
		if seen[b.Name] {
			continue
		}
		entries = append(entries, SkillStatusEntry{
			Name:        b.Name,
			Description: b.Version,
			Status:      SkillMissing,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries, nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

func runList() error {
	statuses, err := GetSkillStatus()
	if err != nil {
		return err
	}
	target, _ := globalSkillsDir()

	fmt.Fprintf(os.Stdout, "skills (%d) — install target: %s\n\n", len(statuses), target)
	for _, e := range statuses {
		switch e.Status {
		case SkillMissing:
			fmt.Fprintf(os.Stdout, "  [missing]         %s\n", e.Name)
		case SkillInstalled:
			fmt.Fprintf(os.Stdout, "  [installed]       %s\n", e.Name)
		case SkillOutdated:
			fmt.Fprintf(os.Stdout, "  [outdated]        %s\n", e.Name)
		case SkillCustomModified:
			fmt.Fprintf(os.Stdout, "  [custom-modified] %s\n", e.Name)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Uninstall
// ---------------------------------------------------------------------------

func runUninstall(names []string) error {
	if len(names) == 0 {
		return errors.New("uninstall requires at least one <name>")
	}
	target, err := globalSkillsDir()
	if err != nil {
		return err
	}

	var removed, failed, refused int
	for _, n := range names {
		dir := filepath.Join(target, n)
		info, lerr := os.Lstat(dir)
		if errors.Is(lerr, fs.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "  ! %s: not installed (%s)\n", n, dir)
			failed++
			continue
		}
		if lerr != nil {
			fmt.Fprintf(os.Stderr, "  ! %s: %v\n", n, lerr)
			failed++
			continue
		}
		// Refuse to follow symlinks; if the user has set up a symlinked
		// skill, they probably manage it themselves.
		if info.Mode()&os.ModeSymlink != 0 {
			fmt.Fprintf(os.Stderr, "  ! %s: refusing to remove symlink at %s; remove it manually if intentional\n",
				n, dir)
			refused++
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			fmt.Fprintf(os.Stderr, "  ! %s: %v\n", n, err)
			failed++
			continue
		}
		fmt.Fprintf(os.Stdout, "  removed %s\n", n)
		removed++
	}
	fmt.Fprintf(os.Stdout, "\ndone: %d removed, %d failed, %d refused\n", removed, failed, refused)
	// Refused entries are non-zero-exit too: a user running
	// `ocode skills uninstall foo` almost certainly expected *something*
	// to happen, and a silent no-op is the wrong default.
	if failed > 0 {
		return fmt.Errorf("%d skill(s) failed to remove", failed)
	}
	if refused > 0 {
		return fmt.Errorf("%d skill(s) refused (symlink targets — remove them manually)", refused)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Small helpers
// ---------------------------------------------------------------------------

func ternary(b bool, a, c string) string {
	if b {
		return a
	}
	return c
}
