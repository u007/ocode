//go:build darwin

package computer

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

func TestDarwinKeyCode_KnownAndUnknown(t *testing.T) {
	code, _, ok := darwinKeyCode("ctrl")
	if code != 59 || !ok {
		t.Fatalf("ctrl: expected (59, _, true) got (%d, _, %v)", code, ok)
	}
	code, _, ok = darwinKeyCode("cmd")
	if code != 55 || !ok {
		t.Fatalf("cmd: expected (55, _, true) got (%d, _, %v)", code, ok)
	}
	code, _, ok = darwinKeyCode("Return")
	if code != 36 || !ok {
		t.Fatalf("Return: expected (36, _, true) got (%d, _, %v)", code, ok)
	}
	for name, want := range map[string]int{"a": 0, "s": 1, "q": 12, "0": 29, "F5": 96, "F12": 111, "Up": 126} {
		code, _, ok = darwinKeyCode(name)
		if code != want || !ok {
			t.Fatalf("%s: expected (%d, _, true) got (%d, _, %v)", name, want, code, ok)
		}
	}
	_, _, ok = darwinKeyCode("Bogus")
	if ok {
		t.Fatal("Bogus: expected ok=false")
	}
}

func TestDarwinArgs_Click(t *testing.T) {
	args := darwinArgs("/tmp/helper.js", "click", "10", "20", "left", "2")
	if len(args) != 8 {
		t.Fatalf("expected 8 args got %d", len(args))
	}
	if args[0] != "-l" {
		t.Fatalf("args[0] = %q", args[0])
	}
	if args[1] != "JavaScript" {
		t.Fatalf("args[1] = %q", args[1])
	}
	if args[2] != "/tmp/helper.js" {
		t.Fatalf("args[2] = %q", args[2])
	}
	tail := args[3:]
	want := []string{"click", "10", "20", "left", "2"}
	for i, w := range want {
		if tail[i] != w {
			t.Fatalf("tail[%d] = %q want %q", i, tail[i], w)
		}
	}
}

func TestDarwinKeyCombo_OrdersModifiers(t *testing.T) {
	st := &stubRunner{}
	d := &darwinDriver{r: st, script: "/tmp/helper.js"}
	if err := d.Key(context.Background(), "ctrl+shift+s"); err != nil {
		t.Fatalf("Key error: %v", err)
	}
	if len(st.Calls) != 1 {
		t.Fatalf("expected 1 call got %d", len(st.Calls))
	}
	want := "osascript -l JavaScript /tmp/helper.js key 59 56 1"
	if st.Calls[0] != want {
		t.Fatalf("expected %q got %q", want, st.Calls[0])
	}
}

func TestDarwinKeyCombo_RejectsTwoMainKeys(t *testing.T) {
	d := &darwinDriver{r: &stubRunner{}, script: "/tmp/helper.js"}
	err := d.Key(context.Background(), "a+b")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "exactly one non-modifier") {
		t.Fatalf("expected non-modifier error, got %v", err)
	}
}

func TestDarwinAccessibilityErrorIsNoticed(t *testing.T) {
	st := &stubRunner{}
	st.Err = errors.New("computer osascript: exit status 1: accessibility permission denied: allow the terminal")
	d := &darwinDriver{r: st, script: "/tmp/helper.js"}
	err := d.Click(context.Background(), 10, 20, tool.MouseLeft, 1)
	if err == nil {
		t.Fatal("expected error")
	}
	var ne *tool.NoticedError
	if !errors.As(err, &ne) {
		t.Fatalf("expected *tool.NoticedError, got %T: %v", err, err)
	}
}

func TestDarwinScreenshotRemovesTempOnCaptureFailure(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	st := &stubRunner{Err: errors.New("screencapture failed")}
	d := &darwinDriver{r: st, script: "/tmp/helper.js"}
	if _, _, _, err := d.Screenshot(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temp PNG leaked: %v", entries)
	}
}

func TestDarwinScriptWrittenOnceAndRemovedOnShutdown(t *testing.T) {
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	d1, err := newPlatformDriver(&stubRunner{}, sup)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := newPlatformDriver(&stubRunner{}, sup)
	if err != nil {
		t.Fatal(err)
	}
	p1, p2 := d1.(*darwinDriver).script, d2.(*darwinDriver).script
	if p1 != p2 {
		t.Fatalf("helper written twice: %q vs %q", p1, p2)
	}
	if _, err := os.Stat(p1); err != nil {
		t.Fatalf("helper missing: %v", err)
	}
	if err := sup.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p1); !os.IsNotExist(err) {
		t.Fatalf("helper not removed on shutdown: %v", err)
	}
	// A later supervisor (e.g. the next server session) must get a live file again.
	sup2 := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	d3, err := newPlatformDriver(&stubRunner{}, sup2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(d3.(*darwinDriver).script); err != nil {
		t.Fatalf("helper not re-created after shutdown: %v", err)
	}
	if err := sup2.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}
