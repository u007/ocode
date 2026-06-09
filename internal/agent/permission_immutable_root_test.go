package agent

import (
	"path/filepath"
	"testing"
)

func TestIsImmutableReadRoot_GoModCache(t *testing.T) {
	t.Setenv("GOMODCACHE", "")
	t.Setenv("GOPATH", "/Users/james/go")

	cases := []struct {
		path string
		want bool
	}{
		{"/Users/james/go/pkg/mod/charm.land/bubbles/v2@v2.1.0/viewport", true},
		{"/Users/james/go/pkg/mod", true},
		{"/Users/james/go/pkg/modular", false}, // prefix-boundary: not the cache
		{"/Users/james/go/src/foo", false},
		{"/etc/passwd", false},
	}
	for _, c := range cases {
		if got := isImmutableReadRoot(c.path); got != c.want {
			t.Errorf("isImmutableReadRoot(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestIsImmutableReadRoot_GOMODCACHE_overrides(t *testing.T) {
	custom := filepath.Join("/tmp", "modcache")
	t.Setenv("GOMODCACHE", custom)
	t.Setenv("GOPATH", "/Users/james/go")

	if !isImmutableReadRoot(filepath.Join(custom, "foo")) {
		t.Error("GOMODCACHE root should be recognized")
	}
	if isImmutableReadRoot("/Users/james/go/pkg/mod/foo") {
		t.Error("GOPATH cache should be ignored when GOMODCACHE is set")
	}
}
