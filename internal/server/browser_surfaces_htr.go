package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/u007/ocode/internal/browse/cdp"
)

// htrAgentSurfaces drives the browser of the client machine through its htrcli
// daemon. A remote host has no Chrome to drive, so the browser tool reaches the
// client's own browser instead: `ssh -R` forwards the client's daemon to this
// host's loopback on the same port (see htr_reverse.go). The daemon routes each
// command to the extension in the tab's browser. Tabs are named by their numeric
// daemon id, so the tool's tab:<id> state keys carry the daemon's ids.
//
// This surface never starts a daemon: the port belongs to the forward. It never
// falls back to Chrome either: an unreachable daemon is an error.
//
// Refs: the daemon numbers its element refs per page, from @e1 again after every
// full navigation. A ref minted here therefore records the element it named (its
// tag and xpath), and every later use re-finds the daemon ref and refuses the
// action when the element differs. See htrRefStore.

const (
	// htrDefaultTimeoutMs is the daemon's own command timeout when the caller's
	// context has no deadline. It matches the daemon's default.
	htrDefaultTimeoutMs = 30000
	// htrPollEvery is how often a text or gone wait polls the page.
	htrPollEvery = 250 * time.Millisecond
	// htrRefStoreCap bounds the identity records kept for refs. The oldest is
	// dropped first, and a dropped ref fails as unknown, never as a wrong element.
	htrRefStoreCap = 20000
	// htrFieldsMax caps the fields in one fill_form call.
	htrFieldsMax = 50
	// htrNetworkWindowMs is the longest network capture window the client's
	// extension arms for a tab. Capture stops when the window closes.
	htrNetworkWindowMs = 120000
	// htrNetworkRearmAfter is the age at which a tab's network capture is armed
	// again on its next action. It stays under the window, so the window stays open.
	htrNetworkRearmAfter = 100 * time.Second
	// htrUploadMaxBytes caps the bytes one upload sends. The CDP path has no byte
	// cap, so this bounds the base64 request the client receives in one command.
	htrUploadMaxBytes = 16 << 20
	// htrInteractiveSelector selects the elements that can carry an identity-backed
	// ref in a snapshot or find. Elements outside it still appear in a snapshot,
	// but without a ref.
	htrInteractiveSelector = "a[href],button,input:not([type=hidden]),select,textarea,[role=button],[role=link],[role=checkbox],[role=radio],[role=tab],[role=menuitem],[role=menuitemcheckbox],[role=menuitemradio],[role=switch],[role=combobox],[role=option],[role=textbox],[role=searchbox],[role=spinbutton],[role=slider],[role=treeitem],[contenteditable=true],[tabindex]:not([tabindex=\"-1\"]),summary"
)

// htrCommandSeq makes every command id unique: the daemon keys its pending
// commands by id, so two in flight with one id would answer each other.
var htrCommandSeq atomic.Int64

func htrNextCommandID() string {
	return "ocode-" + strconv.FormatInt(htrCommandSeq.Add(1), 10)
}

// htrTab is one tab of a client browser, as the daemon lists it.
type htrTab struct {
	ID      int    `json:"id"`
	URL     string `json:"url"`
	Title   string `json:"title"`
	Active  bool   `json:"active"`
	Browser string `json:"browser"`
}

// htrTarget is the daemon's target selector. Ref, when set, wins over the rest.
type htrTarget struct {
	Ref      string   `json:"ref,omitempty"`
	Selector string   `json:"selector,omitempty"`
	XPath    string   `json:"xpath,omitempty"`
	ID       string   `json:"id,omitempty"`
	Name     string   `json:"name,omitempty"`
	Role     string   `json:"role,omitempty"`
	Label    string   `json:"label,omitempty"`
	Text     string   `json:"text,omitempty"`
	Place    string   `json:"placeholder,omitempty"`
	X        *float64 `json:"x,omitempty"`
	Y        *float64 `json:"y,omitempty"`
}

// htrCommand is the daemon's command body.
type htrCommand struct {
	ID      string         `json:"id"`
	Action  string         `json:"action"`
	Target  *htrTarget     `json:"target,omitempty"`
	Value   string         `json:"value,omitempty"`
	Options map[string]any `json:"options,omitempty"`
}

// htrCommandResult is the daemon's result for one command.
type htrCommandResult struct {
	ID      string          `json:"id"`
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// htrElement is one element a findAll or a find returns.
type htrElement struct {
	Ref        string            `json:"ref"`
	Tag        string            `json:"tag"`
	Text       string            `json:"text"`
	Selector   string            `json:"selector"`
	XPath      string            `json:"xpath"`
	Type       string            `json:"type"`
	Value      string            `json:"value"`
	Attributes map[string]string `json:"attributes"`
}

// htrSnapNode is one node of the daemon's snapshot tree. Box is the node's rectangle
// in viewport CSS pixels, nil when it is not laid out. Viewport and Scroll are the
// viewport's size and the document's scroll offset, set on the root only.
type htrSnapNode struct {
	Role     string         `json:"role"`
	Name     string         `json:"name"`
	Ref      string         `json:"ref"`
	Value    string         `json:"value"`
	State    map[string]any `json:"state"`
	Box      *htrBox        `json:"box"`
	Viewport *htrViewport   `json:"viewport"`
	Scroll   *htrScroll     `json:"scroll"`
	Children []htrSnapNode  `json:"children"`
}

// htrScroll is the document's scroll offset in CSS pixels.
type htrScroll struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// htrBox is a rectangle in viewport CSS pixels.
type htrBox struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// htrViewport is the viewport's size in CSS pixels.
type htrViewport struct {
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// htrPage is the daemon's page info for one tab.
type htrPage struct {
	URL            string `json:"url"`
	Title          string `json:"title"`
	ReadyState     string `json:"readyState"`
	ViewportWidth  int    `json:"viewportWidth"`
	ViewportHeight int    `json:"viewportHeight"`
}

// htrStatusError is a daemon answer with an HTTP error status.
type htrStatusError struct {
	status int
	msg    string
}

func (e *htrStatusError) Error() string {
	return fmt.Sprintf("daemon answered HTTP %d: %s", e.status, e.msg)
}

// htrRefIdentity is what a minted ref named when it was minted. frame is the
// iframe selector the element was found in, empty for the top document.
type htrRefIdentity struct {
	tab       int
	frame     string
	daemonRef string
	tag       string
	xpath     string
}

// htrUploadFile is one file of an uploadFiles command: its name, its type and its
// bytes. The daemon never reads a host path for ocode.
type htrUploadFile struct {
	Name     string `json:"name"`
	MimeType string `json:"mimeType"`
	Base64   string `json:"base64"`
}

// htrEvent is one entry of the daemon's event buffer.
type htrEvent struct {
	Seq       int             `json:"seq"`
	Kind      string          `json:"kind"`
	Timestamp int64           `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

// htrEvents is the daemon's answer to GET /api/events: the entries after the
// cursor, how many the caller missed, and the oldest sequence still held.
type htrEvents struct {
	Entries            []htrEvent `json:"entries"`
	Dropped            int        `json:"dropped"`
	OldestAvailableSeq int        `json:"oldestAvailableSeq"`
}

// htrConsoleData is the data of one console event, as the extension records it.
type htrConsoleData struct {
	Level  string   `json:"level"`
	Args   []string `json:"args"`
	Source string   `json:"source"`
}

// htrNetworkData is the data of one network event: the extension's NetworkEntry,
// of which only the request identity and the status are read.
type htrNetworkData struct {
	RequestID string `json:"requestId"`
	URL       string `json:"url"`
	Method    string `json:"method"`
	Status    int    `json:"status"`
}

// htrArm is one tab's network capture arm, as of the last time it was armed. A
// bounded window (the Chrome transport) closes window after armedAt. An always-on
// transport (Firefox webRequest) has bounded false. gaps counts the stretches a
// bounded window had closed before a re-arm, and gapFrom and gapTo bound the last one.
type htrArm struct {
	armedAt time.Time
	bounded bool
	window  time.Duration
	gaps    int
	gapFrom time.Time
	gapTo   time.Time
}

// htrArmed records the tabs whose network capture is armed in this process. The
// mutex is held across the arm call, so two surfaces for one tab cannot both send it.
type htrArmed struct {
	mu   sync.Mutex
	tabs map[int]htrArm
}

// htrRefStore maps each key a minted ref carries to the identity it named. Keys
// are unique for the process, so a ref from an earlier page or tab can never
// match a later record.
type htrRefStore struct {
	mu    sync.Mutex
	next  int64
	byKey map[int64]htrRefIdentity
	order []int64
}

func newHTRRefStore() *htrRefStore {
	return &htrRefStore{byKey: map[int64]htrRefIdentity{}}
}

func (s *htrRefStore) add(id htrRefIdentity) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	key := s.next
	s.byKey[key] = id
	s.order = append(s.order, key)
	for len(s.order) > htrRefStoreCap {
		delete(s.byKey, s.order[0])
		s.order = s.order[1:]
	}
	return key
}

func (s *htrRefStore) get(key int64) (htrRefIdentity, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.byKey[key]
	return id, ok
}

// htrAgentSurfaces is the browserSurfaces the tool uses on a remote host.
type htrAgentSurfaces struct {
	base   string
	token  string
	client *http.Client
	refs   *htrRefStore
	armed  *htrArmed
}

// newHTRAgentSurfaces builds the surfaces for the daemon at base (for example
// http://127.0.0.1:3845) with its bearer token.
func newHTRAgentSurfaces(base, token string) htrAgentSurfaces {
	return htrAgentSurfaces{base: base, token: token, client: &http.Client{}, refs: newHTRRefStore(), armed: &htrArmed{tabs: map[int]htrArm{}}}
}

// call sends one request to the daemon and decodes the envelope's data into out
// (nil discards it). Every failure is logged with what was attempted.
func (h htrAgentSurfaces) call(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			log.Printf("browser/htr: %s %s: encode request: %v", method, path, err)
			return fmt.Errorf("encode request for %s %s: %w", method, path, err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, h.base+path, reader)
	if err != nil {
		log.Printf("browser/htr: %s %s: build request: %v", method, path, err)
		return fmt.Errorf("build request for %s %s: %w", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		log.Printf("browser/htr: %s %s: daemon unreachable at %s: %v", method, path, h.base, err)
		return fmt.Errorf("the client's htrcli daemon is not reachable at %s (the ssh forward or htrcli serve on the client is down): %w", h.base, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("browser/htr: %s %s: read response: %v", method, path, err)
		return fmt.Errorf("read response to %s %s: %w", method, path, err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		log.Printf("browser/htr: %s %s: daemon refused the token (HTTP %d)", method, path, resp.StatusCode)
		return fmt.Errorf("the client's htrcli daemon refused the bearer token (HTTP %d): set browser.htr_token or the token in the host's ~/.htrcli/config.json", resp.StatusCode)
	}
	var env struct {
		OK    bool            `json:"ok"`
		Data  json.RawMessage `json:"data"`
		Error string          `json:"error"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		snippet := string(data)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		log.Printf("browser/htr: %s %s: HTTP %d with a body that is not an envelope: %v", method, path, resp.StatusCode, err)
		return fmt.Errorf("daemon answered %s %s with HTTP %d and no envelope (%s)", method, path, resp.StatusCode, snippet)
	}
	if !env.OK {
		log.Printf("browser/htr: %s %s: HTTP %d: %s", method, path, resp.StatusCode, env.Error)
		return &htrStatusError{status: resp.StatusCode, msg: env.Error}
	}
	if out == nil || len(env.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		log.Printf("browser/htr: %s %s: decode data: %v", method, path, err)
		return fmt.Errorf("decode data of %s %s: %w", method, path, err)
	}
	return nil
}

// htrTimeoutMs is the daemon's timeout for one command: the time left on ctx,
// so the daemon answers before the caller gives up.
func htrTimeoutMs(ctx context.Context) int {
	deadline, ok := ctx.Deadline()
	if !ok {
		return htrDefaultTimeoutMs
	}
	ms := int(time.Until(deadline) / time.Millisecond)
	if ms < 1 {
		return 1
	}
	return ms
}

// Tabs lists the client browser's tabs, sorted by daemon id.
func (h htrAgentSurfaces) Tabs(ctx context.Context) ([]htrTab, error) {
	var tabs []htrTab
	if err := h.call(ctx, http.MethodGet, "/api/tabs", nil, &tabs); err != nil {
		return nil, err
	}
	sort.Slice(tabs, func(i, j int) bool { return tabs[i].ID < tabs[j].ID })
	return tabs, nil
}

// tabInfo reads one tab. A tab the daemon does not hold wraps cdp.ErrNoTarget,
// so a close of a tab already gone only drops the agent's record of it. The error
// names the tabs the daemon does hold.
func (h htrAgentSurfaces) tabInfo(ctx context.Context, tab int) (htrTab, error) {
	var info htrTab
	err := h.call(ctx, http.MethodGet, "/api/tabs/"+strconv.Itoa(tab), nil, &info)
	if err == nil {
		return info, nil
	}
	var status *htrStatusError
	if errors.As(err, &status) && status.status == http.StatusNotFound {
		if tabs, lerr := h.Tabs(ctx); lerr == nil {
			return htrTab{}, fmt.Errorf("client tab %d: %w: %v (client tabs: %s)", tab, cdp.ErrNoTarget, err, htrTabIDList(tabs))
		}
		// intentionally not logged: call logs the listing failure, and the
		// original not-found error is what the caller needs.
		return htrTab{}, fmt.Errorf("client tab %d: %w: %v", tab, cdp.ErrNoTarget, err)
	}
	return htrTab{}, fmt.Errorf("client tab %d: %w", tab, err)
}

// htrTabIDList names the tab ids in a listing, or "none".
func htrTabIDList(tabs []htrTab) string {
	if len(tabs) == 0 {
		return "none"
	}
	ids := make([]string, 0, len(tabs))
	for _, t := range tabs {
		ids = append(ids, strconv.Itoa(t.ID))
	}
	return strings.Join(ids, ", ")
}

// htrTabIDFromKey reads the daemon tab id from a tab:<id> state key.
func htrTabIDFromKey(stateKey string) (int, error) {
	raw, ok := strings.CutPrefix(stateKey, "tab:")
	if !ok {
		return 0, fmt.Errorf("state key %q is not a tab key", stateKey)
	}
	id, err := strconv.Atoi(raw)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("tab %q is not a client browser tab id: the client's tabs are numbered (see action=tabs)", raw)
	}
	return id, nil
}

// Surface returns the client tab stateKey names. The tab is already live in the
// client's browser, so url is not loaded. The tab's network capture window is
// armed on the first call for it in this process.
func (h htrAgentSurfaces) Surface(ctx context.Context, stateKey, _ string) (browserSurface, string, error) {
	tab, err := htrTabIDFromKey(stateKey)
	if err != nil {
		return nil, "", err
	}
	info, err := h.tabInfo(ctx, tab)
	if err != nil {
		return nil, "", err
	}
	surf := htrSurface{h: h, tab: tab, browser: info.Browser}
	if err := surf.armNetwork(ctx); err != nil {
		return nil, "", err
	}
	return surf, "", nil
}

// Open opens a tab in the client's browser, in the background so the user's
// active tab stays. An empty url opens about:blank.
func (h htrAgentSurfaces) Open(ctx context.Context, rawURL string) (string, error) {
	if rawURL == "" {
		rawURL = "about:blank"
	}
	res, err := h.background(ctx, htrCommand{Action: "openTab", Options: map[string]any{"url": rawURL}}, "", false)
	if err != nil {
		return "", fmt.Errorf("open client tab: %w", err)
	}
	var opened struct {
		TabID int `json:"tabId"`
	}
	if err := json.Unmarshal(res.Data, &opened); err != nil || opened.TabID <= 0 {
		log.Printf("browser/htr: openTab: no tabId in %s: %v", res.Data, err)
		return "", fmt.Errorf("openTab answered without a tab id: %s", res.Data)
	}
	return "tab:" + strconv.Itoa(opened.TabID), nil
}

// Close closes the client tab stateKey names, through the browser that holds it.
func (h htrAgentSurfaces) Close(ctx context.Context, stateKey string) error {
	tab, err := htrTabIDFromKey(stateKey)
	if err != nil {
		return err
	}
	info, err := h.tabInfo(ctx, tab)
	if err != nil {
		return err
	}
	if info.Browser == "" {
		return fmt.Errorf("client tab %d reports no browser, so the close cannot be routed", tab)
	}
	_, err = h.background(ctx, htrCommand{Action: "closeTab", Options: map[string]any{"tabId": tab}}, info.Browser, true)
	return err
}

// background runs a tab-less command in a browser's background worker. A strict
// browser answers only from that browser.
func (h htrAgentSurfaces) background(ctx context.Context, cmd htrCommand, browser string, strict bool) (htrCommandResult, error) {
	cmd.ID = htrNextCommandID()
	body := map[string]any{"command": cmd, "timeout": htrTimeoutMs(ctx)}
	if browser != "" {
		body["browser"] = browser
	}
	if strict {
		body["browserStrict"] = true
	}
	var res htrCommandResult
	if err := h.call(ctx, http.MethodPost, "/api/background/command", body, &res); err != nil {
		return htrCommandResult{}, err
	}
	if !res.Success {
		return htrCommandResult{}, commandFailure(cmd.Action, res.Error)
	}
	return res, nil
}

// commandFailure turns a daemon command failure into an error. The daemon's own
// text is kept as it is, so a stale-ref message still says "stale ref".
func commandFailure(action, text string) error {
	if text == "" {
		return fmt.Errorf("command %q failed without a message", action)
	}
	return errors.New(text)
}

// htrSurface is one client tab, as the tool drives it.
type htrSurface struct {
	h       htrAgentSurfaces
	tab     int
	browser string
}

// command runs one tab-routed command. A daemon failure is logged and returned
// with the daemon's text.
func (s htrSurface) command(ctx context.Context, cmd htrCommand) (htrCommandResult, error) {
	cmd.ID = htrNextCommandID()
	body := map[string]any{"command": cmd, "timeout": htrTimeoutMs(ctx)}
	if s.browser != "" {
		body["browser"] = s.browser
	}
	var res htrCommandResult
	if err := s.h.call(ctx, http.MethodPost, "/api/tabs/"+strconv.Itoa(s.tab)+"/command", body, &res); err != nil {
		return htrCommandResult{}, err
	}
	if !res.Success {
		log.Printf("browser/htr: tab %d action %s: %s", s.tab, cmd.Action, res.Error)
		return htrCommandResult{}, commandFailure(cmd.Action, res.Error)
	}
	return res, nil
}

// htrWithFrame sets options.frame, the same-origin iframe selector the daemon
// searches in, on a command's options. An empty frame is the top document and
// leaves the options as they are.
func htrWithFrame(opts map[string]any, frame string) map[string]any {
	if frame == "" {
		return opts
	}
	if opts == nil {
		opts = map[string]any{}
	}
	opts["frame"] = frame
	return opts
}

// armNetwork arms the tab's network capture when it was never armed in this process
// or when the last arm is older than htrNetworkRearmAfter, so the bounded window
// stays open while the tab is used. A re-arm after a window closed records the
// stretch the capture did not cover. A failed arm is not recorded, so the next call
// for the tab tries again.
func (s htrSurface) armNetwork(ctx context.Context) error {
	s.h.armed.mu.Lock()
	defer s.h.armed.mu.Unlock()
	prev, armed := s.h.armed.tabs[s.tab]
	if armed && time.Since(prev.armedAt) < htrNetworkRearmAfter {
		return nil
	}
	res, err := s.command(ctx, htrCommand{Action: "networkCapture", Options: map[string]any{"durationMs": htrNetworkWindowMs}})
	if err != nil {
		return fmt.Errorf("arm network capture on client tab %d: %w", s.tab, err)
	}
	var out struct {
		Armed      *bool  `json:"armed"`
		DurationMs int    `json:"durationMs"`
		Transport  string `json:"transport"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		return fmt.Errorf("arm network capture on client tab %d: decode answer: %w", s.tab, err)
	}
	if out.Armed == nil || !*out.Armed {
		return fmt.Errorf("arm network capture on client tab %d: the client did not answer armed: %s", s.tab, res.Data)
	}
	now := time.Now()
	next := htrArm{armedAt: now}
	switch out.Transport {
	case "cdp":
		if out.DurationMs <= 0 {
			return fmt.Errorf("arm network capture on client tab %d: the client armed a window of %d ms", s.tab, out.DurationMs)
		}
		next.bounded = true
		next.window = time.Duration(out.DurationMs) * time.Millisecond
	case "webRequest":
	default:
		return fmt.Errorf("arm network capture on client tab %d: unknown transport %q", s.tab, out.Transport)
	}
	if armed {
		next.gaps, next.gapFrom, next.gapTo = prev.gaps, prev.gapFrom, prev.gapTo
		if closed := prev.armedAt.Add(prev.window); prev.bounded && now.After(closed) {
			next.gaps++
			next.gapFrom = closed
			next.gapTo = now
		}
	}
	s.h.armed.tabs[s.tab] = next
	return nil
}

// NetworkNote says when the tab's network capture was last armed, for how long, and
// any stretch it did not cover. A network read adds it, so a short window is visible
// to the model and requests outside the capture are not silently missing.
func (s htrSurface) NetworkNote() string {
	s.h.armed.mu.Lock()
	arm, ok := s.h.armed.tabs[s.tab]
	s.h.armed.mu.Unlock()
	if !ok {
		return ""
	}
	var b strings.Builder
	if arm.bounded {
		fmt.Fprintf(&b, "network capture was last armed at %s for a %d s window", arm.armedAt.Format(time.RFC3339), int(arm.window/time.Second))
	} else {
		fmt.Fprintf(&b, "network capture was last armed at %s and is always on in this browser", arm.armedAt.Format(time.RFC3339))
	}
	if arm.gaps > 0 {
		fmt.Fprintf(&b, "; %d stretch(es) had no capture, the last from %s to %s, and requests in them were not recorded", arm.gaps, arm.gapFrom.Format(time.RFC3339), arm.gapTo.Format(time.RFC3339))
	}
	return b.String()
}

// htrTargetFor converts a target the agent wrote into the daemon's selector, with
// the same prefix forms the CDP path and the htrcli CLI use. An @eN ref is handled
// before this, by the ref table.
func htrTargetFor(target string) *htrTarget {
	for _, p := range []struct {
		prefix string
		set    func(t *htrTarget, v string)
	}{
		{"name=", func(t *htrTarget, v string) { t.Name = v }},
		{"role=", func(t *htrTarget, v string) { t.Role = v }},
		{"text=", func(t *htrTarget, v string) { t.Text = v }},
		{"label=", func(t *htrTarget, v string) { t.Label = v }},
		{"placeholder=", func(t *htrTarget, v string) { t.Place = v }},
		{"id=", func(t *htrTarget, v string) { t.ID = v }},
		{"xpath=", func(t *htrTarget, v string) { t.XPath = v }},
	} {
		if v, ok := strings.CutPrefix(target, p.prefix); ok {
			t := &htrTarget{}
			p.set(t, v)
			return t
		}
	}
	return &htrTarget{Selector: target}
}

// findAll lists the elements a selector matches in frame. With mint, the daemon
// mints a ref for each one.
func (s htrSurface) findAll(ctx context.Context, frame, selector string, mint bool) ([]htrElement, error) {
	cmd := htrCommand{Action: "findAll", Target: htrTargetFor(selector)}
	var opts map[string]any
	if mint {
		opts = map[string]any{"assignRef": true}
	}
	cmd.Options = htrWithFrame(opts, frame)
	res, err := s.command(ctx, cmd)
	if err != nil {
		return nil, err
	}
	var els []htrElement
	if err := json.Unmarshal(res.Data, &els); err != nil {
		return nil, fmt.Errorf("decode matches of %q on client tab %d: %w", selector, s.tab, err)
	}
	return els, nil
}

// findRef re-finds the element a daemon ref names in frame now. A ref the daemon
// no longer knows, or an element that is gone, is nil.
func (s htrSurface) findRef(ctx context.Context, frame, daemonRef string) (*htrElement, error) {
	res, err := s.command(ctx, htrCommand{Action: "find", Target: &htrTarget{Ref: daemonRef}, Options: htrWithFrame(nil, frame)})
	if err != nil {
		return nil, err
	}
	var el *htrElement
	if err := json.Unmarshal(res.Data, &el); err != nil {
		return nil, fmt.Errorf("decode element of %s on client tab %d: %w", daemonRef, s.tab, err)
	}
	return el, nil
}

// snapshotTree reads the daemon's snapshot of frame, empty for the top document.
func (s htrSurface) snapshotTree(ctx context.Context, frame string) (htrSnapNode, error) {
	res, err := s.command(ctx, htrCommand{Action: "snapshot", Options: htrWithFrame(nil, frame)})
	if err != nil {
		return htrSnapNode{}, err
	}
	var root htrSnapNode
	if err := json.Unmarshal(res.Data, &root); err != nil {
		return htrSnapNode{}, fmt.Errorf("decode snapshot of client tab %d: %w", s.tab, err)
	}
	return root, nil
}

// mintedTree is one snapshot with the agent's refs for the nodes it could verify.
type mintedTree struct {
	root     htrSnapNode
	refOf    map[string]string // daemon ref to the ref the agent sees
	viewport *htrViewport      // the snapshot root's viewport, nil when absent
	scroll   *htrScroll        // the snapshot root's scroll offset, nil when absent
}

// mintTree reads frame's snapshot and mints a ref for each node that also appears
// in an interactive findAll of the same frame. That findAll gives the element
// identity the ref is checked against later. The daemon returns the same ref for
// the same element across the two calls, so the two lists agree. A node without
// identity gets no ref: the agent reaches it by a selector instead.
func (s htrSurface) mintTree(ctx context.Context, frame string, refs *cdp.RefTable) (mintedTree, error) {
	els, err := s.findAll(ctx, frame, htrInteractiveSelector, true)
	if err != nil {
		return mintedTree{}, err
	}
	byRef := make(map[string]htrElement, len(els))
	for _, el := range els {
		if el.Ref != "" {
			byRef[el.Ref] = el
		}
	}
	root, err := s.snapshotTree(ctx, frame)
	if err != nil {
		return mintedTree{}, err
	}
	m := mintedTree{root: root, refOf: map[string]string{}, viewport: root.Viewport, scroll: root.Scroll}
	walkSnapshot(&m.root, func(n *htrSnapNode) {
		el, ok := byRef[n.Ref]
		if !ok || n.Ref == "" || el.XPath == "" {
			return
		}
		if _, done := m.refOf[n.Ref]; done {
			return
		}
		key := s.h.refs.add(htrRefIdentity{tab: s.tab, frame: frame, daemonRef: n.Ref, tag: el.Tag, xpath: el.XPath})
		m.refOf[n.Ref] = refs.Mint(key)
	})
	return m, nil
}

// walkSnapshot visits n and its subtree in document order.
func walkSnapshot(n *htrSnapNode, visit func(*htrSnapNode)) {
	walkSnapshotDepth(n, 0, func(x *htrSnapNode, _ int) { visit(x) })
}

// walkSnapshotDepth visits n and its subtree in document order, with each node's
// depth below n.
func walkSnapshotDepth(n *htrSnapNode, depth int, visit func(*htrSnapNode, int)) {
	visit(n, depth)
	for i := range n.Children {
		walkSnapshotDepth(&n.Children[i], depth+1, visit)
	}
}

// htrStates lists the element states of a snapshot node in the CDP order: a true
// flag by name, a tristate or string as name=value, a false flag not at all.
func htrStates(state map[string]any) []string {
	out := []string{}
	for _, k := range cdp.SnapshotStateOrder() {
		switch v := state[k].(type) {
		case bool:
			if v {
				out = append(out, k)
			}
		case string:
			if v != "" {
				out = append(out, k+"="+v)
			}
		}
	}
	return out
}

// htrDisabled reports whether a snapshot node is disabled.
func htrDisabled(state map[string]any) bool {
	v, _ := state["disabled"].(bool)
	return v
}

// htrCutUTF16 cuts s to at most limit UTF-16 units and reports whether it cut.
func htrCutUTF16(s string, limit int) (string, bool) {
	units := 0
	for i, r := range s {
		w := 1
		if r >= 0x10000 {
			w = 2
		}
		if units+w > limit {
			return s[:i], true
		}
		units += w
	}
	return s, false
}

// bodyText reads the text of frame's body, empty for the top document. The
// daemon's getText returns the text content of the body, which includes script and
// style text; the CDP path reads the visible text instead.
func (s htrSurface) bodyText(ctx context.Context, frame string) (string, error) {
	res, err := s.command(ctx, htrCommand{Action: "getText", Target: &htrTarget{Selector: "body"}, Options: htrWithFrame(nil, frame)})
	if err != nil {
		return "", err
	}
	var text string
	if err := json.Unmarshal(res.Data, &text); err != nil {
		return "", fmt.Errorf("decode page text of client tab %d: %w", s.tab, err)
	}
	return text, nil
}

// page reads the tab's URL, title, load state and viewport.
func (s htrSurface) page(ctx context.Context) (htrPage, error) {
	var p htrPage
	if err := s.h.call(ctx, http.MethodGet, "/api/page?tab="+strconv.Itoa(s.tab), nil, &p); err != nil {
		return htrPage{}, err
	}
	return p, nil
}

// elementRef returns the daemon ref an element action acts on, in frame. An @eN
// ref must name the element it named when it was minted, in the same frame, or the
// action is refused as stale. A selector must match exactly one element, as the CDP
// path requires.
func (s htrSurface) elementRef(ctx context.Context, frame string, refs *cdp.RefTable, target string) (string, error) {
	if strings.HasPrefix(target, "@e") {
		key, err := refs.Lookup(target)
		if err != nil {
			return "", err
		}
		id, ok := s.h.refs.get(key)
		if !ok {
			return "", fmt.Errorf("stale ref: %s is no longer known to the browser tool (its identity record was dropped); re-run snapshot or find", target)
		}
		if id.tab != s.tab {
			return "", fmt.Errorf("ref %s was minted for client tab %d, not tab %d; re-run snapshot or find on this tab", target, id.tab, s.tab)
		}
		if id.frame != frame {
			return "", fmt.Errorf("ref %s was minted in frame %q, not frame %q; pass the frame it came from, or re-run snapshot or find", target, id.frame, frame)
		}
		el, err := s.findRef(ctx, frame, id.daemonRef)
		if err != nil {
			if strings.Contains(err.Error(), "stale ref") {
				return "", fmt.Errorf("stale ref: %s no longer names an element in the page (it changed or navigated); re-run snapshot or find", target)
			}
			return "", err
		}
		if el == nil || el.Tag != id.tag || el.XPath != id.xpath {
			return "", fmt.Errorf("stale ref: %s no longer names the element it named (the page changed or navigated); re-run snapshot or find", target)
		}
		return id.daemonRef, nil
	}
	els, err := s.findAll(ctx, frame, target, true)
	if err != nil {
		return "", err
	}
	switch len(els) {
	case 0:
		return "", fmt.Errorf("no element matches %q", target)
	case 1:
		if els[0].Ref == "" {
			return "", fmt.Errorf("%q matched an element the daemon would not ref", target)
		}
		return els[0].Ref, nil
	default:
		return "", fmt.Errorf("%q matches %d elements; a selector must match exactly one: use a ref from snapshot or find", target, len(els))
	}
}

// mintElement mints the ref the agent sees for one element found in frame,
// recording the identity the ref is checked against.
func (s htrSurface) mintElement(el htrElement, frame string, refs *cdp.RefTable) string {
	key := s.h.refs.add(htrRefIdentity{tab: s.tab, frame: frame, daemonRef: el.Ref, tag: el.Tag, xpath: el.XPath})
	return refs.Mint(key)
}

// Screenshot captures the tab's viewport. The daemon captures the named tab, not
// the user's active one, which the capture was verified against.
func (s htrSurface) Screenshot(ctx context.Context) ([]byte, int, int, error) {
	var b64 string
	if err := s.h.call(ctx, http.MethodGet, "/api/screenshot?tab="+strconv.Itoa(s.tab), nil, &b64); err != nil {
		return nil, 0, 0, fmt.Errorf("capture client tab %d: %w", s.tab, err)
	}
	png, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		log.Printf("browser/htr: screenshot of tab %d: decode base64: %v", s.tab, err)
		return nil, 0, 0, fmt.Errorf("decode screenshot of client tab %d: %w", s.tab, err)
	}
	p, err := s.page(ctx)
	if err != nil {
		return nil, 0, 0, err
	}
	return png, p.ViewportWidth, p.ViewportHeight, nil
}

// PageText returns the body text, cut to the CDP path's cap.
func (s htrSurface) PageText(ctx context.Context) (string, bool, error) {
	text, err := s.bodyText(ctx, "")
	if err != nil {
		return "", false, err
	}
	cut, truncated := htrCutUTF16(text, cdp.PageTextMaxChars)
	return cut, truncated, nil
}

// Click sends a click at viewport CSS pixels.
func (s htrSurface) Click(ctx context.Context, x, y float64) error {
	if _, err := s.command(ctx, htrCommand{Action: "click", Target: &htrTarget{X: &x, Y: &y}}); err != nil {
		return fmt.Errorf("coordinate click over htrcli failed: %w", err)
	}
	return nil
}

// InsertText types text into the focused element: a type command with no target.
func (s htrSurface) InsertText(ctx context.Context, text string) error {
	_, err := s.command(ctx, htrCommand{Action: "type", Value: text})
	return err
}

// PressKey presses a named key on the focused element, which is what the daemon's
// targetless pressKey does.
func (s htrSurface) PressKey(ctx context.Context, name string) error {
	if name == "" {
		return errors.New("key needs a key name")
	}
	_, err := s.command(ctx, htrCommand{Action: "pressKey", Value: name})
	return err
}

// ScrollPage scrolls the window by the CDP path's share of one viewport height:
// dir is 1 for down and -1 for up. The daemon's scrollBy takes the distance in
// pixels, so the viewport height is read from the page first.
func (s htrSurface) ScrollPage(ctx context.Context, dir int) error {
	if dir != 1 && dir != -1 {
		return fmt.Errorf("scroll direction must be 1 (down) or -1 (up), got %d", dir)
	}
	p, err := s.page(ctx)
	if err != nil {
		return err
	}
	if p.ViewportHeight <= 0 {
		return fmt.Errorf("scroll client tab %d: the page reports viewport height %d", s.tab, p.ViewportHeight)
	}
	dy := float64(dir) * cdp.PageScrollFraction * float64(p.ViewportHeight)
	_, err = s.command(ctx, htrCommand{Action: "scrollBy", Options: map[string]any{"dx": 0, "dy": dy}})
	return err
}

// Navigate loads url in the tab. The client's browser does the fetching, so the
// egress protections of the Chrome path do not apply; the tool's URL check runs
// before this call.
func (s htrSurface) Navigate(ctx context.Context, url string) error {
	_, err := s.command(ctx, htrCommand{Action: "navigate", Value: url})
	return err
}

// Back goes back in the tab's history.
func (s htrSurface) Back(ctx context.Context) error {
	_, err := s.command(ctx, htrCommand{Action: "goBack"})
	return err
}

// Forward goes forward in the tab's history.
func (s htrSurface) Forward(ctx context.Context) error {
	_, err := s.command(ctx, htrCommand{Action: "goForward"})
	return err
}

// Reload reloads the tab.
func (s htrSurface) Reload(ctx context.Context) error {
	_, err := s.command(ctx, htrCommand{Action: "reload"})
	return err
}

// Snapshot renders frame's tree in the CDP path's line format, with the refs that
// carry identity.
func (s htrSurface) Snapshot(ctx context.Context, frame string, refs *cdp.RefTable) (string, []cdp.RefElement, error) {
	m, err := s.mintTree(ctx, frame, refs)
	if err != nil {
		return "", nil, err
	}
	var lines []string
	var minted []cdp.RefElement
	size := 0
	stop := ""
	walkSnapshotDepth(&m.root, 0, func(n *htrSnapNode, depth int) {
		if stop != "" {
			return
		}
		if len(lines) >= cdp.SnapshotMaxElements {
			stop = fmt.Sprintf("element cap of %d reached", cdp.SnapshotMaxElements)
			return
		}
		ref := m.refOf[n.Ref]
		line := cdp.SnapshotLine(depth, n.Role, n.Name, n.Value, ref, htrStates(n.State))
		if size+len(line)+1 > cdp.SnapshotMaxBytes {
			stop = fmt.Sprintf("byte cap of %d bytes reached", cdp.SnapshotMaxBytes)
			return
		}
		lines = append(lines, line)
		size += len(line) + 1
		if ref != "" {
			minted = append(minted, cdp.RefElement{Ref: ref, Role: n.Role, Name: n.Name})
		}
	})
	text := strings.Join(lines, "\n")
	if stop != "" {
		text += fmt.Sprintf("\n[truncated: %s; %d elements shown]", stop, len(lines))
	}
	return text, minted, nil
}

// htrOutdatedClientError is the refusal for a snapshot that lacks a field Controls
// needs. The client extension predates that field, so every read fails the same way.
func htrOutdatedClientError(tab int, field string) error {
	return fmt.Errorf("controls of client tab %d: the snapshot carries no %s; the htrcli extension is out of date, update and reload it, and use snapshot, find, text or screenshot meanwhile: %w", tab, field, cdp.ErrClientExtensionOutdated)
}

// Controls reads the top document's controls: the snapshot's nodes that carry a
// verified ref and a control role, with the same operations and fingerprint as the
// CDP path. Each control carries its box and whether it is in the viewport, and the
// viewport's size and the scroll offset are reported, so the model can place a control
// on a screenshot. A snapshot without its viewport or scroll offset is an error: the
// client extension predates them and must be updated.
func (s htrSurface) Controls(ctx context.Context, refs *cdp.RefTable) (cdp.ControlsPage, error) {
	m, err := s.mintTree(ctx, "", refs)
	if err != nil {
		return cdp.ControlsPage{}, err
	}
	if m.viewport == nil {
		return cdp.ControlsPage{}, htrOutdatedClientError(s.tab, "viewport")
	}
	if m.scroll == nil {
		return cdp.ControlsPage{}, htrOutdatedClientError(s.tab, "scroll offset")
	}
	vw, vh := int(m.viewport.W), int(m.viewport.H)
	page, err := s.page(ctx)
	if err != nil {
		return cdp.ControlsPage{}, err
	}
	text, err := s.bodyText(ctx, "")
	if err != nil {
		return cdp.ControlsPage{}, err
	}
	viewport, truncated := htrCutUTF16(text, cdp.ViewportTextMaxChars)
	controls := []cdp.Control{}
	omitted := 0
	var contexts []string
	var walk func(n *htrSnapNode)
	walk = func(n *htrSnapNode) {
		pushed := cdp.IsControlContextRole(n.Role) && n.Name != ""
		if pushed {
			contexts = append(contexts, n.Name)
		}
		if ops, ok := cdp.ControlOperationsFor(n.Role, htrDisabled(n.State)); ok {
			if ref := m.refOf[n.Ref]; ref != "" {
				if len(controls) >= cdp.ControlsMaxElements {
					omitted++
				} else {
					c := cdp.Control{
						Ref:        ref,
						Role:       n.Role,
						Label:      cdp.ClipText(n.Name, cdp.ControlLabelMax),
						Value:      cdp.ClipText(n.Value, cdp.ControlValueMax),
						States:     htrStates(n.State),
						Operations: ops,
					}
					if len(contexts) > 0 {
						c.Context = cdp.ClipText(contexts[len(contexts)-1], cdp.ControlLabelMax)
					}
					if n.Box != nil {
						box := cdp.ControlBox{X: n.Box.X, Y: n.Box.Y, W: n.Box.W, H: n.Box.H}
						c.Box = &box
						c.InViewport = cdp.ControlBoxInViewport(box, vw, vh)
					}
					controls = append(controls, c)
				}
			}
		}
		for i := range n.Children {
			walk(&n.Children[i])
		}
		if pushed {
			contexts = contexts[:len(contexts)-1]
		}
	}
	walk(&m.root)
	fingerprint, err := cdp.ControlsFingerprint(page.URL, viewport, controls, omitted)
	if err != nil {
		return cdp.ControlsPage{}, fmt.Errorf("controls: %w", err)
	}
	return cdp.ControlsPage{
		URL:               page.URL,
		Title:             page.Title,
		Fingerprint:       fingerprint,
		ViewportText:      viewport,
		ViewportTruncated: truncated,
		ViewportWidth:     vw,
		ViewportHeight:    vh,
		ScrollX:           m.scroll.X,
		ScrollY:           m.scroll.Y,
		Settled:           page.ReadyState == "complete",
		Controls:          controls,
		Omitted:           omitted,
	}, nil
}

// FindElements lists the elements a selector matches, each with a ref. Total is
// every match. The daemon takes the first match of a bare selector; this path
// returns them all and the agent chooses.
func (s htrSurface) FindElements(ctx context.Context, frame, selector string, max int, refs *cdp.RefTable) (cdp.FindOutput, error) {
	if max <= 0 || max > cdp.FindMaxElements {
		return cdp.FindOutput{}, fmt.Errorf("find: max must be between 1 and %d, got %d", cdp.FindMaxElements, max)
	}
	els, err := s.findAll(ctx, frame, selector, true)
	if err != nil {
		return cdp.FindOutput{}, err
	}
	out := cdp.FindOutput{Total: len(els), Truncated: len(els) > max}
	for i, el := range els {
		if i >= max {
			break
		}
		path := el.Selector
		if path == "" {
			path = ":root"
		}
		fe := cdp.FoundElement{Tag: el.Tag, Text: cdp.ClipText(el.Text, cdp.FindTextMax), Selector: path}
		if el.Ref == "" || el.XPath == "" {
			fe.Note = "no ref: the daemon reported no element identity to check it against"
		} else {
			fe.Ref = s.mintElement(el, frame, refs)
		}
		out.Elements = append(out.Elements, fe)
	}
	return out, nil
}

// Wait waits for an element to appear or disappear, or for text to appear. The
// client extension ignores the gone flag in this build, so every wait here polls
// the page itself and is bounded by spec.Timeout.
func (s htrSurface) Wait(ctx context.Context, frame string, spec cdp.WaitSpec) error {
	deadline := time.Now().Add(spec.Timeout)
	for {
		present, err := s.present(ctx, frame, spec)
		if err != nil {
			return err
		}
		if present != spec.Gone {
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("wait: timeout after %s", spec.Timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(htrPollEvery):
		}
	}
}

// present reports whether a wait's element or text is in frame now.
func (s htrSurface) present(ctx context.Context, frame string, spec cdp.WaitSpec) (bool, error) {
	if spec.Text != "" {
		text, err := s.bodyText(ctx, frame)
		if err != nil {
			return false, err
		}
		return strings.Contains(text, spec.Text), nil
	}
	els, err := s.findAll(ctx, frame, spec.Selector, false)
	if err != nil {
		return false, err
	}
	return len(els) > 0, nil
}

// eventsOf reads the daemon's buffered events of one kind for the tab, from the
// start of the buffer. The read is bounded by the default command timeout.
func (s htrSurface) eventsOf(kind string) (htrEvents, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(htrDefaultTimeoutMs)*time.Millisecond)
	defer cancel()
	var ev htrEvents
	path := "/api/events?kind=" + kind + "&tab=" + strconv.Itoa(s.tab) + "&since=0"
	if err := s.h.call(ctx, http.MethodGet, path, nil, &ev); err != nil {
		return htrEvents{}, fmt.Errorf("read %s events of client tab %d: %w", kind, s.tab, err)
	}
	return ev, nil
}

// newestOf returns the last max items of all, oldest first. The daemon keeps its
// events oldest first, so the newest are at the end.
func newestOf[T any](all []T, max int) []T {
	if max <= 0 {
		return nil
	}
	if len(all) > max {
		return all[len(all)-max:]
	}
	return all
}

// consoleEntriesOf maps the daemon's console events to console entries, oldest
// first. An event whose data does not parse fails the whole read.
func consoleEntriesOf(ev htrEvents) ([]cdp.ConsoleEntry, error) {
	out := make([]cdp.ConsoleEntry, 0, len(ev.Entries))
	for _, e := range ev.Entries {
		var d htrConsoleData
		if err := json.Unmarshal(e.Data, &d); err != nil {
			return nil, fmt.Errorf("console event seq %d: decode data: %w", e.Seq, err)
		}
		text, textTruncated := cdp.TruncateRunes(strings.Join(d.Args, " "), cdp.ConsoleTextRunes)
		location, locationTruncated := cdp.TruncateBytes(d.Source, cdp.ConsoleLocationBytes)
		out = append(out, cdp.ConsoleEntry{
			TS:                e.Timestamp,
			Level:             d.Level,
			Kind:              "console",
			Text:              text,
			TextTruncated:     textTruncated,
			Location:          location,
			LocationTruncated: locationTruncated,
		})
	}
	return out, nil
}

// networkEntriesOf maps the daemon's network events to request records, oldest
// first. The extension records a request only once it completes, so every entry
// is finished. An entry with no response status has no recorded response, and it
// is not marked failed: the extension does not say whether the load failed.
func networkEntriesOf(ev htrEvents) ([]cdp.NetworkEntry, error) {
	out := make([]cdp.NetworkEntry, 0, len(ev.Entries))
	for _, e := range ev.Entries {
		var d htrNetworkData
		if err := json.Unmarshal(e.Data, &d); err != nil {
			return nil, fmt.Errorf("network event seq %d: decode data: %w", e.Seq, err)
		}
		url, urlTruncated := cdp.TruncateBytes(d.URL, cdp.NetworkURLBytes)
		rec := cdp.NetworkEntry{
			RequestID:    d.RequestID,
			Method:       d.Method,
			URL:          url,
			URLTruncated: urlTruncated,
			ResourceType: "unknown",
			Status:       d.Status,
			Finished:     true,
		}
		out = append(out, rec)
	}
	return out, nil
}

// ConsoleEntries returns the newest max console entries of the tab, oldest first,
// and how many older entries the daemon has evicted. A failed read is an error, so
// the model is never shown an empty list for a read that did not happen.
func (s htrSurface) ConsoleEntries(max int) ([]cdp.ConsoleEntry, int, error) {
	ev, err := s.eventsOf("console")
	if err != nil {
		return nil, 0, err
	}
	all, err := consoleEntriesOf(ev)
	if err != nil {
		return nil, 0, fmt.Errorf("console of client tab %d: %w", s.tab, err)
	}
	return newestOf(all, max), ev.Dropped, nil
}

// NetworkEntries returns the newest max request records of the tab, oldest first,
// and how many older records the daemon has evicted. A failed read is an error.
func (s htrSurface) NetworkEntries(max int) ([]cdp.NetworkEntry, int, error) {
	ev, err := s.eventsOf("network")
	if err != nil {
		return nil, 0, err
	}
	all, err := networkEntriesOf(ev)
	if err != nil {
		return nil, 0, fmt.Errorf("network of client tab %d: %w", s.tab, err)
	}
	return newestOf(all, max), ev.Dropped, nil
}

// ClickElement clicks the element a ref or selector names in frame.
func (s htrSurface) ClickElement(ctx context.Context, frame, target string, refs *cdp.RefTable) (string, error) {
	ref, err := s.elementRef(ctx, frame, refs, target)
	if err != nil {
		return "", err
	}
	if _, err := s.command(ctx, htrCommand{Action: "click", Target: &htrTarget{Ref: ref}, Options: htrWithFrame(nil, frame)}); err != nil {
		return "", err
	}
	return "clicked " + target, nil
}

// findRefOrGone is findRef for a ref that must still name an element in frame.
func (s htrSurface) findRefOrGone(ctx context.Context, frame, daemonRef string) (htrElement, error) {
	el, err := s.findRef(ctx, frame, daemonRef)
	if err != nil {
		return htrElement{}, err
	}
	if el == nil {
		return htrElement{}, fmt.Errorf("element %s is no longer in the page", daemonRef)
	}
	return *el, nil
}

// FillElement sets a text field's value and reads it back. An empty value clears
// the field, because the daemon treats an empty fill value as missing.
func (s htrSurface) FillElement(ctx context.Context, frame, target, value string, refs *cdp.RefTable) (string, error) {
	ref, err := s.elementRef(ctx, frame, refs, target)
	if err != nil {
		return "", err
	}
	if value == "" {
		_, err = s.command(ctx, htrCommand{Action: "clear", Target: &htrTarget{Ref: ref}, Options: htrWithFrame(nil, frame)})
	} else {
		_, err = s.command(ctx, htrCommand{Action: "fill", Target: &htrTarget{Ref: ref}, Value: value, Options: htrWithFrame(nil, frame)})
	}
	if err != nil {
		return "", err
	}
	el, err := s.findRefOrGone(ctx, frame, ref)
	if err != nil {
		return "", err
	}
	if el.Value != value {
		return "", fmt.Errorf("fill %s did not stick: the field reads %q", target, el.Value)
	}
	return "filled " + target, nil
}

// TypeIntoElement focuses the element and types the text into it.
func (s htrSurface) TypeIntoElement(ctx context.Context, frame, target, text string, refs *cdp.RefTable) (string, error) {
	ref, err := s.elementRef(ctx, frame, refs, target)
	if err != nil {
		return "", err
	}
	if _, err := s.command(ctx, htrCommand{Action: "type", Target: &htrTarget{Ref: ref}, Value: text, Options: htrWithFrame(nil, frame)}); err != nil {
		return "", err
	}
	return fmt.Sprintf("typed %d characters into %s", len([]rune(text)), target), nil
}

// checkedState reads the checked state of the element a ref names in frame, with
// the daemon's isChecked action.
func (s htrSurface) checkedState(ctx context.Context, frame, daemonRef string) (bool, error) {
	res, err := s.command(ctx, htrCommand{Action: "isChecked", Target: &htrTarget{Ref: daemonRef}, Options: htrWithFrame(nil, frame)})
	if err != nil {
		return false, err
	}
	var out struct {
		Checked *bool `json:"checked"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		return false, fmt.Errorf("decode checked state of %s on client tab %d: %w", daemonRef, s.tab, err)
	}
	if out.Checked == nil {
		return false, fmt.Errorf("isChecked of %s on client tab %d answered without a checked value: %s", daemonRef, s.tab, res.Data)
	}
	return *out.Checked, nil
}

// SetChecked checks or unchecks an element and reads its checked state back.
func (s htrSurface) SetChecked(ctx context.Context, frame, target string, checked bool, refs *cdp.RefTable) (string, error) {
	ref, err := s.elementRef(ctx, frame, refs, target)
	if err != nil {
		return "", err
	}
	action, verb := "uncheck", "unchecked"
	if checked {
		action, verb = "check", "checked"
	}
	if _, err := s.command(ctx, htrCommand{Action: action, Target: &htrTarget{Ref: ref}, Options: htrWithFrame(nil, frame)}); err != nil {
		return "", err
	}
	got, err := s.checkedState(ctx, frame, ref)
	if err != nil {
		return "", err
	}
	if got != checked {
		return "", fmt.Errorf("%s %s did not stick: it reads checked=%t", action, target, got)
	}
	return fmt.Sprintf("%s %s (checked=%t)", verb, target, got), nil
}

// SelectElement chooses an option of a native select and reads the element back.
// The daemon's select matches the option's value; the read-back is reported as it
// stands, so an option chosen by its text shows the value the page holds.
func (s htrSurface) SelectElement(ctx context.Context, frame, target, want string, refs *cdp.RefTable) (string, error) {
	ref, err := s.elementRef(ctx, frame, refs, target)
	if err != nil {
		return "", err
	}
	if _, err := s.command(ctx, htrCommand{Action: "select", Target: &htrTarget{Ref: ref}, Value: want, Options: htrWithFrame(nil, frame)}); err != nil {
		return "", err
	}
	el, err := s.findRefOrGone(ctx, frame, ref)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("selected %q in %s (the element reads %q)", want, target, el.Value), nil
}

// Pick chooses the option of the element in frame whose text is text. The daemon
// answers the value now shown; a failed match is an error that lists the options.
// The mode is read from the element, as the CDP path does: a select, or a combobox.
func (s htrSurface) Pick(ctx context.Context, frame, target, text string, refs *cdp.RefTable) (cdp.PickResult, error) {
	if strings.TrimSpace(text) == "" {
		return cdp.PickResult{}, errors.New("pick: option text cannot be empty")
	}
	ref, err := s.elementRef(ctx, frame, refs, target)
	if err != nil {
		return cdp.PickResult{}, fmt.Errorf("pick: %w", err)
	}
	el, err := s.findRefOrGone(ctx, frame, ref)
	if err != nil {
		return cdp.PickResult{}, fmt.Errorf("pick: %w", err)
	}
	res, err := s.command(ctx, htrCommand{Action: "pick", Target: &htrTarget{Ref: ref}, Value: text, Options: htrWithFrame(nil, frame)})
	if err != nil {
		return cdp.PickResult{}, fmt.Errorf("pick: %w", err)
	}
	var out struct {
		Value   string `json:"value"`
		Matched string `json:"matched"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		return cdp.PickResult{}, fmt.Errorf("pick: decode answer on client tab %d: %w", s.tab, err)
	}
	if out.Matched == "" {
		return cdp.PickResult{}, fmt.Errorf("pick: no option matched %q", text)
	}
	mode := "combobox"
	if el.Tag == "select" {
		mode = "select"
	}
	return cdp.PickResult{Mode: mode, Selected: out.Value}, nil
}

// htrFieldKind names a field the way the CDP fill-form report does.
func htrFieldKind(el htrElement) string {
	switch {
	case el.Tag == "select":
		return "select"
	case el.Tag == "input" && el.Type == "checkbox":
		return "checkbox"
	case el.Tag == "input" && el.Type == "radio":
		return "radio"
	case el.Tag == "input" && el.Type == "file":
		return "file"
	}
	return "text"
}

// FillForm applies the fields in order, then reads every field back. A boolean
// sets a checkbox or radio, a string fills or selects, and null skips. A file list
// is refused: see UploadFiles.
func (s htrSurface) FillForm(ctx context.Context, frame string, fields []cdp.FormField, refs *cdp.RefTable, _ func(path string) error) ([]cdp.FieldReport, error) {
	if len(fields) > htrFieldsMax {
		return nil, fmt.Errorf("fill-form: %d fields exceeds the cap of %d; split the form into several calls", len(fields), htrFieldsMax)
	}
	reports := make([]cdp.FieldReport, len(fields))
	refsWritten := make([]string, len(fields))
	wantBool := make([]*bool, len(fields))
	for i, f := range fields {
		r := &reports[i]
		r.Selector = f.Selector
		raw := bytes.TrimSpace(f.Value)
		if string(raw) == "null" {
			r.Status = cdp.FieldSkipped
			continue
		}
		if len(raw) > 0 && raw[0] == '[' {
			r.Kind = "file"
			r.Status = cdp.FieldFailed
			r.Error = "file fields are not supported over htrcli; see upload"
			continue
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			r.Status, r.Error = cdp.FieldFailed, "value is not JSON: "+err.Error()
			continue
		}
		ref, err := s.elementRef(ctx, frame, refs, f.Selector)
		if err != nil {
			r.Status, r.Error = cdp.FieldFailed, err.Error()
			continue
		}
		el, err := s.findRefOrGone(ctx, frame, ref)
		if err != nil {
			r.Status, r.Error = cdp.FieldFailed, err.Error()
			continue
		}
		r.Kind = htrFieldKind(el)
		refsWritten[i] = ref
		switch x := v.(type) {
		case bool:
			r.Requested = strconv.FormatBool(x)
			wantBool[i] = &x
			action := "uncheck"
			if x {
				action = "check"
			}
			_, err = s.command(ctx, htrCommand{Action: action, Target: &htrTarget{Ref: ref}, Options: htrWithFrame(nil, frame)})
		case string:
			r.Requested = x
			switch {
			case r.Kind == "select":
				_, err = s.command(ctx, htrCommand{Action: "select", Target: &htrTarget{Ref: ref}, Value: x, Options: htrWithFrame(nil, frame)})
			case x == "":
				_, err = s.command(ctx, htrCommand{Action: "clear", Target: &htrTarget{Ref: ref}, Options: htrWithFrame(nil, frame)})
			default:
				_, err = s.command(ctx, htrCommand{Action: "fill", Target: &htrTarget{Ref: ref}, Value: x, Options: htrWithFrame(nil, frame)})
			}
		default:
			r.Status, r.Error = cdp.FieldFailed, "value must be a string, a boolean, null or a file list"
			refsWritten[i] = ""
			continue
		}
		if err != nil {
			r.Status, r.Error = cdp.FieldFailed, err.Error()
			refsWritten[i] = ""
		}
	}
	// Read back after every write, so a later field that rewrites an earlier one is
	// caught. Checked state comes from isChecked, as SetChecked reads it; text values
	// from find.
	for i := range fields {
		r := &reports[i]
		if r.Status != "" || refsWritten[i] == "" {
			continue
		}
		if wantBool[i] != nil {
			got, err := s.checkedState(ctx, frame, refsWritten[i])
			if err != nil {
				r.Status, r.Error = cdp.FieldFailed, "read-back: "+err.Error()
				continue
			}
			r.Actual = strconv.FormatBool(got)
			if got == *wantBool[i] {
				r.Status = cdp.FieldOK
			} else {
				r.Status = cdp.FieldMismatch
			}
			continue
		}
		el, err := s.findRefOrGone(ctx, frame, refsWritten[i])
		if err != nil {
			r.Status, r.Error = cdp.FieldFailed, "read-back: "+err.Error()
			continue
		}
		r.Actual = el.Value
		if el.Value == r.Requested {
			r.Status = cdp.FieldOK
		} else {
			r.Status = cdp.FieldMismatch
		}
	}
	return reports, nil
}

// UploadFiles sets files on the file input that target names in frame. Each path
// is checked before anything is read: it must be absolute, must resolve to a
// regular file, and must pass the caller's policy on the resolved path. The bytes
// are then read here and sent base64 in one uploadFiles command. No host path is
// sent, so the client never reads a file of this host's.
func (s htrSurface) UploadFiles(ctx context.Context, frame, target string, paths []string, refs *cdp.RefTable, allowed func(path string) error) (string, error) {
	if allowed == nil {
		return "", errors.New("upload: nil allowed callback; the caller must supply the upload path policy")
	}
	if len(paths) == 0 {
		return "", errors.New("upload: no files given")
	}
	if len(paths) > browserUploadMaxFiles {
		return "", fmt.Errorf("upload: %d files exceeds the cap of %d", len(paths), browserUploadMaxFiles)
	}
	resolved := make([]string, len(paths))
	var total int64
	for i, p := range paths {
		if !filepath.IsAbs(p) {
			return "", fmt.Errorf("upload: %s is not an absolute path", p)
		}
		r, err := filepath.EvalSymlinks(p)
		if err != nil {
			return "", fmt.Errorf("upload: %s: %w", p, err)
		}
		if err := allowed(r); err != nil {
			return "", fmt.Errorf("upload refused for %s: %w", r, err)
		}
		info, err := os.Stat(r)
		if err != nil {
			return "", fmt.Errorf("upload: %s: %w", r, err)
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("upload: %s is not a regular file", r)
		}
		total += info.Size()
		if total > htrUploadMaxBytes {
			return "", fmt.Errorf("upload: the files total more than the %d byte cap", htrUploadMaxBytes)
		}
		resolved[i] = r
	}
	ref, err := s.elementRef(ctx, frame, refs, target)
	if err != nil {
		return "", err
	}
	files := make([]htrUploadFile, len(resolved))
	names := make([]string, len(resolved))
	for i, r := range resolved {
		data, err := os.ReadFile(r)
		if err != nil {
			return "", fmt.Errorf("upload: read %s: %w", r, err)
		}
		// application/octet-stream is the generic type for an extension with no
		// registered type.
		mimeType := mime.TypeByExtension(filepath.Ext(r))
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		names[i] = filepath.Base(r)
		files[i] = htrUploadFile{Name: names[i], MimeType: mimeType, Base64: base64.StdEncoding.EncodeToString(data)}
	}
	if _, err := s.command(ctx, htrCommand{Action: "uploadFiles", Target: &htrTarget{Ref: ref}, Options: htrWithFrame(map[string]any{"filesData": files}, frame)}); err != nil {
		return "", err
	}
	return fmt.Sprintf("uploaded %d file(s) to %s: %s", len(files), target, strings.Join(names, ", ")), nil
}

// htrSurface must meet the tool's interface.
var _ browserSurface = htrSurface{}

// wireRemoteHTRSurfaces points the browser tool at the client's htrcli daemon on a
// remote host. The decision is made once, at boot, so the tools array stays stable;
// reachability is checked per call. Without a token the host keeps its own Chrome,
// and the reason is logged. Nothing here starts a daemon: the port belongs to the
// forward.
func (s *Server) wireRemoteHTRSurfaces(htrToken string) {
	d := cdp.ResolveSharedDaemon(cdp.HTRSharedInput{Token: htrToken, Shared: true})
	if d.Token == "" {
		log.Printf("browser/htr: remote host keeps its own Chrome for the browser tool: %s", d.Notice)
		return
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", d.Port)
	s.handler.browseSurfaces = newHTRAgentSurfaces(base, d.Token)
	log.Printf("browser/htr: browser tool drives the client's browser through htrcli at %s", base)
}
