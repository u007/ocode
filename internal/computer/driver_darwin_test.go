//go:build darwin

package computer

import (
	"context"
	"errors"
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
	code, _, ok = darwinKeyCode("a")
	if code != 0 || !ok {
		t.Fatalf("a: expected (0, _, true) got (%d, _, %v)", code, ok)
	}
	code, _, ok = darwinKeyCode("F5")
	if code != 126 || !ok {
		t.Fatalf("F5: expected (126, _, true) got (%d, _, %v)", code, ok)
	}
	_, _, ok = darwinKeyCode("Bogus")
	if ok {
		t.Fatal("Bogus: expected ok=false")
	}
}

func TestDarwinArgs_Click(t *testing.T) {
	args := darwinArgs("click", "10", "20", "left", "2")
	if len(args) != 8 {
		t.Fatalf("expected 8 args got %d", len(args))
	}
	if args[0] != "-l" {
		t.Fatalf("args[0] = %q", args[0])
	}
	if args[1] != "JavaScript" {
		t.Fatalf("args[1] = %q", args[1])
	}
	if args[2] != scriptPath {
		t.Fatalf("args[2] = %q want %q", args[2], scriptPath)
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
	d := &darwinDriver{r: st}
	if err := d.Key(context.Background(), "ctrl+shift+a"); err != nil {
		t.Fatalf("Key error: %v", err)
	}
	if len(st.Calls) != 1 {
		t.Fatalf("expected 1 call got %d", len(st.Calls))
	}
	if st.Calls[0] != "key 59 56 0" {
		t.Fatalf("expected %q got %q", "key 59 56 0", st.Calls[0])
	}
}

func TestDarwinKeyCombo_RejectsTwoMainKeys(t *testing.T) {
	d := &darwinDriver{r: &stubRunner{}}
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
	st.Err = errors.New("not allowed to send keystrokes")
	d := &darwinDriver{r: st}
	err := d.Click(context.Background(), 10, 20, tool.MouseLeft, 1)
	if err == nil {
		t.Fatal("expected error")
	}
	var ne *tool.NoticedError
	if !errors.As(err, &ne) {
		t.Fatalf("expected *tool.NoticedError, got %T: %v", err, err)
	}
}
