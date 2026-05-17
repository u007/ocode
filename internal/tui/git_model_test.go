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
