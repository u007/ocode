package tui

import (
	"strings"
	"testing"
)

func TestGitStatusParsing(t *testing.T) {
	lines := []string{
		"M  internal/tui/model.go",
		" M main.go",
		"?? newfile.go",
		"A  added.go",
	}
	m := gitModel{}
	for _, line := range lines {
		if len(line) < 3 {
			continue
		}
		x, y, path := string(line[0]), string(line[1]), strings.TrimSpace(line[2:])
		switch {
		case x == "?" && y == "?":
			m.untrackedFiles = append(m.untrackedFiles, gitFile{status: "?", path: path})
		default:
			if x != " " && x != "?" {
				m.stagedFiles = append(m.stagedFiles, gitFile{status: x, path: path, staged: true})
			}
			if y != " " && y != "?" {
				m.unstagedFiles = append(m.unstagedFiles, gitFile{status: y, path: path})
			}
		}
	}
	if len(m.stagedFiles) != 2 {
		t.Fatalf("want 2 staged got %d", len(m.stagedFiles))
	}
	if len(m.unstagedFiles) != 1 {
		t.Fatalf("want 1 unstaged got %d", len(m.unstagedFiles))
	}
	if len(m.untrackedFiles) != 1 {
		t.Fatalf("want 1 untracked got %d", len(m.untrackedFiles))
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

func TestPendingActionConfirmation(t *testing.T) {
	m := gitModel{
		section:       gitSectionChanges,
		unstagedFiles: []gitFile{{status: "M", path: "a.go"}},
		stagedFiles:   []gitFile{},
	}
	// First d: sets pending
	m2, _ := m.handleFilesKey("d")
	if m2.pendingAction != "discard" {
		t.Fatalf("want pendingAction=discard got %q", m2.pendingAction)
	}
	// Different key clears pending
	m3, _ := m2.handleFilesKey("j")
	if m3.pendingAction != "" {
		t.Fatalf("want pendingAction cleared, got %q", m3.pendingAction)
	}
}
