---
type: Concept
title: UML viewer (Go symbol index, two diagram views, open-at-line)
description: "The web UI's `uml` session sub-tab: the internal/umlinfo Go AST index, its fingerprint cache and caps, the /api/uml contract including the remote-host proxy rule, package-map and class-diagram views with LOD, per-session view-state persistence, and the Monaco line-reveal race it shares with chat file links."
resource: internal/umlinfo/umlinfo.go; internal/umlinfo/goanalyze.go; internal/server/handler_uml.go; web/src/components/UML/UmlPanel.tsx; web/src/components/UML/UmlDiagram.tsx; web/src/components/UML/umlViewState.ts
tags: [web, uml, go-ast, index, diagram, remote, persistence]
timestamp: 2026-11-25T17:20:34Z
status: active
---
# UML viewer

The `uml` session sub-tab shows a project's declarations without opening its
files, from one Go AST index that feeds two SVG views and a searchable symbol
list. Clicking a symbol opens its file at the declaration line in the Files tab.

## Two views

- **Package map** — one cluster per package, sized by counts, with edges between
  clusters; layered by longest path over the import graph, a cycle falling back
  to layer 0 (`layerPackages`, `web/src/components/UML/UmlDiagram.tsx:37`).
- **Class diagram** — per-layer grids of type boxes whose members appear at zoom
  1.15 and boxes at 0.6 (`UmlDiagram.tsx:264-265`), laid out by hand with no
  layout library. Both views share ctrl/cmd+wheel zoom (0.35–2.6), drag pan and
  selection.

## Sub-tab plumbing

`uml` is a member of `SessionSubTabId` (`web/src/stores/projectStore.tsx:9`) and
is accepted by `toSubTab` (`:527`), with an unknown value falling back to
`chat`. The Go side never learns the tab's meaning: `internal/tabs` stores
`SubTab string` (`internal/tabs/tabs.go:43`) in `tabs.json`, so adding `"uml"`
needed **no store or handler change** and `tabs.json` keeps its cross-process
merge semantics.

## Index shape

`umlinfo.Index` is `version`, `root`, `generated_at`, `monorepo`, `truncated`,
`packages`, `symbols`, `edges`. A symbol carries `kind`
(`struct | interface | type | func | method`), `lang`, `pkg`, `file` (relative
to root), `line`/`end_line` (1-based), `doc`, `receiver` for methods (unwrapped
from pointer/generic receivers), `sig`, `members` (fields and methods, capped at
40) and `is_test`; a package is a directory plus label and counts; an edge is
`from` → `to`, kind `import`. The package, symbol and edge lists are sorted
deterministically (only `generated_at` varies between builds).

## Scan and analysis rules

`BuildIndex` walks the root with `WalkDir`, honouring root-level `.gitignore`
and `.ignore` through a gitignore matcher and skipping the usual junk
directories (`node_modules`, `vendor`, `dist`, …). Non-regular entries are
skipped, which closes the symlink-escape hole: a symlink inside the project can
never pull in a file from outside it. Caps sit next to `SchemaVersion`
(`internal/umlinfo/umlinfo.go:30`): 20k files, 1 MiB per file, 50k symbols, 40
members, 240 chars of doc; any hit sets `Index.Truncated`, shown as a UI badge.

Parsing uses `go/parser` with `SkipObjectResolution | ParseComments`, keeps only
the first GoDoc paragraph, and no model generates prose. Monorepo markers
(`go.work`, `pnpm-workspace.yaml`, `package.json`, `pyproject.toml`) count at the
root only, and import edges map back to directories through the root `go.mod`
module path.

## Fingerprint and cache

`Fingerprint` (`umlinfo.go:192`) hashes the sorted `(rel path, size, mtimeNano)`
of every `.go` file and is the only freshness signal — there is no watcher.
`Indexer` keys entries by absolute root and keeps at most 4 projects; `Get` hits
only on a matching fingerprint; `StartBuild` (`umlinfo.go:495`) parses in a
background goroutine (`crashguard.Go`, 30 s deadline) behind a per-root
`building` single-flight, so concurrent polls never start parallel parses
(`Build` is the synchronous path tests use). **A failed rebuild keeps the
previous index** (failure and time recorded on the entry), and **a truncated
fingerprint appends `:truncated`**, which cannot match a stored fingerprint and
so forces a rebuild instead of a false hit.

## HTTP contract

Routes sit in `Server.registerRoutes()` behind `authMiddleware`
(`internal/server/server.go:445-446`). `HandleUml` (`handler_uml.go:37`) returns
`200` + `{state:"ready", index}` on a fresh fingerprint, else `202` +
`{state:"indexing"}` while starting the background build, and the client polls
`/api/uml` again every 1 s; `400` when `project` is missing; `403` when it is
outside `allowedProjectRoots()`. Resolution is containment against those roots —
never `h.workDir`, never `os.Getwd()`, since a server hosts many projects and a
desktop app can boot with cwd `/`. The handler holds no `h.mu`: a build must
never park a request behind another session's turn.

## Remote projects

`?host=` hands off to `remoteUml` (`handler_uml.go:129`), which proxies to that
host's own `ocode serve` over the workspace connection (`pulseRemoteRequest`;
30 s, 64 MiB body cap) without forwarding `host`, so the remote server indexes
its own filesystem. **A remote project is never answered from a local index** —
the paths exist on the other machine, so a local walk would fail containment or
index nothing. A 4xx passes through; a missing connection, timeout or other
failure is `502`; there is no fallback. The host's own `202` decodes as a `200`
with `state:"indexing"`, so the client's poll loop is identical either way.

## UI behaviour and view state

`UmlPanel` fetches once, polls on `202`, and filters client-side: AND-tokenized
search over name, file, package, signature, doc, receiver and member names,
intersected with kind/language/package chips (`matchesFilter`). At most 400 rows
render (`UmlPanel.tsx:96`) and the count line says how many were hidden. View,
zoom/pan, selected symbol and the query/chips persist in localStorage under
`ocode.ui.umlViewState.v1` (`umlViewState.ts:9`), keyed
`${host ?? "local"}::${root}::${sessionId}` — the same host-scoped convention as
the PDF viewer's `ocode.ui.previewViewState.v1` (see
[PDF viewer zoom, Space navigation, and find](pdf-viewer-zoom-find.md)). Writes
debounce 250 ms and a quota failure is swallowed: view state is a nicety, never
a failed click. Which tab is open is `tabs.json`'s job, not this store's.

## Open-at-line and the Monaco reveal race

Clicking a symbol dispatches the standard `ocode:open-file` event
(`OPEN_FILE_EVENT`, `UmlPanel.tsx:113`), so it shares the Files-tab path with
chat file links rather than adding a UML-specific opener.

That path had a bug, fixed in the same series: `FileEditor` applied a pending
highlight on one 50 ms timer, Monaco loads its model lazily, so the timer could
fire against an empty or short model and `revealLineInCenter` clamped to line 1.
It now polls every 50 ms until the model's line count covers the target (capped
at 40 attempts, ~2 s), then applies anyway so a non-line highlight still lands
(`FileEditor.tsx:926`). The fix also repairs chat file links carrying `:line`;
see [Chat file links with relative or ~ hrefs silently did
nothing](../gotchas/chat-file-links-silent-noop.md).

## Deferred (recorded in `TODO.md`)

TS/JS/Python analysis behind a future `umltreesitter` build tag (cgo
tree-sitter cannot link in the `CGO_ENABLED=0` remote binaries), elkjs layout,
small-model comment generation, call graphs, export, watch mode, cross-machine
view-state sync, and any change to the caps. Two v1 limits are deliberate: each
poll is a full stat-only fingerprint walk (~0.14 s on 101 packages), and the
remote path is verified against a fake host only — a real `ocode serve --remote`
answering `/api/uml` is still unverified.