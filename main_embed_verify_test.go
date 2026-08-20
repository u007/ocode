package main

import (
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
)

// TestBundledModelConfigsResolve verifies that the files compiled into the
// binary via //go:embed (the concrete, non-wildcard *.OCODE.md at the repo
// root) are actually reachable through LoadModelContext's bundled fallback.
// This catches the regression where a new model prompt file is added at the
// repo root but not listed in the //go:embed directive in main.go, so it is
// never baked into the build.
func TestBundledModelConfigsResolve(t *testing.T) {
	agent.SetBundledModelConfigFS(bundledModelConfigFS())
	defer agent.SetBundledModelConfigFS(nil)

	cases := []struct {
		model    string
		wantText string
	}{
		{"muse-spark-1.2", "Engineering conventions"},
		{"deepseek-v4-flash", "Model-Specific Instructions"},
	}
	for _, tc := range cases {
		ctx := agent.LoadModelContext(tc.model)
		if ctx == "" {
			t.Fatalf("embedded model context for %s is empty; is it listed in the //go:embed directive in main.go?", tc.model)
		}
		if !strings.Contains(ctx, tc.wantText) {
			t.Fatalf("embedded context for %s missing expected content %q:\n%s", tc.model, tc.wantText, ctx)
		}
	}
}
