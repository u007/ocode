# Part 01 — `internal/knowledge` package

Spec: `docs/superpowers/specs/2026-07-06-okf-context-agent-design.md` (sections: OKF bundle, Tools, Write-back §2). Read it before starting.

Global constraints (self-contained copy): activation marker is `okf_version: "0.1"` in root `docs/index.md` frontmatter; all mutations under `WithBundleLock` (`docs/.okf.lock`); reserved files `index.md`/`log.md` never writable directly; unknown frontmatter keys survive rewrite; `type` required on write; deprecation = `status: deprecated` frontmatter, never deletion; search results sorted + paginated; caught errors logged with context; `go build ./...` + `go test ./internal/knowledge/` per task; TDD; commit per task with `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

New dependency: promote `gopkg.in/yaml.v3` (already in `go.sum` as indirect) to a direct require in `go.mod` (`go get gopkg.in/yaml.v3@v3`). Do NOT hand-roll YAML parsing — the existing `internal/skill/loader.go:parseFrontmatter` map[string]string approach cannot round-trip lists or unknown keys.

---

## Task 1: Bundle core — types, frontmatter round-trip, bundle detection

**Files:**
- Create: `internal/knowledge/doc.go` — the `Doc` type and frontmatter round-trip
- Create: `internal/knowledge/bundle.go` — bundle detection and enumeration
- Test: `internal/knowledge/doc_test.go`, `internal/knowledge/bundle_test.go`

**Interfaces produced (later tasks depend on these exact names):**
- `type Doc struct` — fields: `Path` (bundle-relative), `Type`, `Title`, `Description`, `Resource string`, `Tags []string`, `Timestamp time.Time`, `Status string` (empty or `deprecated`), `DeprecatedReason string`, `Extra *yaml.Node` (preserved unknown keys, order intact), `Body string`, `Conforming bool` (false when frontmatter missing/unparseable — tolerated per OKF).
- `func ParseDoc(relPath string, raw []byte) (*Doc, error)` — never returns an error for missing/bad frontmatter (sets `Conforming=false`, logs at debug); errors only on I/O-level impossibilities.
- `func (d *Doc) Render() ([]byte, error)` — serializes frontmatter (known fields + `Extra` unknown keys in original order) + body; a Parse→Render cycle of any conforming doc must be byte-stable for the frontmatter keys it didn't change.
- `type Bundle struct` — `Root string` (absolute path of `docs/`), `OKFVersion string`.
- `func DetectBundle(workDir string) (*Bundle, bool)` — returns `(bundle, true)` only when `<workDir>/docs/index.md` exists and its frontmatter has non-empty `okf_version`. A mkdocs-style `index.md` without the marker returns `(nil, false)`.
- `func (b *Bundle) Docs() ([]*Doc, error)` — walk bundle, parse every non-reserved `.md`, skip `index.md`/`log.md`/dot-files, sorted by path.

**Steps:**
- [ ] Add `gopkg.in/yaml.v3` direct require; `go mod tidy`.
- [ ] Write failing tests covering: parse of a conforming doc (all recommended fields + two unknown keys); Render preserves the unknown keys and their order; parse of a frontmatter-less file yields `Conforming=false` and full content as `Body`; parse of broken YAML tolerated the same way; `DetectBundle` false on missing docs/, false on marker-less `index.md`, true with marker; `Docs()` skips reserved files and returns sorted order; round-trip of a doc where only `Timestamp` is updated leaves every other frontmatter line byte-identical.
- [ ] Run tests, confirm they fail for the right reason (undefined symbols).
- [ ] Implement `doc.go` + `bundle.go` minimally to pass. Frontmatter round-trip via `yaml.Node` (decode into node, extract known fields, keep the remainder as `Extra`).
- [ ] `go test ./internal/knowledge/ -v` → PASS; `go build ./...`.
- [ ] Commit: `feat(knowledge): OKF doc parsing, round-trip, bundle detection`.

---

## Task 2: Index/log generation + cross-instance bundle lock

**Files:**
- Create: `internal/knowledge/index.go` — index + log generation
- Create: `internal/knowledge/lock.go` — flock-style lock
- Test: `internal/knowledge/index_test.go`, `internal/knowledge/lock_test.go`

**Interfaces consumed:** `Bundle`, `Doc` from Task 1.

**Interfaces produced:**
- `func GenerateIndex(b *Bundle) error` — regenerates `docs/index.md` from all conforming docs' frontmatter: root frontmatter block containing only `okf_version` (preserve existing value), then sections grouped by top-level directory (root-level docs under a `# Concepts` section), each entry a bundle-relative markdown link + ` - ` + description, entries sorted by title. Non-conforming files listed under a final `# Unclassified` section (filename links, no description). Deprecated docs annotated `(deprecated)`.
- `func AppendLog(b *Bundle, action, docPath, summary string) error` — `action` one of `Creation|Update|Deprecation|Deletion`; creates `docs/log.md` with `# Directory Update Log` heading if absent; inserts under today's `## YYYY-MM-DD` heading (creating it at the top if absent — newest first) a `* **<action>**: <summary> ([<docPath>](/<docPath>))` bullet.
- `func WithBundleLock(root string, fn func() error) error` — acquires an exclusive advisory lock on `<root>/.okf.lock` (create if missing, `syscall.Flock` LOCK_EX, released on return; bounded wait ~10s then error). Every mutation in Task 3 and later parts wraps itself in this.
- On index-generation failure: log the error, leave the previous `index.md` intact (write to temp file + rename).

**Steps:**
- [ ] Write failing tests: index generated from a fixture bundle (3 conforming docs in 2 dirs, 1 non-conforming, 1 deprecated) matches expected grouping/sorting/annotations and keeps `okf_version`; regeneration is idempotent; log append creates file, prepends new date group, appends within existing date group, preserves older groups; `WithBundleLock` serializes two concurrent goroutines (observable via shared counter) and times out when the lock is held by another process-level holder.
- [ ] Run tests → fail on undefined symbols.
- [ ] Implement `index.go` (temp-file + rename write) and `lock.go`.
- [ ] `go test ./internal/knowledge/ -v` → PASS; `go build ./...`.
- [ ] Commit: `feat(knowledge): index/log generation and cross-instance bundle lock`.

---

## Task 3: Store operations — search, get, write, deprecate

**Files:**
- Create: `internal/knowledge/store.go`
- Test: `internal/knowledge/store_test.go`

**Interfaces consumed:** everything from Tasks 1–2.

**Interfaces produced (the agent tool layer in Part 03 calls exactly these):**
- `type Store struct` — constructed via `func NewStore(b *Bundle) *Store`.
- `type SearchResult struct` — `Path`, `Type`, `Title`, `Description string`, `Tags []string`, `Score float64`, `Snippet string`.
- `func (s *Store) Search(query string, tags []string, docType string, page, pageSize int) ([]SearchResult, int, error)` — frontmatter-aware scan: filter by tags (all must match) and `docType` (exact, case-insensitive) when provided; keyword-match query terms against title/description/tags (weighted higher) and body; return sorted by score then title, paginated, plus total count. Deprecated docs ranked last and marked in snippet.
- `func (s *Store) Get(relPath string) (*Doc, error)` — path-guard (must resolve inside bundle root, reject `..` traversal), returns the parsed doc including non-conforming ones.
- `func (s *Store) Write(relPath string, fm DocMeta, body string) error` — `type DocMeta struct` mirrors `Doc` frontmatter fields (no `Extra`). Fails loud (returned error, no fixup) when: `Type` empty, path outside bundle, path is a reserved file, extension not `.md`. On update: parse existing doc first, preserve its `Extra` unknown keys, merge new fields. Sets `Timestamp` to now. Under `WithBundleLock`: write doc, `AppendLog` (Creation or Update, decided by prior existence), `GenerateIndex`.
- `func (s *Store) Deprecate(relPath string, reason string) error` — same guards; sets `Status: deprecated` + `DeprecatedReason`, preserves all other frontmatter and body; under lock: write + `AppendLog(Deprecation)` + `GenerateIndex`.

**Steps:**
- [ ] Write failing tests: search by keyword hits title match above body match; tag filter; type filter; pagination totals; deprecated ranked last; `Get` path-traversal rejected; `Write` rejects empty type / reserved file / outside-root / non-md; `Write` create vs update log actions; unknown-key preservation across a `Write` update; `Deprecate` sets status and leaves body + unknown keys intact; every mutation regenerated the index (assert index content changed accordingly).
- [ ] Run tests → fail.
- [ ] Implement `store.go`.
- [ ] `go test ./internal/knowledge/ -v` → PASS; `go build ./...`.
- [ ] Commit: `feat(knowledge): store with search/get/write/deprecate`.
