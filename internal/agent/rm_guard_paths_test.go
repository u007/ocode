package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathUnder(t *testing.T) {
	sep := string(filepath.Separator)
	if !pathUnder(sep+"a"+sep+"b", sep) {
		t.Error("/a/b should be under /")
	}
	if pathUnder(sep, sep) {
		t.Error("/ is not strictly under /")
	}
	if pathUnder(sep+"ab", sep+"a") {
		t.Error("/ab is not under /a")
	}
}

func TestDangerousRmReasonSymlinkGlobAndAncestorGit(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	parent, err := os.MkdirTemp(home, ".ocode-rm-low-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(parent) })
	// Project lives under a dir literally named ".git": that must not poison
	// every delete inside it.
	root := filepath.Join(parent, ".git", "proj")
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, filepath.Join(root, "sub", "toroot")); err != nil {
		t.Fatal(err)
	}
	pm := NewPermissionManager()
	pm.SetWorkDir(root)

	if r := dangerousRmReason(pm, splitShellFields("rm -rf sub")); r != "" {
		t.Errorf("rm -rf sub under a .git ancestor = %q, want none", r)
	}
	for _, cmd := range []string{
		"rm -rf sub/toroot",
		"rm -rf .GIT",
		"rm -rf sub/*",
		"rm -rf $DIR",
	} {
		if dangerousRmReason(pm, splitShellFields(cmd)) == "" {
			t.Errorf("dangerousRmReason(%q) = \"\", want a reason", cmd)
		}
	}
}
