package browse

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/browse/cdp"
)

// headlessFake implements chromeManager plus headlessAttacher. Targets are zero
// values: these tests exercise the accessors' selection logic, not CDP.
type headlessFake struct {
	procFakeManager
	ensureCalls []string
	ensureNew   bool
	openCalls   int
	openKey     string
	ensureErr   error
	closeCalls  []string
}

func (f *headlessFake) EnsureHeadless(_ context.Context, stateKey string) (*cdp.Target, bool, error) {
	f.ensureCalls = append(f.ensureCalls, stateKey)
	if f.ensureErr != nil {
		return nil, false, f.ensureErr
	}
	return &cdp.Target{}, f.ensureNew, nil
}

func (f *headlessFake) CloseTarget(_ context.Context, stateKey string) error {
	f.closeCalls = append(f.closeCalls, stateKey)
	return nil
}

func (f *headlessFake) OpenHeadless(context.Context) (*cdp.Target, string, error) {
	f.openCalls++
	return &cdp.Target{}, f.openKey, nil
}

// TestAgentTarget_ErrorsWhenManagerNotConfigured pins that an agent tool gets
// an explicit error, never a silent nil target, before Chrome is configured.
func TestAgentTarget_ErrorsWhenManagerNotConfigured(t *testing.T) {
	s := New("apitoken", nil)
	if _, _, err := s.AgentTarget(context.Background(), "tab:a", ""); err == nil {
		t.Fatal("AgentTarget returned no error without a Chrome manager")
	}
	if _, err := s.OpenAgentTab(context.Background(), ""); err == nil {
		t.Fatal("OpenAgentTab returned no error without a Chrome manager")
	}
}

// TestAgentTarget_RejectsManagerWithoutHeadlessAttach pins that a manager lacking
// the headless attach (the test fakes) fails instead of attaching a panel-less
// target some other way.
func TestAgentTarget_RejectsManagerWithoutHeadlessAttach(t *testing.T) {
	s := New("apitoken", nil)
	s.SetCDPManager(&procFakeManager{})
	if _, _, err := s.AgentTarget(context.Background(), "tab:a", ""); err == nil {
		t.Fatal("AgentTarget accepted a manager without headless attach")
	}
}

// TestAgentTarget_UsesHeadlessAttachForTheTabKey pins that the tool's tab key is
// the one attached, and that an existing target is returned without a load.
func TestAgentTarget_UsesHeadlessAttachForTheTabKey(t *testing.T) {
	s := New("apitoken", nil)
	f := &headlessFake{}
	s.SetCDPManager(f)
	if _, _, err := s.AgentTarget(context.Background(), "tab:restored", "https://example.com/"); err != nil {
		t.Fatalf("AgentTarget: %v", err)
	}
	if len(f.ensureCalls) != 1 || f.ensureCalls[0] != "tab:restored" {
		t.Fatalf("EnsureHeadless calls = %v, want [tab:restored]", f.ensureCalls)
	}
}

// TestAgentTarget_ExistingTargetIsNotNavigated pins that the stored URL only
// loads into a target this call attached: a page already in a panel keeps its
// place. (created=false with a URL must not reach Navigate, which would panic
// on the zero target.)
func TestAgentTarget_ExistingTargetIsNotNavigated(t *testing.T) {
	s := New("apitoken", nil)
	s.SetCDPManager(&headlessFake{ensureNew: false})
	if _, _, err := s.AgentTarget(context.Background(), "tab:shown", "https://example.com/"); err != nil {
		t.Fatalf("AgentTarget: %v", err)
	}
}

// TestOpenAgentTab_ReturnsFreshKey pins that OpenAgentTab delegates to the
// manager's fresh-key open and returns its key.
func TestOpenAgentTab_ReturnsFreshKey(t *testing.T) {
	s := New("apitoken", nil)
	f := &headlessFake{openKey: "tab:new1"}
	s.SetCDPManager(f)
	key, err := s.OpenAgentTab(context.Background(), "")
	if err != nil || key != "tab:new1" || f.openCalls != 1 {
		t.Fatalf("OpenAgentTab = (%q, %v) after %d opens", key, err, f.openCalls)
	}
}

// fakeChrome answers the CDP calls a headless attach and its first navigation
// make, over a pipe, so the real cdp.Manager and cdp.Target run against it.
// Every command is recorded; unknown methods get an empty result.
type fakeChrome struct {
	mu           sync.Mutex
	readyState   string
	failNavigate bool
	// failReadyState makes the readyState probe fail, which is how a cancelled
	// or broken wait is simulated after a successful navigation.
	failReadyState bool
	// onNavigate, when set, runs as Page.navigate is answered.
	onNavigate func()
	seq        int
	calls      []fakeCall
}

type fakeCall struct {
	Method string
	Params string
}

// newFakeChromeManager returns a real manager wired to a fakeChrome. readyState
// is what document.readyState reports; failNavigate makes Page.navigate return a
// CDP error.
func newFakeChromeManager(t *testing.T, readyState string, failNavigate bool) (*cdp.Manager, *fakeChrome) {
	t.Helper()
	f := &fakeChrome{readyState: readyState, failNavigate: failNavigate}
	clientR, chromeW := io.Pipe()
	chromeR, clientW := io.Pipe()
	m := cdp.NewManager(cdp.ManagerOptions{})
	m.SetLauncher(func(context.Context) (*cdp.Conn, <-chan int, func(), error) {
		return cdp.NewConn(clientR, clientW), make(chan int), func() {}, nil
	})
	go f.serve(chromeR, chromeW)
	t.Cleanup(func() {
		_ = m.Close(context.Background())
		_ = clientR.Close()
		_ = chromeR.Close()
		_ = clientW.Close()
		_ = chromeW.Close()
	})
	return m, f
}

func (f *fakeChrome) serve(r io.Reader, w io.Writer) {
	var buf []byte
	tmp := make([]byte, 4096)
	for {
		n, err := r.Read(tmp)
		buf = append(buf, tmp[:n]...)
		for {
			i := strings.IndexByte(string(buf), 0)
			if i < 0 {
				break
			}
			frame := buf[:i]
			buf = buf[i+1:]
			f.handle(frame, w)
		}
		if err != nil {
			return
		}
	}
}

func (f *fakeChrome) handle(frame []byte, w io.Writer) {
	var msg struct {
		ID        *int64          `json:"id"`
		Method    string          `json:"method"`
		Params    json.RawMessage `json:"params"`
		SessionID string          `json:"sessionId"`
	}
	if err := json.Unmarshal(frame, &msg); err != nil || msg.ID == nil {
		return
	}
	result, errText := f.reply(msg.Method, string(msg.Params))
	out := map[string]any{"id": *msg.ID}
	if errText != "" {
		out["error"] = map[string]any{"code": -32000, "message": errText}
	} else {
		out["result"] = result
	}
	if msg.SessionID != "" {
		out["sessionId"] = msg.SessionID
	}
	b, _ := json.Marshal(out)
	_, _ = w.Write(append(b, 0))
}

func (f *fakeChrome) reply(method, params string) (any, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeCall{Method: method, Params: params})
	f.seq++
	switch method {
	case "Target.createBrowserContext":
		return map[string]string{"browserContextId": fmt.Sprintf("ctx-%d", f.seq)}, ""
	case "Target.createTarget":
		return map[string]string{"targetId": fmt.Sprintf("target-%d", f.seq)}, ""
	case "Target.attachToTarget":
		return map[string]string{"sessionId": fmt.Sprintf("sess-%d", f.seq)}, ""
	case "Target.getTargetInfo":
		return map[string]any{"targetInfo": map[string]string{"type": "page", "url": "about:blank"}}, ""
	case "Runtime.evaluate":
		if strings.Contains(params, "document.readyState") {
			if f.failReadyState {
				return nil, "readyState probe failed"
			}
			return map[string]any{"result": map[string]string{"type": "string", "value": f.readyState}}, ""
		}
		return map[string]any{}, ""
	case "Page.navigate":
		if f.onNavigate != nil {
			f.onNavigate()
		}
		if f.failNavigate {
			return nil, "fake: navigation refused"
		}
		return map[string]string{"frameId": "frame-1"}, ""
	}
	return map[string]any{}, ""
}

// recorded returns the calls made so far for one method.
func (f *fakeChrome) recorded(method string) []fakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []fakeCall
	for _, c := range f.calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

// withLoadCap shortens the readyState wait for one test.
func withLoadCap(t *testing.T, d time.Duration) {
	t.Helper()
	prev := agentLoadCap
	agentLoadCap = d
	t.Cleanup(func() { agentLoadCap = prev })
}

// A freshly attached tab is navigated, and AgentTarget returns once the page is
// complete, with no note.
func TestAgentTargetLoadsStoredURLAndWaitsForComplete(t *testing.T) {
	m, chrome := newFakeChromeManager(t, "complete", false)
	s := New("apitoken", nil)
	s.SetCDPManager(&realManagerAdapter{Manager: m})
	tgt, note, err := s.AgentTarget(context.Background(), "tab:fresh", "https://example.com/")
	if err != nil {
		t.Fatalf("AgentTarget: %v", err)
	}
	if tgt == nil || note != "" {
		t.Fatalf("AgentTarget = (target %v, note %q), want a target and no note", tgt != nil, note)
	}
	navs := chrome.recorded("Page.navigate")
	if len(navs) != 1 || !strings.Contains(navs[0].Params, "https://example.com/") {
		t.Fatalf("Page.navigate calls = %+v, want one to the stored URL", navs)
	}
}

// A page still loading at the cap is returned with a note, not an error.
func TestAgentTargetNoteWhenPageDoesNotFinishLoading(t *testing.T) {
	withLoadCap(t, 300*time.Millisecond)
	m, _ := newFakeChromeManager(t, "loading", false)
	s := New("apitoken", nil)
	s.SetCDPManager(&realManagerAdapter{Manager: m})
	tgt, note, err := s.AgentTarget(context.Background(), "tab:slow", "https://example.com/")
	if err != nil {
		t.Fatalf("AgentTarget: %v", err)
	}
	if tgt == nil {
		t.Fatal("AgentTarget returned no target for a page that is still loading")
	}
	if !strings.Contains(note, "did not finish loading within 300ms") {
		t.Fatalf("note = %q, want the load-timeout note", note)
	}
	if _, ok := m.LookupTarget("tab:slow"); !ok {
		t.Fatal("a slow page's tab was dropped")
	}
}

// When Page.navigate fails, the blank tab is closed again and the call errors.
func TestAgentTargetNavigateFailureClosesBlankTarget(t *testing.T) {
	m, chrome := newFakeChromeManager(t, "complete", true)
	s := New("apitoken", nil)
	s.SetCDPManager(&realManagerAdapter{Manager: m})
	tgt, note, err := s.AgentTarget(context.Background(), "tab:refused", "https://example.com/")
	if err == nil || !strings.Contains(err.Error(), "navigation refused") {
		t.Fatalf("AgentTarget err = %v, want the navigation error", err)
	}
	if tgt != nil || note != "" {
		t.Fatalf("AgentTarget returned (target %v, note %q) alongside an error", tgt != nil, note)
	}
	if _, ok := m.LookupTarget("tab:refused"); ok {
		t.Fatal("the blank tab is still registered after its navigation failed")
	}
	if n := len(chrome.recorded("Target.closeTarget")); n != 1 {
		t.Fatalf("Target.closeTarget calls = %d, want 1", n)
	}
}

// A cancelled caller cannot stop the cleanup: the blank tab is still closed.
func TestAgentTargetNavigateFailureCleansUpAfterCallerCancel(t *testing.T) {
	m, chrome := newFakeChromeManager(t, "complete", true)
	s := New("apitoken", nil)
	s.SetCDPManager(&realManagerAdapter{Manager: m})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	chrome.mu.Lock()
	chrome.onNavigate = cancel
	chrome.mu.Unlock()
	if _, _, err := s.AgentTarget(ctx, "tab:cancelled", "https://example.com/"); err == nil {
		t.Fatal("AgentTarget succeeded on a cancelled context")
	}
	if _, ok := m.LookupTarget("tab:cancelled"); ok {
		t.Fatal("a cancelled caller left the blank tab registered")
	}
	if n := len(chrome.recorded("Target.closeTarget")); n != 1 {
		t.Fatalf("Target.closeTarget calls = %d, want 1", n)
	}
}

// A failure while waiting for the page to load discards the tab this call
// attached: otherwise it stays registered with no owner and keeps Chrome alive.
func TestAgentTargetLoadWaitFailureClosesBlankTarget(t *testing.T) {
	m, chrome := newFakeChromeManager(t, "complete", false)
	s := New("apitoken", nil)
	s.SetCDPManager(&realManagerAdapter{Manager: m})
	chrome.mu.Lock()
	chrome.failReadyState = true
	chrome.mu.Unlock()
	tgt, note, err := s.AgentTarget(context.Background(), "tab:waitfail", "https://example.com/")
	if err == nil || !strings.Contains(err.Error(), "wait for") {
		t.Fatalf("AgentTarget err = %v, want the load-wait error", err)
	}
	if tgt != nil || note != "" {
		t.Fatalf("AgentTarget returned (target %v, note %q) alongside an error", tgt != nil, note)
	}
	if _, ok := m.LookupTarget("tab:waitfail"); ok {
		t.Fatal("the tab is still registered after the load wait failed")
	}
	if n := len(chrome.recorded("Target.closeTarget")); n != 1 {
		t.Fatalf("Target.closeTarget calls = %d, want 1", n)
	}
}

// CloseAgentTab reaches the manager's CloseTarget through the headless attacher.
func TestCloseAgentTabDelegatesToManager(t *testing.T) {
	s := New("apitoken", nil)
	f := &headlessFake{}
	s.SetCDPManager(f)
	if err := s.CloseAgentTab(context.Background(), "tab:gone"); err != nil {
		t.Fatalf("CloseAgentTab: %v", err)
	}
	if len(f.closeCalls) != 1 || f.closeCalls[0] != "tab:gone" {
		t.Fatalf("CloseTarget calls = %v, want [tab:gone]", f.closeCalls)
	}
	if err := New("apitoken", nil).CloseAgentTab(context.Background(), "tab:x"); err == nil {
		t.Fatal("CloseAgentTab without a manager returned no error")
	}
}
