# Files Tab Content Search — Implementation Plan

## Executive Summary

Add a **content search** mode to the ocode files tab that searches file contents within the project, supports file extension filtering with wildcards (e.g. `*.go`, `*.ts`), and previews matching results with line numbers and highlighted matching content. This builds on the existing files tab infrastructure and follows established patterns from the log tab's search/filter UX.

---

## Current State Analysis

### Files Tab Architecture

**File: `internal/tui/files_model.go`** (1200+ lines)

The files tab is a self-contained sub-model (`filesModel`) with:

- **Two-panel layout**: tree picker (35% width) + file preview (65% width)
- **Tree panel**: Expandable directory tree with `fileNode` items, cursor-based navigation, multi-select support
- **Preview panel**: `viewport.Model` for scrollable content display with syntax highlighting via Chroma
- **Modes**: `filesModeNormal`, `filesModePrompt`, `filesModeDeleteConfirm`, `filesModeEdit`
- **Existing fuzzy mode**: The `/` key triggers `m.fuzzy = true`, which shows a file-path fuzzy filter (NOT content search)

**Key struct:**
```go
type filesModel struct {
    workDir         string
    nodes           []fileNode       // tree items
    cursor          int              // selected tree index
    preview         viewport.Model   // preview viewport
    fuzzy           bool             // path fuzzy filter active
    query           string           // fuzzy filter query
    allPaths        []string         // all file paths for fuzzy filter
    previewPath     string           // current previewed file
    previewRaw      string           // raw (unhighlighted) content
    previewRawLines []string         // split raw lines
    previewLines    []string         // split highlighted lines
    // ... (editor, git status, inline editor fields)
}
```

**Update flow:**
```go
func (m filesModel) Update(msg tea.Msg, w, h int) (filesModel, tea.Cmd) {
    // Priority: editor picker → prompt → delete confirm → inline edit → fuzzy → preview → tree
    if m.fuzzy {
        return m.updateFuzzy(msg)      // existing path filter
    }
    if m.panel == filesPanelPreview {
        return m.updatePreview(msg)
    }
    return m.updateTree(msg, w, h)
}
```

**View method** (`filesModel.View`): Builds two `lipgloss` bordered panes (tree + preview) joined horizontally with the app header above.

### Existing Search/Filter Patterns

1. **File path fuzzy filter** (`/` key in files tab):
   - `m.fuzzy = true` activates inline filter
   - `buildAllPaths()` walks the workDir to collect all file paths
   - `fuzzyFilter(items, query)` in `fuzzy.go` scores/ranks items
   - Shows top 3 matches inline above the tree
   - `enter` navigates to the first match

2. **Log tab search** (`internal/tui/model.go:2840-2886`):
   - Typing anywhere enters characters into `m.logSearch`
   - `esc` clears search; `backspace` deletes last char
   - `refreshLogViewport()` filters entries by `logFuzzyMatch(search, kind+" "+message)`
   - Kind filters toggle via `1-4` keys
   - Search bar rendered as `/ search…` hint

3. **Grep tool** (`internal/tool/search.go:180-345`):
   - `GrepTool.Execute()` performs content search with regex support
   - Supports `include` glob patterns for file filtering
   - Can search specific directories
   - Returns lines with match context

### Preview Rendering

```go
func loadPreviewCmd(n fileNode) tea.Cmd {
    // Reads file, detects binary, applies syntax highlighting
    return filesPreviewMsg{
        path, content (highlighted), raw, size, language, editable
    }
}

func (m *filesModel) applyPreview(msg filesPreviewMsg) {
    m.previewPath = msg.path
    m.previewRaw = msg.raw
    m.previewRawLines = strings.Split(msg.raw, "\n")
    m.previewLines = strings.Split(msg.content, "\n")
    m.preview.SetContent(msg.content)
    m.preview.GotoTop()
}
```

### Layout Constants

```go
const appHeaderHeight = 2  // top pad + title row

// Files tab tree layout:
treeW = w * 35 / 100
previewW = w - treeW - 3
previewH = h - 3
```

---

## Proposed Solution

### New Mode: Content Search

Add a new `filesModeContentSearch` mode to the files tab, activated by `Ctrl+F` (to avoid conflict with the existing `/` path filter). This mode:

1. **Searches file contents** using the project's grep tool logic (or a lightweight in-process implementation)
2. **Filters by extension** with wildcard support (e.g. `*.go`, `*.ts,*.js`)
3. **Displays results** in the preview panel as a scrollable list with filename, line number, and highlighted match
4. **Clicks/enter on a result** navigates to that file and scrolls to the matching line

### Data Structures

```go
type filesMode int

const (
    filesModeNormal filesMode = iota
    filesModePrompt
    filesModeDeleteConfirm
    filesModeEdit
    filesModeContentSearch  // NEW
)

// Content search result
type filesSearchResult struct {
    path     string   // relative file path
    line     int      // 1-based line number
    lineText string   // the matching line content
    column   int      // 0-based start column of match
    length   int      // length of matched text
}

// Content search state (new fields in filesModel)
type filesSearchState struct {
    query       string              // search query (regex or literal)
    extensions  string              // extension filter, e.g. "*.go,*.ts"
    results     []filesSearchResult // matched results
    cursor      int                 // selected result index
    searching   bool                // search in progress
    searchID    int                 // debounce/invalidate ID
    totalHits   int                 // total match count
    fileCount   int                 // number of files with matches
}
```

### New Fields in `filesModel`

```go
type filesModel struct {
    // ... existing fields ...

    // Content search
    search        filesSearchState
    searchInput   textarea.Model   // for query input
    filterInput   textarea.Model   // for extension filter input
    searchFocus   int              // 0=query, 1=extension filter
}
```

### UI Layout for Search Mode

When `filesModeContentSearch` is active, the preview panel transforms:

```
╭──── Tree (35%) ────────╮ ╭──── Preview / Search (65%) ────────────────────╮
│ ▾ internal/            │ │ 🔍 *.go  |  search: handleError              │
│   ▾ tui/               │ │ ────────────────────────────────────────────── │
│     model.go           │ │  3 matches in 2 files                        │
│     files_model.go  ◂──│ │                                               │
│     git_model.go       │ │  files_model.go:142  func (m filesModel) Up…  │
│   ▾ agent/             │ │  files_model.go:289  return m.updateFuzzy(msg)│
│     agent.go           │ │  model.go:1512       m.files, cmd = m.files.U…│
│                        │ │                                               │
╰────────────────────────╯ ╰───────────────────────────────────────────────╯
```

### Key Bindings

| Key | Action |
|-----|--------|
| `Ctrl+F` | Enter content search mode |
| `Esc` | Exit search mode, return to normal |
| `Tab` | Toggle focus between query and extension filter |
| `Enter` | Navigate to selected result (open file, scroll to line) |
| `j/↓` | Move result cursor down |
| `k/↑` | Move result cursor up |
| `n` | Next result |
| `N` | Previous result |
| Type chars | Append to focused input (query or filter) |
| `Backspace` | Delete last char from focused input |

### Search Implementation Strategy

**Option A: In-process Go search (Recommended)**

Use `filepath.Walk` + `strings.Index`/`regexp` for synchronous search. This is simpler and avoids subprocess overhead. For projects under ~10K files, this completes in <100ms.

```go
func (m *filesModel) executeContentSearch() tea.Cmd {
    return func() tea.Msg {
        results := []filesSearchResult{}
        var re *regexp.Regexp
        if m.search.query != "" {
            re, _ = regexp.Compile(strings.ToLower(m.search.query))
        }
        exts := parseExtensions(m.search.extensions) // ["go", "ts"]

        filepath.Walk(m.workDir, func(path string, info os.FileInfo, err error) error {
            if err != nil || info.IsDir() {
                return nil
            }
            // Skip hidden dirs
            if strings.HasPrefix(info.Name(), ".") {
                if info.IsDir() { return filepath.SkipDir }
                return nil
            }
            // Extension filter
            if len(exts) > 0 {
                ext := strings.TrimPrefix(filepath.Ext(path), ".")
                if !matchExtension(ext, exts) { return nil }
            }
            // Read and search
            data, err := os.ReadFile(path)
            if err != nil { return nil }
            lines := strings.Split(string(data), "\n")
            rel, _ := filepath.Rel(m.workDir, path)
            for i, line := range lines {
                if matchLine(line, re) {
                    results = append(results, filesSearchResult{
                        path: rel, line: i + 1, lineText: line,
                    })
                }
            }
            return nil
        })
        return filesSearchResultMsg{results: results}
    }
}
```

**Option B: Shell out to `grep -rn`**

Use `exec.Command("grep", "-rn", ...)` for large projects. More complex error handling but faster for massive codebases.

### Message Types

```go
type filesSearchResultMsg struct {
    results []filesSearchResult
}
```

### Update Flow Changes

```go
func (m filesModel) Update(msg tea.Msg, w, h int) (filesModel, tea.Cmd) {
    switch msg := msg.(type) {
    case filesSearchResultMsg:
        m.search.results = msg.results
        m.search.searching = false
        m.search.totalHits = len(msg.results)
        return m, nil
    // ... existing cases ...
    case tea.KeyPressMsg:
        // ... existing mode checks ...
        if m.mode == filesModeContentSearch {
            return m.updateContentSearch(msg)
        }
        // ... rest of existing flow ...
    }
    return m, nil
}
```

### View Changes

The `View` method already supports conditional rendering based on `m.mode`. The search mode replaces the preview pane content:

```go
func (m filesModel) View(w, h int, styles Styles, chatUnread, exitPending bool) string {
    // ... existing tree rendering ...

    if m.mode == filesModeContentSearch {
        previewContent = m.renderSearchResults(previewW-2, h-3, styles)
    } else {
        // ... existing preview rendering ...
    }

    // ... rest of existing View ...
}
```

---

## Implementation Phases

### Phase 1: Search State & Mode (files_model.go)

1. Add `filesModeContentSearch` constant
2. Add `filesSearchState` struct and fields to `filesModel`
3. Add `searchInput` and `filterInput` textarea models
4. Initialize them in `newFilesModel()`
5. Add `updateContentSearch(msg)` handler
6. Wire `Ctrl+F` to activate search mode

### Phase 2: Search Execution (files_search.go — new file)

1. Create `internal/tui/files_search.go`
2. Implement `executeContentSearch()` as a `tea.Cmd`
3. Implement `parseExtensions()` — splits `"*.go,*.ts"` → `["go", "ts"]`
4. Implement `matchExtension(ext, exts)` — checks if extension matches any filter
5. Implement `matchLine(line, re)` — case-insensitive search (regex or literal)
6. Add `filesSearchResultMsg` type
7. Handle the result message in `Update`

### Phase 3: Search Results View (files_model.go)

1. Implement `renderSearchResults(w, h, styles)` method
2. Render search bar: `🔍 {filter} | {query}`
3. Render result count: `{N} matches in {M} files`
4. Render scrollable result list with:
   - Filename (hint style)
   - Line number (selected style when cursor matches)
   - Matching line content (truncated to width)
5. Add scrollbar to results list

### Phase 4: Navigation & Interaction (files_model.go)

1. `Enter` on result → load file in preview, switch to `filesPanelPreview`, scroll to line
2. `j/k` navigation through results
3. `n/N` for next/previous
4. `Tab` to toggle query ↔ extension filter focus
5. Live search: execute on each keystroke (debounced)
6. Mouse click on result row → navigate to that result

### Phase 5: Selection & Copy Support

1. In-app text selection for search results (following the existing `selectionState` pattern)
2. `Ctrl+Y` to copy all results to clipboard
3. Selection highlight using `applySelectionHighlight`

### Phase 6: Testing

1. Unit tests for `parseExtensions`, `matchExtension`, `matchLine`
2. Unit tests for `updateContentSearch` key handling
3. Integration test for search execution (mock filesystem)
4. Layout test ensuring search mode fits within terminal bounds

---

## Risk Assessment

| Risk | Mitigation |
|------|------------|
| Slow search on large projects | Debounce search execution (300ms); add file count limit; cancel in-progress searches via `searchID` |
| Regex injection | Escape user input when not in regex mode; use `regexp.Compile` with error handling |
| Binary file crashes | Skip files with null bytes (matching existing preview pattern) |
| Memory pressure from large result sets | Cap results at 1000; show "too many results" hint |
| Conflict with existing `/` fuzzy mode | Use `Ctrl+F` for content search; keep `/` for path filter |
| Terminal height overflow | Use `maxHeight` constraints on result list; scrollbar for overflow |

---

## Files to Create/Modify

### New Files
- `internal/tui/files_search.go` — Search execution logic
- `internal/tui/files_search_test.go` — Unit tests

### Modified Files
- `internal/tui/files_model.go` — Add mode, fields, update/view logic
- `internal/tui/files_model_test.go` — Add tests for search mode
- `internal/tui/model.go` — Wire `Ctrl+F` key binding; handle `filesSearchResultMsg` at top level

---

## Success Criteria

1. ✅ `Ctrl+F` enters content search mode with a visible search UI
2. ✅ Typing a query searches file contents and shows results in real-time
3. ✅ Extension filter (`*.go`, `*.ts,*.js`) correctly limits search scope
4. ✅ Results show filename, line number, and matching content
5. ✅ `Enter` on a result opens the file and scrolls to the matching line
6. ✅ `Esc` exits search mode cleanly
7. ✅ Search handles hidden directories and binary files gracefully
8. ✅ All existing file tab functionality (tree, preview, edit, fuzzy) continues working
9. ✅ Layout works correctly at various terminal sizes
10. ✅ Tests pass and cover key behaviors
