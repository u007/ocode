---
okf_version: 0.1
type: concept
title: Knowledge Bundle System
description: Internal architecture of the OKF v0.1 knowledge bundle — bundle detection, scanning, frontmatter parsing, doc search, .okfignore exclusion, and store CRUD with documented edge cases and gotchas.
tags: [knowledge, bundle, okf, frontmatter, search, pagination, okfignore]
timestamp: 2026-07-06T19:32:06+08:00
---

# Knowledge Bundle System

The OKF (Open Knowledge Format) knowledge bundle is ocode's curated project documentation system, rooted at `docs/`. It stores markdown files with YAML frontmatter and provides search, CRUD, and auto-maintained index/log.

## Bundle Structure

```
docs/
├── .okfignore          # Exclusion patterns (optional)
├── .okf.lock           # Cross-instance flock lock (auto-managed)
├── index.md            # Auto-generated doc index
├── log.md              # Auto-generated change log
├── knowledge-bundle.md # (this doc)
└── ...                 # Other .md docs with YAML frontmatter
```

## Bundle Detection

`DetectBundle(workDir)` checks whether `<workDir>/docs/index.md` exists and its frontmatter contains `okf_version: "0.1"`. A mkdocs/docusaurus-style `index.md` without this marker returns `(nil, false)` — init is refused on pre-existing non-OKF `index.md` files to prevent data loss.

`InitBundle(workDir)` creates the bundle structure under a lock. It is non-destructive and idempotent.

## Bundle Scanning (`Bundle.Docs()`)

The `Docs()` method walks the `docs/` directory recursively. The walk applies these rules in order:

1. **Hidden files/dirs** (dot-prefixed) — skipped entirely; directories are skipped with `filepath.SkipDir`
2. **Bundle-relative path** — computed early so it's available for both file and directory exclusion checks
3. **Directories** — checked against `.okfignore` patterns (matched against both **basename** and **relative path**); match → `SkipDir`
4. **Non-`.md` files** — skipped
5. **Reserved files** (`index.md`, `log.md`) — skipped
6. **Exclusion patterns** — both `DefaultExcludedPatterns` and `.okfignore` patterns applied via `excludedFile()` (matched against basename for defaults, both basename and relPath for `.okfignore`)
7. **Parse** — remaining files are parsed via `ParseDoc()` and included in the result

Default exclusion patterns (`DefaultExcludedPatterns`):
- `PLAN-*.md`
- `*.OCODE.md`

### Directory Matching Gotcha

`excludedDir()` matches `.okfignore` patterns against both the **basename** and the **bundle-relative path**. This means a pattern like `legacy/archived/` correctly matches the directory `docs/legacy/archived/`.

**Before the fix** (commit `79b68b9`): `excludedDir()` only matched against the basename (`archived`), so `legacy/archived/` never triggered. The `relPath` was computed after the directory check, so it wasn't available.

**After the fix**: `relPath` is computed before the directory check, and `excludedDir()` checks both `name` and `relPath`, matching `excludedFile()` behavior.

## Frontmatter Parsing

`ParseDoc(relPath, raw)` extracts YAML frontmatter from markdown files using `extractFrontmatter()`. The frontmatter is delimited by `---` markers.

### Supported delimiter variants

| Pattern | Line endings | Location |
|---------|-------------|----------|
| `---\n...\n---\n` | LF | Standard |
| `---\r\n...\r\n---\r\n` | CRLF | Windows |
| `---\n...\n---` | LF | EOF, no trailing newline |
| `---\r\n...\r\n---` | CRLF | EOF, no trailing newline |
| `---\n---\n...` | Any | Empty frontmatter |
| `---\n---...` | Any | Closing at start of rest |

### EOF No-Trailing-Newline Gotcha

`extractFrontmatter()` failed to detect the closing `---` delimiter when it appeared at **EOF without a trailing newline** (e.g., `---\ntitle: test\n---` at EOF). This caused documents with this pattern to be incorrectly marked as non-conforming.

**Before the fix** (commit `79b68b9`): Only `\n---\n` and `\n---\r\n` were checked as closing delimiters. A document ending with `\n---` (no trailing newline) was not recognized.

**After the fix**: Added `HasSuffix` checks for `\n---` (LF) and `\r\n---` (CRLF) at EOF, with correct `delimLen` calculation (4 for LF, 5 for CRLF).

### Round-trip Fidelity

Unknown frontmatter keys are preserved in a `yaml.Node` tree (`Extra` field) for round-trip fidelity. The `Render()` method re-serializes the full frontmatter node tree, then appends the body. This means editing a doc with `doc_write` preserves non-standard frontmatter keys.

## Doc Search (`Store.Search`)

The `Search()` method provides case-insensitive word-level AND matching across title, description, and body fields:

- **Query tokenization**: The query is split into whitespace-separated words. A doc matches only if ALL words appear somewhere in the doc (title, description, or body). This avoids the brittleness of exact substring matching — `"OKF knowledge bundle"` will match even though no single field contains that exact contiguous string.
- **Scoring**: For single-token queries: title match = 3, description = 2, body = 1. For multi-token queries: each token found in the title adds 3 to the score, each in the description adds 2, each in the body adds 1.
- **Tag filtering**: AND logic — a doc must have ALL requested tags
- **DocType filtering**: exact match on the `type` field
- **Sorting**: score descending (higher relevance first), then by path for stable order
- **Non-conforming docs**: excluded from search results

### Pagination Gotcha

`Store.Search()` uses **0-based page arithmetic** (`paginate` computes `start := page * pageSize`). Page 0 returns the first `pageSize` results.

`DocSearchTool` (the context agent's tool) exposes `page` as a **1-based** parameter to the LLM. The conversion happens in the tool handler.

**Before the fix** (commit `79b68b9`): `DocSearchTool` passed its 1-based `page` directly to `Search()`, so page 1 computed `start = 1 * 20 = 20`, skipping the first 20 results.

**After the fix**: `DocSearchTool` converts to 0-based (`zeroBasedPage := params.Page - 1`) before calling `Search()`.

## Store CRUD

### Write

`Store.Write()` creates or updates a document:

- **`type`** is required (error if empty)
- **Path validation**: must be within bundle root, not reserved (`index.md`, `log.md`), no path traversal
- **Timestamp**: set to current UTC time (truncated to seconds)
- **Merge behavior**: if the doc already exists and is conforming, known fields are updated and unknown frontmatter keys are preserved. If the existing doc is non-conforming, a fresh conforming doc is created but the original body is preserved.
- **Side effects**: appends to `log.md` and regenerates `index.md`
- **Locking**: all file mutations happen under `WithBundleLock`

### Get

`Store.Get(relPath)` reads and parses a single doc by bundle-relative path.

### Deprecate

`Store.Deprecate(relPath, reason)` sets `status: deprecated` with the given reason. Updates `log.md` and regenerates `index.md`.

## Index and Log

### `index.md`

Auto-generated by `GenerateIndex()`. Lists all conforming docs grouped by their frontmatter type. Non-conforming files are listed below a separator. The root frontmatter (containing `okf_version`) is preserved.

### `log.md`

Auto-appended by `AppendLog()` on every write, deprecation, and deletion. Each entry records the action, doc path, and title.

## Locking

`WithBundleLock(root, fn)` uses POSIX `flock` on `docs/.okf.lock` to prevent concurrent mutations across instances. All write operations (Write, Deprecate, InitBundle, GenerateIndex, AppendLog) run under this lock.
