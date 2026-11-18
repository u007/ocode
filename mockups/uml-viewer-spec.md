# UML / Code-Structure Viewer — v1 spec

Status: reviewed design, pre-implementation (2026-11-18) — design direction approved by user (languages, tab type, layout, persistence); implementation sequencing per review (Go-first). Design artifact + mockup: `mockups/uml-viewer-mockup.html`.

## Goal

A new session sub-tab (`uml`) in the web/desktop SPA that browses the open project with an AST-derived symbol index and renders it as an interactive UML-style diagram. Requirements (from the user, 2026-11-18):

- AST-based code browse with **monorepo support** (go.work, pnpm/npm workspaces, pyproject).
- Languages: **Go + TypeScript/JavaScript + Python**.
- Whole-repo view that stays compact via **LOD clustering**; per-package expansion on demand.
- Click a symbol → editor opens at **exact file:line**.
- **Automatic comments population**: existing doc comments on the node; missing ones via opt-in "Generate with small model" action (never auto-fired in v1).
- Compact diagram, **zoom in/out**, pan.
- Persists as a **session tab** (survives restart via `tabs.json`).
- **Searchable + filterable** symbol list; clicking a result **focuses the diagram** (zoom-pan + pulse).
- Switchable views: **Package map**, **Class diagram**, **elkjs layered** (mockup demonstrates all three).

## v1 scope

Sequencing (advisor + reviewer, 2026-11-18): the user approved Go+TS/JS+Python and elkjs as the target. v1 ships the Go analyzer first (stdlib `go/parser`, `SkipObjectResolution`) with TS/JS/Python analyzers behind the same interface, reported **unavailable** until the `umltreesitter` build tag lands. elkjs is used only for the optional third view on focused subgraphs, never on the package-map/class-diagram path. Everything else from the requirements list is in.

### Deferred (record in TODO.md, do not build in v1)

- TS/JS/Python parsing (tree-sitter behind `umltreesitter` build tag; `CGO_ENABLED` is off for the remote binaries, so plain builds must keep working via a stub).
- Small-model comment generation (opt-in per node).
- elkjs Web Worker + lazy chunk; call graphs; export (PNG/SVG/mermaid); incremental re-parse/watch; cross-machine view-state sync; sequence diagrams.

## Server

- Package `internal/umlinfo`: `BuildIndex(ctx, root) (*Index, error)`.
  - Walk `root`, skipping `.git`, `node_modules`, `vendor`, `dist`, `.worktrees`, nested modules' caches; **respect `.gitignore`** (use `github.com/go-git/go-git/v5/plumbing/format/gitignore`? — no new dep: shell out? No. Use a minimal matcher or reuse existing `internal/tool/ignore.go` helpers if suitable).
  - Monorepo detection: nearest `go.work`, `pnpm-workspace.yaml`, `package.json#workspaces`, `pyproject.toml [tool.uv.workspace]` / `poetry` — each member dir is a cluster root; otherwise the project root is the single cluster.
  - v1 Go: parse `*.go` files (excluding `_test.go`? **include** — they carry symbols users navigate to; flag them). Collect: packages (dir-based id, rel path), symbols (struct/interface/func/method; name, kind, file, line, doc (first GoDoc paragraph, ≤240 chars), receiver, signature), edges (package→package imports, from import specs; method→receiver-type).
  - Hard caps: ≤20,000 Go files, ≤1 MB/file, total symbol cap 50,000 with truncation marker.
  - Fingerprint: `(rel path, size, mtime_ns)` list hash → cache key; in-memory LRU (max 4 entries) guarded per-project, built outside any server lock with a 30 s deadline.
- Endpoint: `GET /api/uml?project=<root>` (+ `&host=` for remote later — v1 **refuses `host` != local**, 400, matching remote-scope rules; remote indexing runs on the host later).
  - `project` is validated against `allowedProjectRoots()` — never resolved against `h.workDir` or `os.Getwd()`. Out-of-boundary roots get the same refusal the Files/Git endpoints use.
  - `200 {state:"ready", index:{...}}` when fresh; otherwise `202 {state:"indexing"}` after kicking off a background build. Client polls the same URL. The request that starts the build returns immediately; the HTTP connection is never pinned to the build.
  - Session-scoped project resolution: the web client passes the **session's project root** (the same value Files/Git tabs use), never `h.workDir` semantics.
- Index JSON schema (top level): `{version:1, root, generated_at, monorepo:"go.work"|...|"none", truncated:bool, packages:[{id,label,lang,counts,x?,y?}], symbols:[{id,name,kind,lang,pkg,file,line,end_line,doc,receiver?,sig,members:[{kind,name,sig}]}], edges:[{from,to,kind}]}`. `file` always **relative to root**; client joins with root for `ocode:open-file`.

## Web (`web/src/components/UML/`)

- New **session sub-tab** `uml` alongside chat/agents/changes/logs/status/preview:
  - `SessionSubTabId` union + `toSubTab()` + `subTabs[]` in `SessionSubTabs.tsx` (icon `Boxes`/`Network` from lucide).
  - `App.tsx`: `<TabsContent value="uml" forceMount={false}>` — render **only when visited** (heavy SVG; `visitedTabsRef` pattern, not forced mount). Key the load state with `tabLoadKey(host, projectPath, sessionId + ":uml")` and the `useKeyedLoad` store.
  - Handlers: `RESET`/`hide on session switch` need no special case; the tab is content-only (no turn state).
  - `tabs.json` round-trip: merge semantics unchanged — the Go store passes `sub_tab` through opaquely (verified `internal/tabs/tabs.go`), the PUT still replaces only the projects it names, and only the TS side validates the value. No new key classes are introduced.
- Components (lazy via `React.lazy` from App):
  - `UmlPanel.tsx` — 3-pane (list | canvas | inspector); header row with zoom controls, view switch, index status (indexing spinner / ready / error+Retry).
  - `UmlDiagram.tsx` — SVG renderer. **Package map** + **Class diagram** on a hand-rolled layout (cluster-local grid, columns by layer); **elkjs layered** is a separate lazy chunk run only on a focused subgraph (visible cluster ± neighbors), full-repo elk is explicitly out.
  - LOD: `scale < 0.6` → packages only; `0.6–1.4` → packages + class nodes; `> 1.4` → members. Continuous zoom 0.35–2.6, ctrl+wheel, drag-pan, reset button, minimap canvas.
  - Click list row → `selectSymbol` → zoom/pan centers node (scale ≈1.8), `pulse` class 900 ms × 3. Click node → inspector. Double-click node / inspector button → dispatch `ocode:open-file` `{path: abs, line, projectRoot}`.
  - `UmlSymbolList.tsx` — substring search (name/file/pkg/doc/member), filter chips (lang, kind, package), keyboard ↑↓/↵/esc via shared `useListNavigation`; result count; virtualized with `@tanstack/react-virtual` (already a dep) — 2k+ rows.
- Persistence: opened-tab state in `tabs.json` (`sub_tab:"uml"`); **view state** (zoom, pan, selected id, search/filter text, active view) in `localStorage` under `ocode.ui.umlViewState.v1`, keyed `host::projectRoot::sessionId` (same scheme as previewViewState).
- Jump failure (deleted file): if the open-file dispatch target fails server-side, the editor tab shows its existing error — no silent no-op.

## Risks / notes

- tree-sitter cgo: only under `umltreesitter` tag; CI/remote cross-builds stay `CGO_ENABLED=0` and get Go-only. `make` targets unaffected.
- Cache invalidation: mtime fingerprint per file; full reindex on `git checkout` bursts is acceptable (walk is ~ms for 20k files, parse dominates).
- This worktree is the only place changes land; main checkout stays untouched (concurrent sessions).

## Acceptance

- `go test ./internal/umlinfo/` green; `go build ./...` green.
- `cd web && bun check . && npx vitest run src/components/UML` green.
- Manual: open UML tab on the ocode repo → 200-ish Go packages cluster; search `Agent`; Enter focuses node; Open at line lands editor on `internal/agent/agent.go:412`; reload → tab + zoom restored.
