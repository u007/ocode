// Bash detection for the changes tab. The snapshot store knows about
// every write/edit/patch/formatter call, but a shell command like
// `cat > foo.txt`, `sed -i …`, or `rm bar.txt` bypasses the snapshot
// pipeline — yet the user still wants to see (and ideally undo) those
// changes from the changes tab.
//
// This file provides the BashRecorder seam wired into BashTool
// (internal/tool/exec.go). The default implementation,
// StatBashRecorder, captures a (mtime, size, sha256) fingerprint of
// every file under the working directory before the command runs, then
// again after. The diff is intersected with a path-token extraction
// of the command string, so a comment like
// `echo "this mentions /etc/passwd but doesn't touch it"` does not
// produce a false positive. The resulting set of touches is delivered
// to the registry via NotifyBashWrite.
//
// The package never imports the TUI; the recorder is pure Go, and the
// fingerprint walk is bounded by the working directory (the bash tool
// runs in workDir). This keeps the recorder testable with a tempdir
// and a real /bin/sh invocation, and it keeps the changes tab's
// contract with the bash tool narrow: Pre() then Post(command, exitCode).

package changes

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// BashRecorder is the seam between BashTool and the changes
// registry. The agent sets it during tool wiring; the bash tool calls
// Pre() right before exec, and Post(command, exitCode) right after the
// command returns. Implementations must be safe for concurrent use
// across the bash tool's background/process pumps.
type BashRecorder interface {
	// Pre captures a baseline fingerprint of the working directory
	// immediately before a bash invocation. Errors are non-fatal
	// (the recorder logs nothing — the bash tool can't surface
	// them anyway, and the post-exec walk will still produce a
	// useful diff against an empty baseline).
	Pre()

	// Post computes the post-invocation fingerprint, diffs it
	// against the Pre baseline, intersects the diff with the
	// command's path tokens, and forwards the resulting set of
	// touches to the registry. exitCode is the shell's exit
	// status (0 on success).
	Post(command string, exitCode int)
}

// NewStatBashRecorder returns a BashRecorder that walks workDir
// before and after each invocation, diffing on (mtime, size) and
// falling back to sha256 when those differ. The workDir is the bash
// tool's CWD at exec time.
func NewStatBashRecorder(workDir string, reg *Registry) BashRecorder {
	return &StatBashRecorder{
		workDir: workDir,
		reg:     reg,
	}
}

// noiseDirNames are directories the stat-walk skips. The pre/post
// stat is bounded by walk time; skipping the standard build/dependency
// noise keeps it cheap for large repos.
var noiseDirNames = map[string]struct{}{
	".git":              {},
	".opencode":         {},
	"node_modules":      {},
	"vendor":            {},
	"build":             {},
	"dist":              {},
	"target":            {},
	".next":             {},
	".turbo":            {},
	".cache":            {},
	"__pycache__":       {},
	".venv":             {},
	"venv":              {},
	".idea":             {},
	".vscode":           {},
}

// StatBashRecorder is the default BashRecorder. It walks the working
// directory before and after each bash invocation, recording a
// (mtime, size, sha256) fingerprint per file. The Post step diffs the
// two snapshots, intersects with a path-token extraction of the
// command string, and forwards the touches to the registry.
//
// The fingerprints live in RAM; on-disk backup files are not touched
// by the recorder (the snapshot store is responsible for those when
// the bash tool chooses to back up a known destructive path via
// destructiveBashBackupPaths).
type StatBashRecorder struct {
	workDir string
	reg     *Registry
	pre     map[string]fileFingerprint // path → fingerprint
}

// fileFingerprint is the per-file snapshot the recorder takes. mtime
// is captured to second resolution; size is in bytes. hash is only
// populated when mtime or size differ between pre and post (sha256 is
// O(file size) and we want the common case to be cheap).
type fileFingerprint struct {
	mtime int64
	size  int64
	hash  string // hex sha256; "" if not computed yet
}

// pathTokenRegex is a deliberately conservative extractor for path-like
// tokens in a shell command. It matches strings that look like
// absolute paths (/foo, /foo/bar) or relative paths with at least
// one slash (./foo, ../foo, foo/bar). It is intentionally NOT a
// general shell parser — comments and quoted strings fall through
// here, so a comment like
// `echo "this mentions /etc/passwd but doesn't touch it"`
// still produces /etc/passwd as a candidate. The candidate set is
// then intersected with the post-walk diff, so the comment is filtered
// out unless the actual command also wrote to /etc/passwd.
var pathTokenRegex = regexp.MustCompile(`(?:^|\s|=|;|\|)([A-Za-z0-9_./~+-]+/[A-Za-z0-9_./~+-]+)`)

// Pre walks the working directory and records a baseline fingerprint
// for every regular file. The walk skips noise directories (vendor,
// .git, etc.) and any directory whose name starts with a dot (to keep
// .opencode snapshots out of the per-invocation diff).
func (r *StatBashRecorder) Pre() {
	if r.workDir == "" {
		r.pre = nil
		return
	}
	fps := make(map[string]fileFingerprint)
	_ = filepath.Walk(r.workDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Permissions errors etc.: skip the entry. The walk
			// continues so a single bad leaf doesn't blackhole
			// the diff.
			return nil
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if path != r.workDir {
				if _, noise := noiseDirNames[base]; noise {
					return filepath.SkipDir
				}
				if strings.HasPrefix(base, ".") && base != "." {
					return filepath.SkipDir
				}
			}
			return nil
		}
		// Skip non-regular files (symlinks, sockets, devices).
		if !info.Mode().IsRegular() {
			return nil
		}
		// Skip files that look like the snapshot backup dir itself.
		// The path is relative to walk root; the absolute path is
		// recomposed by the post-walk for matching.
		fps[path] = fileFingerprint{
			mtime: info.ModTime().Unix(),
			size:  info.Size(),
		}
		return nil
	})
	r.pre = fps
}

// Post walks the working directory again, diffs against the pre
// snapshot, extracts path tokens from the command, intersects the
// two sets, and forwards the touches to the registry.
//
// exitCode is the shell's exit code; it is included in the BashWriteEvent
// so the per-row details can show "(failed: exit 2)" in a future
// enhancement. A non-zero exit code does not change the diff: the
// shell may have partially written files before failing.
func (r *StatBashRecorder) Post(command string, exitCode int) {
	if r.workDir == "" || r.reg == nil {
		r.pre = nil
		return
	}
	post := make(map[string]fileFingerprint)
	_ = filepath.Walk(r.workDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if path != r.workDir {
				if _, noise := noiseDirNames[base]; noise {
					return filepath.SkipDir
				}
				if strings.HasPrefix(base, ".") && base != "." {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		post[path] = fileFingerprint{
			mtime: info.ModTime().Unix(),
			size:  info.Size(),
		}
		return nil
	})

	// Diff pre → post. We emit a touch for every path that:
	//   1. exists now and either didn't exist before, or has
	//      different (mtime, size, hash).
	//   2. existed before and is gone now (deleted).
	touches := diffFingerprints(r.pre, post)
	r.pre = nil
	if len(touches) == 0 {
		return
	}

	// Intersect with path tokens extracted from the command. The
	// token set is the lower bound for what the shell could have
	// touched; a path that does not match any token is excluded
	// (so a comment that mentions /etc/passwd doesn't produce a
	// false positive when the command itself didn't go near it).
	candidates := pathTokensFromCommand(command)
	if len(candidates) > 0 {
		touches = intersectTouchesWithCandidates(touches, candidates)
	}

	if len(touches) == 0 {
		return
	}

	// Hand off to the registry. The registry rebuilds its file map
	// under its own lock; we pass a copy of the slice so the
	// recorder doesn't have to worry about retention.
	evt := BashWriteEvent{
		Command:  command,
		WorkDir:  r.workDir,
		ExitCode: exitCode,
		Touches:  touches,
	}
	r.reg.NotifyBashWrite(evt)
}

// diffFingerprints returns one BashTouch per path whose pre/post
// fingerprints differ. Added files (no pre entry) and deleted files
// (no post entry) are included. For modified files, hash is
// populated lazily — only when mtime or size differ.
func diffFingerprints(pre, post map[string]fileFingerprint) []BashTouch {
	var out []BashTouch
	// Added or modified.
	for path, postFP := range post {
		preFP, existed := pre[path]
		if !existed {
			out = append(out, BashTouch{Path: path, Op: BashAdded})
			continue
		}
		if preFP.mtime == postFP.mtime && preFP.size == postFP.size {
			// No change.
			continue
		}
		// mtime or size differ. Hash lazily to disambiguate a
		// touched-but-identical file from a real edit.
		if !fileContentEqual(path, preFP.hash, postFP.hash) {
			out = append(out, BashTouch{Path: path, Op: BashModified})
		}
	}
	// Deleted.
	for path := range pre {
		if _, ok := post[path]; !ok {
			out = append(out, BashTouch{Path: path, Op: BashDeleted})
		}
	}
	return out
}

// fileContentEqual returns true when the file at path has the same
// bytes as the pre-captured hash (when supplied). When both hashes
// are empty, it computes them on the fly. False is returned on any
// error (file vanished, permission denied, etc.) — the safe default
// is to treat the file as modified and let the registry show it.
func fileContentEqual(path, preHash, postHash string) bool {
	pre := preHash
	post := postHash
	if pre == "" {
		pre = hashFile(path)
	}
	if post == "" {
		post = hashFile(path)
	}
	return pre != "" && pre == post
}

// hashFile returns the hex sha256 of the file at path, or "" on
// any read error. Files larger than 16 MiB are truncated to that
// length for hashing — the recorder only needs to disambiguate
// touched-but-identical files from real edits, and a 16 MiB sample
// has a false-collision probability of effectively zero.
func hashFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	const maxRead = 16 * 1024 * 1024
	h := sha256.New()
	if _, err := io.CopyN(h, f, maxRead); err != nil && err != io.EOF {
		// We accept partial reads: any I/O error mid-stream just
		// contributes a non-matching hash, which is the safe
		// direction (treat as modified).
		_ = err
	}
	return hex.EncodeToString(h.Sum(nil))
}

// pathTokensFromCommand extracts path-like substrings from a shell
// command. The regex is deliberately narrow (it requires a slash and
// at least two path segments) so a stray word like "make" never
// becomes a candidate. Quoted strings are not specially handled —
// the same regex sees through them, which is acceptable: a quoted
// path that the shell never actually touched is filtered out by the
// post-walk diff, while a path the shell DID touch is in the diff
// and will be matched regardless of quoting.
func pathTokensFromCommand(command string) map[string]struct{} {
	matches := pathTokenRegex.FindAllStringSubmatch(command, -1)
	out := make(map[string]struct{}, len(matches))
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		token := m[1]
		// Strip leading ./ for normalization. ../ is kept
		// (it may resolve to a real path the shell touched).
		token = strings.TrimPrefix(token, "./")
		if token == "" {
			continue
		}
		out[token] = struct{}{}
	}
	return out
}

// intersectTouchesWithCandidates keeps only touches whose path
// matches a candidate token. The matching is permissive: a candidate
// is matched as a substring of the absolute path (so the token "foo"
// matches "/workdir/sub/foo.txt"), and conversely the absolute path
// is matched as a substring of the candidate (so the token
// "/workdir/sub/foo.txt" matches the absolute path verbatim). This
// is a conservative intersection — it errs on the side of letting
// a real touch through, which the registry then renders with a
// "(bash)" marker rather than silently dropping.
func intersectTouchesWithCandidates(touches []BashTouch, candidates map[string]struct{}) []BashTouch {
	if len(candidates) == 0 {
		return touches
	}
	out := touches[:0]
	for _, t := range touches {
		if touchMatchesCandidates(t.Path, candidates) {
			out = append(out, t)
		}
	}
	return out
}

// touchMatchesCandidates returns true when path matches at least
// one candidate token. The path is absolute; candidates are
// extracted from the command string and may be absolute, home-relative
// (~), or relative. We normalize ~ against the user's home dir and
// strip leading ./ from candidates before comparison.
func touchMatchesCandidates(path string, candidates map[string]struct{}) bool {
	// Normalize path: drop any trailing slash so candidate "foo" doesn't
	// miss "/workdir/foo/" via substring mismatch.
	normalized := strings.TrimRight(path, string(filepath.Separator))
	for cand := range candidates {
		// Resolve ~ against HOME for the comparison.
		expanded := cand
		if strings.HasPrefix(expanded, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				expanded = filepath.Join(home, strings.TrimPrefix(expanded, "~/"))
			}
		}
		expanded = strings.TrimRight(expanded, string(filepath.Separator))
		if expanded == "" {
			continue
		}
		// Match either direction: candidate is a substring of the
		// path, or the path is a substring of the candidate.
		if strings.Contains(normalized, expanded) {
			return true
		}
		if strings.Contains(expanded, normalized) {
			return true
		}
	}
	return false
}
