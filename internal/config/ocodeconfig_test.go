package config

import (
	"testing"
)

func TestResolveEditor(t *testing.T) {
	t.Run("config wins", func(t *testing.T) {
		cfg := &OcodeConfig{Editor: "nvim"}
		t.Setenv("VISUAL", "emacs")
		if got := ResolveEditor(cfg); got != "nvim" {
			t.Fatalf("want nvim got %s", got)
		}
	})
	t.Run("VISUAL fallback", func(t *testing.T) {
		t.Setenv("VISUAL", "emacs")
		t.Setenv("EDITOR", "nano")
		if got := ResolveEditor(nil); got != "emacs" {
			t.Fatalf("want emacs got %s", got)
		}
	})
	t.Run("EDITOR fallback", func(t *testing.T) {
		t.Setenv("VISUAL", "")
		t.Setenv("EDITOR", "nano")
		if got := ResolveEditor(nil); got != "nano" {
			t.Fatalf("want nano got %s", got)
		}
	})
	t.Run("vi default", func(t *testing.T) {
		t.Setenv("VISUAL", "")
		t.Setenv("EDITOR", "")
		if got := ResolveEditor(nil); got != "vi" {
			t.Fatalf("want vi got %s", got)
		}
	})
}
