//go:build !windows

package server

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
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
