package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/crashguard"
)

// Remote (SSH/WSL) reach for the Pulse assistant's read tools. A remote
// project's shells and sessions live in the host's own `ocode serve --remote`
// process, so nothing in this process's registries describes them. The tools ask
// that server over the workspace's tunnel instead, the way the browser does
// through HandleRemoteProxy, with the same bearer token.
//
// Two rules from CLAUDE.md "Remote connects" hold here. A tool call never dials:
// it uses connectedWorkspace, so an LLM loop cannot start an SSH connect or park
// on one, and a host that is not connected is reported as such, not retried. And
// every call carries its own deadline, with hosts fanned out in parallel, so one
// hung tunnel cannot stall the answer for the others.

// pulseRemoteTimeout bounds one request to a remote host's server.
const pulseRemoteTimeout = 8 * time.Second

var pulseRemoteClient = &http.Client{Timeout: pulseRemoteTimeout}

// errPulseRemoteNotConnected marks a host this server has no live connection to.
var errPulseRemoteNotConnected = errors.New("remote host is not connected")

// pulseRemoteGetJSON GETs path on a connected host's own server and decodes the
// JSON body into out. A non-200 becomes an error carrying the remote's message.
func (h *Handler) pulseRemoteGetJSON(host string, port int, path string, query url.Values, out any) error {
	if h.remoteHosts == nil {
		return errors.New("remote connections are not available on this server")
	}
	ws, ok := h.remoteHosts.connectedWorkspace(host, port)
	if !ok {
		return fmt.Errorf("%w: %s (open one of its projects in the app to connect it)", errPulseRemoteNotConnected, host)
	}
	apiURL, err := url.Parse(ws.APIURL())
	if err != nil {
		return fmt.Errorf("parse API URL: %w", err)
	}
	target := apiURL.ResolveReference(&url.URL{Path: path, RawQuery: query.Encode()})
	req, err := http.NewRequest(http.MethodGet, target.String(), nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if tok := ws.Token(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := pulseRemoteClient.Do(req)
	if err != nil {
		return fmt.Errorf("host %s: %w", host, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("host %s: read response: %w", host, err)
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		msg := strings.TrimSpace(string(body))
		if json.Unmarshal(body, &e) == nil && e.Error != "" {
			msg = e.Error
		}
		return &pulseRemoteStatusError{Host: host, Status: resp.StatusCode, Msg: msg}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("host %s: decode response: %w", host, err)
	}
	return nil
}

// pulseRemoteStatusError is a remote server's refusal, with its HTTP status so a
// caller can tell "no such history" (404) from a real failure.
type pulseRemoteStatusError struct {
	Host   string
	Status int
	Msg    string
}

func (e *pulseRemoteStatusError) Error() string {
	return fmt.Sprintf("host %s answered %d: %s", e.Host, e.Status, e.Msg)
}

// pulseRemotePort is the saved SSH port for a host's projects (0 = default). Two
// saved projects on one host name the same connection, so the first one decides.
func (h *Handler) pulseRemotePort(host string) int {
	if p, ok := h.firstSavedProjectForHost(host); ok {
		return p.RemotePort
	}
	return 0
}

// pulseRemoteHostConnected reports whether a live connection to host exists,
// without dialing.
func (h *Handler) pulseRemoteHostConnected(host string) bool {
	if h.remoteHosts == nil {
		return false
	}
	_, ok := h.remoteHosts.connectedWorkspace(host, h.pulseRemotePort(host))
	return ok
}

// pulseRemoteTabKey splits a terminal-tab store key into (host, path). A local
// project's key is its bare path; a remote one's is "<host>::<path>"
// (projectTerminalsKey in the web client). host is "" for a local key.
func pulseRemoteTabKey(key string) (host, path string) {
	if i := strings.Index(key, "::"); i > 0 {
		return key[:i], key[i+2:]
	}
	return "", key
}

// --- project_list ---

type pulseProjectEntry struct {
	Path string `json:"path"`
	Name string `json:"name"`
	// Host is "" for a local project, else the SSH/WSL target it lives on.
	Host string `json:"host,omitempty"`
	// Kind is "ssh" or "wsl" for a remote project.
	Kind  string `json:"kind,omitempty"`
	Group string `json:"group,omitempty"`
	// Connected says whether this server holds a live connection to the host. A
	// remote project that is not connected still lists, but its terminals cannot be read.
	Connected *bool `json:"connected,omitempty"`
}

func (h *Handler) pulseProjectListTool(raw json.RawMessage) (string, error) {
	if err := decodePulseArgs(raw, &struct{}{}); err != nil {
		return "", err
	}
	if h.projects == nil {
		return "", errors.New("project store is unavailable")
	}
	list := h.projects.List()
	connected := make(map[string]bool)
	entries := make([]pulseProjectEntry, 0, len(list))
	for _, p := range list {
		e := pulseProjectEntry{Path: p.Path, Name: p.Name, Host: p.Host, Group: p.Group}
		if p.Host != "" {
			e.Kind = p.RemoteKind
			if e.Kind == "" {
				e.Kind = "ssh"
			}
			c, seen := connected[p.Host]
			if !seen {
				c = h.pulseRemoteHostConnected(p.Host)
				connected[p.Host] = c
			}
			e.Connected = &c
		}
		entries = append(entries, e)
	}
	// Local projects first, then by host, then path: stable for the prompt cache
	// and for a reader scanning for one project.
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Host != entries[j].Host {
			return entries[i].Host < entries[j].Host
		}
		return entries[i].Path < entries[j].Path
	})
	return pulseCapped(len(entries), func(n int, truncated bool) any {
		return struct {
			Projects  []pulseProjectEntry `json:"projects"`
			Truncated bool                `json:"truncated,omitempty"`
		}{append([]pulseProjectEntry{}, entries[:n]...), truncated}
	})
}

// --- remote terminals ---

// pulseRemoteLive fetches the live terminal rows of one connected host.
func (h *Handler) pulseRemoteLive(host string) ([]pulseTerminalRow, error) {
	port := h.pulseRemotePort(host)
	var rows []pulseTerminalRow
	const page = 200
	for offset := 0; ; offset += page {
		var resp struct {
			Terminals []pulseTerminalRow `json:"terminals"`
			Total     int                `json:"total"`
		}
		q := url.Values{"limit": {fmt.Sprint(page)}, "offset": {fmt.Sprint(offset)}}
		if err := h.pulseRemoteGetJSON(host, port, "/api/pulse/terminals", q, &resp); err != nil {
			return nil, err
		}
		rows = append(rows, resp.Terminals...)
		if len(resp.Terminals) == 0 || len(rows) >= resp.Total || offset > 20*page {
			return rows, nil
		}
	}
}

// pulseRemoteLiveByHost fetches the live rows of several hosts in parallel. A
// host that fails maps to its error, not to an empty list, so the caller can say
// why its terminals show no live state.
func (h *Handler) pulseRemoteLiveByHost(hosts []string) (map[string][]pulseTerminalRow, map[string]error) {
	rows := make(map[string][]pulseTerminalRow, len(hosts))
	errs := make(map[string]error)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, host := range hosts {
		wg.Add(1)
		crashguard.Go(func() {
			defer wg.Done()
			r, err := h.pulseRemoteLive(host)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs[host] = err
				return
			}
			rows[host] = r
		})
	}
	wg.Wait()
	return rows, errs
}
