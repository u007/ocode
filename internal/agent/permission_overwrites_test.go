package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnalyzeFileBackups(t *testing.T) {
	// An explicit non-temp base: t.TempDir() is itself under a temp root.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(home, ".ocode-backup-facts-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	for _, name := range []string{"a.go", "b.go", "a_test.go"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a, b := filepath.Join(root, "a.go"), filepath.Join(root, "b.go")

	t.Run("saved file replaced by git show and by a temp copy", func(t *testing.T) {
		f := analyzeFileBackups("cd "+root+" && cp a.go /tmp/m_a.go && cp /tmp/s_a.go a.go 2>/dev/null || git show :a.go > a.go; mv a_test.go /tmp/m_at.go; go test ./...", "/")
		if len(f.SavedThenReplaced) != 1 || f.SavedThenReplaced[0] != (fileBackup{File: a, SavedTo: "/tmp/m_a.go"}) {
			t.Fatalf("SavedThenReplaced=%+v", f.SavedThenReplaced)
		}
		if len(f.MovedToTemp) != 1 || f.MovedToTemp[0].File != filepath.Join(root, "a_test.go") {
			t.Fatalf("MovedToTemp=%+v", f.MovedToTemp)
		}
		if len(f.ReplacedWithoutBackup) != 0 {
			t.Fatalf("ReplacedWithoutBackup=%v, want none", f.ReplacedWithoutBackup)
		}
	})
	t.Run("a backup of another file does not cover the overwrite", func(t *testing.T) {
		f := analyzeFileBackups("cp b.go /tmp/b.bak && git show HEAD:a.go > a.go", root)
		if len(f.SavedThenReplaced) != 0 {
			t.Fatalf("SavedThenReplaced=%+v, want none", f.SavedThenReplaced)
		}
		if len(f.ReplacedWithoutBackup) != 1 || f.ReplacedWithoutBackup[0] != a {
			t.Fatalf("ReplacedWithoutBackup=%v, want [%s]", f.ReplacedWithoutBackup, a)
		}
	})
	t.Run("backup after the overwrite does not count", func(t *testing.T) {
		f := analyzeFileBackups("git show HEAD:a.go > a.go && cp a.go /tmp/a.bak", root)
		if len(f.SavedThenReplaced) != 0 || len(f.ReplacedWithoutBackup) != 1 {
			t.Fatalf("facts=%+v", f)
		}
	})
	t.Run("ordinary commands yield no facts", func(t *testing.T) {
		for _, cmd := range []string{
			"go test ./... > /tmp/out.txt 2>&1",
			"git show HEAD:a.go > /tmp/a_head.go",
			"git show HEAD:a.go > new_file.go",
			"cp /tmp/a.bak a.go",
			"echo hi > " + b,
			"git show HEAD:$F > $F",
		} {
			if f := analyzeFileBackups(cmd, root); !f.empty() {
				t.Fatalf("analyzeFileBackups(%q)=%+v, want empty", cmd, f)
			}
		}
	})
}

// allSavedFirst must fail closed: any write the analysis cannot vouch for
// withholds the "all saved" fact even though one file was verifiably saved.
func TestFileBackupsAllSavedFirstFailsClosed(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(home, ".ocode-backup-facts-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	for _, name := range []string{"a.go", "b.go"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const swap = "cp a.go /tmp/a.bak && git show :a.go > a.go"
	if f := analyzeFileBackups(swap+" && go test ./... 2>&1 | grep -E 'ok|FAIL' | head -4", root); !f.allSavedFirst() {
		t.Fatalf("plain verified swap: allSavedFirst=false, facts=%+v", f)
	}
	for name, tail := range map[string]string{
		"second file overwritten by echo": " && echo x > b.go",
		"second file overwritten by cp":   " && cp /tmp/other.go b.go",
		"project to project copy":         " && cp a.go b.go",
		"tee into project":                " && echo x | tee b.go",
		"sed in place":                    " && sed -i '' 's/a/b/' b.go",
		"rm in project":                   " && rm b.go",
		"backup removed afterwards":       " && rm /tmp/a.bak",
		"backup dir removed afterwards":   " && rm -rf /tmp",
		"backup overwritten by redirect":  " && echo x > /tmp/a.bak",
		"backup overwritten by cp":        " && cp b.go /tmp/a.bak",
		"backup moved away":               " && mv /tmp/a.bak /tmp/elsewhere",
		"unresolvable redirect":           " && echo x > $OUT",
		"git with global option":          " && git -C /elsewhere show :b.go > b.go",
		"git checkout":                    " && git checkout -- b.go",
		"opaque command head":             " && $tool b.go",
		"cd to a variable":                " && cd $DIR && git show :b.go > b.go",
		"find -delete":                    " && find . -name '*.go' -delete",
	} {
		t.Run(name, func(t *testing.T) {
			if f := analyzeFileBackups(swap+tail, root); f.allSavedFirst() {
				t.Fatalf("allSavedFirst=true for %q, facts=%+v", swap+tail, f)
			}
		})
	}
}
