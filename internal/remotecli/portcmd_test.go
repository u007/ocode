package remotecli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/remote"
	"github.com/u007/ocode/internal/tool"
)

func TestParsePortSpec(t *testing.T) {
	cases := []struct {
		spec       string
		wantRemote int
		wantLocal  int
		wantErr    bool
	}{
		{"3000", 3000, 3000, false},
		{"3000:4000", 3000, 4000, false},
		{"0", 0, 0, true},
		{"70000", 0, 0, true},
		{"abc", 0, 0, true},
		{"3000:abc", 0, 0, true},
	}
	for _, c := range cases {
		remotePort, localPort, err := parsePortSpec(c.spec)
		if c.wantErr {
			if err == nil {
				t.Errorf("parsePortSpec(%q): expected error, got none", c.spec)
			}
			continue
		}
		if err != nil {
			t.Errorf("parsePortSpec(%q): unexpected error: %v", c.spec, err)
			continue
		}
		if remotePort != c.wantRemote || localPort != c.wantLocal {
			t.Errorf("parsePortSpec(%q) = (%d, %d), want (%d, %d)", c.spec, remotePort, localPort, c.wantRemote, c.wantLocal)
		}
	}
}

func newTestHook(t *testing.T) (*storePortMapHook, *remote.ForwardManager) {
	t.Helper()
	store, err := projects.NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	ref := projects.ProjectRef{Host: "user@host", Path: "/proj"}
	if err := store.AddRemote(ref.Host, ref.Path); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	fm := remote.NewForwardManager(sup, remote.Target{Kind: remote.KindSSH, Host: "host"})
	return &storePortMapHook{store: store, ref: ref}, fm
}

func TestHandleIgnoresNonPortLines(t *testing.T) {
	hook, fm := newTestHook(t)
	if _, ok := hook.Handle(fm, "hello there"); ok {
		t.Fatal("Handle should not recognize a non-/port line")
	}
	if _, ok := hook.Handle(fm, ""); ok {
		t.Fatal("Handle should not recognize an empty line")
	}
}

func TestHandleStatusEmptyByDefault(t *testing.T) {
	hook, fm := newTestHook(t)
	out, ok := hook.Handle(fm, "/port")
	if !ok {
		t.Fatal("Handle(/port) should be recognized")
	}
	if !strings.Contains(out, "no extra port maps") {
		t.Fatalf("Handle(/port) = %q, want an empty-state message", out)
	}
	// Bare "/port" and "/port status" must behave identically.
	out2, ok := hook.Handle(fm, "/port status")
	if !ok || out2 != out {
		t.Fatalf("Handle(/port status) = (%q, %v), want (%q, true)", out2, ok, out)
	}
}

func TestHandleRemoveUnknownPortReportsError(t *testing.T) {
	hook, fm := newTestHook(t)
	out, ok := hook.Handle(fm, "/port remove 3000")
	if !ok {
		t.Fatal("Handle(/port remove ...) should be recognized")
	}
	if !strings.Contains(out, "not found") {
		t.Fatalf("Handle(/port remove 3000) = %q, want a not-found error", out)
	}
}

func TestHandleUnknownSubcommand(t *testing.T) {
	hook, fm := newTestHook(t)
	out, ok := hook.Handle(fm, "/port frobnicate")
	if !ok {
		t.Fatal("Handle(/port frobnicate) should still be recognized as a /port line")
	}
	if !strings.Contains(out, "unknown /port subcommand") {
		t.Fatalf("Handle(/port frobnicate) = %q, want an unknown-subcommand message", out)
	}
}
