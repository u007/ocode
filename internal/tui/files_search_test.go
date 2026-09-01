package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestCompileContentSearchPatternDefaultsToCaseInsensitiveLiteral(t *testing.T) {
	re, err := compileContentSearchPattern("a.b", filesContentSearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !re.MatchString("A.B") {
		t.Fatal("expected the default matcher to ignore case")
	}
	if re.MatchString("axb") {
		t.Fatal("expected the default matcher to treat the query literally")
	}
}

func TestCompileContentSearchPatternOptions(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		options filesContentSearchOptions
		match   string
		noMatch string
	}{
		{
			name:    "case sensitive",
			query:   "Foo",
			options: filesContentSearchOptions{caseSensitive: true},
			match:   "Foo",
			noMatch: "foo",
		},
		{
			name:    "whole literal word",
			query:   "cat",
			options: filesContentSearchOptions{wholeWord: true},
			match:   "a cat sleeps",
			noMatch: "concatenate",
		},
		{
			name:    "regex",
			query:   `f.o`,
			options: filesContentSearchOptions{matchMode: filesContentSearchRegex},
			match:   "foo",
			noMatch: "bar",
		},
		{
			name:    "whole regex",
			query:   `foo|bar`,
			options: filesContentSearchOptions{matchMode: filesContentSearchRegex, wholeWord: true},
			match:   "a bar",
			noMatch: "foobar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re, err := compileContentSearchPattern(tt.query, tt.options)
			if err != nil {
				t.Fatal(err)
			}
			if !re.MatchString(tt.match) {
				t.Fatalf("expected %q to match", tt.match)
			}
			if re.MatchString(tt.noMatch) {
				t.Fatalf("expected %q not to match", tt.noMatch)
			}
		})
	}
}

func TestCompileContentSearchPatternRejectsInvalidRegex(t *testing.T) {
	_, err := compileContentSearchPattern("[", filesContentSearchOptions{matchMode: filesContentSearchRegex})
	if err == nil {
		t.Fatal("expected invalid regex to return an error")
	}
}

func TestContentSearchReportsInvalidRegex(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cmd, cancel := startContentSearchCmdWithOptions(dir, "[", "", false, filesContentSearchOptions{matchMode: filesContentSearchRegex})
	defer func() {
		select {
		case <-cancel:
		default:
			close(cancel)
		}
	}()
	msg := cmd()
	done, ok := msg.(filesContentSearchDoneMsg)
	if !ok {
		t.Fatalf("expected done message, got %T", msg)
	}
	if done.err == nil {
		t.Fatal("expected invalid regex error")
	}
}

func TestContentSearchZeroResultsPreservesGeneration(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cmd, cancel := startContentSearchCmdWithOptions(dir, "not-present", "", false, filesContentSearchOptions{generation: 42})
	defer func() {
		select {
		case <-cancel:
		default:
			close(cancel)
		}
	}()
	done, ok := cmd().(filesContentSearchDoneMsg)
	if !ok {
		t.Fatalf("expected zero-result search to finish, got %T", done)
	}
	if done.generation != 42 {
		t.Fatalf("expected generation 42, got %d", done.generation)
	}
	if done.err != nil {
		t.Fatal(done.err)
	}
}

func TestContentSearchWorkerCompletesAfterBatches(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("foo\nfoo\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cmd, cancel := startContentSearchCmd(dir, "foo", "", false)
	defer func() {
		select {
		case <-cancel:
		default:
			close(cancel)
		}
	}()

	var results int
	for {
		msg := cmd()
		switch msg := msg.(type) {
		case filesContentSearchBatchMsg:
			results += len(msg.batch)
			cmd = waitSearchEvent(msg.ch, msg.cancel)
		case filesContentSearchDoneMsg:
			if msg.err != nil {
				t.Fatal(msg.err)
			}
			if results != 2 {
				t.Fatalf("expected 2 results, got %d", results)
			}
			return
		default:
			t.Fatalf("unexpected message %T", msg)
		}
	}
}

func TestContentSearchCancellationIsSafe(t *testing.T) {
	m := filesModel{contentSearchCancel: make(chan struct{}), contentSearchLoading: true}
	m.cancelContentSearch()
	m.cancelContentSearch()
	if m.contentSearchCancel != nil {
		t.Fatal("expected cancellation token to be cleared")
	}
}

func TestFilesContentSearchIgnoresStaleDoneGeneration(t *testing.T) {
	current := make(chan struct{})
	m := filesModel{
		mode:                    filesModeContentSearch,
		contentSearchCancel:     current,
		contentSearchGeneration: 2,
		contentSearchLoading:    true,
	}
	updated, _ := m.Update(filesContentSearchDoneMsg{cancel: current, generation: 1}, 100, 30)
	if !updated.contentSearchLoading || updated.contentSearchDone {
		t.Fatal("expected a stale generation done message to be ignored")
	}
}

func TestContentSearchCancellationClosesWorkerChannel(t *testing.T) {
	dir := t.TempDir()
	var content strings.Builder
	for range 100 {
		content.WriteString("needle\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(content.String()), 0644); err != nil {
		t.Fatal(err)
	}

	cmd, cancel := startContentSearchCmdWithOptions(dir, "needle", "", false, filesContentSearchOptions{generation: 7})
	first, ok := cmd().(filesContentSearchBatchMsg)
	if !ok {
		t.Fatalf("expected initial result batch, got %T", first)
	}
	close(cancel)
	// Draining the producer-owned channel blocks until the worker's defer
	// closes it, proving cancellation does not leave the walk stuck.
	for range first.ch {
	}
}

func TestModelRoutesContentSearchWhileOnAnotherTab(t *testing.T) {
	cancel := make(chan struct{})
	m := model{
		activeTab: tabChat,
		width:     100,
		height:    30,
		files: filesModel{
			mode:                 filesModeContentSearch,
			contentSearchCancel:  cancel,
			contentSearchLoading: true,
		},
	}
	updated, _ := m.Update(filesContentSearchBatchMsg{
		batch:  []filesContentSearchResult{{relPath: "main.go", line: 1, text: "foo"}},
		ch:     make(chan filesContentSearchBatchMsg),
		cancel: cancel,
	},
	)
	got := updated.(model)
	if len(got.files.contentSearchResults) != 1 {
		t.Fatalf("expected result to be retained off-tab, got %#v", got.files.contentSearchResults)
	}
}

func TestFilesContentSearchControlsMarkResultsStale(t *testing.T) {
	m := filesModel{
		mode:                 filesModeContentSearch,
		contentSearchQuery:   "foo",
		contentSearchDone:    true,
		contentSearchResults: []filesContentSearchResult{{relPath: "main.go"}},
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab}, 100, 30)
	if updated.contentSearchPanel != filesContentSearchExtFilter {
		t.Fatalf("expected Tab to focus extensions, got %v", updated.contentSearchPanel)
	}
	updated, _ = updated.Update(tea.KeyPressMsg{Code: tea.KeyTab}, 100, 30)
	updated, _ = updated.Update(tea.KeyPressMsg{Code: tea.KeySpace}, 100, 30)
	if !updated.contentSearchDirty || len(updated.contentSearchResults) != 0 {
		t.Fatal("expected changing match settings to clear stale results")
	}
}

func TestFilesContentSearchViewShowsMatchControls(t *testing.T) {
	m := filesModel{mode: filesModeContentSearch}
	view := stripANSI(m.contentView(80, 20, ApplyThemeColors("tokyonight")))
	for _, want := range []string{
		"Match: literal",
		"Case: insensitive",
		"Word: off",
		"Ignored: excluded",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected search view to contain %q, got:\n%s", want, view)
		}
	}
}

func TestFilesContentSearchViewShowsSearchError(t *testing.T) {
	m := filesModel{
		mode:               filesModeContentSearch,
		contentSearchDone:  true,
		contentSearchError: "error parsing regexp: missing ]",
	}
	view := stripANSI(m.contentView(80, 20, ApplyThemeColors("tokyonight")))
	if !strings.Contains(view, "Search error: error parsing regexp: missing ]") {
		t.Fatalf("expected search error in view, got:\n%s", view)
	}
}

func TestFilesContentSearchClickUsesVisibleResultWindow(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	results := make([]filesContentSearchResult, 20)
	for i := range results {
		results[i] = filesContentSearchResult{relPath: "main.go", line: 1, text: "package main"}
	}
	m := model{
		activeTab: tabFiles,
		width:     100,
		height:    20,
		files: filesModel{
			workDir:              dir,
			mode:                 filesModeContentSearch,
			contentSearchDone:    true,
			contentSearchResults: results,
			contentSearchCursor:  15,
		},
	}
	start, end := m.files.contentSearchResultWindow(contentSearchPaneHeight(m.height))
	if start == 0 || end <= start {
		t.Fatalf("expected a scrolled result window, got %d:%d", start, end)
	}

	click := tea.Mouse{
		Button: tea.MouseLeft,
		X:      filesTreeWidth(m.width) + 2,
		Y:      appHeaderHeight + 1 + contentSearchResultsStartLine,
	}
	updated, _, handled := m.handleMouseAction(click, true)
	if !handled {
		t.Fatal("expected first visible result click to be handled")
	}
	got := updated.(model)
	if got.files.contentSearchCursor != start {
		t.Fatalf("expected click to select result %d, got %d", start, got.files.contentSearchCursor)
	}
}

func TestFilesContentSearchOptionChangesInvalidateActiveGeneration(t *testing.T) {
	tests := []struct {
		name  string
		panel filesContentSearchPanel
		key   tea.KeyPressMsg
	}{
		{name: "query", panel: filesContentSearchQuery, key: tea.KeyPressMsg{Code: 'x', Text: "x"}},
		{name: "match mode", panel: filesContentSearchMatchField, key: tea.KeyPressMsg{Code: tea.KeySpace}},
		{name: "case", panel: filesContentSearchCase, key: tea.KeyPressMsg{Code: tea.KeySpace}},
		{name: "whole word", panel: filesContentSearchWholeWord, key: tea.KeyPressMsg{Code: tea.KeySpace}},
		{name: "ignored files", panel: filesContentSearchIgnore, key: tea.KeyPressMsg{Code: tea.KeySpace}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldCancel := make(chan struct{})
			m := filesModel{
				mode:                 filesModeContentSearch,
				contentSearchQuery:   "foo",
				contentSearchPanel:   tt.panel,
				contentSearchLoading: true,
				contentSearchCancel:  oldCancel,
				contentSearchResults: []filesContentSearchResult{{relPath: "old.go"}},
			}
			updated, _ := m.Update(tt.key, 100, 30)
			select {
			case <-oldCancel:
			default:
				t.Fatal("expected option change to cancel the old generation")
			}
			if !updated.contentSearchDirty || updated.contentSearchLoading || len(updated.contentSearchResults) != 0 {
				t.Fatal("expected option change to invalidate the active search state")
			}

			staleBatch := filesContentSearchBatchMsg{
				batch:  []filesContentSearchResult{{relPath: "stale.go"}},
				ch:     make(chan filesContentSearchBatchMsg),
				cancel: oldCancel,
			}
			updated, _ = updated.Update(staleBatch, 100, 30)
			if len(updated.contentSearchResults) != 0 {
				t.Fatal("expected a stale batch to be discarded")
			}
		})
	}
}

func TestWaitSearchEventHandlesClosedResultChannel(t *testing.T) {
	ch := make(chan filesContentSearchBatchMsg)
	cancel := make(chan struct{})
	close(ch)
	msg := waitSearchEvent(ch, cancel)()
	done, ok := msg.(filesContentSearchDoneMsg)
	if !ok || done.cancel != cancel {
		t.Fatalf("expected closed channel to produce done message, got %#v", msg)
	}
}

func TestContentSearchCancelledCommandReturnsWithoutPanic(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd, cancel := startContentSearchCmd(dir, "main", "", false)
	close(cancel)
	msg := cmd()
	if _, ok := msg.(filesContentSearchDoneMsg); !ok {
		t.Fatalf("expected cancelled search to finish, got %T", msg)
	}
}
