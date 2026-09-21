package server

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/u007/ocode/internal/remote"
)

// This file ports the local git pipeline (gitStatusForDir /
// diffFilesForDir / gitWorkspaceForDir) to remote projects: the same git
// invocations run on the remote host through remoteWork, parsed into the
// identical response types so the web Git tab renders remote repos exactly
// like local ones.

// remoteGitStatus is gitStatusForDir over the transport. It batches all of
// the cheap status queries into one exec (one ssh round trip) and parses
// the delimited output locally.
func remoteGitStatus(ctx context.Context, rw remoteWork) GitStatus {
	status := GitStatus{
		StagedFiles:  []string{},
		ChangedFiles: []string{},
	}
	// ASCII record separator; git cannot emit it in these fields.
	const sep = "\x1e"
	script := remoteGitCommand(rw.Path, "status", "--porcelain=v2", "--branch") + "\n" +
		"echo " + sep + "\n" +
		remoteGitCommand(rw.Path, "rev-parse", "--abbrev-ref", "HEAD") + "\n" +
		"echo " + sep + "\n" +
		remoteGitCommand(rw.Path, "diff", "--name-only", "--cached") + "\n" +
		"echo " + sep + "\n" +
		remoteGitCommand(rw.Path, "diff", "--name-only") + "\n" +
		"echo " + sep + "\n" +
		remoteGitCommand(rw.Path, "status", "--porcelain", "-u") + "\n" +
		"echo " + sep + "\n" +
		remoteGitCommand(rw.Path, "rev-parse", "--is-inside-work-tree") + " 2>/dev/null || echo not-a-repo\n"
	out, err := remoteRun(ctx, rw, script)
	if err != nil {
		return status
	}
	sections := strings.Split(out, sep)
	get := func(i int) string { return strings.TrimSpace(sectionAt(sections, i)) }
	status.Branch = get(1)
	status.StagedFiles = nonEmptyLines(get(2))
	status.ChangedFiles = nonEmptyLines(get(3))
	// Untracked entries from porcelain -u ("?? path") merge into the
	// unstaged list — same behavior as the local pipeline.
	seen := map[string]bool{}
	for _, f := range status.StagedFiles {
		seen[f] = true
	}
	for _, f := range status.ChangedFiles {
		seen[f] = true
	}
	for _, line := range nonEmptyLines(get(4)) {
		if len(line) < 4 {
			continue
		}
		if !strings.Contains(line[:2], "?") {
			continue
		}
		f := strings.Trim(line[3:], `"`)
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		status.ChangedFiles = append(status.ChangedFiles, f)
	}
	status.HasChanges = len(status.StagedFiles) > 0 || len(status.ChangedFiles) > 0

	// Divergence from the porcelain=v2 --branch section.
	branchLine := get(0)
	status.HasUpstream = false
	status.Ahead = 0
	status.Behind = 0
	for _, line := range strings.Split(branchLine, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "# branch.ab ") {
			continue
		}
		parts := strings.Split(line, " ")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "+0" || p == "-0" || p == "+" || p == "-" || p == "#" || p == "branch.ab" {
				continue
			}
			if strings.HasPrefix(p, "+") {
				status.Ahead, _ = strconv.Atoi(p[1:])
				status.HasUpstream = true
			} else if strings.HasPrefix(p, "-") {
				status.Behind, _ = strconv.Atoi(p[1:])
				status.HasUpstream = true
			}
		}
		break
	}
	if branchLine == "" || !strings.Contains(branchLine, "# branch.ab") {
		upOut, upErr := remoteRun(ctx, rw, remoteGitCommand(rw.Path, "rev-parse", "--abbrev-ref", "@{upstream}"))
		upstream := strings.TrimSpace(upOut)
		if upErr == nil && upstream != "" && upstream != "HEAD" && !strings.Contains(upstream, "fatal:") && !strings.Contains(upstream, "unknown") {
			status.HasUpstream = true
		}
	}
	_, repoErr := remoteRun(ctx, rw, remoteGitCommand(rw.Path, "rev-parse", "--is-inside-work-tree"))
	status.IsRepo = err == nil && repoErr == nil
	return status
}

// sectionAt returns section i of a separator-split output, or "" past the
// end (the trailing separator yields a final empty section; defensive
// indexing keeps the parser total).
func sectionAt(sections []string, i int) string {
	if i < len(sections) {
		return sections[i]
	}
	return ""
}

// nonEmptyLines splits s into trimmed, non-empty lines.
func nonEmptyLines(s string) []string {
	out := []string{}
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// remoteGitWorkspace is gitWorkspaceForDir over the transport: status +
// staged diff + unstaged diff (with untracked patches) in one payload.
func remoteGitWorkspace(ctx context.Context, rw remoteWork) GitWorkspace {
	ws := GitWorkspace{
		Status:   remoteGitStatus(ctx, rw),
		Staged:   []GitDiffFile{},
		Unstaged: []GitDiffFile{},
	}
	if !ws.Status.IsRepo {
		return ws
	}
	ws.Staged = remoteDiffFiles(ctx, rw, true, "")
	ws.Unstaged = remoteDiffFiles(ctx, rw, false, "")
	return ws
}

// remoteDiffFiles is diffFilesForDir over the transport. Diffs travel raw
// (byte-exact) so parseUnifiedDiff behaves identically to the local path.
func remoteDiffFiles(ctx context.Context, rw remoteWork, staged bool, pathFilter string) []GitDiffFile {
	files := []GitDiffFile{}
	sep := "\x1f"
	var script strings.Builder
	script.WriteString(remoteGitCommand(rw.Path, "diff", "--no-color", "-u"))
	if staged {
		script.WriteString(" --cached")
	}
	// Only append a pathspec when a filter was given. ShellQuote("") is "''"
	// (truthy), so quoting alone can't gate this: `git diff -- ''` is an empty
	// pathspec that matches NOTHING and silently empties the whole file list.
	if pathFilter != "" {
		script.WriteString(" -- " + remoteQuoteSpecPath(pathFilter))
	}
	script.WriteString("\necho " + sep + "\n")
	if !staged {
		script.WriteString(remoteGitCommand(rw.Path, "status", "--porcelain", "-u"))
		if pathFilter != "" {
			script.WriteString(" -- " + remoteQuoteSpecPath(pathFilter))
		}
		script.WriteString("\necho " + sep + "\n")
	}
	diffOut, statusOut := "", ""
	parts := strings.Split(remoteRunTrimOrEmpty(ctx, rw, script.String()), sep)
	get := func(i int) string { return sectionAt(parts, i) }
	if staged {
		diffOut = get(0)
	} else {
		diffOut = get(0)
		statusOut = get(1)
	}
	if diffOut != "" {
		files = append(files, parseUnifiedDiff(diffOut)...)
	}
	if !staged {
		for _, line := range strings.Split(statusOut, "\n") {
			if len(line) < 4 {
				continue
			}
			statusCode := line[:2]
			filePath := strings.Trim(line[3:], `"`)
			if !strings.Contains(statusCode, "?") {
				continue
			}
			if !remoteSpecIsSafe(filePath) {
				continue
			}
			patch := remoteRunTrimOrEmpty(ctx, rw, remoteGitCommand(rw.Path, "diff", "--no-index", "/dev/null", remoteQuoteSpecPath(filePath)))
			files = append(files, GitDiffFile{Path: filePath, Status: "untracked", Patch: patch})
		}
	}
	return files
}

// remoteRunTrimOrEmpty runs the script and returns stdout with the
func remoteRunTrimOrEmpty(ctx context.Context, rw remoteWork, script string) string {
	out, err := remoteRun(ctx, rw, script)
	if err != nil {
		return ""
	}
	return out
}

// remoteQuoteSpecPath quotes a repo-relative pathspec for a remote git
// invocation. Unlike remoteAbsJoin'd filesystem paths, these are validated
// by remoteSpecIsSafe first (defense in depth: quoted anyway).
func remoteQuoteSpecPath(p string) string {
	return remote.ShellQuote(p)
}

// remoteSpecIsSafe reports whether an untracked path from remote porcelain
// output can be re-embedded in a shell command. Porcelain quotes paths with
// special characters ("..."), so any path that still contains a quote or
// control character after the trim is skipped rather than passed through.
func remoteSpecIsSafe(p string) bool {
	if p == "" {
		return false
	}
	ok, err := remoteSafeSpec(p)
	return err == nil && ok == p
}

// remoteGitLog is HandleGitLog's commit list over the transport.
func remoteGitLog(ctx context.Context, rw remoteWork, limit int) ([]GitCommit, error) {
	out, err := remoteRun(ctx, rw, remoteGitCommand(rw.Path,
		"log", "-n", strconv.Itoa(limit),
		"--pretty=format:%H%x00%h%x00%an%x00%ae%x00%aI%x00%s"))
	if err != nil {
		// Not a repo or no commits yet → empty list (never null).
		return []GitCommit{}, nil
	}
	commits := make([]GitCommit, 0, limit)
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(line, "\x00")
		if len(fields) < 6 {
			continue
		}
		commits = append(commits, GitCommit{
			Hash:    fields[0],
			Short:   fields[1],
			Author:  fields[2],
			Email:   fields[3],
			Date:    fields[4],
			Message: fields[5],
		})
	}
	return commits, nil
}

// remoteGitShow resolves a commit and returns its parsed diff — the
// HandleGitShow pipeline over the transport.
func remoteGitShow(ctx context.Context, rw remoteWork, rev string) ([]GitDiffFile, error) {
	revTrim := strings.TrimSpace(rev)
	if revTrim == "" || strings.HasPrefix(revTrim, "-") {
		return nil, fmt.Errorf("unknown commit")
	}
	// ShellQuote the rev and append ^{commit} as a fixed literal: the
	// shell concatenates 'rev'^{commit} into one argv (rev^{commit})
	// without interpreting metacharacters inside the quotes. Without
	// this, a ?commit= value like "x;touch /tmp/pwned" terminates the
	// git invocation and executes arbitrary shell on the remote host
	// (remoteGitCommand joins args verbatim — only the dir is quoted).
	quoted := remote.ShellQuote(revTrim) + "^{commit}"
	resolved, err := remoteRun(ctx, rw, remoteGitCommand(rw.Path, "rev-parse", "--verify", quoted))
	if err != nil || strings.TrimSpace(resolved) == "" {
		return nil, fmt.Errorf("unknown commit")
	}
	resolvedTrim := strings.TrimSpace(resolved)
	// The resolver must yield a hex object id; anything else is a
	// rejected lookup (or a compromised remote echoing shell). Quoted
	// anyway so the second invocation can't re-inject.
	if !isHexHash(resolvedTrim) {
		return nil, fmt.Errorf("unknown commit")
	}
	out, err := remoteRun(ctx, rw, remoteGitCommand(rw.Path, "show", "--no-color", "--format=", remote.ShellQuote(resolvedTrim)))
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(out) == "" {
		return []GitDiffFile{}, nil
	}
	return parseUnifiedDiff(out), nil
}

// isHexHash reports whether s looks like a git object id (full 40-hex or
// an unambiguous abbreviation). Used to gate remote rev-parse output
// before it is re-embedded in the follow-up `git show`.
func isHexHash(s string) bool {
	if len(s) < 4 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}

// remoteGitDiff is HandleGitDiff's file list over the transport.
func remoteGitDiff(ctx context.Context, rw remoteWork, staged bool, pathFilter string) ([]GitDiffFile, error) {
	if _, err := remoteRun(ctx, rw, remoteGitCommand(rw.Path, "rev-parse", "--git-dir")); err != nil {
		return []GitDiffFile{}, nil
	}
	return remoteDiffFiles(ctx, rw, staged, pathFilter), nil
}

// clampGitLogLimit applies the shared 1..200 log-limit bound.
func clampGitLogLimit(limit int) int {
	if limit < 1 {
		return 1
	}
	if limit > 200 {
		return 200
	}
	return limit
}

// remoteGitStashList is HandleGitStashList over the transport. A repo with no
// stashes (or a git failure such as a non-repo path) yields an empty list,
// never null; a transport failure is returned so the caller does not render
// an unreachable host as "no stashes".
func remoteGitStashList(ctx context.Context, rw remoteWork) ([]GitStash, error) {
	out, err := remoteRun(ctx, rw, remoteGitCommand(rw.Path, "stash", "list", "--format="+gitStashListFormat))
	if err != nil {
		if isRemoteTransportError(err) {
			return nil, err
		}
		return []GitStash{}, nil
	}
	return parseGitStashList(out), nil
}

// remoteGitStashShow returns one stash entry's parsed diff over the transport.
// The stash rev is built from the integer index on the server and shell-quoted
// before it reaches the remote command.
func remoteGitStashShow(ctx context.Context, rw remoteWork, index int) ([]GitDiffFile, error) {
	rev, err := stashRev(index)
	if err != nil {
		return nil, err
	}
	resolved, err := remoteRun(ctx, rw, remoteGitCommand(rw.Path, "rev-parse", "--verify", remote.ShellQuote(rev)+"^{commit}"))
	if err != nil || strings.TrimSpace(resolved) == "" {
		return nil, fmt.Errorf("unknown stash")
	}
	out, err := remoteRun(ctx, rw, remoteGitCommand(rw.Path, "stash", "show", "-p", "--no-color", "--include-untracked", remote.ShellQuote(rev)))
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(out) == "" {
		return []GitDiffFile{}, nil
	}
	return parseUnifiedDiff(out), nil
}
