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
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/u007/ocode/internal/remote"
)

// fsDownloadMaxBytes caps one locally-downloaded file. The bytes are streamed,
// so the cap bounds transfer time rather than memory; it mirrors the local
// upload cap. A var so a test can lower it.
var fsDownloadMaxBytes int64 = 512 << 20

// fsDownloadRemoteMaxBytes mirrors the remote upload cap: the bytes cross an ssh
// stdout pipe and occupy one long-running exec slot for their duration. A var
// so a test can lower it.
var fsDownloadRemoteMaxBytes int64 = 100 << 20

// fsDownloadIdleTimeout bounds how long a remote download may go without a byte
// arriving. An idle bound is what lets a slow but healthy link finish while a
// dead one fails promptly.
const fsDownloadIdleTimeout = 60 * time.Second

// fsDownloadOverallTimeout is the backstop for the whole remote transfer.
const fsDownloadOverallTimeout = 30 * time.Minute

// HandleFSDownload streams one regular file from inside a project as an
// attachment, so the Files tree can save it to the user's machine. It is the
// same project-containment rule as the tree's other fs endpoints; it is NOT
// /api/files/raw, whose extension allowlist exists for previews. Like
// /api/files/content it returns any readable file inside the project, so a
// download grants no access the tree's existing read paths do not already grant.
func (h *Handler) HandleFSDownload(w http.ResponseWriter, r *http.Request) {
	if host := hostParam(r); host != "" {
		h.fsDownloadRemote(w, r, host)
		return
	}
	h.fsDownloadLocal(w, r)
}

func (h *Handler) fsDownloadLocal(w http.ResponseWriter, r *http.Request) {
	dir, ok := h.mutationProjectDir(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown project")
		return
	}
	p := r.URL.Query().Get("path")
	resolved, err := resolveFSPath(dir, p, "read")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := os.Stat(resolved)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to stat file: "+err.Error())
		return
	}
	if !info.Mode().IsRegular() {
		writeError(w, http.StatusBadRequest, "only regular files can be downloaded")
		return
	}
	if info.Size() > fsDownloadMaxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "file is too large to download")
		return
	}
	f, err := os.Open(resolved)
	if err != nil {
		if os.IsPermission(err) {
			writeError(w, http.StatusForbidden, "file is not readable")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to open file: "+err.Error())
		return
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			slog.Warn("fs download: close failed", "path", resolved, "err", closeErr)
		}
	}()
	writeDownloadHeaders(w, filepath.Base(filepath.Clean(p)), info.Size())
	w.WriteHeader(http.StatusOK)
	if _, copyErr := io.Copy(w, f); copyErr != nil {
		// Headers are already out, so the status cannot change; the short body
		// is what tells the client the transfer failed.
		slog.Warn("fs download: stream failed", "path", resolved, "err", copyErr)
	}
}

// fsDownloadRemote streams `cat` over ssh. The size is stat'ed first so the
// Content-Length is exact, the cap is enforced before any bytes move, and a
// failure before the first byte still gets a JSON error instead of a 200 with
// an empty body. Once bytes are out the status is committed; a mid-transfer
// failure is logged and the short body tells the client.
func (h *Handler) fsDownloadRemote(w http.ResponseWriter, r *http.Request, host string) {
	rw, err := h.remoteWorkFor(host, r.URL.Query().Get("project"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	abs, _, ok := h.remoteTargetOf(w, rw, r.URL.Query().Get("path"), "read")
	if !ok {
		return
	}
	size, found, err := remoteFileSize(r.Context(), rw, abs)
	if err != nil {
		if errors.Is(err, errRemoteIsDir) {
			writeError(w, http.StatusBadRequest, "only regular files can be downloaded")
			return
		}
		writeRemoteDownloadError(w, err)
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	if size > fsDownloadRemoteMaxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "file is too large to download from a remote project")
		return
	}

	cmd, err := remote.ExecCommand(rw.Target, "cat -- "+remote.ShellQuote(abs))
	if err != nil {
		writeRemoteDownloadError(w, &remoteTransportError{msg: err.Error()})
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	idle := time.AfterFunc(fsDownloadIdleTimeout, cancel)
	defer idle.Stop()
	stream := &fsDownloadStream{
		w:       w,
		name:    path.Base(abs),
		size:    size,
		onBytes: func() { idle.Reset(fsDownloadIdleTimeout) },
	}
	var stderr bytes.Buffer
	cmd.Stdout = stream
	cmd.Stderr = &stderr
	runErr := runBounded(ctx, rw.Target, cmd, fsDownloadOverallTimeout, remoteExecLongRunning)
	if runErr != nil {
		if !stream.started {
			if !isRemoteTransportError(runErr) && strings.TrimSpace(stderr.String()) != "" {
				runErr = errors.New(strings.TrimSpace(stderr.String()))
			}
			writeRemoteDownloadError(w, runErr)
			return
		}
		slog.Warn("fs download: remote stream failed mid-transfer", "host", host, "path", abs, "sent", stream.written, "size", size, "err", runErr)
		return
	}
	stream.start()
	if stream.written != size {
		slog.Warn("fs download: remote file size changed during transfer", "host", host, "path", abs, "sent", stream.written, "stat_size", size)
	}
}

// fsDownloadStream is the stdout sink for the remote `cat`. Headers go out on
// the first byte (or on success for an empty file), never before, so an error
// that happens before any data can still be answered with a JSON status.
type fsDownloadStream struct {
	w       http.ResponseWriter
	name    string
	size    int64
	started bool
	written int64
	onBytes func()
}

func (s *fsDownloadStream) start() {
	if s.started {
		return
	}
	s.started = true
	writeDownloadHeaders(s.w, s.name, s.size)
	s.w.WriteHeader(http.StatusOK)
}

func (s *fsDownloadStream) Write(p []byte) (int, error) {
	s.start()
	s.onBytes()
	n, err := s.w.Write(p)
	s.written += int64(n)
	return n, err
}

// writeRemoteDownloadError maps a remote failure that happened before any byte
// was sent onto a status: capacity refusal is 503 + Retry-After, a dead transport
// is 502, anything else is 500.
func writeRemoteDownloadError(w http.ResponseWriter, err error) {
	switch {
	case isRemoteExecRefusal(err):
		w.Header().Set("Retry-After", "5")
		writeError(w, http.StatusServiceUnavailable, err.Error())
	case isRemoteTransportError(err):
		writeError(w, http.StatusBadGateway, "remote read failed: "+err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "download failed: "+err.Error())
	}
}

func writeDownloadHeaders(w http.ResponseWriter, name string, size int64) {
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Length", strconv.FormatInt(size, 10))
	h.Set("Content-Disposition", attachmentDisposition(name))
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
}

// attachmentDisposition builds an attachment header with an ASCII fallback
// filename and an RFC 5987 filename* carrying the real name, so non-ASCII and
// quote-containing names survive the download.
func attachmentDisposition(name string) string {
	fallback := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, fallback, rfc5987Escape(name))
}

// rfc5987Escape percent-encodes every byte outside the RFC 5987 attr-char set.
func rfc5987Escape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isRFC5987AttrChar(c) {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

func isRFC5987AttrChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return strings.IndexByte("!#$&+-.^_`|~", c) >= 0
}
