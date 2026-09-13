package computer

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

// newTestLinuxDriver builds a linuxDriver for the given backend over a
// recording stub runner, so tests exercise the production argv path.
func newTestLinuxDriver(backend string) (*linuxDriver, *stubRunner) {
	s := &stubRunner{}
	return &linuxDriver{r: s, backend: backend}, s
}

// wantCalls compares the recorded stub calls against the expected
// command lines in order.
func wantCalls(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("call count: got %d want %d\ngot:  %q\nwant: %q", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call %d:\n got  %q\n want %q", i, got[i], want[i])
		}
	}
}

// TestLinuxBackend_Detection verifies backend detection from
// XDG_SESSION_TYPE: wayland → "wayland", else → "x11".
func TestLinuxBackend_Detection(t *testing.T) {
	tests := []struct {
		envValue string
		want     string
	}{
		{"wayland", "wayland"},
		{"x11", "x11"},
		{"", "x11"},
		{"something_else", "x11"},
	}
	for _, tc := range tests {
		got := linuxBackend(func(key string) string {
			if key == "XDG_SESSION_TYPE" {
				return tc.envValue
			}
			return ""
		})
		if got != tc.want {
			t.Errorf("XDG_SESSION_TYPE=%q: got %q want %q", tc.envValue, got, tc.want)
		}
	}
}

// TestLinuxX11_Click verifies the X11 click argv moves the pointer to
// the target and repeats with the right button number.
func TestLinuxX11_Click(t *testing.T) {
	for _, tc := range []struct {
		name   string
		button tool.MouseButton
		count  int
		want   string
	}{
		{"left", tool.MouseLeft, 1, "xdotool mousemove 100 200 click --repeat 1 1"},
		{"right", tool.MouseRight, 1, "xdotool mousemove 100 200 click --repeat 1 3"},
		{"middle", tool.MouseMiddle, 1, "xdotool mousemove 100 200 click --repeat 1 2"},
		{"double", tool.MouseLeft, 2, "xdotool mousemove 100 200 click --repeat 2 1"},
	} {
		d, s := newTestLinuxDriver("x11")
		if err := d.Click(context.Background(), 100, 200, tc.button, tc.count); err != nil {
			t.Fatalf("%s: Click error: %v", tc.name, err)
		}
		wantCalls(t, s.Calls, []string{tc.want})
	}
}

// TestLinuxWayland_Click verifies the Wayland click moves the pointer
// first (ydotool click takes no coordinates) then clicks count times.
func TestLinuxWayland_Click(t *testing.T) {
	for _, tc := range []struct {
		name   string
		button tool.MouseButton
		code   string
	}{
		{"left", tool.MouseLeft, "0xC0"},
		{"right", tool.MouseRight, "0xC1"},
		{"middle", tool.MouseMiddle, "0xC2"},
	} {
		d, s := newTestLinuxDriver("wayland")
		if err := d.Click(context.Background(), 100, 200, tc.button, 1); err != nil {
			t.Fatalf("%s: Click error: %v", tc.name, err)
		}
		wantCalls(t, s.Calls, []string{
			"ydotool mousemove --absolute -- 100 200",
			"ydotool click " + tc.code,
		})
	}

	d, s := newTestLinuxDriver("wayland")
	if err := d.Click(context.Background(), 5, 6, tool.MouseLeft, 2); err != nil {
		t.Fatalf("double Click error: %v", err)
	}
	wantCalls(t, s.Calls, []string{
		"ydotool mousemove --absolute -- 5 6",
		"ydotool click 0xC0",
		"ydotool click 0xC0",
	})
}

// TestLinuxMove verifies the move argv for both backends.
func TestLinuxMove(t *testing.T) {
	d, s := newTestLinuxDriver("x11")
	if err := d.Move(context.Background(), 12, 34); err != nil {
		t.Fatalf("x11 Move error: %v", err)
	}
	wantCalls(t, s.Calls, []string{"xdotool mousemove 12 34"})

	d, s = newTestLinuxDriver("wayland")
	if err := d.Move(context.Background(), 12, 34); err != nil {
		t.Fatalf("wayland Move error: %v", err)
	}
	wantCalls(t, s.Calls, []string{"ydotool mousemove --absolute -- 12 34"})
}

// TestLinuxDrag verifies the X11 chain and that Wayland issues one
// ydotool subcommand per invocation (ydotool accepts only one).
func TestLinuxDrag(t *testing.T) {
	d, s := newTestLinuxDriver("x11")
	if err := d.Drag(context.Background(), 1, 2, 3, 4); err != nil {
		t.Fatalf("x11 Drag error: %v", err)
	}
	wantCalls(t, s.Calls, []string{
		"xdotool mousemove 1 2 mousedown 1 mousemove 3 4 mouseup 1",
	})

	d, s = newTestLinuxDriver("wayland")
	if err := d.Drag(context.Background(), 1, 2, 3, 4); err != nil {
		t.Fatalf("wayland Drag error: %v", err)
	}
	wantCalls(t, s.Calls, []string{
		"ydotool mousemove --absolute -- 1 2",
		"ydotool click 0x40",
		"ydotool mousemove --absolute -- 3 4",
		"ydotool click 0x80",
	})
}

// TestLinuxX11_Scroll verifies the pointer moves to the target before
// the wheel clicks and that directions map to buttons 4/5/6/7.
func TestLinuxX11_Scroll(t *testing.T) {
	for _, tc := range []struct {
		dir    string
		button string
	}{
		{"up", "4"},
		{"down", "5"},
		{"left", "6"},
		{"right", "7"},
	} {
		d, s := newTestLinuxDriver("x11")
		if err := d.Scroll(context.Background(), 10, 20, tc.dir, 3); err != nil {
			t.Fatalf("%s: Scroll error: %v", tc.dir, err)
		}
		wantCalls(t, s.Calls, []string{
			"xdotool mousemove 10 20 click --repeat 3 --delay 20 " + tc.button,
		})
	}
}

// TestLinuxWayland_Scroll verifies the pointer moves first and that the
// wheel deltas follow the kernel convention (positive REL_WHEEL is up,
// positive REL_HWHEEL is right). Coordinates are positional after "--"
// because the -x/-y flags only exist from ydotool 1.0.4, and both axes
// are always supplied because mousemove requires exactly two.
func TestLinuxWayland_Scroll(t *testing.T) {
	for _, tc := range []struct {
		dir   string
		wheel string
	}{
		{"up", "0 3"},
		{"down", "0 -3"},
		{"right", "3 0"},
		{"left", "-3 0"},
	} {
		d, s := newTestLinuxDriver("wayland")
		if err := d.Scroll(context.Background(), 10, 20, tc.dir, 3); err != nil {
			t.Fatalf("%s: Scroll error: %v", tc.dir, err)
		}
		wantCalls(t, s.Calls, []string{
			"ydotool mousemove --absolute -- 10 20",
			"ydotool mousemove --wheel -- " + tc.wheel,
		})
	}
}

// TestLinuxScroll_UnknownDirection verifies an unrecognised direction
// is an error on both backends rather than a silent no-op.
func TestLinuxScroll_UnknownDirection(t *testing.T) {
	for _, backend := range []string{"x11", "wayland"} {
		d, s := newTestLinuxDriver(backend)
		err := d.Scroll(context.Background(), 1, 1, "sideways", 1)
		if err == nil {
			t.Fatalf("%s: expected error for unknown direction", backend)
		}
		if len(s.Calls) != 0 {
			t.Errorf("%s: expected no commands run, got %q", backend, s.Calls)
		}
	}
}

// TestLinuxType verifies text travels on stdin via --file - so it never
// lands in an argv.
func TestLinuxType(t *testing.T) {
	d, s := newTestLinuxDriver("x11")
	if err := d.Type(context.Background(), "hello"); err != nil {
		t.Fatalf("x11 Type error: %v", err)
	}
	wantCalls(t, s.Calls, []string{"xdotool type --delay 12 --file - < stdin"})

	d, s = newTestLinuxDriver("wayland")
	if err := d.Type(context.Background(), "hello"); err != nil {
		t.Fatalf("wayland Type error: %v", err)
	}
	wantCalls(t, s.Calls, []string{"ydotool type --file - < stdin"})
	for _, c := range s.Calls {
		if strings.Contains(c, "hello") {
			t.Errorf("typed text leaked into argv: %q", c)
		}
	}
}

// TestLinuxKey verifies xdotool passes the combo through unchanged and
// ydotool receives press/release code pairs.
func TestLinuxKey(t *testing.T) {
	d, s := newTestLinuxDriver("x11")
	if err := d.Key(context.Background(), "ctrl+s"); err != nil {
		t.Fatalf("x11 Key error: %v", err)
	}
	wantCalls(t, s.Calls, []string{"xdotool key ctrl+s"})

	d, s = newTestLinuxDriver("wayland")
	if err := d.Key(context.Background(), "ctrl+c"); err != nil {
		t.Fatalf("wayland Key error: %v", err)
	}
	wantCalls(t, s.Calls, []string{"ydotool key 29:1 46:1 46:0 29:0"})
}

// TestLinuxKey_Unknown verifies an unknown key name is rejected before
// any command runs.
func TestLinuxKey_Unknown(t *testing.T) {
	d, s := newTestLinuxDriver("wayland")
	if err := d.Key(context.Background(), "ctrl+Nonesuch"); err == nil {
		t.Fatal("expected error for unknown key name")
	}
	if len(s.Calls) != 0 {
		t.Errorf("expected no commands run, got %q", s.Calls)
	}
}

// TestLinuxCursor verifies X11 parses xdotool getmouselocation --shell
// and that Wayland reports the unsupported action.
func TestLinuxCursor(t *testing.T) {
	d, s := newTestLinuxDriver("x11")
	s.Stdout = "X=412\nY=300\nSCREEN=0\nWINDOW=12345\n"
	x, y, err := d.Cursor(context.Background())
	if err != nil {
		t.Fatalf("x11 Cursor error: %v", err)
	}
	if x != 412 || y != 300 {
		t.Errorf("got %d,%d want 412,300", x, y)
	}
	wantCalls(t, s.Calls, []string{"xdotool getmouselocation --shell"})

	d, s = newTestLinuxDriver("x11")
	s.Stdout = "SCREEN=0\nWINDOW=12345\n"
	if _, _, err := d.Cursor(context.Background()); err == nil {
		t.Fatal("expected error when X=/Y= are absent")
	}

	d, s = newTestLinuxDriver("wayland")
	_, _, err = d.Cursor(context.Background())
	if err == nil {
		t.Fatal("expected error on wayland")
	}
	if !strings.Contains(err.Error(), "cursor_position not supported on wayland") {
		t.Errorf("unexpected error text: %v", err)
	}
	if len(s.Calls) != 0 {
		t.Errorf("expected no commands run, got %q", s.Calls)
	}
}

// TestLinuxScreenshot verifies the capture commands and screen-size
// source for both backends.
func TestLinuxScreenshot(t *testing.T) {
	d, s := newTestLinuxDriver("x11")
	s.Stdout = "1920 1080\n"
	_, w, h, err := d.Screenshot(context.Background())
	if err != nil {
		t.Fatalf("x11 Screenshot error: %v", err)
	}
	if w != 1920 || h != 1080 {
		t.Errorf("got %dx%d want 1920x1080", w, h)
	}
	if len(s.Calls) != 2 {
		t.Fatalf("expected 2 calls got %q", s.Calls)
	}
	if !strings.HasPrefix(s.Calls[0], "scrot -o ") {
		t.Errorf("call 0 = %q, want scrot -o <tmp>", s.Calls[0])
	}
	if s.Calls[1] != "xdotool getdisplaygeometry" {
		t.Errorf("call 1 = %q", s.Calls[1])
	}

	d, s = newTestLinuxDriver("wayland")
	if _, _, _, err := d.Screenshot(context.Background()); err == nil {
		t.Fatal("expected error decoding an empty capture file")
	}
	if len(s.Calls) != 1 || !strings.HasPrefix(s.Calls[0], "grim ") {
		t.Errorf("expected a single grim call, got %q", s.Calls)
	}
}

// TestLinuxScreenshot_RemovesTempOnError verifies the capture temp file
// does not leak when the capture command fails.
func TestLinuxScreenshot_RemovesTempOnError(t *testing.T) {
	d, s := newTestLinuxDriver("x11")
	s.Err = errors.New("scrot boom")
	if _, _, _, err := d.Screenshot(context.Background()); err == nil {
		t.Fatal("expected Screenshot error")
	}
	if len(s.Calls) != 1 {
		t.Fatalf("expected 1 call got %q", s.Calls)
	}
	path := strings.TrimPrefix(s.Calls[0], "scrot -o ")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("temp file %q still exists after error (stat err %v)", path, err)
	}
}

// TestLinuxMissingBinaryIsNoticed verifies that when a binary
// is missing (exec.ErrNotFound), the error is a NoticedError
// containing the apt install hint for both backends.
func TestLinuxMissingBinaryIsNoticed(t *testing.T) {
	// X11 backend hint should mention xdotool and scrot
	hint := linuxInstallHint("x11")
	if !strings.Contains(hint, "xdotool") {
		t.Errorf("x11 hint missing xdotool: %q", hint)
	}
	if !strings.Contains(hint, "scrot") {
		t.Errorf("x11 hint missing scrot: %q", hint)
	}

	// Wayland backend hint should mention ydotool and grim
	hint = linuxInstallHint("wayland")
	if !strings.Contains(hint, "ydotool") {
		t.Errorf("wayland hint missing ydotool: %q", hint)
	}
	if !strings.Contains(hint, "grim") {
		t.Errorf("wayland hint missing grim: %q", hint)
	}
	if !strings.Contains(hint, "daemon") || !strings.Contains(hint, "uinput") {
		t.Errorf("wayland hint missing ydotoold/uinput note: %q", hint)
	}
	if !strings.Contains(hint, "wlroots") {
		t.Errorf("wayland hint missing wlroots note: %q", hint)
	}

	// Verify that exec.ErrNotFound maps to NoticedError with hint
	err := linuxError(exec.ErrNotFound, "x11")
	noticed, ok := err.(*tool.NoticedError)
	if !ok {
		t.Fatalf("expected *tool.NoticedError got %T: %v", err, err)
	}
	if !errors.Is(noticed.Err, exec.ErrNotFound) {
		t.Errorf("expected Err to wrap exec.ErrNotFound")
	}
	if !strings.Contains(noticed.Notice, "apt install") {
		t.Errorf("notice missing apt install hint: %q", noticed.Notice)
	}
}

// TestLinuxDriver_NoticesMissingBinary verifies the driver surfaces a
// missing binary as a NoticedError through a real driver call.
func TestLinuxDriver_NoticesMissingBinary(t *testing.T) {
	for _, tc := range []struct {
		backend string
		want    string
	}{
		{"x11", "xdotool scrot"},
		{"wayland", "ydotool grim"},
	} {
		d, s := newTestLinuxDriver(tc.backend)
		s.Err = exec.ErrNotFound
		err := d.Move(context.Background(), 1, 1)
		noticed, ok := err.(*tool.NoticedError)
		if !ok {
			t.Fatalf("%s: expected *tool.NoticedError got %T: %v", tc.backend, err, err)
		}
		if !strings.Contains(noticed.Notice, tc.want) {
			t.Errorf("%s: notice %q missing %q", tc.backend, noticed.Notice, tc.want)
		}
	}
}

// TestYdotoolKeyCombo verifies that ctrl+c produces the
// expected code:press/code:release pairs: 29:1 46:1 46:0 29:0.
func TestYdotoolKeyCombo(t *testing.T) {
	codes, err := ydotoolKeyCombo("ctrl+c")
	if err != nil {
		t.Fatalf("ydotoolKeyCombo error: %v", err)
	}
	got := strings.Join(codes, " ")
	want := "29:1 46:1 46:0 29:0"
	if got != want {
		t.Errorf("ctrl+c: got %q want %q", got, want)
	}
}

// TestLinuxKeyMap_AgainstHeader spot-checks key codes against
// linux/input-event-codes.h, including the digit row (KEY_1 is 2,
// KEY_0 is 11) and r/s, which are not adjacent to their letters.
func TestLinuxKeyMap_AgainstHeader(t *testing.T) {
	want := map[string]int{
		"ctrl": 29, "shift": 42, "alt": 56, "super": 125,
		"Return": 28, "Tab": 15, "Escape": 1, "space": 57,
		"BackSpace": 14, "Delete": 111,
		"Up": 103, "Down": 108, "Left": 105, "Right": 106,
		"Home": 102, "End": 107, "Page_Up": 104, "Page_Down": 109,
		"F1": 59, "F10": 68, "F11": 87, "F12": 88,
		"a": 30, "q": 16, "r": 19, "s": 31, "z": 44, "m": 50,
		"1": 2, "9": 10, "0": 11,
	}
	for name, code := range want {
		got, _, ok := linuxKeyCode(name)
		if !ok {
			t.Errorf("%q missing from linuxKeyMap", name)
			continue
		}
		if got != code {
			t.Errorf("%q: got %d want %d", name, got, code)
		}
	}
}

// TestYdotoolKeyCombo_NonModifierCount verifies a combo must carry
// exactly one non-modifier key.
func TestYdotoolKeyCombo_NonModifierCount(t *testing.T) {
	for _, combo := range []string{"ctrl", "ctrl+a+b"} {
		if _, err := ydotoolKeyCombo(combo); err == nil {
			t.Errorf("%q: expected error", combo)
		}
	}
}
