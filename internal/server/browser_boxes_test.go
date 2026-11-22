package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/boxsession"
	"github.com/u007/ocode/internal/browsertabs"
	"github.com/u007/ocode/internal/tool"
)

// fakeBoxes is a browserBoxes whose box is a fakeSurfaces. It records every call
// as "start:<session>", "terminate:<session>" or "surfaces:<session>", so a test
// can assert that a refused call never reached the box manager.
type fakeBoxes struct {
	status   browserBoxStatus
	startErr error
	termErr  error
	// termKind is the kind Terminate reports for the box it acted on.
	termKind string
	surfErr  error
	surfaces browserSurfaces
	def      string
	defErr   error
	defCalls int
	calls    []string
	// retains records the retain flag of every Start, so a test can see what the tool passed.
	retains []bool
}

func (f *fakeBoxes) Start(ctx context.Context, sessionID string, retain bool) (browserBoxStatus, error) {
	f.calls = append(f.calls, "start:"+sessionID)
	f.retains = append(f.retains, retain)
	if f.startErr != nil {
		return browserBoxStatus{}, f.startErr
	}
	return f.status, nil
}

func (f *fakeBoxes) Terminate(ctx context.Context, sessionID string) (string, error) {
	f.calls = append(f.calls, "terminate:"+sessionID)
	return f.termKind, f.termErr
}

func (f *fakeBoxes) Surfaces(ctx context.Context, sessionID string) (browserSurfaces, string, error) {
	f.calls = append(f.calls, "surfaces:"+sessionID)
	if f.surfErr != nil {
		return nil, "", f.surfErr
	}
	return f.surfaces, f.boxID(), nil
}

// boxID is the id of the fake's running box: status.BoxID, or box-1 when unset. A
// test replaces the box by changing status.BoxID.
func (f *fakeBoxes) boxID() string {
	if f.status.BoxID == "" {
		return "box-1"
	}
	return f.status.BoxID
}

func (f *fakeBoxes) DefaultBackend() (string, error) {
	f.defCalls++
	if f.defErr != nil {
		return "", f.defErr
	}
	if f.def == "" {
		return browserBackendLocal, nil
	}
	return f.def, nil
}

// boxSession is the context a call carries for the session sid. The tool reads the
// session id from the todo scope, as the agent attaches it for every tool call.
func boxSession(sid string) context.Context {
	return tool.WithTodoScope(context.Background(), tool.TodoScope{ProjectRoot: testProjectKey, SessionID: sid})
}

// boxSetup builds the local tool of newTestBrowserTool with box as its box manager.
// The box's surfaces are a fakeSurfaces over boxSurf, which is returned too.
func boxSetup(t *testing.T, box *fakeBoxes) (*browserTool, *fakeSurfaces, *fakeSurfaces, *fakeSurface) {
	t.Helper()
	local := &fakeSurface{png: solidPNG(t, 200, 100), cssW: 200, cssH: 100}
	b, _ := newTestBrowserTool(t, local, 1568)
	boxSurf := &fakeSurface{png: solidPNG(t, 200, 100), cssW: 200, cssH: 100, url: "https://box.example/"}
	bs := &fakeSurfaces{surf: boxSurf, openKey: "tab:bx1"}
	box.surfaces = bs
	b.withBoxes(box)
	return b, b.surfaces.(*fakeSurfaces), bs, boxSurf
}

// browserDefinitionSHA and browserDescriptionSHA are the sha256 of the local-only
// tool's Definition and Description as they stood before the box backend existed.
// With no boxes attached the tools array must still hash to them.
const (
	browserDefinitionSHA  = "fd796b550fe805b7ced7c5c4d062752e84e7f5c60415bfca22392494a69c9b31"
	browserDescriptionSHA = "3ad88013b11382b531f54e40bb1ef7448cdb69ef862f46e88c3d16a87c8d99f6"
)

// TestBrowserBoxes_DefinitionUnchangedWithoutBoxes pins the local-only schema to
// its pre-change bytes, and checks none of the box additions appear in it.
func TestBrowserBoxes_DefinitionUnchangedWithoutBoxes(t *testing.T) {
	b := newBrowserTool(testProjectKey, 1568, nil, nil)
	raw, err := json.Marshal(b.Definition())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(raw)); got != browserDefinitionSHA {
		t.Fatalf("local-only browser definition changed: sha256 %s, want %s", got, browserDefinitionSHA)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(b.Description()))); got != browserDescriptionSHA {
		t.Fatalf("local-only browser description changed: sha256 %s, want %s", got, browserDescriptionSHA)
	}
	for _, q := range []string{`"backend"`, `"start"`, `"terminate"`, `"retain"`} {
		if strings.Contains(string(raw), q) {
			t.Errorf("local-only definition contains %s", q)
		}
	}
	if strings.Contains(b.Description(), "upstash") {
		t.Error("local-only description mentions upstash")
	}
}

// TestBrowserBoxes_DefinitionWithBoxesIsStatic pins the boxed schema: the backend
// property, the box actions and retain are present, the output is deterministic and
// it embeds no tab id or session.
func TestBrowserBoxes_DefinitionWithBoxesIsStatic(t *testing.T) {
	b := newBrowserTool(testProjectKey, 1568, nil, nil).withBoxes(&fakeBoxes{})
	first, err := json.Marshal(b.Definition())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	second, _ := json.Marshal(b.Definition())
	if string(first) != string(second) {
		t.Fatal("boxed Definition changed between calls")
	}
	s := string(first)
	for _, want := range []string{`"backend"`, `"enum":["local","upstash"]`, `"retain"`, `"start","terminate"]`} {
		if !strings.Contains(s, want) {
			t.Errorf("boxed definition lacks %s", want)
		}
	}
	if strings.Contains(s, "tab:") || strings.Contains(s, "bx1") || strings.Contains(s, "sess-1") {
		t.Error("boxed definition embeds a live tab id or session")
	}
	if strings.Contains(s, "default") {
		t.Error("boxed definition says which backend is the default")
	}
}

// TestBrowserBoxes_RoutesByExplicitAndDefaultBackend pins that a call goes to the
// backend it names, or to the session default when it names none.
func TestBrowserBoxes_RoutesByExplicitAndDefaultBackend(t *testing.T) {
	box := &fakeBoxes{status: browserBoxStatus{BoxID: "box-1"}, def: browserBackendUpstash}
	b, local, bs, _ := boxSetup(t, box)
	ctx := boxSession("sess-1")

	// The default is upstash: open with no backend lands on the box.
	if out, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"open"}`)); err != nil || !strings.Contains(out, "bx1") {
		t.Fatalf("default-backend open = %q, %v; want a box tab bx1", out, err)
	}
	if _, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"text"}`)); err != nil {
		t.Fatalf("default-backend text: %v", err)
	}
	if !hasEntry(bs.surfaceCalls, "tab:bx1|") {
		t.Fatalf("box surfaces were not attached for the default backend: %v", bs.surfaceCalls)
	}
	if len(local.surfaceCalls) != 0 || len(local.openURLs) != 0 {
		t.Fatalf("the default upstash call reached the local browser: attach %v open %v", local.surfaceCalls, local.openURLs)
	}

	// An explicit local names the Browser-panel strip, even with a box default.
	out, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"tabs","backend":"local"}`))
	if err != nil || !strings.Contains(out, "strip order") {
		t.Fatalf("explicit local tabs = %q, %v; want the local strip listing", out, err)
	}
	if strings.Contains(out, "bx1") {
		t.Fatalf("explicit local tabs lists a box tab: %q", out)
	}
}

// TestBrowserBoxes_BoxErrorIsNeverALocalFallback pins that a failure to reach the
// box is returned as the tool error and never answered by the local browser.
func TestBrowserBoxes_BoxErrorIsNeverALocalFallback(t *testing.T) {
	box := &fakeBoxes{surfErr: errors.New("browser box: no box is running for this session; call action=start first")}
	b, local, bs, _ := boxSetup(t, box)
	ctx := boxSession("sess-1")
	for _, args := range []string{
		`{"action":"tabs","backend":"upstash"}`,
		`{"action":"navigate","url":"https://example.com/","backend":"upstash"}`,
		`{"action":"text","tab":"b1","backend":"upstash"}`,
	} {
		_, err := b.ExecuteCtx(ctx, json.RawMessage(args))
		if err == nil || !strings.Contains(err.Error(), "call action=start") {
			t.Fatalf("%s: err = %v, want the box's error returned", args, err)
		}
	}
	if len(local.surfaceCalls) != 0 || len(local.openURLs) != 0 || len(local.closeKeys) != 0 {
		t.Fatalf("a box failure reached the local browser: attach %v open %v close %v", local.surfaceCalls, local.openURLs, local.closeKeys)
	}
	if len(bs.surfaceCalls) != 0 {
		t.Fatalf("a box failure attached a box tab: %v", bs.surfaceCalls)
	}
}

// TestBrowserBoxes_UpstashWithoutBoxesIsUnavailable pins the error for a tool with no
// box manager: an explicit upstash names what is missing, and the box actions do not
// exist on the tool at all.
func TestBrowserBoxes_UpstashWithoutBoxesIsUnavailable(t *testing.T) {
	local := &fakeSurface{}
	b, _ := newTestBrowserTool(t, local, 1568)
	ctx := boxSession("sess-1")
	if _, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"tabs","backend":"upstash"}`)); err == nil || err.Error() != "browser: the upstash backend is not available in this session" {
		t.Fatalf("explicit upstash tabs: err = %v, want the not-available error", err)
	}
	if _, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"start","backend":"upstash"}`)); err == nil || !strings.Contains(err.Error(), "unknown action") {
		t.Fatalf("start without boxes: err = %v, want an unknown-action error", err)
	}
	if _, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"tabs","backend":"nope"}`)); err == nil || !strings.Contains(err.Error(), "unknown backend") {
		t.Fatalf("unknown backend err = %v, want an unknown-backend error", err)
	}
}

// TestBrowserBoxes_TabNamespacesAreIsolated pins that a local tab and a box tab with
// the same id never share state: the strip, the opened lists, element refs and the
// screenshot a coordinate click uses.
func TestBrowserBoxes_TabNamespacesAreIsolated(t *testing.T) {
	box := &fakeBoxes{status: browserBoxStatus{BoxID: "box-1"}}
	b, local, bs, _ := boxSetup(t, box)
	bs.openKey = "tab:b1" // the same id as the local strip's active tab
	ctx := boxSession("sess-1")

	if out, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"open","backend":"upstash"}`)); err != nil || !strings.Contains(out, "opened tab b1") {
		t.Fatalf("box open = %q, %v", out, err)
	}
	// The local listing still shows only the strip, with its own active tab.
	localOut, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"tabs","backend":"local"}`))
	if err != nil {
		t.Fatalf("local tabs: %v", err)
	}
	if strings.Contains(localOut, "opened by this agent") || !strings.Contains(localOut, "b1 (active)") {
		t.Fatalf("local tabs picked up the box's tab: %q", localOut)
	}
	// The box listing shows only the tab this agent opened there, not the strip.
	boxOut, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"tabs","backend":"upstash"}`))
	if err != nil {
		t.Fatalf("upstash tabs: %v", err)
	}
	if !strings.Contains(boxOut, "b1 (opened by this agent)") || strings.Contains(boxOut, "b2") || strings.Contains(boxOut, "b3") {
		t.Fatalf("upstash tabs = %q; want only the opened box tab", boxOut)
	}
	if got := b.openedIn(browserBackendLocal); len(got) != 0 {
		t.Fatalf("the local opened list = %v after a box open; want empty", got)
	}
	if got := b.openedIn(browserBackendUpstash); len(got) != 1 || got[0] != "b1" {
		t.Fatalf("the upstash opened list = %v; want [b1]", got)
	}
	// The last tab opened is per backend: the local side has none to default to.
	if id, err := b.resolveTab(browserBackendUpstash, browsertabs.ProjectBrowsers{}, "", nil); err != nil || id != "b1" {
		t.Fatalf("upstash default tab = %q, %v; want b1", id, err)
	}
	if _, err := b.resolveTab(browserBackendLocal, browsertabs.ProjectBrowsers{}, "", nil); !errors.Is(err, errNoBrowserTab) {
		t.Fatalf("local default tab with none opened locally = %v; want errNoBrowserTab", err)
	}
	// Element refs are per backend, so an @eN from one backend never names the other's node.
	if b.refsFor(browserBackendLocal, "b1") == b.refsFor(browserBackendUpstash, "b1") {
		t.Fatal("local and upstash share an element ref table for tab b1")
	}
	// A coordinate click needs a screenshot of the same backend and tab. A local
	// screenshot of b1 must not let an upstash click on b1 use its coordinates.
	if _, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"screenshot","tab":"b1","backend":"local"}`)); err != nil {
		t.Fatalf("local screenshot: %v", err)
	}
	if _, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"click","tab":"b1","x":5,"y":5,"backend":"upstash"}`)); err == nil || !strings.Contains(err.Error(), "needs a screenshot") {
		t.Fatalf("upstash click used the local screenshot: err = %v", err)
	}
	if len(local.surfaceCalls) == 0 {
		t.Fatal("the local screenshot did not reach the local browser")
	}
}

// TestBrowserBoxes_StartAndTerminate pins the happy path: start reports the running
// box, its expiry in UTC, whether it was reused and the billing reminder, and carries
// no CDP address or token. terminate deletes the session's box.
func TestBrowserBoxes_StartAndTerminate(t *testing.T) {
	expires := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	box := &fakeBoxes{status: browserBoxStatus{BoxID: "box-1", ExpiresAt: expires}}
	b, _, _, _ := boxSetup(t, box)
	ctx := boxSession("sess-1")

	out, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"start","backend":"upstash"}`))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	for _, want := range []string{"box-1", "2026-11-21T10:00:00Z", "running", "bills", "action=terminate", "created"} {
		if !strings.Contains(out, want) {
			t.Errorf("start result lacks %q: %s", want, out)
		}
	}
	for _, leak := range []string{"ws://", "wss://", "token", "cdp", "http"} {
		if strings.Contains(strings.ToLower(out), leak) {
			t.Errorf("start result contains %q: %s", leak, out)
		}
	}
	if got := strings.Join(box.calls, ","); got != "start:sess-1" {
		t.Fatalf("box calls after start = %q, want start:sess-1 only", got)
	}

	box.status.Reused = true
	out, err = b.ExecuteCtx(ctx, json.RawMessage(`{"action":"start","backend":"upstash"}`))
	if err != nil || !strings.Contains(out, "already running") {
		t.Fatalf("second start = %q, %v; want the already-running text", out, err)
	}

	out, err = b.ExecuteCtx(ctx, json.RawMessage(`{"action":"terminate","backend":"upstash"}`))
	if err != nil || !strings.Contains(out, "terminated") {
		t.Fatalf("terminate = %q, %v", out, err)
	}
	if got := box.calls[len(box.calls)-1]; got != "terminate:sess-1" {
		t.Fatalf("last box call = %q, want terminate:sess-1", got)
	}

	box.startErr = errors.New("box quota exceeded")
	if _, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"start","backend":"upstash"}`)); err == nil || !strings.Contains(err.Error(), "quota") {
		t.Fatalf("start with a box error = %v, want it returned", err)
	}
}

// TestBrowserBoxes_StartAndTerminateNeedUpstash pins that the box actions are valid
// only on the upstash backend, explicit or by default, and need a session.
func TestBrowserBoxes_StartAndTerminateNeedUpstash(t *testing.T) {
	box := &fakeBoxes{status: browserBoxStatus{BoxID: "box-1"}}
	b, _, _, _ := boxSetup(t, box)
	ctx := boxSession("sess-1")
	for _, args := range []string{`{"action":"start","backend":"local"}`, `{"action":"start"}`, `{"action":"terminate","backend":"local"}`} {
		if _, err := b.ExecuteCtx(ctx, json.RawMessage(args)); err == nil || !strings.Contains(err.Error(), "needs backend=upstash") {
			t.Fatalf("%s: err = %v, want the needs-upstash refusal", args, err)
		}
	}
	if len(box.calls) != 0 {
		t.Fatalf("a refused box action reached the box manager: %v", box.calls)
	}
	if _, err := b.ExecuteCtx(context.Background(), json.RawMessage(`{"action":"start","backend":"upstash"}`)); err == nil || !strings.Contains(err.Error(), "session id") {
		t.Fatalf("start without a session id: err = %v, want a session error", err)
	}
	if _, err := b.ExecuteCtx(context.Background(), json.RawMessage(`{"action":"tabs","backend":"upstash"}`)); err == nil || !strings.Contains(err.Error(), "session id") {
		t.Fatalf("upstash tabs without a session id: err = %v, want a session error", err)
	}
	if len(box.calls) != 0 {
		t.Fatalf("a call without a session reached the box manager: %v", box.calls)
	}
}

// TestBrowserBoxes_SubAgentCannotStartTerminateOrRetain pins that every sub-agent is
// refused start, terminate and retain before any permission decision, while it may
// still use the box its session runs.
func TestBrowserBoxes_SubAgentCannotStartTerminateOrRetain(t *testing.T) {
	box := &fakeBoxes{status: browserBoxStatus{BoxID: "box-1"}}
	b, _, _, _ := boxSetup(t, box)
	ctx := boxSession("sess-1")
	for _, name := range []string{"general", "explore"} {
		child := b.ForAgent(name).(*browserTool)
		for _, args := range []string{
			`{"action":"start","backend":"upstash"}`,
			`{"action":"terminate","backend":"upstash"}`,
			`{"action":"open","backend":"upstash","retain":true}`,
		} {
			if child.Refusal(json.RawMessage(args)) == "" {
				t.Errorf("%s agent: Refusal(%s) is empty; want a refusal", name, args)
			}
			if _, err := child.ExecuteCtx(ctx, json.RawMessage(args)); err == nil || !strings.Contains(err.Error(), "sub-agent") {
				t.Errorf("%s agent: %s err = %v, want the sub-agent refusal", name, args, err)
			}
		}
	}
	if len(box.calls) != 0 {
		t.Fatalf("a refused sub-agent call reached the box manager: %v", box.calls)
	}
	general := b.ForAgent("general").(*browserTool)
	if r := general.Refusal(json.RawMessage(`{"action":"tabs","backend":"upstash"}`)); r != "" {
		t.Fatalf("a general sub-agent is refused tabs on the box: %s", r)
	}
}

// TestBrowserBoxes_RetainIsRefused pins where retain is refused. Once the retained-box
// phase exists, retain is valid only with action=start on the upstash backend: every
// other action, and a start on the local backend, is refused before any box call. A
// valid start passes retain=true to the box manager.
func TestBrowserBoxes_RetainIsRefused(t *testing.T) {
	box := &fakeBoxes{status: browserBoxStatus{BoxID: "box-1", Kind: boxsession.KindRetained, LoginState: "work"}}
	b, _, _, _ := boxSetup(t, box)
	ctx := boxSession("sess-1")
	for _, args := range []string{
		`{"action":"open","backend":"upstash","retain":true}`,
		`{"action":"tabs","backend":"upstash","retain":true}`,
		`{"action":"terminate","backend":"upstash","retain":true}`,
		`{"action":"start","backend":"local","retain":true}`,
	} {
		if _, err := b.ExecuteCtx(ctx, json.RawMessage(args)); err == nil || !strings.Contains(err.Error(), browserRetainRule) {
			t.Fatalf("%s: err = %v, want the retain rule", args, err)
		}
	}
	if len(box.calls) != 0 {
		t.Fatalf("a refused retain call reached the box manager: %v", box.calls)
	}
	out, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"start","backend":"upstash","retain":true}`))
	if err != nil || !strings.Contains(out, "created retained box box-1") {
		t.Fatalf("retained start = %q, %v; want the created-retained text", out, err)
	}
	if len(box.retains) != 1 || !box.retains[0] {
		t.Fatalf("the manager saw retain flags %v, want one true", box.retains)
	}
}

// TestBrowserBoxes_RetainedStartTexts pins the three retained start results: a new
// box, a resumed one (which also drops this agent's remembered upstash tabs, since a
// pause closed them), and one already running. Each says the box is kept between
// sessions and when it is deleted.
func TestBrowserBoxes_RetainedStartTexts(t *testing.T) {
	deleteAt := time.Date(2026, 12, 20, 9, 0, 0, 0, time.UTC)
	box := &fakeBoxes{status: browserBoxStatus{BoxID: "box-7", Kind: boxsession.KindRetained, LoginState: "work", ExpiresAt: deleteAt}}
	b, _, _, _ := boxSetup(t, box)
	ctx := boxSession("sess-1")
	const kept = "It is kept between sessions: it is paused when idle or when you call action=terminate, and deleted if unused until 2026-12-20T09:00:00Z (UTC). A pause closes every open tab."

	out, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"start","backend":"upstash","retain":true}`))
	if err != nil || !strings.Contains(out, `created retained box box-7 for login state "work".`) || !strings.HasSuffix(out, kept) {
		t.Fatalf("created retained start = %q, %v", out, err)
	}

	b.recordOpened(browserTarget{backend: browserBackendUpstash, sessionID: "sess-1"}, "tab:stale")
	box.status.Resumed = true
	out, err = b.ExecuteCtx(ctx, json.RawMessage(`{"action":"start","backend":"upstash","retain":true}`))
	if err != nil || !strings.Contains(out, `resumed retained box box-7 for login state "work". Its earlier tabs are gone.`) || !strings.HasSuffix(out, kept) {
		t.Fatalf("resumed retained start = %q, %v", out, err)
	}
	if got := b.openedIn(browserBackendUpstash); len(got) != 0 {
		t.Fatalf("a resumed box kept this agent's remembered tabs: %v", got)
	}

	box.status.Resumed = false
	box.status.Reused = true
	out, err = b.ExecuteCtx(ctx, json.RawMessage(`{"action":"start","backend":"upstash","retain":true}`))
	if err != nil || !strings.Contains(out, "retained box box-7 was already running for this session.") || !strings.HasSuffix(out, kept) {
		t.Fatalf("reused retained start = %q, %v", out, err)
	}
	if strings.Contains(out, "restored") {
		t.Fatalf("a reused box repeats the restore sentence: %q", out)
	}
}

// TestBrowserBoxes_RetainedTerminatePauses pins the terminate text for a retained box,
// which is read from the kind the manager reports, and that the ephemeral text is
// unchanged for an ephemeral box.
func TestBrowserBoxes_RetainedTerminatePauses(t *testing.T) {
	box := &fakeBoxes{status: browserBoxStatus{BoxID: "box-1"}, termKind: boxsession.KindRetained}
	b, _, _, _ := boxSetup(t, box)
	ctx := boxSession("sess-1")
	out, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"terminate","backend":"upstash"}`))
	if err != nil || out != "paused the retained browser box for this session. Its tabs are gone, and its login state is saved. Call browser action=start with backend=upstash and retain=true to resume it. To delete it, use Settings, Upstash Box." {
		t.Fatalf("retained terminate = %q, %v", out, err)
	}

	box.termKind = boxsession.KindEphemeral
	out, err = b.ExecuteCtx(ctx, json.RawMessage(`{"action":"terminate","backend":"upstash"}`))
	if err != nil || out != "terminated the browser box for this session. Its tabs are gone; start a new box before using backend=upstash again." {
		t.Fatalf("ephemeral terminate = %q, %v", out, err)
	}
}

// TestBrowserBoxes_PausedAndNeedsLoginStateReachTheModel pins that a paused box and a
// missing login state are returned as their own errors, not wrapped into a generic
// message, and that a paused box drops this agent's remembered upstash tabs.
func TestBrowserBoxes_PausedAndNeedsLoginStateReachTheModel(t *testing.T) {
	box := &fakeBoxes{status: browserBoxStatus{BoxID: "box-1"}}
	b, _, _, _ := boxSetup(t, box)
	ctx := boxSession("sess-1")

	b.recordOpened(browserTarget{backend: browserBackendUpstash, sessionID: "sess-1"}, "tab:bx1")
	box.surfErr = boxsession.ErrPaused
	if _, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"tabs","backend":"upstash"}`)); !errors.Is(err, boxsession.ErrPaused) {
		t.Fatalf("tabs on a paused box = %v, want ErrPaused", err)
	}
	if got := b.openedIn(browserBackendUpstash); len(got) != 0 {
		t.Fatalf("a paused box kept this agent's remembered tabs: %v", got)
	}

	box.surfErr = nil
	box.startErr = boxsession.ErrRetainNeedsLoginState
	if _, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"start","backend":"upstash","retain":true}`)); !errors.Is(err, boxsession.ErrRetainNeedsLoginState) {
		t.Fatalf("retain without a login state = %v, want ErrRetainNeedsLoginState", err)
	}
}

// TestBrowserBoxes_URLRefusedOnTheBox pins the box's address list: a loopback,
// private or localhost URL is refused before any box call, while the local browser
// keeps allowing loopback.
func TestBrowserBoxes_URLRefusedOnTheBox(t *testing.T) {
	box := &fakeBoxes{status: browserBoxStatus{BoxID: "box-1"}}
	b, local, bs, _ := boxSetup(t, box)
	ctx := boxSession("sess-1")
	refused := []string{
		`{"action":"navigate","url":"http://127.0.0.1:9222/json/version","backend":"upstash"}`,
		`{"action":"navigate","url":"http://localhost:3000/","backend":"upstash"}`,
		`{"action":"navigate","url":"http://[fd00::1]/","backend":"upstash"}`,
		`{"action":"open","url":"http://10.0.0.5/","backend":"upstash"}`,
		`{"action":"open","url":"http://2130706433/","backend":"upstash"}`,
	}
	for _, args := range refused {
		if _, err := b.ExecuteCtx(ctx, json.RawMessage(args)); err == nil || !strings.Contains(err.Error(), "upstash box") {
			t.Errorf("%s: err = %v, want the box URL refusal", args, err)
		}
	}
	if len(box.calls) != 0 || len(bs.surfaceCalls) != 0 || len(bs.openURLs) != 0 {
		t.Fatalf("a refused box URL reached the box: calls %v attach %v open %v", box.calls, bs.surfaceCalls, bs.openURLs)
	}
	if _, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"navigate","tab":"b1","url":"http://127.0.0.1:8080/"}`)); err != nil {
		t.Fatalf("the local browser refused loopback, which it must still allow: %v", err)
	}
	if len(local.surfaceCalls) == 0 {
		t.Fatal("the local navigate never reached the local browser")
	}
}

// TestBrowserBoxes_UploadsRefusedOnTheBox pins that uploads, and a fill_form file
// list, are refused on the box before any path is read or any page is touched.
func TestBrowserBoxes_UploadsRefusedOnTheBox(t *testing.T) {
	box := &fakeBoxes{status: browserBoxStatus{BoxID: "box-1"}}
	b, _, bs, boxSurf := boxSetup(t, box)
	ctx := boxSession("sess-1")
	cases := map[string]string{
		"upload":         `{"action":"upload","target":"#file","files":["/etc/hosts"],"backend":"upstash"}`,
		"fill_form list": `{"action":"fill_form","backend":"upstash","fields":[{"selector":"#f","value":["/etc/hosts"]}]}`,
	}
	for name, args := range cases {
		_, err := b.ExecuteCtx(ctx, json.RawMessage(args))
		if err == nil || err.Error() != errBrowserBoxUploads.Error() {
			t.Errorf("%s: err = %v, want %q", name, err, errBrowserBoxUploads)
		}
	}
	if len(box.calls) != 0 || len(bs.surfaceCalls) != 0 || len(boxSurf.got()) != 0 {
		t.Fatalf("a refused upload reached the box: calls %v attach %v surface %v", box.calls, bs.surfaceCalls, boxSurf.got())
	}
}

// TestBrowserBoxes_CloseOpenedTabsUsesTheBoxSession pins teardown: the box tabs close
// through the box session recorded when they were opened, with no session context,
// and a box that is already gone is logged, never returned.
func TestBrowserBoxes_CloseOpenedTabsUsesTheBoxSession(t *testing.T) {
	box := &fakeBoxes{status: browserBoxStatus{BoxID: "box-1"}}
	b, _, bs, _ := boxSetup(t, box)
	if _, err := b.ExecuteCtx(boxSession("sess-1"), json.RawMessage(`{"action":"open","backend":"upstash"}`)); err != nil {
		t.Fatalf("box open: %v", err)
	}
	if err := b.CloseOpenedTabs(context.Background()); err != nil {
		t.Fatalf("CloseOpenedTabs = %v, want nil", err)
	}
	if !hasEntry(bs.closeKeys, "tab:bx1") {
		t.Fatalf("the box tab was not closed at teardown: %v", bs.closeKeys)
	}
	if got := box.calls[len(box.calls)-1]; got != "surfaces:sess-1" {
		t.Fatalf("teardown reached the box as %q, want the recorded session", got)
	}

	// A new box tab, then the box disappears before teardown.
	if _, err := b.ExecuteCtx(boxSession("sess-1"), json.RawMessage(`{"action":"open","backend":"upstash"}`)); err != nil {
		t.Fatalf("second box open: %v", err)
	}
	box.surfErr = errors.New("box is gone")
	if err := b.CloseOpenedTabs(context.Background()); err != nil {
		t.Fatalf("CloseOpenedTabs with the box gone = %v, want the failure logged, not returned", err)
	}
}

// TestBrowserBoxes_UnreadableDefaultBackendIsAToolError pins that a call omitting
// backend returns the settings error, and never falls back to either backend. An
// explicit backend never reads the setting at all.
func TestBrowserBoxes_UnreadableDefaultBackendIsAToolError(t *testing.T) {
	box := &fakeBoxes{defErr: errors.New("read the Upstash Box settings: invalid character")}
	b, local, bs, _ := boxSetup(t, box)
	ctx := boxSession("sess-1")

	_, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"tabs"}`))
	if err == nil || !strings.Contains(err.Error(), "cannot read the browser backend setting") || !strings.Contains(err.Error(), "invalid character") {
		t.Fatalf("call without backend: err = %v, want the backend-setting error", err)
	}
	if len(local.surfaceCalls) != 0 || len(local.openURLs) != 0 || len(bs.surfaceCalls) != 0 || len(box.calls) != 0 {
		t.Fatalf("an unreadable setting reached a browser: local %v open %v box %v calls %v", local.surfaceCalls, local.openURLs, bs.surfaceCalls, box.calls)
	}
	if box.defCalls != 1 {
		t.Fatalf("DefaultBackend calls = %d after the omitted-backend call, want 1", box.defCalls)
	}

	if _, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"tabs","backend":"local"}`)); err != nil {
		t.Fatalf("explicit local tabs with an unreadable setting: %v", err)
	}
	if _, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"tabs","backend":"upstash"}`)); err != nil {
		t.Fatalf("explicit upstash tabs with an unreadable setting: %v", err)
	}
	if box.defCalls != 1 {
		t.Fatalf("explicit backends read the setting: DefaultBackend calls = %d, want still 1", box.defCalls)
	}
}

// TestBrowserBoxes_ReplacedBoxDropsItsTabsRefsAndScreenshot pins the stale-tab fix:
// the tabs this agent opened on a box, their element refs and the screenshot are
// dropped once the session's box is replaced. A tab id from the old box is then an
// error, never a blank tab attached on the new box.
func TestBrowserBoxes_ReplacedBoxDropsItsTabsRefsAndScreenshot(t *testing.T) {
	box := &fakeBoxes{status: browserBoxStatus{BoxID: "box-1"}}
	b, _, bs, _ := boxSetup(t, box)
	ctx := boxSession("sess-1")

	if out, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"open","backend":"upstash"}`)); err != nil || !strings.Contains(out, "opened tab bx1") {
		t.Fatalf("open on box-1 = %q, %v", out, err)
	}
	if _, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"screenshot","tab":"bx1","backend":"upstash"}`)); err != nil {
		t.Fatalf("screenshot on box-1: %v", err)
	}
	b.refsFor(browserBackendUpstash, "bx1")
	// The same box keeps its tabs: a text call on bx1 still works before the swap.
	if _, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"text","tab":"bx1","backend":"upstash"}`)); err != nil {
		t.Fatalf("text on box-1 before the replacement: %v", err)
	}

	// The box is replaced: a new id, and a new Chrome with its own tabs.
	fresh := &fakeSurfaces{surf: bs.surf, openKey: "tab:bx2"}
	box.status.BoxID = "box-2"
	box.surfaces = fresh

	_, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"text","tab":"bx1","backend":"upstash"}`))
	if err == nil || !strings.Contains(err.Error(), "unknown tab; take action=tabs") {
		t.Fatalf("text on the old tab id after the replacement: err = %v, want the unknown-tab error", err)
	}
	if len(fresh.surfaceCalls) != 0 {
		t.Fatalf("the old tab id attached a tab on the new box: %v", fresh.surfaceCalls)
	}
	out, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"tabs","backend":"upstash"}`))
	if err != nil || out != "no browser tabs are open on the upstash box; use action=open to open one" {
		t.Fatalf("tabs after the replacement = %q, %v; want no tabs", out, err)
	}
	if got := b.openedIn(browserBackendUpstash); len(got) != 0 {
		t.Fatalf("opened upstash tabs after the replacement = %v, want none", got)
	}
	b.mu.Lock()
	sp := b.spaces[browserBackendUpstash]
	refs, boxID := len(sp.refs), sp.boxID
	b.mu.Unlock()
	if refs != 0 || boxID != "box-2" {
		t.Fatalf("upstash space after the replacement: %d ref table(s), box %q; want none, box-2", refs, boxID)
	}
	if b.shot != nil {
		t.Fatal("the screenshot of the old box survived the replacement")
	}
}

// TestBrowserURLRefusalMessagesShareOneFormat pins that the local and the box
// refusals reach the caller as one sentence each, without a doubled "browser" prefix.
func TestBrowserURLRefusalMessagesShareOneFormat(t *testing.T) {
	local := checkBrowserURL("http://169.254.169.254/")
	if local == nil || local.Error() != "browser may not open link-local or cloud-metadata addresses" {
		t.Fatalf("local metadata refusal = %v", local)
	}
	box := checkBoxBrowserURL("http://127.0.0.1:9222/json/version")
	if box == nil || !strings.HasPrefix(box.Error(), "browser on the upstash box may not open ") {
		t.Fatalf("box loopback refusal = %v", box)
	}
	for _, err := range []error{local, box} {
		if strings.Contains(err.Error(), "browser: browser") {
			t.Errorf("refusal %q doubles the browser prefix", err)
		}
	}
}

// TestBrowserBoxes_CloseOnAReplacedBoxSkipsTheOldTabs pins that the run-end teardown
// never closes a tab id from an old box on the box that replaced it: the same id
// may name another agent's tab there.
func TestBrowserBoxes_CloseOnAReplacedBoxSkipsTheOldTabs(t *testing.T) {
	box := &fakeBoxes{status: browserBoxStatus{BoxID: "box-1"}}
	b, _, _, _ := boxSetup(t, box)
	ctx := boxSession("sess-1")
	if _, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"open","backend":"upstash"}`)); err != nil {
		t.Fatalf("open on box-1: %v", err)
	}
	fresh := &fakeSurfaces{surf: &fakeSurface{}, openKey: "tab:bx1"}
	box.status.BoxID = "box-2"
	box.surfaces = fresh

	b.closeBoxTabs(ctx)
	if len(fresh.closeKeys) != 0 {
		t.Fatalf("teardown closed %v on the replacement box, want nothing", fresh.closeKeys)
	}
}

// hasEntry reports whether want is an element of list, or a prefix of one.
func hasEntry(list []string, want string) bool {
	for _, s := range list {
		if s == want || strings.HasPrefix(s, want) {
			return true
		}
	}
	return false
}
