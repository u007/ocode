package cdp

import (
	"reflect"
	"testing"
)

func TestKeyEventParams_BackspaceCarriesVirtualKeyCode(t *testing.T) {
	p := keyEventParams("linux", KeyEvent{Kind: "down", Key: "Backspace", Code: "Backspace"})
	if p["type"] != "rawKeyDown" {
		t.Fatalf("type = %v, want rawKeyDown", p["type"])
	}
	if p["windowsVirtualKeyCode"] != 8 {
		t.Fatalf("VK = %v, want 8", p["windowsVirtualKeyCode"])
	}
	// nativeVirtualKeyCode must stay absent: it makes Chrome re-dispatch
	// unhandled keys to the native window, which freezes macOS headless.
	if _, ok := p["nativeVirtualKeyCode"]; ok {
		t.Fatalf("nativeVirtualKeyCode present: %v", p["nativeVirtualKeyCode"])
	}
	if _, ok := p["commands"]; ok {
		t.Fatalf("linux must not carry mac editing commands: %v", p["commands"])
	}
}

func TestKeyEventParams_MacEditingCommands(t *testing.T) {
	cases := []struct {
		code string
		mods int
		want []string
	}{
		{"Backspace", 0, []string{"deleteBackward"}},
		{"Backspace", 1, []string{"deleteWordBackward"}}, // Option+Backspace
		{"KeyA", 4, []string{"selectAll"}},               // Cmd+A
		{"KeyC", 4, []string{"copy"}},
		{"KeyX", 4, []string{"cut"}},
		{"KeyZ", 4 | 8, []string{"redo"}}, // Shift+Cmd+Z
		{"ArrowLeft", 8, []string{"moveLeftAndModifySelection"}},
		{"ArrowLeft", 4 | 8, []string{"moveToLeftEndOfLineAndModifySelection"}},
		{"KeyV", 4, nil}, // paste is bridged via Input.insertText, never Chrome's clipboard
		{"KeyA", 0, nil},
	}
	for _, c := range cases {
		p := keyEventParams("darwin", KeyEvent{Kind: "down", Code: c.code, Modifiers: c.mods})
		got, _ := p["commands"].([]string)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s mods=%d: commands = %v, want %v", c.code, c.mods, got, c.want)
		}
	}
	// Commands ride only on the down event.
	if _, ok := keyEventParams("darwin", KeyEvent{Kind: "up", Code: "KeyA", Modifiers: 4})["commands"]; ok {
		t.Fatal("keyUp must not carry commands")
	}
}

func TestKeyEventParams_TextSelectsKeyDown(t *testing.T) {
	p := keyEventParams("darwin", KeyEvent{Kind: "down", Key: "a", Code: "KeyA", Text: "a"})
	if p["type"] != "keyDown" || p["unmodifiedText"] != "a" || p["windowsVirtualKeyCode"] != 65 {
		t.Fatalf("params = %v", p)
	}
	p = keyEventParams("darwin", KeyEvent{Kind: "down", Key: "Enter", Code: "Enter", Text: "\r"})
	if p["type"] != "keyDown" || p["windowsVirtualKeyCode"] != 13 {
		t.Fatalf("enter params = %v", p)
	}
	p = keyEventParams("darwin", KeyEvent{Kind: "down", Key: "a", Code: "KeyA", Text: "a", AutoRepeat: true})
	if p["autoRepeat"] != true {
		t.Fatalf("autoRepeat missing: %v", p)
	}
}

func TestVirtualKeyCode(t *testing.T) {
	for code, want := range map[string]int{"KeyA": 65, "KeyZ": 90, "Digit0": 48, "Digit9": 57, "Tab": 9, "F5": 116, "Unidentified": 0, "": 0} {
		if got := virtualKeyCode(code); got != want {
			t.Errorf("%q = %d, want %d", code, got, want)
		}
	}
}
