package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/u007/ocode/internal/remote"
)

// fsUploadMaxBytes caps one locally-pasted file. Finder/Explorer paste sends
// the file's bytes, so this is the only limit protecting the server from a
// multi-gigabyte drop; 512 MiB covers screenshots, PDFs and archives while
// still being bounded. A var (not a const) so a test can lower it instead of
// allocating half a gigabyte.
var fsUploadMaxBytes int64 = 512 << 20

// fsUploadRemoteMaxBytes is deliberately lower: the bytes cross an ssh stdin
// pipe and occupy one long-running exec slot for their duration, so a huge
// upload starves other remote work for that host.
const fsUploadRemoteMaxBytes = 100 << 20

// fsUploadIdleTimeout bounds how long a remote upload may go without a byte
// arriving. An idle bound (rather than a fixed total) is what makes a slow but
// healthy link work while a dead one fails promptly.
const fsUploadIdleTimeout = 60 * time.Second

// fsUploadOverallTimeout is the backstop for the whole remote transfer.
const fsUploadOverallTimeout = 30 * time.Minute

// validUploadName rejects names that cannot be written safely or portably.
// A paste carries a filename from the OS clipboard, so this is the boundary
// between untrusted input and a file path: no separators (never a path), no
// traversal, no NUL, no characters Windows cannot represent (a project is
// often edited from more than one platform), and no Windows device names.
// Unicode is NOT normalized here — the OS handles APFS/NTFS equivalence, and
// os.O_EXCL is what actually prevents a collision from clobbering a file.
func validUploadName(name string) error {
	if name == "" {
		return errors.New("name is required")
	}
	if len(name) > 255 {
		return errors.New("name is too long")
	}
	if name == "." || name == ".." {
		return errors.New(`name must not be "." or ".."`)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return errors.New("name contains a control character")
		}
	}
	if strings.ContainsAny(name, `/\`) {
		return errors.New("name must not contain a path separator")
	}
	if strings.ContainsAny(name, `:*?"<>|`) {
		return errors.New(`name must not contain any of : * ? " < > |`)
	}
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return errors.New("name must not end with a dot or a space")
	}
	// Windows reserves the device name before the FIRST dot, so NUL.tar.gz and
	// CON.backup.txt are reserved too.
	stem := strings.ToUpper(name)
	if i := strings.Index(stem, "."); i >= 0 {
		stem = stem[:i]
	}
	switch stem {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return fmt.Errorf("%q is a reserved device name", name)
	}
	return nil
}

// HandleFSUpload writes one file's bytes into a directory inside a project.
// The body is the raw file (application/octet-stream), not JSON: pasted files
// can be binary, and the existing /api/files/content save path takes a JSON
// string. Admission is the same registered-project rule as every other fs
// mutation, keyed by ?project= (+ ?host= for a remote project).
func (h *Handler) HandleFSUpload(w http.ResponseWriter, r *http.Request) {
	// A raw-bytes body with a non-JSON type is a CORS "simple request" when
	// sent as text/plain: no preflight, so a foreign page could write files.
	// application/octet-stream forces a preflight that corsMiddleware denies
	// for non-allowed origins.
	if r.Header.Get("Content-Type") != "application/octet-stream" {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/octet-stream")
		return
	}
	if host := hostParam(r); host != "" {
		h.fsUploadRemote(w, r, host)
		return
	}
	h.fsUploadLocal(w, r)
}

func (h *Handler) fsUploadLocal(w http.ResponseWriter, r *http.Request) {
	dir, ok := h.mutationProjectDir(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown project")
		return
	}
	destDir := r.URL.Query().Get("dest_dir")
	name := r.URL.Query().Get("name")
	if destDir == "" {
		writeError(w, http.StatusBadRequest, "dest_dir is required")
		return
	}
	if err := validUploadName(name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	dest, err := resolveFSPath(dir, destDir, "read")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// resolveFSPath only proves the path exists; an upload target must be a
	// directory (the sibling copy/move endpoints learn this from their os.Stat
	// calls, but here it is the difference between a file and a folder).
	info, statErr := os.Stat(dest)
	if statErr != nil && !os.IsNotExist(statErr) {
		writeError(w, http.StatusInternalServerError, "failed to stat dest_dir: "+statErr.Error())
		return
	}
	if statErr != nil || !info.IsDir() {
		writeError(w, http.StatusBadRequest, "dest_dir is not a directory")
		return
	}

	// O_EXCL is what makes this safe: it never follows an existing symlink and
	// never truncates a file, so uniqueDest's probe and this create can disagree
	// harmlessly — we just try the next free name. (os.Rename would silently
	// overwrite, which is why the upload does not stage-then-rename.)
	var (
		target string
		file   *os.File
	)
	for attempt := 0; attempt < 16; attempt++ {
		cand := uniqueDest(dest, name)
		f, openErr := os.OpenFile(cand, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if openErr == nil {
			target, file = cand, f
			break
		}
		if !errors.Is(openErr, os.ErrExist) {
			writeError(w, http.StatusInternalServerError, "failed to create file: "+openErr.Error())
			return
		}
	}
	if file == nil {
		writeError(w, http.StatusConflict, "could not find a free file name")
		return
	}

	body := http.MaxBytesReader(w, r.Body, fsUploadMaxBytes)
	if _, copyErr := io.Copy(file, body); copyErr != nil {
		if closeErr := file.Close(); closeErr != nil {
			slog.Warn("fs upload: close after failed copy failed", "path", target, "err", closeErr)
		}
		removePartialUpload(target) // never leave a truncated file behind
		var maxErr *http.MaxBytesError
		if errors.As(copyErr, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "file is too large to paste")
			return
		}
		writeError(w, http.StatusBadRequest, "upload failed: "+copyErr.Error())
		return
	}
	if closeErr := file.Close(); closeErr != nil {
		removePartialUpload(target)
		writeError(w, http.StatusInternalServerError, "failed to finish file: "+closeErr.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "path": target})
}

// removePartialUpload deletes a file this upload created but could not finish.
// A failure is logged, not returned: the caller is already reporting the upload
// error, and the stray file is what the log line is for.
func removePartialUpload(target string) {
	if err := os.Remove(target); err != nil {
		slog.Warn("fs upload: remove partial file failed", "path", target, "err", err)
	}
}

func (h *Handler) fsUploadRemote(w http.ResponseWriter, r *http.Request, host string) {
	rw, err := h.remoteMutationWorkFor(r.Context(), host, r.URL.Query().Get("project"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	destDir := r.URL.Query().Get("dest_dir")
	name := r.URL.Query().Get("name")
	if destDir == "" {
		writeError(w, http.StatusBadRequest, "dest_dir is required")
		return
	}
	if err := validUploadName(name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	destAbs, ok := h.remoteUploadDestDir(w, rw, destDir)
	if !ok {
		return
	}
	target := remoteUniqueDest(r.Context(), rw, destAbs, name)
	body := http.MaxBytesReader(w, r.Body, fsUploadRemoteMaxBytes)
	created, err := remoteStreamFile(r.Context(), rw, target, body)
	if err != nil {
		// Only a file this upload created may be removed: `created` is set from the
		// remote CREATED marker, so a name another writer owns is never deleted.
		// Best effort, and on a context that is NOT the request's: the request
		// may be what failed, but a partial file on the host still must go.
		if created {
			if _, rmErr := remoteRun(context.WithoutCancel(r.Context()), rw, "rm -f -- "+remote.ShellQuotePath(target)); rmErr != nil {
				slog.Warn("fs upload: remove partial remote file failed", "host", host, "path", target, "err", rmErr)
			}
		}
		if errors.Is(err, errUploadNameTaken) {
			writeError(w, http.StatusConflict, "destination name is occupied by a file or link; paste again")
			return
		}
		if isRemoteExecRefusal(err) {
			w.Header().Set("Retry-After", "5")
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		if isUploadTooLarge(err) {
			writeError(w, http.StatusRequestEntityTooLarge, "file is too large to paste to a remote project")
			return
		}
		writeError(w, http.StatusInternalServerError, "upload failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "path": target})
}

// remoteUploadDestDir resolves the PASTE destination on the remote host. It
// differs from remoteTargetOf in exactly one case: the project root itself is
// a valid destination ("paste to the root folder"), while remoteTargetOf
// rejects the root for every other operation because "." would let a
// delete/rename target the whole project. The result is also required to be a
// directory — `cat >` a directory path would fail with a confusing error.
func (h *Handler) remoteUploadDestDir(w http.ResponseWriter, rw remoteWork, p string) (string, bool) {
	if p == "" {
		writeError(w, http.StatusBadRequest, "dest_dir is required")
		return "", false
	}
	ctx := contextBackground()
	abs := remoteAbsJoin(rw.Path, p)
	atRoot := strings.TrimRight(abs, "/") == strings.TrimRight(rw.Path, "/")
	if !atRoot {
		resolved, _, ok := h.remoteTargetOf(w, rw, p, "read")
		if !ok {
			return "", false
		}
		abs = resolved
	}
	if !remoteExists(ctx, rw, abs) {
		writeError(w, http.StatusBadRequest, "dest_dir does not exist")
		return "", false
	}
	if !remoteStatIsDir(ctx, rw, abs) {
		writeError(w, http.StatusBadRequest, "dest_dir is not a directory")
		return "", false
	}
	return abs, true
}

// isRemoteExecRefusal reports the long-running exec pool refusing admission.
// That is a capacity answer, not a transport fault: the caller should retry
// later (503 + Retry-After), never treat the project as broken.
func isRemoteExecRefusal(err error) bool {
	return err != nil && strings.Contains(err.Error(), "remote command refused")
}

// isUploadTooLarge unwraps an oversized body that failed while being piped into
// the ssh child. The stdin copy error surfaces through exec's Wait, so the
// concrete *http.MaxBytesError may be reachable only as text.
func isUploadTooLarge(err error) bool {
	if err == nil {
		return false
	}
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return true
	}
	return strings.Contains(err.Error(), "request body too large")
}

// errUploadNameTaken means the remote name was occupied when the upload tried to
// claim it. The probe in remoteUniqueDest uses `[ -e ]`, which is false for a
// dangling symlink, so the name can look free and still be occupied.
var errUploadNameTaken = errors.New("destination name is occupied")

// remoteExclusiveWriteScript claims abs and streams stdin into it, in one
// remote command. The `[ -e ] || [ -L ]` pre-check refuses any existing entry,
// including a dangling symlink that `cat >` would write through. `set -C`
// (noclobber) then makes the open itself O_EXCL, which closes the race between
// check and open and refuses the non-regular entries the check does not name.
// CREATED is printed only after the open succeeds, so the caller knows the file
// is its own before any byte moves and may remove it on failure. No retry: stdin
// may already be partly consumed by the time the remote open fails.
func remoteExclusiveWriteScript(abs string) string {
	return "p=" + remote.ShellQuotePath(abs) + "; " +
		`if [ -e "$p" ] || [ -L "$p" ]; then echo EXISTS; exit 0; fi; ` +
		`set -C; exec 3>"$p" && echo CREATED && exec cat >&3`
}

// hasLine reports whether out contains marker as a whole line, so stray output
// (a login banner, say) cannot be mistaken for a marker.
func hasLine(out, marker string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == marker {
			return true
		}
	}
	return false
}

// remoteStreamFile streams src into a file it creates exclusively at abs on the
// remote host. created reports whether this call created the file, which is
// the only condition under which the caller may remove it. Distinct from
// remoteWriteFile, which buffers a whole []byte payload under the 30s foreground
// bound: this one streams, gets an idle+overall bound, and takes a long-running
// exec slot so a big paste cannot hold up git polling.
func remoteStreamFile(ctx context.Context, rw remoteWork, abs string, src io.Reader) (created bool, err error) {
	cmd, err := remote.ExecCommand(rw.Target, remoteExclusiveWriteScript(abs))
	if err != nil {
		return false, &remoteTransportError{msg: err.Error()}
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	idle := time.AfterFunc(fsUploadIdleTimeout, cancel)
	defer idle.Stop()
	cmd.Stdin = &uploadProgressReader{r: src, onProgress: func() { idle.Reset(fsUploadIdleTimeout) }}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := runBounded(ctx, rw.Target, cmd, fsUploadOverallTimeout, remoteExecLongRunning)
	if hasLine(stdout.String(), "EXISTS") {
		return false, errUploadNameTaken
	}
	created = hasLine(stdout.String(), "CREATED")
	if runErr == nil {
		if !created {
			return false, errors.New("remote create reported success without CREATED marker")
		}
		return true, nil
	}
	if isRemoteTransportError(runErr) {
		return created, runErr
	}
	msg := strings.TrimSpace(stderr.String())
	if msg == "" {
		msg = runErr.Error()
	}
	return created, fmt.Errorf("%s", msg)
}

// uploadProgressReader resets the remote upload's idle watchdog as bytes flow.
type uploadProgressReader struct {
	r          io.Reader
	onProgress func()
}

func (u *uploadProgressReader) Read(p []byte) (int, error) {
	n, err := u.r.Read(p)
	if n > 0 && u.onProgress != nil {
		u.onProgress()
	}
	return n, err
}
