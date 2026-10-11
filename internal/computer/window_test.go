package computer

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

// scriptedRunner answers by command-line prefix so multi-command flows
// (wmctrl + xdotool + xprop) can be exercised with distinct outputs.
type scriptedRunner struct {
	Calls   []string
	Replies map[string]string // prefix -> stdout
	Err     error
	Errs    map[string]error // prefix -> error for just that command
}

func (s *scriptedRunner) run(ctx context.Context, name string, args ...string) (string, error) {
	line := strings.Join(append([]string{name}, args...), " ")
	s.Calls = append(s.Calls, line)
	if s.Err != nil {
		return "", s.Err
	}
	for p, err := range s.Errs {
		if strings.HasPrefix(line, p) {
			return "", err
		}
	}
	for p, out := range s.Replies {
		if strings.HasPrefix(line, p) {
			return out, nil
		}
	}
	return "", nil
}

func (s *scriptedRunner) runStdin(ctx context.Context, stdin string, name string, args ...string) (string, error) {
	return s.run(ctx, name, args...)
}

func TestParseWindowList(t *testing.T) {
	out := "123:1\tSafari\tApple\t10\t20\t800\t600\t0\t1\n456:2\tCode\tmain.go\t-5\t0\t100\t50\t1\t0\n"
	wins, err := parseWindowList(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(wins) != 2 {
		t.Fatalf("got %d windows", len(wins))
	}
	w := wins[0]
	if w.ID != "123:1" || w.App != "Safari" || w.Title != "Apple" || w.X != 10 || w.Y != 20 || w.W != 800 || w.H != 600 || w.Minimized || !w.Focused {
		t.Errorf("window 0 wrong: %+v", w)
	}
	if !wins[1].Minimized || wins[1].Focused || wins[1].X != -5 {
		t.Errorf("window 1 wrong: %+v", wins[1])
	}
}

func TestParseWindowList_Malformed(t *testing.T) {
	for _, in := range []string{"a\tb\tc", "1\ta\tt\tx\t0\t0\t0\t0\t0"} {
		if _, err := parseWindowList(in); err == nil {
			t.Errorf("expected error for %q", in)
		}
	}
}

func TestCheckWindowID(t *testing.T) {
	cases := []struct {
		name string
		id   string
		ok   bool
	}{
		{"darwin", "123:4", true}, {"darwin", "123", false}, {"darwin", "1:2; rm", false},
		{"decimal", "99", true}, {"decimal", "0x99", false},
		{"x11", "0x04600003", true}, {"x11", "123", false}, {"x11", "0x12 -c", false},
	}
	for _, c := range cases {
		re := map[string]interface{ MatchString(string) bool }{
			"darwin": darwinWindowID, "decimal": decimalWindowID, "x11": x11WindowID,
		}[c.name]
		if got := re.MatchString(c.id); got != c.ok {
			t.Errorf("%s %q: got %v want %v", c.name, c.id, got, c.ok)
		}
	}
	if err := checkWindowID(x11WindowID, "bad"); err == nil {
		t.Error("expected error")
	}
}

func TestParseWmctrlList(t *testing.T) {
	out := "0x04600003  0 4242  100  200  800  600  navigator.Firefox  host My  Page  Title\n" +
		"0x05000001 -1 77 0 0 1920 30 panel.Panel host Top Bar\n"
	wins, err := parseWmctrlList(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(wins) != 2 {
		t.Fatalf("got %d", len(wins))
	}
	w := wins[0]
	if w.ID != "0x04600003" || w.App != "Firefox" || w.Title != "My  Page  Title" || w.X != 100 || w.W != 800 {
		t.Errorf("wrong: %+v", w)
	}
	if wins[1].Title != "Top Bar" {
		t.Errorf("title: %q", wins[1].Title)
	}
}

func TestLinuxListWindows(t *testing.T) {
	r := &scriptedRunner{Replies: map[string]string{
		"wmctrl -lGpx":            "0x04600003 0 1 0 0 10 10 a.App h One\n0x04600004 0 1 0 0 10 10 b.Other h Two\n",
		"xdotool getactivewindow": "73400324\n", // 0x04600004
		"xprop -id 0x04600003":    "_NET_WM_STATE(ATOM) = _NET_WM_STATE_HIDDEN\n",
		"xprop -id 0x04600004":    "_NET_WM_STATE(ATOM) = \n",
	}}
	d := &linuxDriver{r: r, backend: "x11"}
	wins, err := d.ListWindows(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !wins[0].Minimized || wins[0].Focused {
		t.Errorf("win0: %+v", wins[0])
	}
	if wins[1].Minimized || !wins[1].Focused {
		t.Errorf("win1: %+v", wins[1])
	}
}

func TestLinuxListWindows_ToleratesPartialFailure(t *testing.T) {
	r := &scriptedRunner{
		Replies: map[string]string{
			"wmctrl -lGpx":         "0x04600003 0 1 0 0 10 10 a.App h One\n0x04600004 0 1 0 0 10 10 b.Other h Two\n",
			"xprop -id 0x04600004": "_NET_WM_STATE(ATOM) = \n",
		},
		Errs: map[string]error{
			"xdotool getactivewindow": errors.New("exit status 1"), // empty desktop
			"xprop -id 0x04600003":    errors.New("BadWindow"),     // closed mid-listing
		},
	}
	d := &linuxDriver{r: r, backend: "x11"}
	wins, err := d.ListWindows(context.Background())
	if err != nil {
		t.Fatalf("partial helper failure must not fail the listing: %v", err)
	}
	if len(wins) != 1 || wins[0].ID != "0x04600004" || wins[0].Focused {
		t.Fatalf("got %+v, want only the surviving unfocused window", wins)
	}
}

func TestParseWindowList_SkipsUnreadableWindow(t *testing.T) {
	out := "#skipped\tSafari window 2\tAXMain unreadable\n123:1\tSafari\tApple\t10\t20\t800\t600\t0\t1\n"
	wins, err := parseWindowList(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(wins) != 1 || wins[0].ID != "123:1" {
		t.Fatalf("got %+v", wins)
	}
}

func TestLinuxWindowActions(t *testing.T) {
	ctx := context.Background()
	r := &scriptedRunner{}
	d := &linuxDriver{r: r, backend: "x11"}
	id := "0x04600003"
	if err := d.FocusWindow(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := d.MoveResizeWindow(ctx, id, 1, 2, 300, 400); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{tool.WindowMinimize, tool.WindowRestore, tool.WindowMaximize, tool.WindowClose} {
		if err := d.SetWindowState(ctx, id, s); err != nil {
			t.Fatal(err)
		}
	}
	wantCalls(t, r.Calls, []string{
		"wmctrl -i -a 0x04600003",
		"wmctrl -i -r 0x04600003 -b remove,maximized_vert,maximized_horz,fullscreen",
		"wmctrl -i -r 0x04600003 -e 0,1,2,300,400",
		"xdotool windowminimize 0x04600003",
		"wmctrl -i -r 0x04600003 -b remove,maximized_vert,maximized_horz",
		"wmctrl -i -a 0x04600003",
		"wmctrl -i -r 0x04600003 -b add,maximized_vert,maximized_horz",
		"wmctrl -i -c 0x04600003",
	})
}

func TestLinuxWindows_RejectsBadInput(t *testing.T) {
	r := &scriptedRunner{}
	d := &linuxDriver{r: r, backend: "x11"}
	ctx := context.Background()
	if err := d.FocusWindow(ctx, "0x1; rm -rf /"); err == nil {
		t.Error("bad id accepted")
	}
	if err := d.SetWindowState(ctx, "0x1", "explode"); err == nil {
		t.Error("bad state accepted")
	}
	if len(r.Calls) != 0 {
		t.Errorf("commands ran: %v", r.Calls)
	}
}

func TestLinuxWindows_WaylandUnsupported(t *testing.T) {
	r := &scriptedRunner{}
	d := &linuxDriver{r: r, backend: "wayland"}
	if _, err := d.ListWindows(context.Background()); err == nil || !strings.Contains(err.Error(), "X11-only") {
		t.Fatalf("got %v", err)
	}
	if len(r.Calls) != 0 {
		t.Errorf("commands ran: %v", r.Calls)
	}
}

func TestLinuxWindows_MissingBinaryHint(t *testing.T) {
	r := &scriptedRunner{Err: exec.ErrNotFound}
	d := &linuxDriver{r: r, backend: "x11"}
	err := d.FocusWindow(context.Background(), "0x1")
	var ne *tool.NoticedError
	if !errors.As(err, &ne) || !strings.Contains(ne.Notice, "wmctrl") {
		t.Fatalf("got %v", err)
	}
}

func TestWindowsScriptHasWindowOps(t *testing.T) {
	for _, op := range []string{`"windows"`, `"window-focus"`, `"window-bounds"`, `"window-state"`} {
		if !strings.Contains(string(windowsScript), op) {
			t.Errorf("script missing op %s", op)
		}
	}
}
