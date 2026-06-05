package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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
	if m2.pendingAction != gitPendingDiscard {
		t.Fatalf("want pendingAction=discard got %q", m2.pendingAction)
	}
	m3, _ := m2.handleFilesKey("j")
	if m3.pendingAction != gitPendingNone {
		t.Fatalf("want pendingAction cleared, got %q", m3.pendingAction)
	}
}

func TestGitOpenBinaryUsesSystemOpener(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "binary.bin")
	if err := os.WriteFile(path, []byte{0, 1, 2, 3}, 0644); err != nil {
		t.Fatal(err)
	}

	m := gitModel{}
	called := false
	m.editorOpener = func(string) tea.Cmd {
		called = true
		return nil
	}

	if !isBinaryFile(path) {
		t.Fatal("expected binary detection to return true")
	}
	if cmd := m.openInEditor(path); cmd == nil {
		t.Fatal("expected binary open to return a system-opener command")
	}
	if called {
		t.Fatal("expected binary open to bypass editorOpener")
	}
}

func TestChangesFileListHighlight(t *testing.T) {
	ApplyThemeColors("opencode")
	m := gitModel{
		section:       gitSectionChanges,
		panel:         gitPanelFiles,
		stagedFiles:   []gitFile{{status: "M", path: "staged.go"}},
		unstagedFiles: []gitFile{{status: "M", path: "unstaged.go"}},
		filesCursor:   0,
	}
	lines := m.renderFileList(40)
	found := false
	want := selectedStyle.Width(40).Render("  M staged.go")
	for _, l := range lines {
		if l == want {
			found = true
		}
	}
	if !found {
		t.Fatal("expected themed selected highlight on selected row, got none")
	}
}

func TestLoadBranchesCurrentMarker(t *testing.T) {
	m := gitModel{}
	raw := "  main\n* feature/foo\n  remotes/origin/main"
	m.parseBranches(raw)
	if m.currentBranch != "feature/foo" {
		t.Fatalf("want currentBranch=feature/foo got %q", m.currentBranch)
	}
	if len(m.branches) != 3 {
		t.Fatalf("want 3 branches got %d", len(m.branches))
	}
}

func TestParseHunks(t *testing.T) {
	diff := "diff --git a/foo.go b/foo.go\n--- a/foo.go\n+++ b/foo.go\n@@ -1,3 +1,4 @@\n package main\n+\n import \"fmt\"\n func main() {\n@@ -10,3 +11,4 @@ func main() {\n \tfmt.Println(\"hello\")\n+\tfmt.Println(\"world\")\n }\n"
	hunks := parseHunks(diff)
	if len(hunks) != 2 {
		t.Fatalf("want 2 hunks got %d", len(hunks))
	}
	if !strings.HasPrefix(hunks[0].header, "@@ -1,3") {
		t.Fatalf("unexpected hunk 0 header: %s", hunks[0].header)
	}
}

func TestGitStagePathStartingWithDashUsesPathSeparator(t *testing.T) {
	dir := initGitRepoForPathSeparatorTest(t)
	writeFileForGitTest(t, dir, "--all", "dash\n")
	writeFileForGitTest(t, dir, "other.txt", "changed\n")

	m := gitModel{
		workDir:        dir,
		section:        gitSectionChanges,
		panel:          gitPanelFiles,
		unstagedFiles:  []gitFile{{status: "M", path: "other.txt"}},
		untrackedFiles: []gitFile{{status: "?", path: "--all"}},
		filesCursor:    1,
		selectedFiles:  map[int]bool{1: true},
	}
	m, _ = m.handleFilesKey("s")

	staged := gitOutputForTest(t, dir, "diff", "--cached", "--name-only")
	if staged != "--all" {
		t.Fatalf("staged files = %q, want only --all", staged)
	}
}

func TestGitUnstagePathStartingWithDashUsesPathSeparator(t *testing.T) {
	dir := initGitRepoForPathSeparatorTest(t)
	writeFileForGitTest(t, dir, "--all", "dash\n")
	writeFileForGitTest(t, dir, "other.txt", "changed\n")
	gitRunForTest(t, dir, "add", "--", "--all", "other.txt")

	m := gitModel{
		workDir:       dir,
		section:       gitSectionChanges,
		panel:         gitPanelFiles,
		stagedFiles:   []gitFile{{status: "A", path: "--all", staged: true}, {status: "M", path: "other.txt", staged: true}},
		filesCursor:   0,
		selectedFiles: map[int]bool{0: true},
	}
	m, _ = m.handleFilesKey("u")

	staged := gitOutputForTest(t, dir, "diff", "--cached", "--name-only")
	if staged != "other.txt" {
		t.Fatalf("staged files after unstage = %q, want only other.txt", staged)
	}
}

func TestGitIgnoreSelectedFileAppendsToGitignore(t *testing.T) {
	dir := initGitRepoForPathSeparatorTest(t)
	writeFileForGitTest(t, dir, "tmp.log", "hello\n")

	m := gitModel{
		workDir:        dir,
		section:        gitSectionChanges,
		panel:          gitPanelFiles,
		untrackedFiles: []gitFile{{status: "?", path: "tmp.log"}},
	}
	got, _ := m.handleFilesKey("i")

	content, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(content)) != "tmp.log" {
		t.Fatalf(".gitignore = %q, want tmp.log", string(content))
	}
	if got.statusMsg != "ignored tmp.log" {
		t.Fatalf("statusMsg = %q, want ignored tmp.log", got.statusMsg)
	}
}

func TestGitIgnoreCustomPathPromptAppendsToGitignore(t *testing.T) {
	dir := initGitRepoForPathSeparatorTest(t)
	m := gitModel{workDir: dir, section: gitSectionChanges, panel: gitPanelFiles}

	m, _ = m.handleFilesKey("I")
	if !m.ignorePathInputMode {
		t.Fatal("expected ignore path prompt to open")
	}
	for _, key := range []tea.KeyPressMsg{{Code: 'b', Text: "b"}, {Code: 'u', Text: "u"}, {Code: 'i', Text: "i"}, {Code: 'l', Text: "l"}, {Code: 'd', Text: "d"}, {Code: '/'}, {Code: '*', Text: "*"}} {
		m, _ = m.Update(key, 80, 24)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, 80, 24)

	content, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(content)) != "build/*" {
		t.Fatalf(".gitignore = %q, want build/*", string(content))
	}
	if m.statusMsg != "ignored build/*" {
		t.Fatalf("statusMsg = %q, want ignored build/*", m.statusMsg)
	}
}

func TestGitRefreshShortcutReloadsState(t *testing.T) {
	dir := initGitRepoForPathSeparatorTest(t)
	writeFileForGitTest(t, dir, "new.txt", "added\n")
	gitRunForTest(t, dir, "add", "--", "new.txt")

	m := gitModel{workDir: dir, section: gitSectionChanges, panel: gitPanelFiles}
	if got := m.renderHints(); !strings.Contains(got, "r refresh") {
		t.Fatalf("expected refresh shortcut in hints, got %q", got)
	}

	m, cmd := m.handleKey(tea.KeyPressMsg{Code: 'r', Text: "r"}, 100, 30)
	if cmd == nil {
		t.Fatal("expected refresh shortcut to return a command")
	}
	msg := cmd()
	refresh, ok := msg.(gitRefreshMsg)
	if !ok {
		t.Fatalf("expected gitRefreshMsg from refresh shortcut, got %T", msg)
	}
	m, _ = m.Update(refresh, 100, 30)
	if len(m.stagedFiles) != 1 || m.stagedFiles[0].path != "new.txt" {
		t.Fatalf("expected refreshed staged files to include new.txt, got staged=%+v unstaged=%+v", m.stagedFiles, m.unstagedFiles)
	}
}

func TestAppendUniqueLineAvoidsDuplicateGitignoreEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(path, []byte("tmp.log\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := appendUniqueLine(path, "tmp.log\n"); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "tmp.log\n" {
		t.Fatalf(".gitignore = %q, want single tmp.log entry", string(content))
	}
}

func initGitRepoForPathSeparatorTest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRunForTest(t, dir, "init")
	gitRunForTest(t, dir, "config", "user.email", "test@example.com")
	gitRunForTest(t, dir, "config", "user.name", "Test User")
	writeFileForGitTest(t, dir, "other.txt", "base\n")
	gitRunForTest(t, dir, "add", "--", "other.txt")
	gitRunForTest(t, dir, "commit", "-m", "base")
	return dir
}

func writeFileForGitTest(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func gitRunForTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func gitOutputForTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
