package browse

import (
	"context"
	"testing"

	"github.com/u007/ocode/internal/browse/cdp"
)

// TestEmitTitleAdapterPassesThrough drives the cdp.Manager → browse title
// publisher seam (mirrors TestEmitNavAdapterStampsChromeMode): the manager's
// EmitTitle closure must reach the browse title publisher with state key,
// title, and URL intact. Unlike nav there is no mode stamp — titles are
// mode-agnostic display data.
func TestEmitTitleAdapterPassesThrough(t *testing.T) {
	s := New("apitoken", nil)
	var got []TitleEvent
	s.SetTitlePublisher(func(_ string, ev TitleEvent) { got = append(got, ev) })

	s.initManager(Options{})
	mgr, ok := s.cdp.(*realManagerAdapter)
	if !ok || mgr == nil || mgr.Manager == nil {
		t.Fatalf("cdp manager not initialized as *realManagerAdapter")
	}

	want := cdp.TitleEvent{StateKey: "tab:title", Title: "Example Domain", URL: "https://example.com/"}
	mgr.Manager.EmitTitleForTest(want)
	if len(got) != 1 {
		t.Fatalf("adapter emitted %d events, want 1", len(got))
	}
	if got[0] != (TitleEvent{StateKey: "tab:title", Title: "Example Domain", URL: "https://example.com/"}) {
		t.Fatalf("adapter output = %+v", got[0])
	}
}

// TestEmitTitleTracksProcMeta pins the side effect emitTitle has beyond the
// publisher: last title/URL per stateKey, which labels BrowserProcesses rows
// and matches Chrome renderers to tabs.
func TestEmitTitleTracksProcMeta(t *testing.T) {
	s := New("apitoken", nil)
	s.emitTitle(TitleEvent{StateKey: "tab:m", Title: "Meta", URL: "https://meta.example/"})
	s.procMu.Lock()
	defer s.procMu.Unlock()
	if s.procTitle["tab:m"] != "Meta" || s.procURL["tab:m"] != "https://meta.example/" {
		t.Fatalf("proc meta = %+v / %+v", s.procTitle, s.procURL)
	}
}

// procFakeManager implements chromeManager plus the optional
// cdpProcessProvider capability for BrowserProcesses tests.
type procFakeManager struct {
	keys      []string
	perf      map[string]map[string]float64
	renderers []cdp.RendererProcess
}

func (f *procFakeManager) Attach(ctx context.Context, _ string, _ cdp.FrameSink) (chromeTarget, error) {
	return nil, nil
}
func (f *procFakeManager) Revoke(_ string)                                        {}
func (f *procFakeManager) SetFiles(_ context.Context, _ string, _ []string) error { return nil }
func (f *procFakeManager) SetScreencastQuality(_ int)                             {}
func (f *procFakeManager) Close(_ context.Context) error                          { return nil }
func (f *procFakeManager) TargetKeys() []string                                   { return f.keys }
func (f *procFakeManager) TargetPerf() map[string]map[string]float64              { return f.perf }
func (f *procFakeManager) RendererProcesses(_ context.Context) ([]cdp.RendererProcess, error) {
	return f.renderers, nil
}

// TestBrowserProcesses_MapsUniqueRenderer pins the happy path: one target,
// one renderer with a matching page title → pid attributed, JS heap always
// reported, first sample CPU is 0 (no prior cpuTime baseline).
func TestBrowserProcesses_MapsUniqueRenderer(t *testing.T) {
	s := New("apitoken", nil)
	s.SetCDPManager(&procFakeManager{
		keys:      []string{"tab:a"},
		perf:      map[string]map[string]float64{"tab:a": {"JSHeapUsedSize": 123456}},
		renderers: []cdp.RendererProcess{{PID: 4242, CPUTime: 10, Title: "Example Domain"}},
	})
	s.emitTitle(TitleEvent{StateKey: "tab:a", Title: "Example Domain", URL: "https://example.com/"})
	rows := s.BrowserProcesses(context.Background())
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want 1", rows)
	}
	r := rows[0]
	if r.TabID != "a" || r.PID != 4242 || r.Shared {
		t.Fatalf("row = %+v, want tab a / pid 4242 / unshared", r)
	}
	if r.JSHeapBytes != 123456 || r.MemBytes == 0 {
		t.Fatalf("heap/mem = %d/%d, want heap 123456 and nonzero mem", r.JSHeapBytes, r.MemBytes)
	}
	if r.CPUPercent != 0 {
		t.Fatalf("first-sample cpu = %v, want 0", r.CPUPercent)
	}
}

// TestBrowserProcesses_SharedRendererFlagsCoalescedTabs pins the documented
// limitation: two tabs on the same renderer title share one pid and both
// rows are flagged shared instead of splitting CPU/RSS.
func TestBrowserProcesses_SharedRendererFlagsCoalescedTabs(t *testing.T) {
	s := New("apitoken", nil)
	s.SetCDPManager(&procFakeManager{
		keys:      []string{"tab:a", "tab:b"},
		perf:      map[string]map[string]float64{},
		renderers: []cdp.RendererProcess{{PID: 777, CPUTime: 5, Title: "Same Site"}},
	})
	s.emitTitle(TitleEvent{StateKey: "tab:a", Title: "Same Site"})
	s.emitTitle(TitleEvent{StateKey: "tab:b", Title: "Same Site"})
	rows := s.BrowserProcesses(context.Background())
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want 2", rows)
	}
	for _, r := range rows {
		if r.PID != 777 || !r.Shared {
			t.Fatalf("row = %+v, want pid 777 + shared", r)
		}
	}
}

// TestBrowserProcesses_NoProviderReturnsNil pins the fallback contract:
// managers without the process capability (nil cdp, test fakes) yield no
// rows so the panel keeps its estimate rows instead of erroring.
func TestBrowserProcesses_NoProviderReturnsNil(t *testing.T) {
	s := New("apitoken", nil)
	if rows := s.BrowserProcesses(context.Background()); rows != nil {
		t.Fatalf("rows = %+v, want nil without a manager", rows)
	}
}
