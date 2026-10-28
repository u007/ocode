package server

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/browse/cdp"
	"github.com/u007/ocode/internal/browsertabs"
)

// closeStrip seeds a store with testProjectKey's strip (strip order b2, b1, b3,
// with b1 and b2 navigated) and an unrelated project. It returns the store and
// the file it lives in, so a second store can write the same file.
func closeStrip(t *testing.T, active string) (*browsertabs.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "browser-tabs.json")
	store, err := browsertabs.NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	surface := func(u string) browsertabs.Surface {
		return browsertabs.Surface{URL: u, History: []string{u}}
	}
	if err := store.ApplyBulk(map[string]browsertabs.ProjectBrowsers{
		testProjectKey: {
			Tabs: []browsertabs.Tab{
				{ID: "b2", Title: "Second"},
				{ID: "b1", Title: "First"},
				{ID: "b3", Title: "Third"},
			},
			ActiveID: active,
			Surfaces: map[string]browsertabs.Surface{
				"b1": surface("https://one.example/"),
				"b2": surface("https://two.example/"),
			},
		},
		"/other/project": {
			Tabs:     []browsertabs.Tab{{ID: "x1", Title: "Elsewhere"}},
			ActiveID: "x1",
			Surfaces: map[string]browsertabs.Surface{"x1": surface("https://other.example/")},
		},
	}); err != nil {
		t.Fatalf("seed ApplyBulk: %v", err)
	}
	return store, path
}

// closeTool is the browser tool for testProjectKey over store, with fake surfaces
// whose Close fails for the stateKeys in closeErrs.
func closeTool(store browserStrip, closeErrs map[string]error) (*browserTool, *fakeSurfaces) {
	fs := &fakeSurfaces{surf: &fakeSurface{}, closeErrs: closeErrs}
	return newBrowserTool(testProjectKey, 1568, store, fs), fs
}

// stripOrder returns the tab ids of a project in strip order, and its active id.
func stripOrder(t *testing.T, store *browsertabs.Store) ([]string, string) {
	t.Helper()
	pb, ok := store.All()[testProjectKey]
	if !ok {
		t.Fatalf("project %q is not in the store", testProjectKey)
	}
	var ids []string
	for _, tab := range pb.Tabs {
		ids = append(ids, tab.ID)
	}
	return ids, pb.ActiveID
}

// TestBrowserClose_ActiveTabPicksTheNextTab pins that closing the active tab makes
// the tab after it in strip order active, and drops the closed tab's surface.
func TestBrowserClose_ActiveTabPicksTheNextTab(t *testing.T) {
	store, _ := closeStrip(t, "b1")
	b, fs := closeTool(store, nil)
	if _, err := browserCall(t, b, `{"action":"close","tab":"b1"}`); err != nil {
		t.Fatalf("close b1: %v", err)
	}
	if !slices.Equal(fs.closeKeys, []string{"tab:b1"}) {
		t.Fatalf("Chrome close calls = %v, want [tab:b1]", fs.closeKeys)
	}
	ids, active := stripOrder(t, store)
	if !slices.Equal(ids, []string{"b2", "b3"}) || active != "b3" {
		t.Fatalf("after closing active b1: strip %v active %q, want [b2 b3] active b3", ids, active)
	}
	if _, ok := store.All()[testProjectKey].Surfaces["b1"]; ok {
		t.Fatal("the closed tab's surface is still stored")
	}
}

// TestBrowserClose_ActiveLastTabPicksThePreviousTab pins the fallback: when the
// active tab is last in strip order and has no next tab, the one before it wins.
func TestBrowserClose_ActiveLastTabPicksThePreviousTab(t *testing.T) {
	store, _ := closeStrip(t, "b3")
	b, _ := closeTool(store, nil)
	if _, err := browserCall(t, b, `{"action":"close","tab":"b3"}`); err != nil {
		t.Fatalf("close b3: %v", err)
	}
	ids, active := stripOrder(t, store)
	if !slices.Equal(ids, []string{"b2", "b1"}) || active != "b1" {
		t.Fatalf("after closing last active b3: strip %v active %q, want [b2 b1] active b1", ids, active)
	}
}

// TestBrowserClose_NonActiveTabKeepsTheActiveTab pins that closing a tab that is
// not active leaves the active tab alone.
func TestBrowserClose_NonActiveTabKeepsTheActiveTab(t *testing.T) {
	store, _ := closeStrip(t, "b1")
	b, _ := closeTool(store, nil)
	if _, err := browserCall(t, b, `{"action":"close","tab":"b2"}`); err != nil {
		t.Fatalf("close b2: %v", err)
	}
	ids, active := stripOrder(t, store)
	if !slices.Equal(ids, []string{"b1", "b3"}) || active != "b1" {
		t.Fatalf("after closing non-active b2: strip %v active %q, want [b1 b3] active b1", ids, active)
	}
}

// TestBrowserClose_LastTabRemovesTheProject pins that closing every tab of a
// project removes the project, and leaves the other project in the store.
func TestBrowserClose_LastTabRemovesTheProject(t *testing.T) {
	store, _ := closeStrip(t, "b1")
	b, _ := closeTool(store, nil)
	for _, id := range []string{"b2", "b3", "b1"} {
		if _, err := browserCall(t, b, `{"action":"close","tab":"`+id+`"}`); err != nil {
			t.Fatalf("close %s: %v", id, err)
		}
	}
	if _, ok := store.All()[testProjectKey]; ok {
		t.Fatal("the project with no tabs is still stored")
	}
	if _, ok := store.All()["/other/project"]; !ok {
		t.Fatal("closing this project's tabs removed another project")
	}
}

// TestBrowserClose_AbsentChromeTargetStillRemovesTheStripEntry pins that a tab
// whose Chrome target was never attached still leaves the strip, with no error.
func TestBrowserClose_AbsentChromeTargetStillRemovesTheStripEntry(t *testing.T) {
	store, _ := closeStrip(t, "b1")
	absent := fmt.Errorf("browse: close tab:b2: %w", cdp.ErrNoTarget)
	b, _ := closeTool(store, map[string]error{"tab:b2": absent})
	if _, err := browserCall(t, b, `{"action":"close","tab":"b2"}`); err != nil {
		t.Fatalf("close of a never-attached tab: %v", err)
	}
	ids, _ := stripOrder(t, store)
	if !slices.Equal(ids, []string{"b1", "b3"}) {
		t.Fatalf("strip after closing an absent target = %v, want [b1 b3]", ids)
	}
}

// TestBrowserClose_ChromeErrorKeepsTheStripEntry pins that a real Chrome close
// failure is returned and leaves the strip and its revision untouched.
func TestBrowserClose_ChromeErrorKeepsTheStripEntry(t *testing.T) {
	store, _ := closeStrip(t, "b1")
	before := store.All()[testProjectKey].Revision
	b, _ := closeTool(store, map[string]error{"tab:b2": errors.New("chrome went away")})
	_, err := browserCall(t, b, `{"action":"close","tab":"b2"}`)
	if err == nil || !strings.Contains(err.Error(), "chrome went away") {
		t.Fatalf("close error = %v, want the Chrome failure", err)
	}
	pb := store.All()[testProjectKey]
	if len(pb.Tabs) != 3 || pb.Revision != before {
		t.Fatalf("a failed Chrome close changed the strip: %d tabs, revision %d (was %d)", len(pb.Tabs), pb.Revision, before)
	}
}

// TestBrowserClose_AgentOpenedTabWritesNoStrip pins that closing a tab the agent
// opened writes nothing to the strip, so teardown does not bump the revision.
func TestBrowserClose_AgentOpenedTabWritesNoStrip(t *testing.T) {
	store, _ := closeStrip(t, "b1")
	before := store.All()[testProjectKey].Revision
	fs := &fakeSurfaces{surf: &fakeSurface{}, openKey: "tab:agent1"}
	b := newBrowserTool(testProjectKey, 1568, store, fs)
	if _, err := browserCall(t, b, `{"action":"open"}`); err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := browserCall(t, b, `{"action":"close","tab":"agent1"}`); err != nil {
		t.Fatalf("close agent1: %v", err)
	}
	if got := store.All()[testProjectKey].Revision; got != before {
		t.Fatalf("closing an agent-opened tab moved the strip revision %d -> %d", before, got)
	}
}

// TestBrowserClose_OtherProjectIsNotRewritten pins the merge: a close writes only
// the project it closes a tab in. The other project keeps its revision and content.
func TestBrowserClose_OtherProjectIsNotRewritten(t *testing.T) {
	store, _ := closeStrip(t, "b1")
	other := store.All()["/other/project"]
	b, _ := closeTool(store, nil)
	if _, err := browserCall(t, b, `{"action":"close","tab":"b1"}`); err != nil {
		t.Fatalf("close b1: %v", err)
	}
	if got := store.All()["/other/project"]; !reflect.DeepEqual(got, other) {
		t.Fatalf("another project changed: before %+v, after %+v", other, got)
	}
}

// racingStrip is a strip whose first conflicts writes lose a race. Before each
// such write another writer of the same file moves the project on (it retitles
// the last tab, which the removal keeps), so the write carries a revision the
// store has already passed.
type racingStrip struct {
	browserStrip
	other     *browsertabs.Store
	conflicts int
	writes    int
}

func (r *racingStrip) ApplyBulk(patch map[string]browsertabs.ProjectBrowsers) error {
	r.writes++
	if r.conflicts > 0 {
		r.conflicts--
		for key := range patch {
			cur := r.other.All()[key]
			tabs := slices.Clone(cur.Tabs)
			tabs[len(tabs)-1].Title = fmt.Sprintf("raced %d", r.writes)
			cur.Tabs = tabs
			if err := r.other.ApplyBulk(map[string]browsertabs.ProjectBrowsers{key: cur}); err != nil {
				return fmt.Errorf("test writer lost its own race: %w", err)
			}
		}
	}
	return r.browserStrip.ApplyBulk(patch)
}

// TestBrowserClose_RevisionConflictIsRetriedAndMerged pins that a conflict is
// retried from a fresh read: the removal lands, and the other writer's change to
// the same project survives it.
func TestBrowserClose_RevisionConflictIsRetriedAndMerged(t *testing.T) {
	store, path := closeStrip(t, "b1")
	other, err := browsertabs.NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	strip := &racingStrip{browserStrip: store, other: other, conflicts: 1}
	b, _ := closeTool(strip, nil)
	if _, err := browserCall(t, b, `{"action":"close","tab":"b2"}`); err != nil {
		t.Fatalf("close b2 after one conflict: %v", err)
	}
	if strip.writes != 2 {
		t.Fatalf("strip writes = %d, want 2 (one conflict, one retry)", strip.writes)
	}
	pb := store.All()[testProjectKey]
	if len(pb.Tabs) != 2 || pb.Tabs[0].ID != "b1" || pb.Tabs[0].Title != "First" {
		t.Fatalf("strip after the retry = %+v", pb.Tabs)
	}
	if pb.Tabs[1].ID != "b3" || pb.Tabs[1].Title != "raced 1" {
		t.Fatalf("the retry lost the other writer's change to b3: %+v", pb.Tabs)
	}
}

// TestBrowserClose_RevisionConflictRetriesAreBounded pins the bound: one attempt
// and three retries, then an error, with the strip left as the other writer left it.
func TestBrowserClose_RevisionConflictRetriesAreBounded(t *testing.T) {
	store, path := closeStrip(t, "b1")
	other, err := browsertabs.NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	strip := &racingStrip{browserStrip: store, other: other, conflicts: 100}
	b, _ := closeTool(strip, nil)
	_, err = browserCall(t, b, `{"action":"close","tab":"b2"}`)
	var conflict *browsertabs.RevisionConflictError
	if err == nil || !errors.As(err, &conflict) {
		t.Fatalf("close after endless conflicts = %v, want a revision conflict error", err)
	}
	if strip.writes != browserStripAttempts {
		t.Fatalf("strip writes = %d, want %d (one attempt and three retries)", strip.writes, browserStripAttempts)
	}
	pb := store.All()[testProjectKey]
	if len(pb.Tabs) != 3 || pb.Tabs[2].Title != fmt.Sprintf("raced %d", strip.writes) {
		t.Fatalf("a failed removal changed the strip: %+v", pb.Tabs)
	}
}

// TestBrowserClose_StripChangePublishesTheSharedEvent pins that a strip write made
// by the tool publishes browser_tabs_changed, the event a client PUT publishes, so
// the Browser panel refetches and drops the closed tab.
func TestBrowserClose_StripChangePublishesTheSharedEvent(t *testing.T) {
	store, _ := closeStrip(t, "b1")
	bus := NewEventBus()
	ch := bus.Subscribe(nil)
	defer bus.Unsubscribe(ch)
	b := newBrowserTool(testProjectKey, 1568, stripPublisher{store: store, bus: bus}, &fakeSurfaces{surf: &fakeSurface{}})
	if _, err := browserCall(t, b, `{"action":"close","tab":"b2"}`); err != nil {
		t.Fatalf("close b2: %v", err)
	}
	select {
	case env := <-ch:
		if env.Event != "browser_tabs_changed" {
			t.Fatalf("event = %q, want browser_tabs_changed", env.Event)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("closing a strip tab published no browser_tabs_changed event")
	}
}
