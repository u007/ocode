package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// TestGitPullFlagFromConfig pins the mapping from the shared config to the flag
// the TUI passes: an absent config/block means the documented rebase default,
// and only an explicit "merge" may produce --no-rebase.
func TestGitPullFlagFromConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  *config.OcodeConfig
		want string
	}{
		{"nil config", nil, "--rebase"},
		{"unset block", &config.OcodeConfig{}, "--rebase"},
		{"explicit rebase", &config.OcodeConfig{Git: config.GitConfig{PullStrategy: config.GitPullStrategyRebase}}, "--rebase"},
		{"merge override", &config.OcodeConfig{Git: config.GitConfig{PullStrategy: config.GitPullStrategyMerge}}, "--no-rebase"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := gitPullFlagFromConfig(tc.cfg); got != tc.want {
				t.Fatalf("gitPullFlagFromConfig() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestGitPullPassesConfiguredReconcileFlag pins that ctrl+p always hands git an
// explicit reconcile flag from Settings → Git. Without it a divergent pull fails
// with "fatal: Need to specify how to reconcile divergent branches" whenever the
// repository (or, for a remote project, the host) has no pull.rebase/pull.ff.
func TestGitPullPassesConfiguredReconcileFlag(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell stub for git")
	}
	cases := []struct{ name, flag string }{
		{"default rebase", "--rebase"},
		{"merge override", "--no-rebase"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			logPath := filepath.Join(dir, "git-argv.log")
			stub := filepath.Join(dir, "git-stub")
			script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + logPath + "'\necho 0\n"
			if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}

			oldBin := gitBinary
			gitBinary = stub
			defer func() { gitBinary = oldBin }()
			oldStrategy := gitPullStrategy
			gitPullStrategy = func() string { return tc.flag }
			defer func() { gitPullStrategy = oldStrategy }()

			m := gitModel{workDir: dir, section: gitSectionChanges, panel: gitPanelFiles}
			_, cmd := m.handleFilesKey("ctrl+p")
			if cmd == nil {
				t.Fatal("ctrl+p produced no command")
			}
			cmd()

			data, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatalf("git stub was not invoked: %v", err)
			}
			want := "pull " + tc.flag
			found := false
			for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				if line == want {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("no %q call in git argv log:\n%s", want, data)
			}
		})
	}
}
