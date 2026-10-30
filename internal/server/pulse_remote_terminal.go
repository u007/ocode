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

// pulseRemoteTerminalProject finds the project (a path on the host) a remote
// terminal belongs to: the shared tab store first (key "<host>::<path>"), else
// the host's own live rows. The (host, path) pair must be a saved remote project,
// the same admission HandleRemoteProxy applies, so the tool cannot be aimed at a
// host or path the operator never added.
func (h *Handler) pulseRemoteTerminalProject(host, terminalID string) (string, error) {
	if _, ok := h.firstSavedProjectForHost(host); !ok {
		return "", fmt.Errorf("%w: %q is not the host of a saved project (see project_list)", errPulseTerminalForeign, host)
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
			kh, kp := pulseRemoteTabKey(k)
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
			return "", err
		}
		for _, r := range rows {
			if r.ID == terminalID {
				path = r.Project
				break
			}
		}
	}
	if path == "" {
		return "", fmt.Errorf("%w: %q on host %q (see terminal_tabs for open terminals)", errPulseTerminalNotFound, terminalID, host)
	}
	saved, ok := h.pulseRemoteSavedPath(host, path)
	if !ok {
		return "", fmt.Errorf("%w: terminal %q belongs to %q on %q, which is not a saved project", errPulseTerminalForeign, terminalID, path, host)
	}
	return saved, nil
}

// pulseRemoteSavedPath maps a path on a host to the saved project that owns it
// and returns the saved path. A host reports the path it expanded
// (/home/me/app) while the saved project keeps what the operator typed (~/app),
// so a saved path with a leading "~" matches by its tail.
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
		rest, tilde := strings.CutPrefix(p.Path, "~")
		if tilde && strings.HasPrefix(rest, "/") && strings.HasSuffix(path, rest) {
			return p.Path, true
		}
	}
	return "", false
}

// pulseRemoteHistoryReader reads a remote terminal's log through the host's own
// GET /api/terminal/{id}/history, with the project as the host sees it.
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

// pulseRemoteTerminalSource resolves a remote terminal to its project and log
// reader. The host must hold a live connection: a remote read never dials.
func (h *Handler) pulseRemoteTerminalSource(host, terminalID string) (string, pulseHistoryReader, error) {
	project, err := h.pulseRemoteTerminalProject(host, terminalID)
	if err != nil {
		return "", nil, err
	}
	if h.remoteHosts == nil {
		return "", nil, errors.New("remote connections are not available on this server")
	}
	port := h.pulseRemotePort(host)
	ws, ok := h.remoteHosts.connectedWorkspace(host, port)
	if !ok {
		return "", nil, fmt.Errorf("%w: %s (open one of its projects in the app to connect it)", errPulseRemoteNotConnected, host)
	}
	// The host's history endpoint admits only registered projects, and the browser
	// path registers on first use. Do the same, so a terminal the operator never
	// opened through the proxy in this process is still readable.
	ctx, cancel := context.WithTimeout(context.Background(), pulseRemoteTimeout)
	defer cancel()
	if err := ensureRemoteProject(ctx, ws, h.remoteHosts, host, port, project); err != nil {
		return "", nil, fmt.Errorf("register project %q on %s: %w", project, host, err)
	}
	return project, h.pulseRemoteHistoryReader(host, project, terminalID), nil
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
// reattaches to it by id, as it does after a reload.
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
	if h.remoteHosts == nil {
		return "", errors.New("remote connections are not available on this server")
	}
	port := h.pulseRemotePort(host)
	ws, ok := h.remoteHosts.connectedWorkspace(host, port)
	if !ok {
		return "", fmt.Errorf("%w: %s (open one of its projects in the app to connect it)", errPulseRemoteNotConnected, host)
	}
	ctx, cancel := context.WithTimeout(context.Background(), pulseRemoteTimeout)
	defer cancel()
	if err := ensureRemoteProject(ctx, ws, h.remoteHosts, host, port, projectPath); err != nil {
		return "", fmt.Errorf("register project %q on %s: %w", projectPath, host, err)
	}

	id := fmt.Sprintf("pulse-term-%d", time.Now().UnixNano())
	if err := pulseRemoteAttachTerminal(ws, id, projectPath); err != nil {
		return "", fmt.Errorf("start terminal on %s: %w", host, err)
	}

	title = strings.TrimSpace(title)
	if title == "" {
		title = "Terminal"
	}
	if h.termTabsStore != nil {
		key := host + "::" + projectPath
		pt := h.termTabsStore.Get(key)
		pt.Terminals = append(pt.Terminals, termtabs.Terminal{ID: id, Title: title})
		if err := h.termTabsStore.Set(key, pt); err != nil {
			log.Printf("pulse terminal_new: persist tab list for %s: %v", host, err)
		}
		h.bus.Publish("terminal_tabs_changed", "", "", nil)
	}
	return marshalPulseString(map[string]any{"terminal_id": id, "host": host, "project_path": projectPath})
}

// pulseRemoteAttachTerminal opens the host's terminal websocket with a fresh id,
// waits for its attach frame (the shell exists by then), and closes the socket so
// the shell stays detached for the browser to pick up.
func pulseRemoteAttachTerminal(ws remoteHostWorkspace, id, projectPath string) error {
	u, err := url.Parse(ws.APIURL())
	if err != nil {
		return fmt.Errorf("parse API URL: %w", err)
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
			return fmt.Errorf("websocket refused (%d): %w", resp.StatusCode, err)
		}
		return err
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(pulseRemoteTerminalAttachTimeout))
	typ, msg, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("wait for attach: %w", err)
	}
	if typ != websocket.TextMessage || !strings.Contains(string(msg), `"attach"`) {
		return fmt.Errorf("unexpected first frame from host: %.80q", msg)
	}
	_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	return nil
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
