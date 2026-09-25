package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/u007/ocode/internal/gitexec"
)

// This file holds the git-conflict and in-progress-operation support for the
// Git tab. It starts with operation-state detection: git records a halted
// merge/rebase/cherry-pick/revert/am/bisect purely as files and directories
// inside the repository's git directory, so detection reads those and nothing
// else.
//
// Nothing here parses the long-format `git status` output. That output is
// human prose and is locale-dependent; the state files are the same source
// git's own status code consults, so reading them keeps ocode aligned with
// git across locales and versions.

// GitOperation describes a git operation that stopped before completing and
// is waiting for the user. It is reported on GitStatus so the web can offer
// the actions that operation actually supports.
type GitOperation struct {
	// Kind is one of: merge, rebase, rebase-interactive, am, cherry-pick,
	// revert, bisect.
	Kind string `json:"kind"`
	// Label is human-facing, e.g. "Rebasing feature/login onto 1a2b3c4 (3/7)".
	Label string `json:"label"`
	// Step and Total are rebase/am progress. Both are 0 when the operation
	// has no progress concept, and the label omits the suffix in that case.
	Step  int `json:"step"`
	Total int `json:"total"`
}

// GitConflict is one unmerged path in the index — a file git could not merge
// on its own and that is waiting for the user to choose a side.
type GitConflict struct {
	// Path is repo-relative.
	Path string `json:"path"`
	// Code is git's two-character porcelain XY for an unmerged entry:
	// DD, AU, UD, UA, DU, AA or UU.
	Code string `json:"code"`
	// Ours and Theirs report whether the stage-2 (ours) and stage-3 (theirs)
	// index entries exist. A false side is a deletion on that side, which
	// matters because `git checkout --ours/--theirs` cannot be used for a
	// deleted side — the resolver must `git rm` it instead.
	Ours   bool `json:"ours"`
	Theirs bool `json:"theirs"`
}

// gitStateEntry is one probed path inside a repository's git directory: its
// name relative to that directory, whether it is a directory, and — for a
// small text file — its contents.
//
// The type is deliberately transport-neutral. The local path fills it in
// with os.Stat, and the remote path (internal/server/handler_remote_git.go)
// fills it in from a probe script, so both feeds the same parser and cannot
// drift apart.
type gitStateEntry struct {
	Name  string
	IsDir bool
	Value string
}

// gitOperationStateNames is the fixed list of paths, relative to a
// repository's git directory, that together describe an in-progress
// operation. Both transports probe exactly this list, so
// gitOperationStateFor is the only place that knows what a rebase looks like.
//
// Names are slash-separated regardless of platform; the local probe converts
// them with filepath.FromSlash before joining.
var gitOperationStateNames = []string{
	"MERGE_HEAD",
	"MERGE_MSG",
	"CHERRY_PICK_HEAD",
	"REVERT_HEAD",
	"BISECT_START",
	"sequencer",
	"sequencer/todo",
	"sequencer/head",
	"rebase-merge",
	"rebase-merge/interactive",
	"rebase-merge/msgnum",
	"rebase-merge/end",
	"rebase-merge/head-name",
	"rebase-merge/onto",
	"rebase-apply",
	"rebase-apply/applying",
	"rebase-apply/next",
	"rebase-apply/last",
	"rebase-apply/head-name",
	"rebase-apply/onto",
}

// gitStateFileReadLimit caps how much of a state file is read. Git's own
// state files are tiny; the cap exists so a pathological or substituted file
// cannot be slurped into memory.
const gitStateFileReadLimit = 4 << 10

// errGitNotARepository reports that a directory is not inside a git
// repository. It is a distinct sentinel so callers can treat "nothing to
// detect here" differently from "detection failed" — the web Git tab probes
// many saved directories that are not repositories, and those must not be
// logged as failures, while a real failure must never be mistaken for a
// quiet repository.
var errGitNotARepository = errors.New("not a git repository")

// isGitNotARepository reports whether err is git saying the directory is not
// in a repository, as opposed to a genuine failure such as git being missing,
// a permission problem, or lock retries being exhausted.
//
// This matches git's own message, the same way gitexec.LockHeld matches
// index-lock text — git exposes no typed error for this, and the phrase is
// part of its stable porcelain-independent failure output.
func isGitNotARepository(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "not a git repository")
}

// probeCLocale pins git's output language for the operation-state probe only.
//
// isGitNotARepository matches git's English "not a git repository" text, so a
// localized git would make every non-repository look like a real failure.
//
// It is deliberately NOT set in gitexec.Env: that environment is also used for
// mutating commands (commit, stash, add), where LC_ALL would override every
// LC_* category and change how the user's hooks, GIT_EDITOR, signing and
// credential helpers behave — including on UTF-8 paths and commit messages.
// This probe is read-only and spawns nothing, so scoping the locale to it is
// free and has no user-visible side effect.
const probeCLocale = "LC_ALL=C"

// gitOperationStateForDir reports the operation in progress at dir.
//
// It spends exactly one git process (`rev-parse --absolute-git-dir`) and then
// stats the fixed probe list. A directory that is not a repository returns
// errGitNotARepository, which callers may treat as "no operation" without
// logging it; any other failure is returned wrapped and is a real error the
// caller must handle or log.
//
// ctx bounds the probe. Callers that share a larger probe budget (the status
// sweep does) pass their context so a wedged repository cannot pin them past
// it; standalone callers may pass context.Background().
func gitOperationStateForDir(ctx context.Context, dir string) (*GitOperation, error) {
	gitDir, err := gitOperationStateGitDir(ctx, dir)
	if err != nil {
		if isGitNotARepository(err) {
			return nil, errGitNotARepository
		}
		return nil, fmt.Errorf("resolve git directory: %w", err)
	}
	return gitOperationStateFor(gitStateEntriesInDir(gitDir)), nil
}

// gitOperationStateGitDir resolves a repository's git directory with a
// locale pinned to C.
//
// It is a read-only `rev-parse`: it writes nothing and takes no index lock, so
// it needs none of gitexec.WithLockRetry's locking machinery. It still starts
// from gitexec.Env so it keeps GIT_OPTIONAL_LOCKS=0 and therefore cannot
// contend for the user's index, which is the whole reason that env exists.
// It runs under ctx so the caller's deadline bounds it.
//
// The inherited LC_ALL is stripped first so exactly one locale entry reaches
// the child. Leaving both would make the effective locale depend on how the OS
// resolves duplicate environment entries.
func gitOperationStateGitDir(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, gitBinary, "rev-parse", "--absolute-git-dir")
	cmd.Dir = dir
	cmd.Env = append(withoutLCAll(gitexec.Env()), probeCLocale)

	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", gitexec.WithOutput(err, stderr.String(), string(out))
	}
	return strings.TrimSpace(string(out)), nil
}

// remoteGitResolveConflict resolves a conflict on a remote (SSH/WSL) project.
//
// It is implemented in Phase 05 of the conflicts plan. Until then it answers
// an explicit 501 rather than silently falling through to the LOCAL
// implementation, which would check out a same-named path on the wrong
// machine.
func (h *Handler) remoteGitResolveConflict(w http.ResponseWriter, r *http.Request, host string) {
	writeError(w, http.StatusNotImplemented, "conflict resolution on remote projects is not implemented yet")
}

// gitRunInDirLiteral runs a git command in dir with GIT_LITERAL_PATHSPECS=1,
// so a path is always treated as a filename and never as pathspec magic.
//
// This is deliberately scoped to the conflict endpoints rather than added to
// gitexec.Env: the shared env is used by every git call in the product, and
// changing pathspec interpretation globally would alter existing behavior.
// Only the endpoints that take a caller-supplied conflict path need it.
//
// The command runs under gitexec.WithLockRetry, because resolving a conflict
// is exactly when the user's editor or terminal may also be touching the
// index.
func gitRunInDirLiteral(dir string, args ...string) (string, error) {
	var out string
	err := gitexec.WithLockRetry(func() error {
		cmd := exec.Command(gitBinary, args...)
		if dir != "" {
			cmd.Dir = dir
		}
		cmd.Env = append(gitexec.Env(), "GIT_LITERAL_PATHSPECS=1")
		var stderr strings.Builder
		cmd.Stderr = &stderr
		b, cmdErr := cmd.Output()
		out = strings.TrimSpace(string(b))
		return gitexec.WithOutput(cmdErr, stderr.String(), out)
	})
	return out, err
}

// withoutLCAll drops any LC_ALL entry so a caller can append exactly one.
func withoutLCAll(env []string) []string {
	out := env[:0]
	for _, kv := range env {
		if strings.HasPrefix(kv, "LC_ALL=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// gitStateEntriesInDir probes the fixed state-name list under gitDir.
//
// A name that simply does not exist is the normal case — most repositories
// have no operation in progress and therefore almost none of these paths —
// so only a not-exist error is skipped quietly. Every other stat failure is
// logged with the path, because it means the state could not be read and the
// operation may be misreported as absent.
func gitStateEntriesInDir(gitDir string) []gitStateEntry {
	entries := make([]gitStateEntry, 0, len(gitOperationStateNames))
	for _, name := range gitOperationStateNames {
		path := filepath.Join(gitDir, filepath.FromSlash(name))
		info, err := os.Stat(path)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				log.Printf("git operation state: stat %s: %v", path, err)
			}
			continue
		}
		if info.IsDir() {
			entries = append(entries, gitStateEntry{Name: name, IsDir: true})
			continue
		}
		entries = append(entries, gitStateEntry{
			Name:  name,
			Value: readGitStateFile(path),
		})
	}
	return entries
}

// readGitStateFile reads at most gitStateFileReadLimit bytes.
//
// A failure yields an empty value, because presence — not contents — is what
// identifies a state, so a half-read state file still reports the operation.
// The failure itself is logged with the path, never the contents: MERGE_MSG
// can contain commit messages, and this data is written to a log.
func readGitStateFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		log.Printf("git operation state: open state file %s: %v", path, err)
		return ""
	}
	defer f.Close()
	buf, err := io.ReadAll(io.LimitReader(f, gitStateFileReadLimit))
	if err != nil {
		log.Printf("git operation state: read state file %s: %v", path, err)
		return ""
	}
	return string(buf)
}

// gitOperationStateFor turns a state dump into the operation in progress, or
// nil when the repository is idle. It is pure: no I/O, no globals mutated.
//
// Entries are indexed into a lookup first, so the result does not depend on
// the order the probe happened to report them in.
//
// Precedence is most-specific-first. A rebase is checked before a merge
// because rebase and merge state are mutually exclusive in practice, but the
// order keeps the answer stable if both are ever present. A single-commit
// cherry-pick or revert is checked before the multi-commit sequencer
// directory, which is what git itself does.
func gitOperationStateFor(entries []gitStateEntry) *GitOperation {
	present := make(map[string]gitStateEntry, len(entries))
	for _, e := range entries {
		present[e.Name] = e
	}
	has := func(name string) bool {
		_, ok := present[name]
		return ok
	}
	value := func(name string) string {
		return strings.TrimSpace(present[name].Value)
	}

	// 1. rebase-merge: the modern rebase (interactive or not).
	if has("rebase-merge") {
		kind := "rebase"
		label := "Rebasing"
		if has("rebase-merge/interactive") {
			kind = "rebase-interactive"
			label = "Interactive rebasing"
		}
		step, total := stateProgress(value("rebase-merge/msgnum"), value("rebase-merge/end"))
		return &GitOperation{
			Kind:  kind,
			Label: label + stateTargets(value("rebase-merge/head-name"), value("rebase-merge/onto")) + stateProgressSuffix(step, total),
			Step:  step,
			Total: total,
		}
	}

	// 2. rebase-apply: `git am`, or an old-style rebase.
	if has("rebase-apply") {
		if has("rebase-apply/applying") {
			step, total := stateProgress(value("rebase-apply/next"), value("rebase-apply/last"))
			return &GitOperation{
				Kind:  "am",
				Label: "Applying patch series" + stateTargets(value("rebase-apply/head-name"), value("rebase-apply/onto")) + stateProgressSuffix(step, total),
				Step:  step,
				Total: total,
			}
		}
		step, total := stateProgress(value("rebase-apply/next"), value("rebase-apply/last"))
		return &GitOperation{
			Kind:  "rebase",
			Label: "Rebasing" + stateTargets(value("rebase-apply/head-name"), value("rebase-apply/onto")) + stateProgressSuffix(step, total),
			Step:  step,
			Total: total,
		}
	}

	// 3 & 4. A single in-progress commit.
	if has("CHERRY_PICK_HEAD") {
		return &GitOperation{Kind: "cherry-pick", Label: "Cherry-picking " + shortSHA(value("CHERRY_PICK_HEAD"))}
	}
	if has("REVERT_HEAD") {
		return &GitOperation{Kind: "revert", Label: "Reverting " + shortSHA(value("REVERT_HEAD"))}
	}

	// 5. A multi-commit sequencer with no per-commit head marker. The todo's
	// leading verb distinguishes the two; anything that is not an explicit
	// revert is treated as a pick, which is the overwhelmingly common case.
	if has("sequencer") {
		if firstTodoVerb(value("sequencer/todo")) == "revert" {
			return &GitOperation{Kind: "revert", Label: "Reverting a commit series"}
		}
		return &GitOperation{Kind: "cherry-pick", Label: "Cherry-picking a commit series"}
	}

	// 6. A merge.
	if has("MERGE_HEAD") {
		label := firstLine(value("MERGE_MSG"))
		if label == "" {
			label = "Merge in progress"
		}
		return &GitOperation{Kind: "merge", Label: label}
	}

	// 7. A bisect. BISECT_START is what git's own status code checks;
	// BISECT_LOG can outlive a finished bisect and would be a false positive.
	if has("BISECT_START") {
		return &GitOperation{Kind: "bisect", Label: "Bisecting — mark a commit good or bad to continue"}
	}

	return nil
}

// parseUnmergedPorcelain parses the unmerged records of
// `git status --porcelain=v2 -z`.
//
// Record shape (fields space-separated, records NUL-separated):
//
//	u <XY> <sub> <m1> <m2> <m3> <mW> <h1> <h2> <h3> <path>
//
// h1/h2/h3 are the object ids of index stages 1/2/3. An all-zero id means that
// stage does not exist, which is how git reports a deletion on that side, so
// the same record yields both the status code and the per-side presence.
//
// Only `u` records are read, so ordinary (`1`/`2`) and header (`#`) records are
// ignored — including a rename record's second, origin-path field, which is a
// separate NUL-terminated entry that does not carry the `u ` prefix.
//
// Verified against git: a modify/delete conflict emits `UD` with an all-zero
// h3. The same all-zero rule for the other unmerged codes is ASSUMED — it
// follows from the stage meaning, and is pinned by the parser's table test
// rather than by a live repository built for each code.
func parseUnmergedPorcelain(out string) []GitConflict {
	conflicts := []GitConflict{}
	for _, record := range strings.Split(out, "\x00") {
		if !strings.HasPrefix(record, "u ") {
			continue
		}
		// SplitN keeps a path containing spaces whole: the path is the 11th
		// field and may itself contain the separator.
		fields := strings.SplitN(record, " ", 11)
		if len(fields) < 11 {
			// A truncated record cannot be interpreted, and guessing would
			// invent a conflict. Drop it.
			continue
		}
		path := fields[10]
		if path == "" {
			continue
		}
		conflicts = append(conflicts, GitConflict{
			Path:   path,
			Code:   fields[1],
			Ours:   !isZeroObjectID(fields[8]),
			Theirs: !isZeroObjectID(fields[9]),
		})
	}
	return conflicts
}

// isZeroObjectID reports whether oid is the all-zero placeholder git uses for
// an absent index stage. An empty string is not treated as zero: a malformed
// record should not be read as a deletion.
func isZeroObjectID(oid string) bool {
	if oid == "" {
		return false
	}
	for _, r := range oid {
		if r != '0' {
			return false
		}
	}
	return true
}

// stateTargets renders the " <branch> onto <sha>" fragment shared by the
// rebase and am labels. Each half is optional so a half-written state
// directory still produces a readable label.
func stateTargets(headName, onto string) string {
	var b strings.Builder
	if branch := strings.TrimPrefix(headName, "refs/heads/"); branch != "" && branch != headName {
		b.WriteString(" " + branch)
	} else if headName != "" {
		b.WriteString(" " + headName)
	}
	if short := shortSHA(onto); short != "" {
		b.WriteString(" onto " + short)
	}
	return b.String()
}

// stateProgressSuffix renders the " (3/7)" fragment, and nothing when there
// is no usable total — showing "(0/0)" would be noise, and showing a
// half-parsed number would be a lie.
func stateProgressSuffix(step, total int) string {
	if total <= 0 {
		return ""
	}
	if step < 0 {
		step = 0
	}
	return " (" + strconv.Itoa(step) + "/" + strconv.Itoa(total) + ")"
}

// stateProgress parses a step/total pair. A non-numeric or negative value is
// reported as absent (0) rather than guessed at, so a corrupt state file
// degrades the label instead of inventing progress.
func stateProgress(stepText, totalText string) (int, int) {
	return stateNonNegativeInt(stepText), stateNonNegativeInt(totalText)
}

func stateNonNegativeInt(text string) int {
	n, err := strconv.Atoi(text)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// shortSHA abbreviates a git object id for display, and returns "" for
// anything that is not an id (so an absent or placeholder value simply drops
// out of the label).
func shortSHA(sha string) string {
	if len(sha) < 7 {
		return ""
	}
	return sha[:7]
}

// firstLine returns the first non-empty line of s.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// firstTodoVerb returns the leading whitespace-delimited token of a sequencer
// todo line ("pick", "revert", "edit", …).
func firstTodoVerb(todo string) string {
	for _, line := range strings.Split(todo, "\n") {
		if fields := strings.Fields(line); len(fields) > 0 {
			return fields[0]
		}
	}
	return ""
}

// GitConflictResolveRequest is the body of POST /api/git/conflict/resolve.
type GitConflictResolveRequest struct {
	// Path is repo-relative. The handler re-validates it; a caller-supplied
	// absolute path or one escaping the repository is rejected.
	Path string `json:"path"`
	// Resolution is one of "ours", "theirs" or "mark".
	Resolution string `json:"resolution"`
}

// conflictMarkerScanLimit bounds how much of a file is scanned for conflict
// markers when marking a file resolved. A file larger than this is refused
// rather than half-checked, because half-checking could accept a file whose
// markers sit past the limit.
const conflictMarkerScanLimit = 4 << 20

// hasConflictMarkers reports whether content still contains unresolved
// conflict markers, so "mark resolved" can refuse to stage a file the user has
// not actually resolved.
//
// Only an opening marker followed by a space, or a closing marker, counts. A
// line consisting solely of the separator run deliberately does NOT count:
// that is a Markdown or reStructuredText setext heading underline and an ASCII
// rule, and treating it as a marker would refuse ordinary prose files. Git
// itself only emits the separator inside a block that also carries the
// opening/closing markers, so the opening marker is the reliable signal.
func hasConflictMarkers(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "<<<<<<< ") || strings.HasPrefix(line, ">>>>>>> ") {
			return true
		}
	}
	return false
}

// HandleGitResolveConflict resolves one conflicted path by keeping a side or
// marking a hand-edited file resolved, then returns the refreshed workspace.
//
// The handler never trusts the request to be current: it re-detects the
// conflicted set and refuses a path that is not actually unmerged, so a stale
// panel cannot resolve something that has moved on.
func (h *Handler) HandleGitResolveConflict(w http.ResponseWriter, r *http.Request) {
	if host := hostParam(r); host != "" {
		h.remoteGitResolveConflict(w, r, host)
		return
	}
	h.gitResolveConflictLocal(w, r)
}

func (h *Handler) gitResolveConflictLocal(w http.ResponseWriter, r *http.Request) {
	var req GitConflictResolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	switch req.Resolution {
	case "ours", "theirs", "mark":
	default:
		writeError(w, http.StatusBadRequest, `resolution must be "ours", "theirs" or "mark"`)
		return
	}

	dir, ok := h.gitDirForMutation(w, r)
	if !ok {
		return
	}

	// Reuse the shared path validation so this endpoint cannot be used to
	// write anywhere the stage/discard endpoints could not.
	_, spec, err := resolveRepoPath(dir, req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if spec == "" || spec == "." {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	// Re-detect the conflict rather than trusting the request. The per-side
	// flags decide whether the chosen side can be checked out: a missing stage
	// means that side is a deletion, which `git checkout` cannot materialize.
	status := gitStatusForDir(dir)
	var conflict *GitConflict
	for i := range status.Conflicts {
		if status.Conflicts[i].Path == spec {
			conflict = &status.Conflicts[i]
			break
		}
	}
	if conflict == nil {
		writeError(w, http.StatusConflict, "path is not a conflicted file: "+spec)
		return
	}

	if req.Resolution == "mark" {
		if err := h.gitMarkResolved(w, dir, spec); err != nil {
			return
		}
		writeJSON(w, http.StatusOK, gitWorkspaceForDir(dir))
		return
	}

	var sideExists bool
	var sideFlag string
	if req.Resolution == "ours" {
		sideExists, sideFlag = conflict.Ours, "--ours"
	} else {
		sideExists, sideFlag = conflict.Theirs, "--theirs"
	}

	var args []string
	if sideExists {
		args = []string{"checkout", sideFlag, "--", spec}
	} else {
		// The chosen side is a deletion. Accepting it means removing the path
		// from the index and the working tree, which is what git rm does and
		// what `checkout --ours/--theirs` cannot do.
		args = []string{"rm", "-f", "--", spec}
	}
	if _, err := gitRunInDirLiteral(dir, args...); err != nil {
		slog.Error("git conflict resolve: side checkout failed",
			"project", dir, "path", spec, "resolution", req.Resolution, "args", args, "err", err)
		writeError(w, http.StatusInternalServerError, "git "+args[0]+" failed: "+err.Error())
		return
	}
	// `git rm` already stages the removal; checkout needs an explicit add.
	if sideExists {
		if _, err := gitRunInDirLiteral(dir, "add", "--", spec); err != nil {
			slog.Error("git conflict resolve: stage resolved side failed",
				"project", dir, "path", spec, "resolution", req.Resolution, "err", err)
			writeError(w, http.StatusInternalServerError, "git add failed: "+err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, gitWorkspaceForDir(dir))
}

// gitMarkResolved stages a hand-edited file after refusing to stage one that
// still contains conflict markers. It writes the error response and returns a
// non-nil error when the caller must stop.
func (h *Handler) gitMarkResolved(w http.ResponseWriter, dir, spec string) error {
	path := filepath.Join(dir, filepath.FromSlash(spec))
	info, err := os.Stat(path)
	if err != nil {
		// A deleted side is legitimate: the user resolved by accepting the
		// removal. There is nothing to scan, so stage the deletion.
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Error("git conflict mark: stat failed", "path", path, "err", err)
			writeError(w, http.StatusInternalServerError, "cannot read "+spec+": "+err.Error())
			return err
		}
		if _, err := gitRunInDirLiteral(dir, "add", "--", spec); err != nil {
			slog.Error("git conflict mark: stage accepted deletion failed",
				"project", dir, "path", spec, "err", err)
			writeError(w, http.StatusInternalServerError, "git add failed: "+err.Error())
			return err
		}
		return nil
	}
	if info.Size() > conflictMarkerScanLimit {
		writeError(w, http.StatusBadRequest,
			spec+" is too large to verify it has no conflict markers; resolve it with ours or theirs instead")
		return errors.New("file too large to scan for conflict markers")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		slog.Error("git conflict mark: read failed", "path", path, "err", err)
		writeError(w, http.StatusInternalServerError, "cannot read "+spec+": "+err.Error())
		return err
	}
	if hasConflictMarkers(string(content)) {
		writeError(w, http.StatusBadRequest, spec+" still contains conflict markers")
		return errors.New("conflict markers present")
	}
	if _, err := gitRunInDirLiteral(dir, "add", "--", spec); err != nil {
		slog.Error("git conflict mark: stage failed", "project", dir, "path", spec, "err", err)
		writeError(w, http.StatusInternalServerError, "git add failed: "+err.Error())
		return err
	}
	return nil
}
