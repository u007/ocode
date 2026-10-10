package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type fakeWindowDriver struct {
	wins  []WindowInfo
	calls []string
}

func (f *fakeWindowDriver) ListWindows(context.Context) ([]WindowInfo, error) { return f.wins, nil }
func (f *fakeWindowDriver) FocusWindow(_ context.Context, id string) error {
	f.calls = append(f.calls, "focus "+id)
	return nil
}
func (f *fakeWindowDriver) MoveResizeWindow(_ context.Context, id string, x, y, w, h int) error {
	f.calls = append(f.calls, "bounds "+id+" "+strings.Join([]string{itoa(x), itoa(y), itoa(w), itoa(h)}, ","))
	return nil
}
func (f *fakeWindowDriver) SetWindowState(_ context.Context, id, s string) error {
	f.calls = append(f.calls, s+" "+id)
	return nil
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func runWindow(t *testing.T, d WindowDriver, args string) (string, error) {
	t.Helper()
	return (&WindowTool{Driver: d}).Execute(json.RawMessage(args))
}

func TestWindowTool_ListSortedFilteredPaginated(t *testing.T) {
	d := &fakeWindowDriver{wins: []WindowInfo{
		{ID: "3", App: "Zed", Title: "b", W: 1, H: 1},
		{ID: "1", App: "Chrome", Title: "Docs", W: 1, H: 1, Focused: true},
		{ID: "2", App: "chrome", Title: "Mail", W: 1, H: 1, Minimized: true},
	}}
	out, err := runWindow(t, d, `{"action":"list"}`)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	if lines[0] != "windows 0-3 of 3" || !strings.Contains(lines[1], "id=1") || !strings.Contains(lines[1], "[focused]") ||
		!strings.Contains(lines[2], "[minimized]") || !strings.Contains(lines[3], "app=\"Zed\"") {
		t.Fatalf("unexpected list:\n%s", out)
	}
	out, _ = runWindow(t, d, `{"action":"list","query":"CHROME","limit":1}`)
	if !strings.HasPrefix(out, "windows 0-1 of 2") || !strings.Contains(out, "more: offset=1") {
		t.Fatalf("filter/page:\n%s", out)
	}
}

func TestWindowTool_MoveResizeFillsMissingFromCurrent(t *testing.T) {
	d := &fakeWindowDriver{wins: []WindowInfo{{ID: "9", X: 10, Y: 20, W: 300, H: 400}}}
	if _, err := runWindow(t, d, `{"action":"move_resize","id":"9","width":500}`); err != nil {
		t.Fatal(err)
	}
	if len(d.calls) != 1 || d.calls[0] != "bounds 9 10,20,500,400" {
		t.Fatalf("calls: %v", d.calls)
	}
}

func TestWindowTool_StaleIDFailsLoudly(t *testing.T) {
	d := &fakeWindowDriver{}
	_, err := runWindow(t, d, `{"action":"move_resize","id":"9","x":1}`)
	if err == nil || !strings.Contains(err.Error(), "no window with id") || len(d.calls) != 0 {
		t.Fatalf("err=%v calls=%v", err, d.calls)
	}
}

func TestWindowTool_Validation(t *testing.T) {
	d := &fakeWindowDriver{}
	for _, args := range []string{
		`{"action":"focus"}`, `{"action":"close"}`, `{"action":"move_resize","id":"1"}`,
		`{"action":"move_resize","id":"1","width":0}`, `{"action":"nope"}`,
		`{"action":"list","limit":999}`, `{"action":"list","offset":-1}`,
	} {
		if _, err := runWindow(t, d, args); err == nil {
			t.Errorf("expected error for %s", args)
		}
	}
	if len(d.calls) != 0 {
		t.Fatalf("driver called: %v", d.calls)
	}
}

func TestWindowTool_StateActions(t *testing.T) {
	d := &fakeWindowDriver{}
	for _, a := range []string{"focus", "minimize", "restore", "maximize", "close"} {
		if _, err := runWindow(t, d, `{"action":"`+a+`","id":"5"}`); err != nil {
			t.Fatal(err)
		}
	}
	want := "focus 5|minimize 5|restore 5|maximize 5|close 5"
	if got := strings.Join(d.calls, "|"); got != want {
		t.Fatalf("got %s", got)
	}
}

func TestWindowTool_NoDriver(t *testing.T) {
	if _, err := (&WindowTool{}).Execute(json.RawMessage(`{"action":"list"}`)); err == nil {
		t.Fatal("expected error")
	}
}
