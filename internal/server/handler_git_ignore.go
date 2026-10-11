package server

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// HandleGitIgnore appends the requested repository-relative paths to the
// repository's root .gitignore and answers with the refreshed git status.
//
// It powers the web/desktop Git tab's row context menu ("Add to .gitignore").
// The entry targets the repository TOPLEVEL (never a nested .gitignore): the
// paths the frontend sends come from `git status --porcelain`, which always
// reports them relative to the toplevel, so a nested project would otherwise
// double-prefix. An already-present line is left alone, and the existing file
// is appended to (O_APPEND) rather than rewritten so a concurrent editor's
// save is not clobbered.
//
// ?host= selects a registered remote (SSH/WSL) project, exactly like the other
// git mutation endpoints; the .gitignore is then read/written on the host.
func (h *Handler) HandleGitIgnore(w http.ResponseWriter, r *http.Request) {
	if host := hostParam(r); host != "" {
		h.gitIgnoreRemote(w, r, host)
		return
	}
	h.gitIgnoreLocal(w, r)
}

func (h *Handler) gitIgnoreLocal(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeGitAction(w, r)
	if !ok {
		return
	}
	dir, valid := h.mutationProjectDir(r)
	if !valid {
		writeError(w, http.StatusBadRequest, "unknown project")
		return
	}
	root, err := gitRunInDir(dir, "rev-parse", "--show-toplevel")
	if err != nil || strings.TrimSpace(root) == "" {
		writeError(w, http.StatusBadRequest, "not a git repository")
		return
	}
	root = strings.TrimSpace(root)

	in := gitIgnoreInputPaths(req)
	if len(in) == 0 {
		writeError(w, http.StatusBadRequest, "no paths provided")
		return
	}
	entries := make([]string, 0, len(in))
	for _, p := range in {
		p = gitignoreUnquotePath(p)
		if p == "" {
			continue
		}
		// Resolve against the TOPLEVEL: the caller's paths are toplevel-relative
		// (git status porcelain), so using the requested (possibly nested)
		// project dir here would double-prefix the entry.
		isDir := strings.HasSuffix(p, "/")
		_, spec, perr := resolveRepoPath(root, p)
		if perr != nil {
			writeError(w, http.StatusBadRequest, perr.Error())
			return
		}
		if isDir {
			// filepath.Clean (inside resolveRepoPath) strips the trailing slash
			// git used to mark an untracked directory; restore it so the entry
			// stays a directory pattern.
			spec = strings.TrimSuffix(spec, "/") + "/"
		}
		line, lerr := gitignoreLineForPath(spec)
		if lerr != nil {
			writeError(w, http.StatusBadRequest, lerr.Error())
			return
		}
		entries = append(entries, line)
	}
	if len(entries) == 0 {
		writeError(w, http.StatusBadRequest, "no paths provided")
		return
	}

	gi := filepath.Join(root, ".gitignore")
	existing, readErr := os.ReadFile(gi)
	if readErr != nil && !os.IsNotExist(readErr) {
		writeError(w, http.StatusInternalServerError, "read .gitignore failed: "+readErr.Error())
		return
	}
	payload, _ := buildGitignoreAppend(existing, entries)
	if len(payload) > 0 {
		f, openErr := os.OpenFile(gi, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if openErr != nil {
			writeError(w, http.StatusInternalServerError, "write .gitignore failed: "+openErr.Error())
			return
		}
		if _, wErr := f.Write(payload); wErr != nil {
			_ = f.Close()
			writeError(w, http.StatusInternalServerError, "write .gitignore failed: "+wErr.Error())
			return
		}
		if cErr := f.Close(); cErr != nil {
			writeError(w, http.StatusInternalServerError, "write .gitignore failed: "+cErr.Error())
			return
		}
	}
	writeLocalGitStatus(w, root)
}

func (h *Handler) gitIgnoreRemote(w http.ResponseWriter, r *http.Request, host string) {
	req, ok := decodeGitAction(w, r)
	if !ok {
		return
	}
	rw, ok := h.remoteGitDirForMutation(w, r, host)
	if !ok {
		return
	}
	in := gitIgnoreInputPaths(req)
	if len(in) == 0 {
		writeError(w, http.StatusBadRequest, "no paths provided")
		return
	}
	entries := make([]string, 0, len(in))
	for _, p := range in {
		p = gitignoreUnquotePath(p)
		if p == "" {
			continue
		}
		abs := remoteAbsJoin(rw.Path, p)
		rel, rerr := remoteRelCheck(rw.Path, abs)
		if rerr != nil {
			writeError(w, http.StatusBadRequest, rerr.Error())
			return
		}
		if rel == "." {
			continue
		}
		line, lerr := gitignoreLineForPath(rel)
		if lerr != nil {
			writeError(w, http.StatusBadRequest, lerr.Error())
			return
		}
		entries = append(entries, line)
	}
	if len(entries) == 0 {
		writeError(w, http.StatusBadRequest, "no paths provided")
		return
	}

	gi := remoteAbsJoin(rw.Path, ".gitignore")
	// Remote has no O_APPEND primitive; read-modify-write through the
	// transport (the payload travels on stdin, never interpolated).
	existing, _, rerr := remoteReadFile(r.Context(), rw, gi)
	if rerr != nil {
		writeError(w, http.StatusInternalServerError, "read remote .gitignore failed: "+rerr.Error())
		return
	}
	payload, _ := buildGitignoreAppend(existing, entries)
	if len(payload) > 0 {
		if werr := remoteWriteFile(r.Context(), rw, gi, payload); werr != nil {
			writeError(w, http.StatusInternalServerError, "write remote .gitignore failed: "+werr.Error())
			return
		}
	}
	writeRemoteGitStatus(w, r.Context(), rw)
}

// gitIgnoreInputPaths merges the request's Paths list with the single-path
// convenience alias, mirroring prepareGitActionFor.
func gitIgnoreInputPaths(req gitActionRequest) []string {
	in := make([]string, 0, len(req.Paths)+1)
	in = append(in, req.Paths...)
	if req.Path != "" {
		in = append(in, req.Path)
	}
	return in
}

// buildGitignoreAppend returns the bytes to append to a .gitignore so that
// every entry becomes a line, skipping entries already present and preserving
// the file's existing line ending. It never rewrites existing content. added
// is the subset that was actually appended.
func buildGitignoreAppend(existing []byte, entries []string) (payload []byte, added []string) {
	seen := make(map[string]bool, len(entries))
	for _, line := range strings.Split(string(existing), "\n") {
		seen[strings.TrimRight(line, "\r")] = true
	}
	eol := "\n"
	if bytes.Contains(existing, []byte("\r\n")) {
		eol = "\r\n"
	}
	var buf bytes.Buffer
	for _, e := range entries {
		e = strings.TrimRight(e, "\r")
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		if buf.Len() == 0 && len(existing) > 0 && !bytes.HasSuffix(existing, []byte("\n")) {
			// The last existing line has no terminator — add one before ours.
			buf.WriteString(eol)
		}
		buf.WriteString(e)
		buf.WriteString(eol)
		added = append(added, e)
	}
	return buf.Bytes(), added
}

// gitignoreLineForPath turns a repository-relative path into a literal,
// toplevel-anchored .gitignore pattern. The leading "/" pins the pattern to
// the repo root (a bare path matches at any depth, which is wrong for our
// toplevel-relative paths); once anchored, a leading "#"/"!" is no longer
// special. Glob metacharacters and spaces are backslash-escaped so the entry
// matches the literal filename. A trailing slash is preserved so a collapsed
// untracked directory stays a directory pattern.
func gitignoreLineForPath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("empty path")
	}
	if strings.ContainsAny(p, "\n\r") {
		return "", fmt.Errorf("path contains a newline")
	}
	p = filepath.ToSlash(p)
	trailing := strings.HasSuffix(p, "/")
	p = strings.TrimSuffix(p, "/")
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "/")
	if p == "" || p == "." {
		return "", fmt.Errorf("invalid path")
	}
	var b strings.Builder
	b.WriteByte('/')
	for _, r := range p {
		switch r {
		case '\\', '*', '?', '[', ']', ' ':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	if trailing {
		b.WriteByte('/')
	}
	return b.String(), nil
}

// gitignoreUnquotePath decodes a path that git C-quoted in `git status
// --porcelain` output (names containing spaces, quotes, backslashes or
// non-ASCII bytes are emitted as a double-quoted string with backslash/octal
// escapes). A path that is not quoted is returned unchanged.
func gitignoreUnquotePath(p string) string {
	if len(p) < 2 || p[0] != '"' || p[len(p)-1] != '"' {
		return p
	}
	body := p[1 : len(p)-1]
	var b strings.Builder
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c != '\\' || i+1 >= len(body) {
			b.WriteByte(c)
			continue
		}
		i++
		switch body[i] {
		case 'a':
			b.WriteByte('\a')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'v':
			b.WriteByte('\v')
		case '\\', '"':
			b.WriteByte(body[i])
		default:
			if body[i] >= '0' && body[i] <= '7' {
				val := int(body[i] - '0')
				for n := 1; n < 3 && i+1 < len(body) && body[i+1] >= '0' && body[i+1] <= '7'; n++ {
					i++
					val = val*8 + int(body[i]-'0')
				}
				b.WriteByte(byte(val))
			} else {
				b.WriteByte(body[i])
			}
		}
	}
	return b.String()
}
