package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/u007/ocode/internal/tui/fastviewport"
)

func TestSlashSuggestionsEmptyPrefixReturnsAll(t *testing.T) {
	got := slashSuggestions("/")
	if len(got) == 0 {
		t.Fatal("expected all commands returned for bare /")
	}
}

func TestSlashSuggestionsFiltersByPrefix(t *testing.T) {
	got := slashSuggestions("/co")
	if len(got) == 0 {
		t.Fatal("expected at least one /co command")
	}
	// Fuzzy ranking: prefix matches outrank substring matches, so the
	// top hit must start with /co even though other matches may also
	// appear lower in the list.
	if !strings.HasPrefix(got[0].name, "/co") {
		t.Errorf("expected top suggestion to start with /co, got %q", got[0].name)
	}
}

func TestSlashSuggestionsNoMatchReturnsEmpty(t *testing.T) {
	got := slashSuggestions("/zzznomatch")
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestSlashSuggestionHasNameAndDesc(t *testing.T) {
	got := slashSuggestions("/help")
	if len(got) == 0 {
		t.Fatal("expected /help suggestion")
	}
	if got[0].name != "/help" {
		t.Errorf("expected name=/help, got %q", got[0].name)
	}
	if got[0].desc == "" {
		t.Error("expected non-empty desc for /help")
	}
}

func TestSlashSuggestionsResolveAliasesToCanonicalCommand(t *testing.T) {
	got := slashSuggestions("/model")
	if len(got) == 0 || got[0].name != "/models" {
		t.Fatalf("expected /model alias to suggest /models, got %#v", got)
	}
}

func TestSlashPopupStateDefaults(t *testing.T) {
	m := model{}
	if m.showSlashPopup {
		t.Error("showSlashPopup should default false")
	}
	if m.slashPopupIndex != 0 {
		t.Error("slashPopupIndex should default 0")
	}
	if m.slashPopupItems != nil {
		t.Error("slashPopupItems should default nil")
	}
}

func TestSlashPopupShowsWhenInputStartsWithSlash(t *testing.T) {
	m := model{input: newTestTextarea()}
	m.input.SetValue("/co")
	m, _ = m.updateSlashPopupState()
	if !m.showSlashPopup {
		t.Fatal("expected popup to show for /co input")
	}
	if len(m.slashPopupItems) == 0 {
		t.Fatal("expected items populated for /co")
	}
}

func TestSlashPopupHidesWhenInputHasSpace(t *testing.T) {
	m := model{input: newTestTextarea(), showSlashPopup: true}
	m.input.SetValue("/compact ")
	m, _ = m.updateSlashPopupState()
	if m.showSlashPopup {
		t.Fatal("expected popup to hide when input contains space")
	}
}

func TestSlashPopupHidesWhenInputNotSlash(t *testing.T) {
	m := model{input: newTestTextarea(), showSlashPopup: true}
	m.input.SetValue("hello")
	m, _ = m.updateSlashPopupState()
	if m.showSlashPopup {
		t.Fatal("expected popup to hide for non-slash input")
	}
}

func TestSlashPopupHidesWhenOtherModalOpen(t *testing.T) {
	m := model{input: newTestTextarea(), showPicker: true}
	m.input.SetValue("/co")
	m, _ = m.updateSlashPopupState()
	if m.showSlashPopup {
		t.Fatal("expected popup to hide when showPicker is true")
	}
}

func TestAtFilePopupFiltersByTypedName(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll("assets", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("assets", "screen.png"), []byte("img"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("notes.txt", []byte("txt"), 0644); err != nil {
		t.Fatal(err)
	}

	m := model{input: newTestTextarea()}
	// Pre-populate the file list cache synchronously (simulates the async cmd completing)
	if msg, ok := buildFileListCache()().(fileListCacheMsg); ok {
		m.fileListCache = msg.items
	}
	m.input.SetValue("look @screen")
	m, _ = m.updateSlashPopupState()
	if !m.showSlashPopup || len(m.slashPopupItems) == 0 {
		t.Fatalf("expected @ file popup, got %#v", m.slashPopupItems)
	}
	if m.slashPopupItems[0].name != "@assets/screen.png" || m.slashPopupItems[0].desc != "image" {
		t.Fatalf("expected image suggestion first, got %#v", m.slashPopupItems[0])
	}
}

func TestAtFilePopupEnterReplacesActiveToken(t *testing.T) {
	m := model{
		input:           newTestTextarea(),
		showSlashPopup:  true,
		slashPopupItems: []slashSuggestion{{name: "@assets/screen.png", desc: "image"}},
	}
	m.input.SetValue("describe @scr")
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := updated.(model)
	if got.showSlashPopup {
		t.Fatal("expected popup closed after Enter")
	}
	if got.input.Value() != "describe @assets/screen.png " {
		t.Fatalf("expected @ token replacement, got %q", got.input.Value())
	}
}

func TestSlashPopupDownArrowMovesIndex(t *testing.T) {
	m := model{
		input:           newTestTextarea(),
		showSlashPopup:  true,
		slashPopupIndex: 0,
		slashPopupItems: []slashSuggestion{{name: "/a", desc: "first"}, {name: "/b", desc: "second"}},
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	got := updated.(model)
	if got.slashPopupIndex != 1 {
		t.Errorf("expected index 1, got %d", got.slashPopupIndex)
	}
}

func TestSlashPopupUpArrowClampsAtZero(t *testing.T) {
	m := model{
		input:           newTestTextarea(),
		showSlashPopup:  true,
		slashPopupIndex: 0,
		slashPopupItems: []slashSuggestion{{name: "/a", desc: "first"}},
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	got := updated.(model)
	if got.slashPopupIndex != 0 {
		t.Errorf("expected index clamped at 0, got %d", got.slashPopupIndex)
	}
}

func TestSlashPopupEscClosesPopup(t *testing.T) {
	m := model{input: newTestTextarea(), showSlashPopup: true, slashPopupItems: []slashSuggestion{{name: "/a", desc: "x"}}}
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	got := updated.(model)
	if got.showSlashPopup {
		t.Fatal("expected popup closed after Esc")
	}
}

func TestSlashPopupEnterInsertsCommandAndClosesPopup(t *testing.T) {
	m := model{
		input:           newTestTextarea(),
		showSlashPopup:  true,
		slashPopupIndex: 1,
		slashPopupItems: []slashSuggestion{{name: "/a", desc: "first"}, {name: "/compact", desc: "Reduce context"}},
	}
	m.input.SetValue("/co")
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := updated.(model)
	if got.showSlashPopup {
		t.Fatal("expected popup closed after Enter")
	}
	if got.input.Value() != "/compact " {
		t.Errorf("expected input '/compact ', got %q", got.input.Value())
	}
}

func TestSlashPopupEnterRunsExactCommand(t *testing.T) {
	m := model{
		input:           newTestTextarea(),
		messages:        []message{{role: roleUser, text: "keep"}},
		showSlashPopup:  true,
		slashPopupItems: []slashSuggestion{{name: "/compact", display: "/compact", desc: "Reduce context"}},
	}
	m.input.SetValue("/compact")
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := derefTestModel(t, updated)
	if got.showSlashPopup {
		t.Fatal("expected popup closed after exact command runs")
	}
	if len(got.messages) != 3 {
		t.Fatalf("expected compact command transcript plus error, got %d messages", len(got.messages))
	}
	if got.messages[1].role != roleUser || got.messages[1].text != "/compact" {
		t.Fatalf("expected compact command to be recorded in transcript, got %#v", got.messages[1])
	}
	if !strings.Contains(got.messages[2].text, "Compaction requires an LLM connection") {
		t.Fatalf("expected no-connection guidance, got %q", got.messages[2].text)
	}
}

func TestSlashPopupEnterRunsAliasWithoutSuggestions(t *testing.T) {
	m := model{input: newTestTextarea(), showSlashPopup: true}
	m.input.SetValue("/clear")
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := derefTestModel(t, updated)
	if len(got.messages) == 0 || !strings.Contains(got.messages[len(got.messages)-1].text, "Started new session") {
		t.Fatalf("expected /clear alias to run /new, got %#v", got.messages)
	}
}

func TestSlashPopupMouseClickSelectsRow(t *testing.T) {
	m := model{
		input:           newTestTextarea(),
		showSlashPopup:  true,
		slashPopupItems: []slashSuggestion{{name: "/compact", desc: "Reduce context"}, {name: "/connect", desc: "Show API keys"}},
	}
	idx, ok := m.slashPopupRowForY(m.viewport.Height() + 5)
	if !ok {
		t.Fatal("expected row hit")
	}
	if idx != 1 {
		t.Errorf("expected row index 1, got %d", idx)
	}
}

func TestSlashPopupMouseClickUsesVisibleWindow(t *testing.T) {
	m := model{
		input:           newTestTextarea(),
		showSlashPopup:  true,
		slashPopupIndex: 8,
		slashPopupItems: []slashSuggestion{
			{name: "/0", desc: "zero"},
			{name: "/1", desc: "one"},
			{name: "/2", desc: "two"},
			{name: "/3", desc: "three"},
			{name: "/4", desc: "four"},
			{name: "/5", desc: "five"},
			{name: "/6", desc: "six"},
			{name: "/7", desc: "seven"},
			{name: "/8", desc: "eight"},
		},
	}
	idx, ok := m.slashPopupRowForY(m.viewport.Height() + 4)
	if !ok {
		t.Fatal("expected first visible row hit")
	}
	if idx != 1 {
		t.Errorf("expected first visible row to map to index 1, got %d", idx)
	}
}

func TestSlashPopupAppearsInLayout(t *testing.T) {
	m := model{
		input:           newTestTextarea(),
		viewport:        fastviewport.New(80, 10),
		width:           80,
		height:          30,
		ready:           true,
		showSlashPopup:  true,
		slashPopupItems: []slashSuggestion{{name: "/compact", display: "/compact", desc: "Reduce context"}},
	}
	content := m.renderContent()
	if !strings.Contains(content, "/compact") {
		t.Error("expected /compact to appear in rendered content when popup is shown")
	}
}

func newTestTextarea() textarea.Model {
	ta := textarea.New()
	ta.Focus()
	return ta
}

func derefTestModel(t *testing.T, value tea.Model) model {
	t.Helper()
	switch got := value.(type) {
	case model:
		return got
	case *model:
		return *got
	default:
		t.Fatalf("expected model, got %T", value)
		return model{}
	}
}

func TestLooksLikeFilePath(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"absolute multi-segment", "/home/user/file.png", true},
		{"absolute single-segment", "/models", false},
		{"slash command", "/compact", false},
		{"slash command with args", "/models gpt-4", false},
		{"absolute image file", "/photo.png", true},
		{"relative path", "src/main.go", false},
		{"empty", "", false},
		{"bare slash", "/", false},
		{"absolute go file", "/tmp/main.go", true},
		{"windows-like path", "C:\\Users\\file.txt", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := looksLikeFilePath(tc.in)
			if got != tc.want {
				t.Errorf("looksLikeFilePath(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestSlashPopupHidesForFilePath(t *testing.T) {
	ta := textarea.New()
	ta.SetValue("/tmp/screen.png")
	m := model{
		input:           ta,
		viewport:        fastviewport.New(80, 10),
		width:           80,
		height:          30,
		ready:           true,
		showSlashPopup:  true, // force it open
		slashPopupItems: []slashSuggestion{{name: "/compact", desc: "test"}},
		slashPopupIndex: 0,
	}
	m, _ = m.updateSlashPopupState()
	if m.showSlashPopup {
		t.Error("expected slash popup to hide for file path input")
	}
	if len(m.slashPopupItems) != 0 {
		t.Error("expected slash popup items to be cleared")
	}
}

func TestShortcodeForPastedFilesConvertsDraggedPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "assets", "screen shot.png")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("img"), 0644); err != nil {
		t.Fatal(err)
	}

	got, ok := shortcodeForPastedFiles(path, dir)
	if !ok {
		t.Fatal("expected pasted file path to convert")
	}
	want := "[file: screen shot.png] "
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestShortcodeForPastedFilesLeavesTextAlone(t *testing.T) {
	got, ok := shortcodeForPastedFiles("please read /tmp/not-a-real-file", t.TempDir())
	if ok {
		t.Fatalf("expected prose not to convert, got %q", got)
	}
}

func TestPasteMsgConvertsDraggedPathInInput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "assets", "screen.png")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("img"), 0644); err != nil {
		t.Fatal(err)
	}

	m := model{input: textarea.New(), activeTab: tabChat, workDir: dir}
	updated, _ := m.Update(tea.PasteMsg{Content: path})
	got := updated.(model)

	if got.input.Value() != "[file: screen.png] " {
		t.Fatalf("expected dragged path to become shortcode, got %q", got.input.Value())
	}
	if got.fileShortcodePaths["[file: screen.png]"] != path {
		t.Fatalf("expected compact shortcode to keep real path, got %#v", got.fileShortcodePaths)
	}
}

func TestFilePopupEscapesSpaceInSuggestion(t *testing.T) {
	m := model{
		input:           textarea.New(),
		slashPopupItems: []slashSuggestion{{name: "@assets/my\\ screen.png", desc: "image"}},
	}
	m.input.SetValue("describe @my")
	m.acceptPopupSuggestion(m.slashPopupItems[0])

	if got := m.input.Value(); got != "describe @assets/my\\ screen.png " {
		t.Fatalf("expected escaped file shortcode, got %q", got)
	}
}

func TestSlashPopupSessionSuggestionReturnsLoadCmd(t *testing.T) {
	m := model{input: textarea.New()}
	cmd := m.acceptPopupSuggestion(slashSuggestion{name: "/session", desc: "resume session"})
	if cmd == nil {
		t.Fatal("expected /session suggestion to return a session load command")
	}
	if got := m.input.Value(); got != "/session " {
		t.Fatalf("expected session suggestion to populate input, got %q", got)
	}
	if !m.showPicker || m.pickerKind != "session" || !m.pickerSessionLoading {
		t.Fatalf("expected session picker to open in loading state, got showPicker=%v kind=%q loading=%v", m.showPicker, m.pickerKind, m.pickerSessionLoading)
	}
}

func TestProcessFileReferencesResolvesCompactShortcode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("hello compact"), 0644); err != nil {
		t.Fatal(err)
	}

	m := model{fileShortcodePaths: map[string]string{"[file: notes.txt]": path}}
	msg := m.processFileReferences("summarize [file: notes.txt]")()
	got := msg.(fileSearchFinishedMsg)
	if got.err != nil {
		t.Fatal(got.err)
	}
	// The shortcode must be resolved into the real path in the user-visible
	// text so the LLM knows which file to read. The file content must NOT be
	// slurped into a system message — see the OOM bug when @-mentioning a
	// multi-GB file (e.g. .mov) would inject the entire binary into context.
	if !strings.Contains(got.processedText, path) || strings.Contains(got.processedText, "[file:") {
		t.Fatalf("expected shortcode to be expanded to path in processedText, got %q", got.processedText)
	}
	if !strings.Contains(got.processedText, "summarize ") {
		t.Fatalf("expected original prompt prefix to be preserved, got %q", got.processedText)
	}
	for _, msg := range got.messages {
		if msg.raw != nil && strings.Contains(msg.raw.Content, "hello compact") {
			t.Fatalf("did not expect file content to be injected; got %+v", msg)
		}
	}
}

func TestProcessFileReferencesNeverInjectsFileContent(t *testing.T) {
	// Regression: @-mentioning a non-image file used to read the entire file
	// into a system message, which spiked memory >18 GB for a .mov. Even when
	// the file is huge and unreadable, we must not call os.ReadFile on it.
	dir := t.TempDir()
	// Write a file that contains a recognizable marker. If anything slurps it
	// in, the assertion will catch it.
	large := filepath.Join(dir, "movie.mov")
	marker := "DO_NOT_INJECT_THIS_MARKER"
	if err := os.WriteFile(large, []byte(marker), 0644); err != nil {
		t.Fatal(err)
	}

	m := model{}
	msg := m.processFileReferences("convert @" + filepath.Base(large) + " to mp4")()
	got := msg.(fileSearchFinishedMsg)
	if got.err != nil {
		t.Fatal(got.err)
	}
	for _, msg := range got.messages {
		if msg.raw != nil && strings.Contains(msg.raw.Content, marker) {
			t.Fatalf("file content was injected into a system message; got %+v", msg)
		}
	}
}

func TestUniqueFileShortcodeHandlesCollision(t *testing.T) {
	// When two files have the same basename, the shortcode function should generate
	// unique labels by appending counters: [file: notes.txt], [file: notes.txt 2], etc.
	dir := t.TempDir()

	// Create two files with the same name in different directories.
	path1 := filepath.Join(dir, "dir1", "notes.txt")
	path2 := filepath.Join(dir, "dir2", "notes.txt")
	os.MkdirAll(filepath.Dir(path1), 0755)
	os.MkdirAll(filepath.Dir(path2), 0755)
	os.WriteFile(path1, []byte(""), 0644)
	os.WriteFile(path2, []byte(""), 0644)

	m := &model{
		fileShortcodePaths: make(map[string]string),
	}

	// Generate shortcodes for both files.
	shortcode1 := m.uniqueFileShortcode(path1)
	m.fileShortcodePaths[shortcode1] = path1

	shortcode2 := m.uniqueFileShortcode(path2)
	m.fileShortcodePaths[shortcode2] = path2

	// They should be different.
	if shortcode1 == shortcode2 {
		t.Fatalf("expected different shortcodes for same basename, got %q and %q", shortcode1, shortcode2)
	}

	// First should be the base label.
	if shortcode1 != "[file: notes.txt]" {
		t.Fatalf("expected first shortcode [file: notes.txt], got %q", shortcode1)
	}

	// Second should have a counter.
	if shortcode2 != "[file: notes.txt 2]" {
		t.Fatalf("expected second shortcode [file: notes.txt 2], got %q", shortcode2)
	}

	// Verify both paths are stored correctly.
	if m.fileShortcodePaths[shortcode1] != path1 {
		t.Fatalf("expected shortcode1 to resolve to path1, got %q", m.fileShortcodePaths[shortcode1])
	}
	if m.fileShortcodePaths[shortcode2] != path2 {
		t.Fatalf("expected shortcode2 to resolve to path2, got %q", m.fileShortcodePaths[shortcode2])
	}
}

func TestUniqueFileShortcodeReusesIfSamePath(t *testing.T) {
	// If the same file path is requested twice, the shortcode should be reused.
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	os.WriteFile(path, []byte(""), 0644)

	m := &model{
		fileShortcodePaths: make(map[string]string),
	}

	shortcode1 := m.uniqueFileShortcode(path)
	m.fileShortcodePaths[shortcode1] = path

	// Request the same path again.
	shortcode2 := m.uniqueFileShortcode(path)

	// Should return the same shortcode.
	if shortcode1 != shortcode2 {
		t.Fatalf("expected same shortcode for same path, got %q and %q", shortcode1, shortcode2)
	}
}
