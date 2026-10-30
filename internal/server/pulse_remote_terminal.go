//go:build !windows

package server

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/u007/ocode/internal/termtabs"
)

// pulseHistoryReader reads [offset, offset+limit) of a terminal's log and returns
// the data with the log's snapshot end. snapshotEnd, when non-nil, pins the read
// to that snapshot. A local log reads the file; a remote one asks its host, so the
// window and screen code reads both the same way.
type pulseHistoryReader func(offset, limit int64, snapshotEnd *int64) ([]byte, int64, error)

// localPulseHistoryReader reads a local project's terminal log.
func localPulseHistoryReader(project, terminalID string) pulseHistoryReader {
	return func(offset, limit int64, snapshotEnd *int64) ([]byte, int64, error) {
		return readTerminalHistoryRangeAt(project, terminalID, offset, limit, snapshotEnd)
	}
}

// pulseRemoteProjectPaths is one remote project as its two sides name it.
type pulseRemoteProjectPaths struct {
	// Saved is the project as the operator saved it ("~/app" or an absolute path).
	// It keys the shared tab store and is what the browser proxy registers on the host.
	Saved string
	// Host is the same project as the host names it: Saved with "~" expanded by the
	// host. The host's terminal and history endpoints take this path.
	Host string
}

// pulseRemoteTerminalProject finds the project a remote terminal belongs to and
// registers it on the host. The shared tab store is searched first (key
// "<host>::<path>"), else the host's own live rows. The (host, path) pair must be a
// saved remote project, the same admission HandleRemoteProxy applies, so the tool
// cannot be aimed at a host or path the operator never added. The host must hold a
// live connection: a remote read never dials.
func (h *Handler) pulseRemoteTerminalProject(host, terminalID string) (pulseRemoteProjectPaths, error) {
	if _, ok := h.firstSavedProjectForHost(host); !ok {
		return pulseRemoteProjectPaths{}, fmt.Errorf("%w: %q is not the host of a saved project (see project_list)", errPulseTerminalForeign, host)
	}
	ws, ok := h.pulseRemoteWorkspace(host)
	if !ok {
		return pulseRemoteProjectPaths{}, pulseRemoteNotConnected(host)
	}
	// A saved "~" project matches a host path only through the expansion the host
	// reports when the project is registered. Register them before matching, so a
	// terminal is still found after this server restarts.
	if err := h.pulseRemoteRegisterTilde(ws, host); err != nil {
		return pulseRemoteProjectPaths{}, err
	}
	path := ""
	if h.termTabsStore != nil {
		all := h.termTabsStore.All()
		keys := make([]string, 0, len(all))
		for k := range all {
			keys = append(keys, k)
		}
		sort.Strings(keys)
	search:
		for _, k := range keys {
			kh, kp := h.pulseSplitTabKey(k)
			if kh != host {
				continue
			}
			for _, t := range all[k].Terminals {
				if t.ID == terminalID {
					path = kp
					break search
				}
			}
		}
	}
	if path == "" {
		rows, err := h.pulseRemoteLive(host)
		if err != nil {
			return pulseRemoteProjectPaths{}, err
		}
		for _, r := range rows {
			if r.ID == terminalID {
				path = r.Project
				break
			}
		}
	}
	if path == "" {
		return pulseRemoteProjectPaths{}, fmt.Errorf("%w: %q on host %q (see terminal_tabs for open terminals)", errPulseTerminalNotFound, terminalID, host)
	}
	saved, ok := h.pulseRemoteSavedPath(host, path)
	if !ok {
		return pulseRemoteProjectPaths{}, fmt.Errorf("%w: terminal %q belongs to %q on %q, which is not a saved project", errPulseTerminalForeign, terminalID, path, host)
	}
	if err := h.pulseRemoteRegister(ws, host, saved); err != nil {
		return pulseRemoteProjectPaths{}, err
	}
	hostPath, ok := h.pulseRemoteHostPath(host, saved)
	if !ok {
		return pulseRemoteProjectPaths{}, fmt.Errorf("host %s did not report where %q is stored, so the terminal's log cannot be located (update the host)", host, saved)
	}
	return pulseRemoteProjectPaths{Saved: saved, Host: hostPath}, nil
}

// pulseRemoteSavedPath maps a path on a host to the saved project that owns it and
// returns the saved path. The path must equal the saved path exactly, or equal the
// host path that a saved "~" project expanded to (pulseRemoteHostPath). A host
// reports the expanded form (/home/me/app) while the saved project keeps what the
// operator typed (~/app), and the host's report of that expansion is the only link
// between them. A suffix match is not one: it would take /var/www/app for ~/app. A
// "~" project whose expansion is not known yet matches nothing.
func (h *Handler) pulseRemoteSavedPath(host, path string) (string, bool) {
	if h.projects == nil {
		return "", false
	}
	for _, p := range h.projects.List() {
		if p.Host != host {
			continue
		}
		if p.Path == path {
			return p.Path, true
		}
		if hostPath, ok := h.pulseRemoteHostPath(host, p.Path); ok && hostPath == path {
			return p.Path, true
		}
	}
	return "", false
}

// pulseRemoteHostPath is the path a saved remote project has on its host: the same
// path for an absolute one, the host's expansion for a "~" one. ok is false for a
// "~" project the host has not reported an expansion for, which is not known until
// the project is registered on the host.
func (h *Handler) pulseRemoteHostPath(host, saved string) (string, bool) {
	if !strings.HasPrefix(saved, "~") {
		return saved, true
	}
	if h.remoteHosts == nil {
		return "", false
	}
	return h.remoteHosts.expandedPath(host, saved)
}

// pulseRemoteRegister registers a saved project on the host, as the browser proxy
// does on first use. The host's terminal and history endpoints admit only
// registered projects. The host's answer is what names a "~" project's host path.
func (h *Handler) pulseRemoteRegister(ws remoteHostWorkspace, host, saved string) error {
	ctx, cancel := context.WithTimeout(context.Background(), pulseRemoteTimeout)
	defer cancel()
	if err := ensureRemoteProject(ctx, ws, h.remoteHosts, host, h.pulseRemotePort(host), saved); err != nil {
		return fmt.Errorf("register project %q on %s: %w", saved, host, err)
	}
	return nil
}

// pulseRemoteRegisterTilde registers every saved "~" project of host whose host path
// is not known yet. Only that registration tells us the expansion, so this lets a
// live terminal be matched to its saved "~" project even after a restart of this
// server.
func (h *Handler) pulseRemoteRegisterTilde(ws remoteHostWorkspace, host string) error {
	if h.projects == nil {
		return nil
	}
	for _, p := range h.projects.List() {
		if p.Host != host || !strings.HasPrefix(p.Path, "~") {
			continue
		}
		if _, ok := h.pulseRemoteHostPath(host, p.Path); ok {
			continue
		}
		if err := h.pulseRemoteRegister(ws, host, p.Path); err != nil {
			return err
		}
	}
	return nil
}

// pulseRemoteHistoryReader reads a remote terminal's log through the host's own
// GET /api/terminal/{id}/history. project is the host's path for the project
// (pulseRemoteProjectPaths.Host), which is the path the endpoint looks up.
func (h *Handler) pulseRemoteHistoryReader(host, project, terminalID string) pulseHistoryReader {
	port := h.pulseRemotePort(host)
	return func(offset, limit int64, snapshotEnd *int64) ([]byte, int64, error) {
		q := url.Values{
			"project_path": {project},
			"offset":       {fmt.Sprint(offset)},
			"limit":        {fmt.Sprint(limit)},
		}
		if snapshotEnd != nil {
			q.Set("snapshot_end", fmt.Sprint(*snapshotEnd))
		}
		var resp terminalHistoryResponse
		path := "/api/terminal/" + url.PathEscape(terminalID) + "/history"
		if err := h.pulseRemoteGetJSON(host, port, path, q, &resp); err != nil {
			var se *pulseRemoteStatusError
			if errors.As(err, &se) {
				switch se.Status {
				case http.StatusNotFound:
					return nil, 0, errTerminalHistoryMissing
				case http.StatusConflict:
					return nil, 0, errTerminalHistoryChanged
				case http.StatusRequestedRangeNotSatisfiable:
					return nil, 0, errTerminalHistoryPastEnd
				}
			}
			return nil, 0, err
		}
		data, err := base64.StdEncoding.DecodeString(resp.Data)
		if err != nil {
			return nil, 0, fmt.Errorf("host %s: decode history: %w", host, err)
		}
		return data, resp.SnapshotEnd, nil
	}
}

// pulseRemoteTerminalSource resolves a remote terminal to its saved project and log
// reader. The project returned is the saved path the operator knows. The reader
// asks the host for the log under the host's own path of that project.
func (h *Handler) pulseRemoteTerminalSource(host, terminalID string) (string, pulseHistoryReader, error) {
	paths, err := h.pulseRemoteTerminalProject(host, terminalID)
	if err != nil {
		return "", nil, err
	}
	return paths.Saved, h.pulseRemoteHistoryReader(host, paths.Host, terminalID), nil
}

// --- remote terminal_new / terminal_kill ---

// pulseRemoteTerminalAttachTimeout bounds the websocket handshake and the wait for
// the host's attach frame when a remote terminal is created.
const pulseRemoteTerminalAttachTimeout = 10 * time.Second

// pulseRemoteTerminalNew starts a shell on a connected remote host, in one of its
// saved projects, and registers its tab. A host's shell is created by a websocket
// attach with a new terminal_id (HandleTerminalWS): the host spawns the pty before
// it upgrades, so this attaches, waits for the attach frame, and detaches. The
// host keeps a detached shell for terminalDetachTTLRemote (24 h), and the browser
// reattaches to it by id, as it does after a reload. If the attach fails after
// the shell exists, the shell is discarded, because no tab would ever reach it.
func (h *Handler) pulseRemoteTerminalNew(host, projectPath, title string) (string, error) {
	if !h.terminalAccessAllowed() {
		return "", errors.New("terminals are disabled on this server (they require authentication or a loopback bind)")
	}
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return "", errors.New("project_path is required")
	}
	// The path is matched verbatim against the saved projects, as the proxy does:
	// a remote path's conventions (~) belong to the host.
	if _, ok := h.remoteProjectEntry(host, projectPath); !ok {
		return "", fmt.Errorf("%w: %q on %q is not a saved remote project (see project_list)", errPulseTerminalForeign, projectPath, host)
	}
	ws, ok := h.pulseRemoteWorkspace(host)
	if !ok {
		return "", pulseRemoteNotConnected(host)
	}
	if err := h.pulseRemoteRegister(ws, host, projectPath); err != nil {
		return "", err
	}

	id := fmt.Sprintf("pulse-term-%d", time.Now().UnixNano())
	spawned, err := pulseRemoteAttachTerminal(ws, id, projectPath)
	if err != nil {
		if spawned {
			h.pulseRemoteDiscardTerminal(host, id)
		}
		return "", fmt.Errorf("start terminal on %s: %w", host, err)
	}

	title = strings.TrimSpace(title)
	if title == "" {
		title = "Terminal"
	}
	if h.termTabsStore != nil {
		// AddTerminal, not Get then Set: the append must not overwrite a tab another
		// client or server process added between the two.
		if err := h.termTabsStore.AddTerminal(host+"::"+projectPath, termtabs.Terminal{ID: id, Title: title}); err != nil {
			log.Printf("pulse terminal_new: persist tab list for %s: %v", host, err)
		}
		h.bus.Publish("terminal_tabs_changed", "", "", nil)
	}
	return marshalPulseString(map[string]any{"terminal_id": id, "host": host, "project_path": projectPath})
}

// pulseRemoteDiscardTerminal kills a shell that pulseRemoteAttachTerminal spawned
// and then could not confirm. Without this the shell stays detached on the host for
// terminalDetachTTLRemote (24 h), and no tab exists to reach it. It is best effort:
// a 404 means the shell is already gone, and any other failure is logged, because
// the caller is already reporting the attach error.
func (h *Handler) pulseRemoteDiscardTerminal(host, id string) {
	err := h.pulseRemoteRequest(host, h.pulseRemotePort(host), http.MethodDelete, "/api/terminal/"+url.PathEscape(id), nil, pulseRemoteTimeout, pulseRemoteBodyCap, nil)
	var se *pulseRemoteStatusError
	if err != nil && !(errors.As(err, &se) && se.Status == http.StatusNotFound) {
		log.Printf("pulse terminal_new: discard terminal %s on %s after a failed attach: %v", id, host, err)
	}
}

// pulseRemoteAttachTerminal opens the host's terminal websocket with a fresh id,
// waits for its attach frame (the shell exists by then), and closes the socket so
// the shell stays detached for the browser to pick up. spawned reports that the
// handshake succeeded. The host has created the shell by then, so the caller must
// discard it when err is set.
func pulseRemoteAttachTerminal(ws remoteHostWorkspace, id, projectPath string) (spawned bool, err error) {
	u, err := url.Parse(ws.APIURL())
	if err != nil {
		return false, fmt.Errorf("parse API URL: %w", err)
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u = u.ResolveReference(&url.URL{Path: "/api/terminal/ws", RawQuery: url.Values{
		"terminal_id":  {id},
		"project_path": {projectPath},
	}.Encode()})
	hdr := http.Header{}
	if tok := ws.Token(); tok != "" {
		hdr.Set("Authorization", "Bearer "+tok)
	}
	dialer := websocket.Dialer{HandshakeTimeout: pulseRemoteTerminalAttachTimeout}
	conn, resp, err := dialer.Dial(u.String(), hdr)
	if err != nil {
		if resp != nil {
			return false, fmt.Errorf("websocket refused (%d): %w", resp.StatusCode, err)
		}
		return false, err
	}
	defer conn.Close()
	spawned = true
	_ = conn.SetReadDeadline(time.Now().Add(pulseRemoteTerminalAttachTimeout))
	typ, msg, err := conn.ReadMessage()
	if err != nil {
		return spawned, fmt.Errorf("wait for attach: %w", err)
	}
	if typ != websocket.TextMessage || !strings.Contains(string(msg), `"attach"`) {
		return spawned, fmt.Errorf("unexpected first frame from host: %.80q", msg)
	}
	_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	return spawned, nil
}

// pulseRemoteTerminalKill kills a live terminal on a connected remote host and
// drops its tab. The terminal must be live on that host and its project must be a
// saved project of that host: the host's kill endpoint takes only an id, so the
// check that the id belongs to a project the operator added happens here.
func (h *Handler) pulseRemoteTerminalKill(host, terminalID string) (string, error) {
	if !h.terminalAccessAllowed() {
		return "", errors.New("terminal access requires server authentication or a loopback bind address")
	}
	if err := h.pulseRemoteSessionHostOK(host); err != nil {
		return "", fmt.Errorf("%w: %v", errPulseTerminalForeign, err)
	}
	ws, ok := h.pulseRemoteWorkspace(host)
	if !ok {
		return "", pulseRemoteNotConnected(host)
	}
	// Live rows name the host's own path, which matches a saved "~" project only
	// through its registered expansion. Register those projects first, so a kill
	// after a restart of this server still matches.
	if err := h.pulseRemoteRegisterTilde(ws, host); err != nil {
		return "", err
	}
	live, err := h.pulseRemoteLive(host)
	if err != nil {
		return "", err
	}
	project := ""
	for _, r := range live {
		if r.ID == terminalID {
			project = r.Project
			break
		}
	}
	if project == "" {
		return "", fmt.Errorf("terminal %s is not live on %s", terminalID, host)
	}
	saved, ok := h.pulseRemoteSavedPath(host, project)
	if !ok {
		return "", fmt.Errorf("terminal %s belongs to project %q on %s, which is not a saved project", terminalID, project, host)
	}
	if err := h.pulseRemoteRequest(host, h.pulseRemotePort(host), http.MethodDelete, "/api/terminal/"+url.PathEscape(terminalID), nil, pulseRemoteTimeout, pulseRemoteBodyCap, nil); err != nil {
		return "", fmt.Errorf("kill terminal %s on %s: %w", terminalID, host, err)
	}
	// The host tears down the shell and its history; the tab list is owned by the
	// clients of this server. Drop the entry so every client stops showing it.
	if h.termTabsStore != nil {
		if err := h.termTabsStore.RemoveTerminal(host+"::"+saved, terminalID); err != nil {
			return "", fmt.Errorf("terminal %s killed but removing its tab entry failed: %w", terminalID, err)
		}
		h.bus.Publish("terminal_tabs_changed", "", "", nil)
	}
	return marshalPulseString(map[string]any{"terminal_id": terminalID, "host": host, "killed": true})
}
