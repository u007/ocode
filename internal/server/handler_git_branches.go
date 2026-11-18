package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/u007/ocode/internal/remote"
)

// GitBranch is one row of GET /api/git/branches: a local branch, a
// remote-tracking branch or a tag.
type GitBranch struct {
	Name string `json:"name"`
	// RefName is the full ref (refs/heads/x, refs/remotes/origin/x,
	// refs/tags/x). It is the exact target a create/checkout request can use,
	// so a name that collides across kinds stays unambiguous.
	RefName string `json:"refname"`
	Hash    string `json:"hash"`
	// Kind is "local", "remote" or "tag".
	Kind    string `json:"kind"`
	Current bool   `json:"current"`
	// Upstream is the short upstream ref ("origin/main"); "" when there is
	// none. Only local branches can have one.
	Upstream string `json:"upstream,omitempty"`
	// Ahead/Behind are relative to Upstream; both 0 when there is no upstream.
	Ahead  int `json:"ahead"`
	Behind int `json:"behind"`
	// UpstreamGone marks an upstream git reports as "[gone]" (deleted on the
	// remote). The branch keeps its configured upstream but has nothing to
	// compare against.
	UpstreamGone bool `json:"upstream_gone,omitempty"`
	// Remote is the remote a remote-tracking branch belongs to ("origin").
	Remote string `json:"remote,omitempty"`
	// Subject and Date describe the commit the ref points at.
	Subject string `json:"subject"`
	Date    string `json:"date"`
}

// GitBranches is the GET /api/git/branches body. Every list is non-nil so an
// empty repository marshals as [] instead of null.
type GitBranches struct {
	Local  []GitBranch `json:"local"`
	Remote []GitBranch `json:"remote"`
	Tags   []GitBranch `json:"tags"`
}

// gitBranchFormat is the for-each-ref template. Fields are NUL-separated and
// the subject comes last, so a subject holding the separator count cannot
// shift a field. %(HEAD) prints "*" on the checked-out branch.
const gitBranchFormat = "--format=%(refname)%00%(objectname)%00%(HEAD)%00%(upstream:short)%00%(upstream:track)%00%(committerdate:iso-strict)%00%(contents:subject)"

// gitBranchArgs builds the argv both transports run.
func gitBranchArgs() []string {
	return []string{"for-each-ref", gitBranchFormat, "refs/heads", "refs/remotes", "refs/tags"}
}

// emptyGitBranches is the "not a repository" answer, matching history's empty
// list rather than an error.
func emptyGitBranches() GitBranches {
	return GitBranches{Local: []GitBranch{}, Remote: []GitBranch{}, Tags: []GitBranch{}}
}

// HandleGitBranches lists local branches, remote-tracking branches and tags
// for one project. Read-only; it never touches the worktree.
func (h *Handler) HandleGitBranches(w http.ResponseWriter, r *http.Request) {
	if host := hostParam(r); host != "" {
		rw, err := h.remoteWorkFor(host, r.URL.Query().Get("project"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		branches, gerr := remoteGitBranches(r.Context(), rw)
		if gerr != nil {
			writeRemoteGitError(w, rw, gerr)
			return
		}
		writeJSON(w, http.StatusOK, branches)
		return
	}

	dir, ok := h.gitProjectDir(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown project"})
		return
	}
	out, err := gitRunInDirRaw(dir, gitBranchArgs()...)
	if err != nil {
		// Not a repository, or an unborn one: an empty view, never an error.
		writeJSON(w, http.StatusOK, emptyGitBranches())
		return
	}
	writeJSON(w, http.StatusOK, parseGitBranches(out))
}

// parseGitBranches parses one gitBranchArgs run. It never returns nil lists,
// and a line that lacks a field is skipped.
func parseGitBranches(out string) GitBranches {
	res := emptyGitBranches()
	for _, line := range strings.Split(out, "\n") {
		fields := strings.SplitN(line, "\x00", 7)
		if len(fields) < 7 {
			continue
		}
		b := GitBranch{
			RefName:  fields[0],
			Hash:     fields[1],
			Current:  strings.TrimSpace(fields[2]) == "*",
			Upstream: fields[3],
			Date:     fields[5],
			Subject:  fields[6],
		}
		b.Ahead, b.Behind, b.UpstreamGone = parseUpstreamTrack(fields[4])

		switch {
		case strings.HasPrefix(b.RefName, "refs/heads/"):
			b.Kind = "local"
			b.Name = strings.TrimPrefix(b.RefName, "refs/heads/")
			res.Local = append(res.Local, b)
		case strings.HasPrefix(b.RefName, "refs/remotes/"):
			name := strings.TrimPrefix(b.RefName, "refs/remotes/")
			// origin/HEAD is a symbolic pointer, not a branch of its own.
			if strings.HasSuffix(name, "/HEAD") {
				continue
			}
			b.Kind = "remote"
			b.Name = name
			if i := strings.IndexByte(name, '/'); i > 0 {
				b.Remote = name[:i]
			}
			res.Remote = append(res.Remote, b)
		case strings.HasPrefix(b.RefName, "refs/tags/"):
			b.Kind = "tag"
			b.Name = strings.TrimPrefix(b.RefName, "refs/tags/")
			res.Tags = append(res.Tags, b)
		}
	}
	return res
}

// parseUpstreamTrack reads for-each-ref's %(upstream:track): "[ahead 1,
// behind 2]", "[gone]", or "" when the branch has no upstream.
func parseUpstreamTrack(track string) (ahead, behind int, gone bool) {
	track = strings.TrimSpace(track)
	if track == "" {
		return 0, 0, false
	}
	if strings.Contains(track, "gone") {
		return 0, 0, true
	}
	for _, part := range strings.Split(strings.Trim(track, "[]"), ",") {
		part = strings.TrimSpace(part)
		word, num, ok := strings.Cut(part, " ")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(num))
		if err != nil {
			continue
		}
		if word == "ahead" {
			ahead = n
		} else if word == "behind" {
			behind = n
		}
	}
	return ahead, behind, false
}

// remoteGitBranches is the host-side counterpart: one for-each-ref over the
// ssh/WSL transport. The format argument carries parentheses, which the remote
// shell would read as a subshell, so it is single-quoted.
func remoteGitBranches(ctx context.Context, rw remoteWork) (GitBranches, error) {
	args := gitBranchArgs()
	out, err := remoteRun(ctx, rw, remoteGitCommand(rw.Path, append([]string{args[0], remote.ShellQuote(args[1])}, args[2:]...)...))
	if err != nil {
		// Same as local: no repository → empty list.
		return emptyGitBranches(), nil
	}
	return parseGitBranches(out), nil
}
