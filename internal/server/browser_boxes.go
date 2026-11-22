package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/u007/ocode/internal/boxsession"
	"github.com/u007/ocode/internal/browse/cdp"
	"github.com/u007/ocode/internal/browsertabs"
)

// browserBoxes is what the browser tool needs from the per-session box manager.
type browserBoxes interface {
	// Start creates the session's box or reports the one already running. It is the only call that creates a box.
	// retain asks for a retained box (kept per login state, paused when idle) instead of an ephemeral one.
	Start(ctx context.Context, sessionID string, retain bool) (browserBoxStatus, error)
	// Terminate ends the session's box: an ephemeral box is deleted, a retained box is paused. It returns
	// the kind of the box it acted on, read before the teardown, or "" when there was none to act on.
	Terminate(ctx context.Context, sessionID string) (kind string, err error)
	// Surfaces returns the running box's tab surfaces and the box's id. It never creates a box: with
	// none running the error tells the model to call start. The id lets the tool tell a replaced box
	// from the one its tabs were opened on.
	Surfaces(ctx context.Context, sessionID string) (browserSurfaces, string, error)
	// DefaultBackend is "local" or "upstash": the backend used when a call omits backend. An unreadable
	// setting is an error, never a guess: a guess could send a call to a paid box, or to the local one
	// when the user chose the box.
	DefaultBackend() (string, error)
}

// browserBoxStatus describes the session's box after Start.
type browserBoxStatus struct {
	BoxID string
	// Kind is boxsession.KindEphemeral or boxsession.KindRetained.
	Kind      string
	ExpiresAt time.Time
	// LoginState is the box's login state name, or "" for none.
	LoginState string
	// LoginStateNotes says what the restore could not apply (refused cookies, oversize
	// site storage). The start result states them. Counts and origin names only.
	LoginStateNotes []string
	Reused          bool
	// Resumed is true when the start woke a paused retained box instead of creating one.
	Resumed bool
}

const (
	browserBackendLocal   = "local"
	browserBackendUpstash = "upstash"
)

// browserBoxTimeout bounds one box-level call: start, terminate and the surface lookup.
const browserBoxTimeout = 2 * time.Minute

// browserBoxActions are the actions that exist only when boxes are attached. The
// browser tool's static action enum (browserActions) never contains them.
var browserBoxActions = []string{"start", "terminate"}

// browserRetainRule is the one rule retain obeys: it is a start on the upstash
// backend. A refusal says which half of it the call broke.
const browserRetainRule = "browser: retain is valid only with action=start on the upstash backend"

// errBrowserBoxUploads is the refusal for any file upload on the upstash backend.
var errBrowserBoxUploads = errors.New("browser: uploads are not supported on the upstash backend")

// browserTabSpace is one backend's tab state for one agent: the tabs the agent
// opened there, their element refs, and, for upstash, the session whose box holds
// those tabs (teardown needs it, because it runs without a session context) and
// the id of that box. A tab id names a tab of one box only, so the space is
// dropped when the session's box is replaced (see bindBox).
type browserTabSpace struct {
	opened    []string
	refs      map[string]*cdp.RefTable
	sessionID string
	boxID     string
}

// browserTarget is what one call resolves to: its backend, the surfaces it drives
// and the session of its box. The local backend has no session.
type browserTarget struct {
	backend   string
	surfaces  browserSurfaces
	sessionID string
}

// backendFor resolves a call's backend: the requested one, else the session's default.
// With no boxes attached every call is local, and asking for upstash is an error.
func (b *browserTool) backendFor(requested string) (string, error) {
	backend := requested
	if backend == "" {
		if b.boxes == nil {
			return browserBackendLocal, nil
		}
		def, err := b.boxes.DefaultBackend()
		if err != nil {
			return "", fmt.Errorf("browser: cannot read the browser backend setting: %w", err)
		}
		backend = def
	}
	switch backend {
	case browserBackendLocal:
		return browserBackendLocal, nil
	case browserBackendUpstash:
		if b.boxes == nil {
			return "", errors.New("browser: the upstash backend is not available in this session")
		}
		return browserBackendUpstash, nil
	}
	if requested == "" {
		return "", fmt.Errorf("browser: the session's default backend %q is not local or upstash", backend)
	}
	return "", fmt.Errorf("browser: unknown backend %q (one of: local, upstash)", backend)
}

// targetFor resolves the surfaces a call drives. The upstash surfaces come from the
// running box and are never a fallback: a box error is returned as it is.
func (b *browserTool) targetFor(ctx context.Context, backend, sessionID string) (browserTarget, error) {
	if backend == browserBackendLocal {
		return browserTarget{backend: backend, surfaces: b.surfaces}, nil
	}
	if sessionID == "" {
		return browserTarget{}, errors.New("browser: the upstash backend needs the session id, which this call does not carry")
	}
	surfaces, boxID, err := b.boxes.Surfaces(ctx, sessionID)
	if err != nil {
		b.forgetBoxTabsOnGone(err)
		return browserTarget{}, err
	}
	if boxID == "" {
		return browserTarget{}, errors.New("browser: the upstash box reported no id, so its tabs cannot be told apart from an earlier box's")
	}
	b.bindBox(boxID)
	return browserTarget{backend: backend, surfaces: surfaces, sessionID: sessionID}, nil
}

// bindBox ties this agent's upstash tab state to boxID, the box its surfaces belong
// to. A different id (a box that expired, was reaped, deleted from the panel or
// replaced by a new start) means every remembered tab, ref and the screenshot
// belong to a box that no longer exists. They are dropped here, before the new
// surfaces are used: a stale tab id would otherwise attach a blank tab on the new
// box, since Surface creates a missing target.
func (b *browserTool) bindBox(boxID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	sp := b.spaceLocked(browserBackendUpstash)
	if sp.boxID == boxID {
		return
	}
	if sp.boxID != "" || len(sp.opened) > 0 || len(sp.refs) > 0 {
		log.Printf("browser: the upstash box changed (%s -> %s); this agent's remembered upstash tabs are dropped", sp.boxID, boxID)
	}
	sp.opened = nil
	sp.refs = map[string]*cdp.RefTable{}
	sp.boxID = boxID
	if b.shot != nil && b.shot.backend == browserBackendUpstash {
		b.shot = nil
	}
}

// stateFor is the project's Browser-panel strip for the local backend. The upstash
// backend has no strip, so it reads as empty.
func (b *browserTool) stateFor(backend string) (browsertabs.ProjectBrowsers, error) {
	if backend != browserBackendLocal {
		return browsertabs.ProjectBrowsers{}, nil
	}
	return b.projectState()
}

// boxAction runs start or terminate. Both are valid only on the upstash backend.
func (b *browserTool) boxAction(ctx context.Context, p browserArgs, backend, sessionID string) (string, error) {
	if backend != browserBackendUpstash {
		return "", fmt.Errorf("browser: %s needs backend=upstash (or a session default of upstash): the local backend has no box", p.Action)
	}
	if sessionID == "" {
		return "", errors.New("browser: the upstash backend needs the session id, which this call does not carry")
	}
	bctx, cancel := context.WithTimeout(ctx, browserBoxTimeout)
	defer cancel()
	if p.Action == "start" {
		st, err := b.boxes.Start(bctx, sessionID, p.Retain)
		if err != nil {
			return "", fmt.Errorf("browser: start the upstash box: %w", err)
		}
		// A resumed box keeps its id, so bindBox would not notice that its tabs went with the pause.
		if st.Resumed {
			b.forgetUpstashTabs()
		}
		if st.Kind == boxsession.KindRetained {
			return retainedStartText(st), nil
		}
		expires := st.ExpiresAt.UTC().Format(time.RFC3339)
		head := fmt.Sprintf("The browser box is running: created box %s for this session.", st.BoxID)
		if st.Reused {
			head = fmt.Sprintf("The browser box is running: box %s was already running for this session.", st.BoxID)
		} else if st.LoginState != "" {
			// A reused box was restored by the start that created it, so only a new box says so.
			head += loginStateRestoreText(st)
		}
		return fmt.Sprintf("%s It expires at %s (UTC). The box bills while it runs, so call browser with action=terminate and backend=upstash when you are done.", head, expires), nil
	}
	kind, err := b.boxes.Terminate(bctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("browser: terminate the upstash box: %w", err)
	}
	// The box's tabs went with it (a retained box's pause closes them too), so this agent forgets them.
	b.forgetUpstashTabs()
	if kind == boxsession.KindRetained {
		return "paused the retained browser box for this session. Its tabs are gone, and its login state is saved. Call browser action=start with backend=upstash and retain=true to resume it. To delete it, use Settings, Upstash Box.", nil
	}
	return "terminated the browser box for this session. Its tabs are gone; start a new box before using backend=upstash again.", nil
}

// retainedStartText is the start result for a retained box: created, resumed, or
// already running for this session.
func retainedStartText(st browserBoxStatus) string {
	var head string
	switch {
	case st.Reused:
		head = fmt.Sprintf("The browser box is running: retained box %s was already running for this session.", st.BoxID)
	case st.Resumed:
		head = fmt.Sprintf("The browser box is running: resumed retained box %s for login state %q. Its earlier tabs are gone.", st.BoxID, st.LoginState)
		head += loginStateRestoreText(st)
	default:
		head = fmt.Sprintf("The browser box is running: created retained box %s for login state %q.", st.BoxID, st.LoginState)
		head += loginStateRestoreText(st)
	}
	deleteAt := st.ExpiresAt.UTC().Format(time.RFC3339)
	return head + fmt.Sprintf(" It is kept between sessions: it is paused when idle or when you call action=terminate, and deleted if unused until %s (UTC). A pause closes every open tab.", deleteAt)
}

// loginStateRestoreText says what the login-state restore applied and what it could
// not. It is empty for a box with no login state.
func loginStateRestoreText(st browserBoxStatus) string {
	if st.LoginState == "" {
		return ""
	}
	s := fmt.Sprintf(" Login state %q is restored: cookies now, site storage on first visit to each site.", st.LoginState)
	for _, note := range st.LoginStateNotes {
		s += fmt.Sprintf(" Login state note: %s.", note)
	}
	return s
}

// forgetUpstashTabs drops this agent's remembered upstash tabs, their refs and the
// upstash screenshot. It is what a box change does (see bindBox), and it runs when
// the box's tabs are gone: a terminate, a pause, or a resume.
func (b *browserTool) forgetUpstashTabs() {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.spaces, browserBackendUpstash)
	if b.shot != nil && b.shot.backend == browserBackendUpstash {
		b.shot = nil
	}
}

// forgetBoxTabsOnGone drops the upstash tab state when the box reported ErrPaused or
// ErrExpired: its tabs no longer exist, so a remembered tab id would attach a blank
// tab on the next box. Other errors leave the state alone.
func (b *browserTool) forgetBoxTabsOnGone(err error) {
	if errors.Is(err, boxsession.ErrPaused) || errors.Is(err, boxsession.ErrExpired) {
		b.forgetUpstashTabs()
	}
}

// refuseBoxUploads refuses, before any file path is checked or any page is touched,
// an upload on the upstash backend, and a fill_form whose field is a file list.
func refuseBoxUploads(backend string, p browserArgs) error {
	if backend != browserBackendUpstash {
		return nil
	}
	if p.Action == "upload" {
		return errBrowserBoxUploads
	}
	if p.Action == "fill_form" {
		for _, f := range p.Fields {
			if isFileListValue(f.Value) {
				return errBrowserBoxUploads
			}
		}
	}
	return nil
}

// isFileListValue reports whether a fill_form value is a file list (a JSON array),
// the form checkFormFields treats as a file upload.
func isFileListValue(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '['
}

// refuseBoxUploadPath is the file policy for the upstash backend: no path is allowed,
// so a file input the surface would otherwise fill is refused rather than read.
func refuseBoxUploadPath(string) error {
	return errBrowserBoxUploads
}

// checkURLFor is the URL check for the backend: the box backend refuses the
// addresses that are the box's own network.
func checkURLFor(backend, u string) error {
	if backend == browserBackendUpstash {
		return checkBoxBrowserURL(u)
	}
	return checkBrowserURL(u)
}

// openedIn returns a copy of the tab ids this agent opened on backend, oldest first.
func (b *browserTool) openedIn(backend string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	sp, ok := b.spaces[backend]
	if !ok {
		return nil
	}
	return append([]string(nil), sp.opened...)
}

// spaceLocked returns backend's tab state, creating it on first use. b.mu must be held.
func (b *browserTool) spaceLocked(backend string) *browserTabSpace {
	sp, ok := b.spaces[backend]
	if !ok {
		sp = &browserTabSpace{refs: map[string]*cdp.RefTable{}}
		b.spaces[backend] = sp
	}
	if sp.refs == nil {
		sp.refs = map[string]*cdp.RefTable{}
	}
	return sp
}

// recordOpened notes a tab this agent opened on the call's backend.
func (b *browserTool) recordOpened(tgt browserTarget, id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	sp := b.spaceLocked(tgt.backend)
	sp.opened = append(sp.opened, id)
	if tgt.backend == browserBackendUpstash {
		sp.sessionID = tgt.sessionID
	}
}

// forgetTab drops a closed tab from backend's opened list and its element refs.
func (b *browserTool) forgetTab(backend, id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	sp := b.spaceLocked(backend)
	sp.opened = slices.DeleteFunc(sp.opened, func(o string) bool { return o == id })
	delete(sp.refs, id)
}

// closeBoxTabs closes the tabs this agent opened on the upstash box when its run
// ends. The box may already be gone (terminated, or expired), so a failure is logged
// and never returned: teardown must not fail on a box that no longer exists.
func (b *browserTool) closeBoxTabs(ctx context.Context) {
	b.mu.Lock()
	var ids []string
	var sid, boxID string
	if sp, ok := b.spaces[browserBackendUpstash]; ok {
		ids = append([]string(nil), sp.opened...)
		sid = sp.sessionID
		boxID = sp.boxID
	}
	b.mu.Unlock()
	if len(ids) == 0 {
		return
	}
	surfaces, currentID, err := b.boxes.Surfaces(ctx, sid)
	if err != nil {
		b.forgetBoxTabsOnGone(err)
		log.Printf("browser: close %d tab(s) on the upstash box: %v", len(ids), err)
		return
	}
	// The tab ids belong to boxID. A session's box that has since been replaced
	// holds other tabs under the same ids, possibly a sibling agent's, so they are
	// never closed on it.
	if currentID != boxID {
		log.Printf("browser: the upstash box changed (%s -> %s); %d remembered tab(s) went with the old box and are not closed", boxID, currentID, len(ids))
		return
	}
	tgt := browserTarget{backend: browserBackendUpstash, surfaces: surfaces, sessionID: sid}
	for _, id := range ids {
		if _, err := b.closeTab(ctx, tgt, id); err != nil {
			log.Printf("browser: close tab %s on the upstash box: %v", id, err)
		}
	}
}

// listBoxTabs is the tabs action on the upstash backend: only the tabs this agent opened there.
func listBoxTabs(opened []string) string {
	if len(opened) == 0 {
		return "no browser tabs are open on the upstash box; use action=open to open one"
	}
	var sb strings.Builder
	sb.WriteString("browser tabs on the upstash box opened by this agent")
	for _, id := range opened {
		fmt.Fprintf(&sb, "\n- %s (opened by this agent)", id)
	}
	return sb.String()
}
