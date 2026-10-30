package server

import (
	"bytes"
	"context"
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

	"github.com/u007/ocode/internal/agent"
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

// pulseRemoteClient has no Timeout of its own: each request carries a context deadline.
var pulseRemoteClient = &http.Client{}

// errPulseRemoteNotConnected marks a host this server has no live connection to.
var errPulseRemoteNotConnected = errors.New("remote host is not connected")

// pulseRemoteBodyCap bounds one remote response body. A whole transcript for a
// search is the largest legitimate read.
const (
	pulseRemoteBodyCap       = 4 << 20
	pulseRemoteTranscriptCap = 32 << 20
)

// pulseRemoteGetJSON GETs path on a connected host's own server and decodes the
// JSON body into out. A non-200 becomes an error carrying the remote's message.
func (h *Handler) pulseRemoteGetJSON(host string, port int, path string, query url.Values, out any) error {
	return h.pulseRemoteRequest(host, port, http.MethodGet, path, query, pulseRemoteTimeout, pulseRemoteBodyCap, out)
}

// pulseRemoteRequest is one request to a connected host's own server, under its
// own deadline. A 2xx is success; out, when non-nil, receives the JSON body (a 204
// or a nil out ignores it). Any other status is a *pulseRemoteStatusError. It never
// dials: a host with no live connection is errPulseRemoteNotConnected.
func (h *Handler) pulseRemoteRequest(host string, port int, method, path string, query url.Values, timeout time.Duration, bodyCap int64, out any) error {
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
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, target.String(), nil)
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, bodyCap+1))
	if err != nil {
		return fmt.Errorf("host %s: read response: %w", host, err)
	}
	if int64(len(body)) > bodyCap {
		return fmt.Errorf("host %s: response is over %d MiB", host, bodyCap>>20)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var e struct {
			Error string `json:"error"`
		}
		msg := strings.TrimSpace(string(body))
		if json.Unmarshal(body, &e) == nil && e.Error != "" {
			msg = e.Error
		}
		return &pulseRemoteStatusError{Host: host, Status: resp.StatusCode, Msg: msg}
	}
	if out == nil || resp.StatusCode == http.StatusNoContent || len(bytes.TrimSpace(body)) == 0 {
		return nil
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

// --- remote sessions on the board ---

const (
	// pulseRemoteBoardTTL is how long a host's rows are reused. The board is built
	// on every assistant turn and every pulse_board call, so without this each turn
	// would cost a round trip per host.
	pulseRemoteBoardTTL = 5 * time.Second
	// pulseRemoteBoardFailTTL is how long a failed host is left alone. A tunnel that
	// hangs would otherwise add its whole timeout to every turn (CLAUDE.md "Remote
	// connects": a failed connect is remembered, never retried per call).
	pulseRemoteBoardFailTTL = 30 * time.Second
	// pulseRemoteBoardTimeout is shorter than pulseRemoteTimeout: this path runs
	// before the model is called, so the operator is waiting behind it.
	pulseRemoteBoardTimeout = 3 * time.Second
	pulseRemoteBoardPages   = 3
)

// pulseHostNote says why a host's sessions are missing from the board, so the
// assistant reports "dev@box is not connected" instead of "nothing is running there".
type pulseHostNote struct {
	Host   string `json:"host"`
	Reason string `json:"reason"`
}

type pulseRemoteBoardEntry struct {
	rows []PulseRow
	err  error
	at   time.Time
}

// pulseRemoteBoardCache holds each host's last board rows per scope. The zero
// value is ready to use.
type pulseRemoteBoardCache struct {
	mu sync.Mutex
	m  map[string]pulseRemoteBoardEntry
}

// pulseRemoteHostList returns the distinct hosts of the saved remote projects,
// sorted. A host is one connection, so the first project's port decides.
func (h *Handler) pulseRemoteHostList() []string {
	if h.projects == nil {
		return nil
	}
	seen := make(map[string]bool)
	var hosts []string
	for _, p := range h.projects.List() {
		if p.Host != "" && !seen[p.Host] {
			seen[p.Host] = true
			hosts = append(hosts, p.Host)
		}
	}
	sort.Strings(hosts)
	return hosts
}

// pulseRemoteHostRows is one host's board rows, served from the cache while fresh.
func (h *Handler) pulseRemoteHostRows(host, scope string) ([]PulseRow, error) {
	if !h.pulseRemoteHostConnected(host) {
		return nil, fmt.Errorf("not connected (open one of its projects in the app to connect it)")
	}
	key := host + "\x00" + scope
	c := &h.pulseRemoteBoard
	c.mu.Lock()
	if e, ok := c.m[key]; ok {
		ttl := pulseRemoteBoardTTL
		if e.err != nil {
			ttl = pulseRemoteBoardFailTTL
		}
		if time.Since(e.at) < ttl {
			c.mu.Unlock()
			return e.rows, e.err
		}
	}
	c.mu.Unlock()

	rows, err := h.fetchPulseRemoteRows(host, scope)
	c.mu.Lock()
	if c.m == nil {
		c.m = make(map[string]pulseRemoteBoardEntry)
	}
	c.m[key] = pulseRemoteBoardEntry{rows: rows, err: err, at: time.Now()}
	c.mu.Unlock()
	return rows, err
}

// fetchPulseRemoteRows pages through the host's own GET /api/pulse. Its rows are
// already built and ordered by the host, so they are tagged with the host and
// merged, not re-derived.
func (h *Handler) fetchPulseRemoteRows(host, scope string) ([]PulseRow, error) {
	port := h.pulseRemotePort(host)
	var rows []PulseRow
	cursor := ""
	for page := 0; page < pulseRemoteBoardPages; page++ {
		q := url.Values{"scope": {scope}, "limit": {fmt.Sprint(pulseMaxLimit)}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		var resp PulsePageResponse
		if err := h.pulseRemoteRequest(host, port, http.MethodGet, "/api/pulse", q, pulseRemoteBoardTimeout, pulseRemoteBodyCap, &resp); err != nil {
			return nil, err
		}
		for _, r := range resp.Items {
			r.Host = host
			rows = append(rows, r)
		}
		if resp.NextCursor == nil || *resp.NextCursor == "" {
			break
		}
		cursor = *resp.NextCursor
	}
	return rows, nil
}

// pulseBoardRows is the Pulse assistant's board: this server's sessions plus those
// of every connected remote host, in the one board order. A host that cannot be
// read is reported in notes and contributes no rows. Hosts are asked in parallel.
func (h *Handler) pulseBoardRows(scope string, now time.Time) ([]PulseRow, []pulseHostNote) {
	rows := buildPulseRows(h.gatherPulseInputs(scope, now), scope, now)
	hosts := h.pulseRemoteHostList()
	if len(hosts) == 0 {
		return rows, nil
	}
	type result struct {
		rows []PulseRow
		err  error
	}
	results := make([]result, len(hosts))
	var wg sync.WaitGroup
	for i, host := range hosts {
		wg.Add(1)
		crashguard.Go(func() {
			defer wg.Done()
			r, err := h.pulseRemoteHostRows(host, scope)
			results[i] = result{r, err}
		})
	}
	wg.Wait()
	var notes []pulseHostNote
	for i, host := range hosts {
		if results[i].err != nil {
			notes = append(notes, pulseHostNote{Host: host, Reason: truncateRunes(results[i].err.Error(), 160)})
			continue
		}
		rows = append(rows, results[i].rows...)
	}
	sortPulseRows(rows)
	return rows, notes
}

// pulseRemoteSessionHost finds the host a session id was last seen on, from the
// cached remote boards. "" when no remote board has it. It never fetches.
func (h *Handler) pulseRemoteSessionHost(id string) string {
	c := &h.pulseRemoteBoard
	c.mu.Lock()
	defer c.mu.Unlock()
	hosts := make([]string, 0, len(c.m))
	for key, e := range c.m {
		for _, r := range e.rows {
			if r.SessionID == id {
				hosts = append(hosts, strings.SplitN(key, "\x00", 2)[0])
				break
			}
		}
	}
	sort.Strings(hosts)
	if len(hosts) == 0 {
		return ""
	}
	return hosts[0]
}

// --- remote session reads ---

// pulseRemoteSessionHostOK admits a host for a session read: it must be the host
// of a saved project, the same admission HandleRemoteProxy applies.
func (h *Handler) pulseRemoteSessionHostOK(host string) error {
	if _, ok := h.firstSavedProjectForHost(host); !ok {
		return fmt.Errorf("%q is not the host of a saved project (see project_list)", host)
	}
	return nil
}

// pulseRemoteSessionTranscript reads a remote session's transcript from its host.
// Without a search it asks for just the newest last messages and returns the
// absolute index of the first one as base, so message indexes mean the same as
// they do for a local session. With a search it needs the whole transcript to
// filter, which is why that read has a larger body cap and a longer deadline.
func (h *Handler) pulseRemoteSessionTranscript(host, id string, last int, all bool) (title, project string, msgs []agent.Message, base int, err error) {
	if id == "" {
		return "", "", nil, 0, errors.New("session_id is required")
	}
	if isPulseSession(id) {
		return "", "", nil, 0, fmt.Errorf("refusing to read the assistant's own session %q", id)
	}
	if err := h.pulseRemoteSessionHostOK(host); err != nil {
		return "", "", nil, 0, err
	}
	q := url.Values{}
	timeout, bodyCap := pulseRemoteTimeout, int64(pulseRemoteBodyCap)
	if all {
		timeout, bodyCap = 30*time.Second, pulseRemoteTranscriptCap
	} else {
		q.Set("limit", fmt.Sprint(last))
	}
	var detail SessionDetail
	if err := h.pulseRemoteRequest(host, h.pulseRemotePort(host), http.MethodGet, "/api/sessions/"+url.PathEscape(id), q, timeout, bodyCap, &detail); err != nil {
		return "", "", nil, 0, fmt.Errorf("read session %q on %s: %w", id, host, err)
	}
	if !all {
		base = max(detail.Total-len(detail.Messages), 0)
	}
	project = h.pulseRemoteRowProject(host, id)
	return detail.Title, project, detail.Messages, base, nil
}

// pulseRemoteRowProject is the project path a cached board row gives a remote
// session, "" when the session has not been on a board read yet.
func (h *Handler) pulseRemoteRowProject(host, id string) string {
	c := &h.pulseRemoteBoard
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, e := range c.m {
		if strings.SplitN(key, "\x00", 2)[0] != host {
			continue
		}
		for _, r := range e.rows {
			if r.SessionID == id {
				return r.ProjectPath
			}
		}
	}
	return ""
}

// pulseRemoteRecap asks the host for a session's recap. The recap is a model call
// on the host, so it runs under the recap deadline, not the short request one.
func (h *Handler) pulseRemoteRecap(host, id string) (string, error) {
	if err := h.pulseRemoteSessionHostOK(host); err != nil {
		return "", err
	}
	var resp struct {
		Recap string `json:"recap"`
	}
	if err := h.pulseRemoteRequest(host, h.pulseRemotePort(host), http.MethodGet, "/api/sessions/"+url.PathEscape(id)+"/recap", nil, pulseRecapTimeout, pulseRemoteBodyCap, &resp); err != nil {
		return "", err
	}
	return resp.Recap, nil
}

// pulseRemoteSessionAsks reads the pending asks a remote session is paused on from
// the host's session state.
func (h *Handler) pulseRemoteSessionAsks(host, id string) (*PendingAsks, error) {
	if err := h.pulseRemoteSessionHostOK(host); err != nil {
		return nil, err
	}
	var resp struct {
		PendingAsks *PendingAsks `json:"pending_asks"`
	}
	if err := h.pulseRemoteRequest(host, h.pulseRemotePort(host), http.MethodGet, "/api/sessions/"+url.PathEscape(id)+"/state", nil, pulseRemoteTimeout, pulseRemoteBodyCap, &resp); err != nil {
		return nil, fmt.Errorf("session %q on %s: %w", id, host, err)
	}
	return resp.PendingAsks, nil
}

// --- host_connect ---

// pulseHostConnectCooldown is how long after one host_connect attempt the next
// one for the same host is answered from the last attempt instead of starting
// another connect. The registry already remembers a failure for a few seconds
// (remoteConnectCooldown), which is short enough that a model retrying in a loop
// would still start an SSH connect every few seconds. This is the assistant's own
// bound on top of it.
const pulseHostConnectCooldown = 30 * time.Second

type pulseConnectAttempt struct {
	at  time.Time
	err string
}

// pulseRemoteConnectState remembers the last host_connect attempt per host. The
// zero value is ready to use.
type pulseRemoteConnectState struct {
	mu   sync.Mutex
	last map[string]pulseConnectAttempt
}

type pulseHostConnectResult struct {
	Host string `json:"host"`
	// State is "connected", "connecting" (the connect is running in the
	// background; check project_list or the board in a moment) or "failed".
	State string `json:"state"`
	Error string `json:"error,omitempty"`
	// RetryAfterSeconds is set when a failed host is being left alone.
	RetryAfterSeconds int `json:"retry_after_seconds,omitempty"`
}

func (h *Handler) pulseRemoteConnectTools() []*pulseTool {
	return []*pulseTool{{
		name:  "host_connect",
		desc:  "Connect a remote SSH/WSL host so its sessions and terminals become readable. Only for a host of a saved project (project_list shows connected: false). It starts the connection and returns after at most a couple of seconds: state \"connecting\" means it is still coming up, so check project_list or the board on a later turn instead of calling again. A failed host is not retried for 30 seconds. Use it only when the operator asks about, or asks you to connect, a host that is not connected. It never reconnects a host that is already connected.",
		props: map[string]any{"host": map[string]any{"type": "string", "description": "Host as project_list shows it"}},
		run:   h.pulseHostConnectTool,
	}}
}

func (h *Handler) pulseHostConnectTool(raw json.RawMessage) (string, error) {
	var args struct {
		Host string `json:"host"`
	}
	if err := decodePulseArgs(raw, &args); err != nil {
		return "", err
	}
	host := strings.TrimSpace(args.Host)
	if host == "" {
		return "", errors.New("host is required")
	}
	if h.remoteHosts == nil {
		return "", errors.New("remote connections are not available on this server")
	}
	// Admission is the browser proxy's: the host must be the host of a saved
	// project, and the connect uses that project's path and port. The tool never
	// names an arbitrary ssh target.
	proj, ok := h.firstSavedProjectForHost(host)
	if !ok {
		return "", fmt.Errorf("%q is not the host of a saved project (see project_list)", host)
	}
	port := proj.RemotePort
	res := pulseHostConnectResult{Host: host}

	if h.pulseRemoteHostConnected(host) {
		res.State = "connected"
		return marshalPulseString(res)
	}

	st := &h.pulseRemoteConnect
	st.mu.Lock()
	if last, seen := st.last[host]; seen {
		if wait := pulseHostConnectCooldown - time.Since(last.at); wait > 0 {
			st.mu.Unlock()
			if h.remoteHosts.status(host, port).Connecting {
				res.State = "connecting"
				return marshalPulseString(res)
			}
			res.State, res.Error, res.RetryAfterSeconds = "failed", last.err, int(wait.Seconds())+1
			if res.Error == "" {
				res.Error = "the last attempt did not finish connected"
			}
			return marshalPulseString(res)
		}
	}
	if st.last == nil {
		st.last = make(map[string]pulseConnectAttempt)
	}
	st.last[host] = pulseConnectAttempt{at: time.Now()}
	st.mu.Unlock()

	// workspaceForPort runs the connect in the background and waits at most
	// remoteConnectWaitBound (2 s) for it, so this call never parks on a slow SSH
	// connect. It joins an attempt already in flight instead of starting another.
	_, err := h.remoteHosts.workspaceForPort(host, proj.Path, port)
	switch {
	case err == nil:
		res.State = "connected"
		st.mu.Lock()
		delete(st.last, host)
		st.mu.Unlock()
	case errors.Is(err, errConnectInProgress):
		res.State = "connecting"
	default:
		res.State, res.Error = "failed", truncateRunes(err.Error(), 300)
		st.mu.Lock()
		st.last[host] = pulseConnectAttempt{at: time.Now(), err: res.Error}
		st.mu.Unlock()
		res.RetryAfterSeconds = int(pulseHostConnectCooldown.Seconds())
	}
	return marshalPulseString(res)
}
