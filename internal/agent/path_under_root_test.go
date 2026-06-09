package agent

import (
	"path/filepath"
	"testing"
)

func TestPathUnderRootFold(t *testing.T) {
	j := func(parts ...string) string { return filepath.Join(parts...) }
	cases := []struct {
		name           string
		resolved, root string
		fold           bool
		want           bool
	}{
		{"exact match no fold", j("/a/b"), j("/a/b"), false, true},
		{"under root no fold", j("/a/b/c"), j("/a/b"), false, true},
		{"prefix boundary no fold", j("/a/bc"), j("/a/b"), false, false},
		{"case differs no fold", j("/A/B"), j("/a/b"), false, false},
		{"empty root", j("/a/b"), "", false, false},
		// Case-insensitive (Windows) matching, using host-native separators.
		{"case differs with fold", j("/Users/X"), j("/users/x"), true, true},
		{"under root with fold", j("/Users/Go/pkg/mod/foo"), j("/users/go/pkg/mod"), true, true},
		{"prefix boundary with fold", j("/users/gopher"), j("/users/go"), true, false},
	}
	for _, c := range cases {
		if got := pathUnderRootFold(c.resolved, c.root, c.fold); got != c.want {
			t.Errorf("%s: pathUnderRootFold(%q,%q,%v)=%v want %v", c.name, c.resolved, c.root, c.fold, got, c.want)
		}
	}
}
