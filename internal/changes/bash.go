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
// contract with the bash tool narrow: base := Pre() then
// Post(base, command, exitCode). The baseline travels with the call
// rather than living on the recorder because parallel tool calls share
// one BashTool (and so one recorder): a shared field let a second Pre
// overwrite the first call's baseline, hiding that call's writes.

package changes

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// BashRecorder is the seam between BashTool and the changes
// registry. The agent sets it during tool wiring; the bash tool calls
// Pre() right before exec, and Post(command, exitCode) right after the
// command returns. Implementations must be safe for concurrent use
// across the bash tool's background/process pumps.
type BashRecorder interface {
	// Pre captures a baseline fingerprint of the working directory
	// immediately before a bash invocation. The baseline is either
	// complete (it covers the whole workDir, and Post may diff it) or it
	// is flagged so Post declines to diff it — see BashBaseline.complete
	// for why diffing a partial fingerprint is actively wrong.
	Pre() BashBaseline

	// Post computes the post-invocation fingerprint, diffs it
	// against the Pre baseline, intersects the diff with the
	// command's path tokens, and forwards the resulting set of
	// touches to the registry. exitCode is the shell's exit
	// status (0 on success).
	Post(base BashBaseline, command string, exitCode int)
}

// BashBaseline is the opaque pre-invocation fingerprint returned by
// BashRecorder.Pre and handed back to Post. Callers never inspect it.
type BashBaseline struct {
	fps map[string]fileFingerprint
	// applicable is false when Pre ran no walk at all: no workDir is
	// bound, or the workDir is a root too large to fingerprint (see
	// unsafeWalkRoot). Post then no-ops silently, as it always has — an
	// inactive recorder is a configuration state, not an error worth a
	// notice on every command.
	applicable bool
	// complete reports whether fps covers the WHOLE workDir.
	//
	// A walk that runs out of time yields a PARTIAL map, and diffing a
	// partial map is actively wrong: diffFingerprints reports every path
	// missing from the pre-walk as BashAdded, so all the files the walk
	// never reached read as newly created. That is how one
	// `cd <workdir> && cat >> TODO.md` on a loaded machine once claimed
	// 4,214 phantom "added" rows in the changes tab. An incomplete
	// baseline is therefore discarded, never diffed.
	complete bool
	// skipReason names the walk that produced an incomplete baseline, for
	// the SkipNotice Post emits. Empty when complete.
	skipReason string
}

// RecorderOption customizes a StatBashRecorder at construction. The
// constructor takes these variadically so existing call sites keep compiling.
type RecorderOption func(*StatBashRecorder)

// WithSkipNotice registers fn to receive a SkipNotice whenever the recorder
// declines to report touches for a command (a truncated walk, an oversized
// event). The recorder has no logger of its own, so without this a skip leaves
// no trace and the next occurrence can only be reconstructed after the fact.
func WithSkipNotice(fn func(SkipNotice)) RecorderOption {
	return func(r *StatBashRecorder) { r.onSkip = fn }
}

// SkipNotice records one bash event the recorder refused to turn into change
// rows, and why. Dropping an event is a deliberate loss of coverage, so it has
// to leave a trace.
type SkipNotice struct {
	// Reason is one of the Skip* constants.
	Reason string
	// Detail elaborates in prose (e.g. which walk truncated).
	Detail string
	// Command is the shell command as issued.
	Command string
	// ExitCode is the shell's exit status.
	ExitCode int
	// Paths is how many candidate touches the event would have contributed.
	Paths int
	// At is when the skip was recorded.
	At time.Time
}

// Skip reasons recorded in a SkipNotice.
const (
	// SkipIncompleteBaseline: the pre or post fingerprint walk hit its
	// time budget, so the diff would have been against a partial tree.
	SkipIncompleteBaseline = "incomplete-baseline"
	// SkipTooManyTouches: the event's diff exceeded maxTouchesPerEvent —
	// a bulk rewrite, another process's writes, or a walk artifact, not
	// one shell command's work.
	SkipTooManyTouches = "too-many-touches"
	// SkipRegistryCeiling: the registry already tracks maxTrackedFiles
	// distinct paths and refused a new one.
	SkipRegistryCeiling = "registry-ceiling"
)

// maxTrackedFiles is the registry-wide ceiling on distinct tracked paths, a
// backstop behind maxTouchesPerEvent: whatever the source, no single session
// can grow the changes list without bound. Reaching it means a defect
// upstream got this far, and a bounded list still renders.
const maxTrackedFiles = 5000

// maxSkipNotices bounds the retained SkipNotice ring.
const maxSkipNotices = 20

// NewStatBashRecorder returns a BashRecorder that walks workDir
// before and after each invocation, diffing on (mtime, size) and
// falling back to sha256 when those differ. The workDir is the bash
// tool's CWD at exec time.
func NewStatBashRecorder(workDir string, reg *Registry, opts ...RecorderOption) BashRecorder {
	r := &StatBashRecorder{
		workDir:    workDir,
		reg:        reg,
		budget:     defaultWalkBudget,
		maxTouches: maxTouchesPerEvent,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(r)
		}
	}
	return r
}

// noiseDirNames are directories the stat-walk skips. The pre/post
// stat is bounded by walk time; skipping the standard build/dependency
// noise keeps it cheap for large repos.
// walkBudget bounds how long a single Pre/Post fingerprint walk may run.
// Without this, a workDir that resolves to the user's home directory (or
// any other huge/slow tree — a network mount, an iCloud-synced folder with
// undownloaded placeholder files) blocks every bash call for as long as the
// walk takes, with no cancellation available (Pre runs before the bash
// exec's timeout context even exists). Exceeding the budget aborts the walk
// early and yields a partial (best-effort) fingerprint rather than hanging.
const defaultWalkBudget = 2 * time.Second

// maxTouchesPerEvent bounds how many touches ONE bash command may contribute
// to the changes list.
//
// A single shell command that legitimately rewrites more than this many files
// under the workDir is a bulk operation (a codemod, `gofmt -w .`, a checkout)
// — work git already tracks and this detector could never undo anyway. The
// flood it does guard against is anything LARGER: a truncated walk, or writes
// made concurrently by another process while this command ran. Those are not
// this command's changes, and a changes list that claims they are is worse
// than one that admits it saw too much to attribute.
const maxTouchesPerEvent = 200

// unsafeWalkRoot reports whether workDir is a directory too large/slow to
// safely fingerprint on every bash call: the filesystem root, or the user's
// home directory. This is the fallback workDir used when a session has no
// real project root bound to it (see cmd/ocode-desktop/main.go), and walking
// it defeats the point of the recorder (there's no meaningful "project" to
// track changes for) while risking a multi-minute stall.
func unsafeWalkRoot(workDir string) bool {
	clean := filepath.Clean(workDir)
	if clean == string(filepath.Separator) {
		return true
	}
	if home, err := os.UserHomeDir(); err == nil && clean == filepath.Clean(home) {
		return true
	}
	return false
}

var noiseDirNames = map[string]struct{}{
	".git":         {},
	".opencode":    {},
	"node_modules": {},
	"vendor":       {},
	"build":        {},
	"dist":         {},
	"target":       {},
	".next":        {},
	".turbo":       {},
	".cache":       {},
	"__pycache__":  {},
	".venv":        {},
	"venv":         {},
	".idea":        {},
	".vscode":      {},
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
	// budget bounds one fingerprint walk. A field, not the bare constant, so
	// tests can force a truncation deterministically instead of racing a
	// loaded machine's clock.
	budget time.Duration
	// maxTouches bounds one event's contribution. See maxTouchesPerEvent.
	maxTouches int
	// onSkip, when set, receives every SkipNotice the recorder emits.
	onSkip func(SkipNotice)
}

// skip records a notice with the registry and the optional callback. The
// registry copy is what makes a skip diagnosable later (nothing else retains
// it); the callback is what makes it visible in the debug panel.
func (r *StatBashRecorder) skip(n SkipNotice) {
	n.At = time.Now()
	if r.reg != nil {
		r.reg.NotifyBashSkipped(n)
	}
	if r.onSkip != nil {
		r.onSkip(n)
	}
}

// fileFingerprint is the per-file snapshot the recorder takes. mtime
// is captured to nanosecond resolution (a same-size edit landing in the
// same second as the pre-walk must not read as "unchanged"); size is in
// bytes. hash is only
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

// walkStatus is the outcome of one fingerprint walk. It exists because
// "no map" has two very different meanings: the recorder never ran (fine,
// silent) or the recorder ran and gave up halfway (must not be diffed).
type walkStatus int

const (
	// walkDisabled: no workDir bound, or a root too large to fingerprint
	// (see unsafeWalkRoot). The recorder is inactive; nothing to report.
	walkDisabled walkStatus = iota
	// walkTruncated: the walk hit its time budget and the map is partial.
	walkTruncated
	// walkComplete: the map covers the whole workDir.
	walkComplete
)

// fingerprint walks the working directory once, recording a (mtime, size)
// fingerprint per regular file. Noise directories (vendor, .git, …) and dot
// directories are skipped. The status is the point of the function: callers
// must not treat a truncated map as a baseline.
func (r *StatBashRecorder) fingerprint() (map[string]fileFingerprint, walkStatus) {
	if r.workDir == "" || unsafeWalkRoot(r.workDir) {
		return nil, walkDisabled
	}
	budget := r.budget
	if budget <= 0 {
		budget = defaultWalkBudget
	}
	fps := make(map[string]fileFingerprint)
	status := walkComplete
	start := time.Now()
	_ = filepath.Walk(r.workDir, func(path string, info os.FileInfo, err error) error {
		if time.Since(start) > budget {
			status = walkTruncated
			return filepath.SkipAll
		}
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
		fps[path] = fileFingerprint{
			mtime: info.ModTime().UnixNano(),
			size:  info.Size(),
		}
		return nil
	})
	// The partial map is returned alongside the status rather than
	// discarded: the status is the caller's signal, and handing back nil
	// would make a truncated walk indistinguishable from an empty tree.
	return fps, status
}

// Pre walks the working directory and records a baseline fingerprint
// for every regular file, flagged complete or not (see BashBaseline).
func (r *StatBashRecorder) Pre() BashBaseline {
	fps, status := r.fingerprint()
	switch status {
	case walkDisabled:
		return BashBaseline{}
	case walkTruncated:
		return BashBaseline{
			applicable: true,
			skipReason: "pre-walk exceeded the fingerprint time budget",
		}
	default:
		return BashBaseline{fps: fps, applicable: true, complete: true}
	}
}

// Post walks the working directory again, diffs against the pre
// snapshot, extracts path tokens from the command, intersects the
// two sets, and forwards the touches to the registry.
//
// exitCode is the shell's exit code; it is included in the BashWriteEvent
// so the per-row details can show "(failed: exit 2)" in a future
// enhancement. A non-zero exit code does not change the diff: the
// shell may have partially written files before failing.
func (r *StatBashRecorder) Post(base BashBaseline, command string, exitCode int) {
	if r.workDir == "" || r.reg == nil || unsafeWalkRoot(r.workDir) {
		return
	}
	// A baseline that never ran a walk leaves the recorder inactive: no rows
	// and no notice, exactly as before. That is a configuration state, not a
	// dropped event.
	if !base.applicable {
		return
	}
	// A truncated baseline is not a baseline. Diffing it reports every file
	// the walk never reached as newly created, so the whole event is
	// discarded and recorded as a skip.
	if !base.complete {
		r.skip(SkipNotice{
			Reason:   SkipIncompleteBaseline,
			Detail:   base.skipReason,
			Command:  command,
			ExitCode: exitCode,
			Paths:    len(base.fps),
		})
		return
	}
	post, status := r.fingerprint()
	if status == walkTruncated {
		r.skip(SkipNotice{
			Reason:   SkipIncompleteBaseline,
			Detail:   "post-walk exceeded the fingerprint time budget",
			Command:  command,
			ExitCode: exitCode,
			Paths:    len(post),
		})
		return
	}

	// Diff pre → post. We emit a touch for every path that:
	//   1. exists now and either didn't exist before, or has
	//      different (mtime, size, hash).
	//   2. existed before and is gone now (deleted).
	touches := diffFingerprints(base.fps, post)
	if len(touches) == 0 {
		return
	}

	// Intersect with path tokens extracted from the command. The
	// token set is the lower bound for what the shell could have
	// touched; a path that does not match any token is excluded
	// (so a comment that mentions /etc/passwd doesn't produce a
	// false positive when the command itself didn't go near it).
	candidates := r.targetTokens(command)
	if len(candidates) > 0 {
		touches = intersectTouchesWithCandidates(touches, candidates)
	}

	// A command that names no path at all leaves the diff unfiltered:
	// `cat >> TODO.md` yields no token (the regex requires a slash), and
	// the same is true of most heredocs and in-place `sed`s. Those writes
	// are the bread and butter of this detector, so the diff is kept —
	// but only while it is small enough to plausibly BE one command's work.
	// Unbounded, this branch is how a concurrent writer's thousands of
	// files became one command's changes; the cap is what makes the
	// fail-open safe.
	cap := r.maxTouches
	if cap <= 0 {
		cap = maxTouchesPerEvent
	}
	if len(touches) > cap {
		r.skip(SkipNotice{
			Reason: SkipTooManyTouches,
			Detail: fmt.Sprintf("diff held %d paths, cap is %d — treated as a bulk rewrite or another process's writes, not this command's",
				len(touches), cap),
			Command:  command,
			ExitCode: exitCode,
			Paths:    len(touches),
		})
		return
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
// bytes as the pre-captured hash. Without a pre-hash there is nothing
// to compare against, so the answer is false: the (mtime, size) change
// that got us here is then reported as a modification. Hashing the
// live file "for both sides" here compared the file to itself and
// returned true for every same-size edit (sed -i, in-place rewrites),
// which is why those never reached the changes tab. False is also
// returned on any read error — the safe default is to treat the file
// as modified and let the registry show it.
func fileContentEqual(path, preHash, postHash string) bool {
	if preHash == "" {
		return false
	}
	post := postHash
	if post == "" {
		post = hashFile(path)
	}
	return post != "" && preHash == post
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

// targetTokens returns the command's path tokens — the lower bound on what
// the shell could have touched — for the intersection in Post.
//
// A token that merely names WHERE the shell is (the workDir the bash tool
// already runs in, or an ancestor of it) is not a target: touch matching is
// deliberately permissive, so a directory token admits every file beneath it
// and the intersection degenerates to "keep the whole diff". Since virtually
// every command begins `cd <workdir> && …`, that made the filter a no-op —
// which is how one command claimed thousands of rows it never wrote.
func (r *StatBashRecorder) targetTokens(command string) map[string]struct{} {
	tokens := pathTokensFromCommand(command)
	if len(tokens) == 0 || r.workDir == "" {
		return tokens
	}
	workDir := filepath.Clean(r.workDir)
	for tok := range tokens {
		if isLocationToken(tok, workDir) {
			delete(tokens, tok)
		}
	}
	return tokens
}

// isLocationToken reports whether a path token names where the shell IS
// rather than what it writes: the workDir it already runs in, or an ancestor
// of it.
//
// Such a token has to go. Touch matching is deliberately permissive (a
// candidate matches as a substring of the path, either direction), so a
// directory token admits every file beneath it and the "intersect with the
// command's paths" filter stops filtering anything. Since virtually every
// command opens with `cd <workdir> && …`, one such token silently authorized
// the entire tree — which is the second of the two defects behind a single
// `cat >> TODO.md` claiming thousands of change rows.
//
// Note this is deliberately NOT "drop every directory token": `cp -r
// <workdir>/docs /tmp/x` names a real subdirectory target, and dropping it
// would lose a detection the tab is built to show.
func isLocationToken(tok, workDir string) bool {
	clean := filepath.Clean(tok)
	if clean == workDir {
		return true
	}
	// An ancestor of the workDir. `cd /Users/james/www && go build ./x` says
	// where the shell went, not what it touched.
	return strings.HasPrefix(workDir, clean+string(filepath.Separator))
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
