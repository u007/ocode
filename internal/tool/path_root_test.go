package tool

import "testing"

func TestPathWithinRootSlash(t *testing.T) {
	if !pathWithinRoot("/TODO.md", "/") {
		t.Fatal("/TODO.md should be within root /")
	}
	if pathWithinRoot("/abc/x", "/ab") {
		t.Fatal("prefix boundary must hold")
	}
}
