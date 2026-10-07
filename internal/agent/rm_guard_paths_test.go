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

// `tmp=$(mktemp -d) … rm -rf "$tmp"` removes a directory the same line created,
// so it is scope-checkable. Anything that leaves the variable's value unknown
// must still be refused.
func TestDangerousRmReasonMktempVariable(t *testing.T) {
	root := t.TempDir()
	pm := NewPermissionManager()
	pm.SetWorkDir(root)
	cases := []struct {
		name    string
		cmd     string
		refused bool
	}{
		{"mktemp var", `tmp=$(mktemp -d) && echo hi > "$tmp/c.json"; rm -rf "$tmp"`, false},
		{"braced", `tmp=$(mktemp -d) && rm -rf "${tmp}"`, false},
		{"backticks", "tmp=`mktemp -d` && rm -rf \"$tmp\"", false},
		{"subpath (empty if mktemp failed)", `tmp=$(mktemp -d) && rm -rf "$tmp/sub/a.json"`, true},
		{"dotdot suffix", `tmp=$(mktemp -d) && rm -rf "$tmp/../x"`, true},
		{"reassigned", `tmp=$(mktemp -d) && tmp=/ && rm -rf "$tmp"`, true},
		{"read rebinds", `tmp=$(mktemp -d) && read tmp </dev/stdin; rm -rf "$tmp"`, true},
		{"printf -v rebinds", `tmp=$(mktemp -d) && printf -v tmp /home/x; rm -rf "$tmp"`, true},
		{"unset", `tmp=$(mktemp -d) && unset tmp; rm -rf "$tmp"`, true},
		{"for rebinds", `tmp=$(mktemp -d) && for tmp in /home; do rm -rf "$tmp"; done`, true},
		{"export reassigned", `tmp=$(mktemp -d) && export tmp=/ && rm -rf "$tmp"`, true},
		{"assigned after rm", `rm -rf "$tmp"; tmp=$(mktemp -d)`, true},
		{"not mktemp", `tmp=$HOME && rm -rf "$tmp"`, true},
		{"mktemp -p elsewhere", `tmp=$(mktemp -d -p /) && rm -rf "$tmp"`, true},
		{"other var", `tmp=$(mktemp -d) && rm -rf "$other"`, true},
		{"var glued to glob", `tmp=$(mktemp -d) && rm -rf "$tmp"/*`, true},
		{"var prefix of longer name", `tmp=$(mktemp -d) && rm -rf "$tmpx"`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := parseShellCommandLine(tc.cmd)
			if err != nil {
				t.Fatal(err)
			}
			refused := false
			for i := range parsed {
				if dangerousRmReasonIn(pm, parsed, i) != "" {
					refused = true
				}
			}
			if refused != tc.refused {
				t.Fatalf("refused=%v want %v for %q", refused, tc.refused, tc.cmd)
			}
		})
	}
}
