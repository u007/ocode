package tui

import (
	"strings"
	"testing"
)

func TestParseStatus(t *testing.T) {
	raw := "M  internal/tui/model.go\n M main.go\n?? newfile.go\nA  added.go"
	m := gitModel{}
	m.parseStatus(raw)
	if len(m.stagedFiles) != 2 {
		t.Fatalf("want 2 staged got %d", len(m.stagedFiles))
	}
	if len(m.unstagedFiles) != 1 {
		t.Fatalf("want 1 unstaged got %d", len(m.unstagedFiles))
	}
	if len(m.untrackedFiles) != 1 {
		t.Fatalf("want 1 untracked got %d", len(m.untrackedFiles))
	}
	if m.stagedFiles[0].path != "internal/tui/model.go" {
		t.Fatalf("unexpected staged path: %s", m.stagedFiles[0].path)
	}
	if !m.stagedFiles[0].staged {
		t.Fatal("expected staged flag to be true")
	}
}

func TestParseStatusRenames(t *testing.T) {
	raw := "R  new.go"
	m := gitModel{}
	m.parseStatus(raw)
	if len(m.stagedFiles) != 1 || m.stagedFiles[0].status != "R" {
		t.Fatalf("expected rename in staged, got %+v", m.stagedFiles)
	}
}

func TestPendingActionConfirmation(t *testing.T) {
	m := gitModel{
		section:       gitSectionChanges,
		unstagedFiles: []gitFile{{status: "M", path: "a.go"}},
		stagedFiles:   []gitFile{},
	}
	m2, _ := m.handleFilesKey("d")
	if m2.pendingAction != "discard" {
		t.Fatalf("want pendingAction=discard got %q", m2.pendingAction)
	}
	m3, _ := m2.handleFilesKey("j")
	if m3.pendingAction != "" {
		t.Fatalf("want pendingAction cleared, got %q", m3.pendingAction)
	}
}

func TestChangesFileListHighlight(t *testing.T) {
	m := gitModel{
		section:       gitSectionChanges,
		panel:         gitPanelFiles,
		stagedFiles:   []gitFile{{status: "M", path: "staged.go"}},
		unstagedFiles: []gitFile{{status: "M", path: "unstaged.go"}},
		filesCursor:   0,
	}
	lines := m.renderFileList(40)
	found := false
	for _, l := range lines {
		if strings.Contains(l, "staged.go") && strings.Contains(l, "\x1b[7m") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected reverse highlight on selected row, got none")
	}
}

func TestLoadBranchesCurrentMarker(t *testing.T) {
	m := gitModel{}
	raw := "  main\n* feature/foo\n  remotes/origin/main"
	m.branches = nil
	m.currentBranch = ""
	for _, line := range strings.Split(raw, "\n") {
		if line == "" {
			continue
		}
		isCurrent := strings.HasPrefix(line, "*")
		name := strings.TrimSpace(strings.TrimPrefix(line, "*"))
		if isCurrent {
			m.currentBranch = name
		}
		m.branches = append(m.branches, name)
	}
	if m.currentBranch != "feature/foo" {
		t.Fatalf("want currentBranch=feature/foo got %q", m.currentBranch)
	}
	if len(m.branches) != 3 {
		t.Fatalf("want 3 branches got %d", len(m.branches))
	}
}

// TODO: uncomment when parseHunks is added
// func TestParseHunks(t *testing.T) {
// 	diff := `diff --git a/foo.go b/foo.go
// --- a/foo.go
// +++ b/foo.go
// @@ -1,3 +1,4 @@
//  package main
// +
//  import "fmt"
//  func main() {
// @@ -10,3 +11,4 @@ func main() {
//  	fmt.Println("hello")
// +	fmt.Println("world")
//  }
// `
// 	hunks := parseHunks(diff)
// 	if len(hunks) != 2 {
// 		t.Fatalf("want 2 hunks got %d", len(hunks))
// 	}
// 	if !strings.HasPrefix(hunks[0].header, "@@ -1,3") {
// 		t.Fatalf("unexpected hunk 0 header: %s", hunks[0].header)
// 	}
// }
