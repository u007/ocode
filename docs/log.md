# Directory Update Log

## 2026-11-18

* **Update**: Corrected the tier-2 denylist clause against `kindByExt`: `.mov` resolves to `video` and `.xlsm` to `excel` because `previewKindForPath` checks `kindByExt` before `NON_PREVIEWABLE_EXTS`, so neither belongs in the "non-browser-playable media" / "legacy binary Office" lists; removed both. Every other extension in the clause (archives, executables, fonts, databases, remaining media, unsupported images, `.doc`/`.ppt`/`.docm`/`.pptm`) verified correct and left untouched. All six `previewKind.ts` citations (five distinct lines: 27, 193, 206, 267, 277) re-verified by symbol grep and still correct. ([gotchas/files-tab-preview-only-routing.md](/gotchas/files-tab-preview-only-routing.md))
* **Update**: `sqlite` was missing from the page's preview-only kind enumeration and from the tier-2 classification model. `PREVIEW_ONLY_KINDS` now lists it (`.sqlite`/`.sqlite3`/`.db`/`.db3` route through `kindByExt` to the DB viewer; `NON_PREVIEWABLE_EXTS` keeps only `.mdb`/`.accdb` among databases, so the denylist clause was corrected too). Re-derived every `previewKind.ts` anchor on the page — `PREVIEW_ONLY_KINDS` 191→193, `isMarkdownPath` 204→206, `previewOnlyKindForPath` 265→267, `isLegacyOfficePath` 275→277, `PreviewKind` 25→27. ([gotchas/files-tab-preview-only-routing.md](/gotchas/files-tab-preview-only-routing.md))
* **Update**: `/docs cleanup` is an LLM audit now, not a preset deprecation list. Without `--yes` it dispatches the `context` subagent (through `dispatchContextAgent`; the server returns the same text as `cleanup_prompt` for the web client to send as a normal turn, like init's `annotate_prompt`) to find duplicated, stale, superseded and orphaned docs, verify staleness against the source, merge duplicates with `doc_write` first, and `doc_deprecate` each removal with a reason. It never deletes. `--yes` is unchanged: one confirmed batch delete of every `status: deprecated` doc under `knowledge.WithBundleLock`, with `Deletion` log entries and index regeneration. Also replaced the inaccurate "per-file confirmation required" claim with the one-batch rule and noted that the maintenance worker's create/update/deprecate actions match the audit's, except the audit cannot delete. No code-line anchors on the page cited `internal/commandctx/docs.go`, `doc_maintenance.go`, `internal/tui/*` or `handler_slash_parity.go`, so none needed re-derivation; the new prose cites symbols only. ([concepts/okf-knowledge-system.md](/concepts/okf-knowledge-system.md))
* **Update**: Restated the removal flow on the store-CRUD page: deprecate first (by hand or through the new `/docs cleanup` audit), then delete every deprecated doc in one batch with `/docs cleanup --yes`. "No `doc_delete` tool" and "removal is never a doc-tool operation" still hold. The page's `store.go:223-233` and `store.go:234-248` anchors were re-checked against the source and are still correct. ([knowledge-bundle.md](/knowledge-bundle.md))
* **Update**: Folded the four load-bearing invariants of the deleted "Permission Evaluation and Unknown Tool Guard" gotcha into a new *Deterministic guards before any judge* section here (unknown tool rejected before argument parsing at `agent.go:4204`; no stat-based access decision except `targetExists` at `permissions.go:8536`; a nonexistent path inside an allowed root stays allowed for non-read tools; the read-target gate runs first at `:2114`). Repointed the now-dangling inbound link, cross-linked from the sandbox page and corrected its stale `Decide` range to 2104-2622, and removed the dead "files-tab-preview-only-routing copy" Related bullet. All anchors re-derived by symbol grep. ([concepts/auto-permission-enforced-categories.md](/concepts/auto-permission-enforced-categories.md), [concepts/sandbox-permission-mode.md](/concepts/sandbox-permission-mode.md), [gotchas/files-tab-preview-only-routing.md](/gotchas/files-tab-preview-only-routing.md))
* **Deletion**: Deleted deprecated doc: Web Compact Feedback Design ([superpowers/specs/2026-09-18-web-compact-feedback-design.md](/superpowers/specs/2026-09-18-web-compact-feedback-design.md))
* **Deletion**: Deleted deprecated doc: Chat Verbosity Display — Design Spec ([specs/2026-09-24-chat-verbosity-display-design.md](/specs/2026-09-24-chat-verbosity-display-design.md))
* **Deletion**: Deleted deprecated doc: Permission Evaluation and Unknown Tool Guard ([gotchas/permission-evaluation-and-unknown-tool-guard.md](/gotchas/permission-evaluation-and-unknown-tool-guard.md))
* **Deletion**: Deleted deprecated doc: Files Tab Auto-Previews Binary/Office Formats (Preview-Only Routing) ([gotchas/files-tab-preview-only-routing copy.md](/gotchas/files-tab-preview-only-routing copy.md))
* **Deletion**: Deleted deprecated doc: Browser Panel Close Reopens Deleted State ([gotchas/browser-panel-close-reopens-state.md](/gotchas/browser-panel-close-reopens-state.md))
* **Update**: Re-derived the drifted preview anchors after `HtmlViewer.tsx` gained its uncontrolled fetch mode and `previewKind.ts` gained two `kindByExt` entries plus a comment line. `HtmlViewer.tsx` 64→142, 85→170, 31→108, 43-59→120-137 (gate 47→124); `previewKind.ts` 85→91, 185→191, 258→265, 268→275 (`:54` still correct). Anchor-only: no prose, meaning or deprecated title/description changed, and `PreviewSurface.tsx:126` was left alone. ([concepts/preview-find.md](/concepts/preview-find.md), [concepts/preview-print-save-as-pdf.md](/concepts/preview-print-save-as-pdf.md), [concepts/web-keyboard-shortcuts.md](/concepts/web-keyboard-shortcuts.md), [gotchas/files-tab-preview-only-routing copy.md](/gotchas/files-tab-preview-only-routing%20copy.md))
* **Update**: HTML preview routing: `.html`/`.htm` now resolve to kind `html` through `kindByExt` (still out of `PREVIEW_ONLY_KINDS`, so Files-tab Edit/Preview/Split is unchanged) and `HtmlViewer` gained the controlled/uncontrolled split — sidebar `PreviewHost`, `PreviewTabPage` and `preview_open` pass no editor text, so it fetches its own bytes and refetches on `revision`. Records the failure mode: an extension missing from `kindByExt` degrades those hosts to `text` even when `splitPreviewKindForPath` handles the Files tab. Re-derived the `previewKind.ts` anchors (185→191, 198→204, 258→265, 268→275). ([gotchas/files-tab-preview-only-routing.md](/gotchas/files-tab-preview-only-routing.md))
* **Update**: Environment rule follow-up: a filter on non-credential names that prints values is now a named allow, the relaxed-`secrets` note covers env listings, with the 2026-11-18 incident, live scores for both profiles and the enforced-profile soft spot. ([concepts/auto-permission-enforced-categories.md](/concepts/auto-permission-enforced-categories.md))
* **Update**: The judge eval now runs two profiles per run (`secrets_enforced`, `secrets_relaxed`) with per-profile score files; added the 2026-11-18 result row. ([concepts/auto-permission-judge-eval.md](/concepts/auto-permission-judge-eval.md))
* **Creation**: Concept page for the Git tab's third sub-view, Branches: GET /api/git/branches (one for-each-ref, `%(upstream:track)` parsing, `origin/HEAD` skipped, empty lists outside a repo), the six new branch/tag ref actions (`branch-force-delete`, `branch-rename`, `rebase`, `tag-delete`, `set-upstream`, `unset-upstream`) with the `refTargetTag` rule and the `refs/remotes/` upstream check, the extended POST /api/git/push (remote/branch/delete/set_upstream), the seven-answer single-ssh probe script, and the client behaviour (dirty-switch warning with no auto-stash, force-delete fallback in the same dialog, tracking checkout of a remote row, short-ref start points; a full `refs/heads/…` checkout detaches HEAD). All anchors re-derived against the worktree. ([concepts/web-git-branches.md](/concepts/web-git-branches.md))
* **Update**: Corrected the Web Git history graph page against `handler_git_refs.go`: the turn guard covers every `writesWorktree` action, which is now seven (`checkout`, `branch-checkout`, `rebase`, `merge`, `cherry-pick`, `revert`, `reset-hard`) rather than four; added the six new ref actions and the tag-only target rule and the `set-upstream` name rule; the remote probe script answers seven lines, not five. Cross-linked the new Branches page. ([concepts/web-git-history-graph.md](/concepts/web-git-history-graph.md))
* **Update**: The stacked-section-heights page now says the History and Branches sub-views both replace the body split and commit box, and links both. ([concepts/web-git-panel-sections.md](/concepts/web-git-panel-sections.md))
* **Update**: Added a See-also section to the worktrees page pointing at the Branches sub-view (where a branch is created, checked out and pushed) and the history page. ([concepts/git-worktrees.md](/concepts/git-worktrees.md))
* **Update**: Keyword filter on the plugin and skill lists in the web UI: PluginsPanel, IntentSkillsDialog, OcodePluginsForm, the CoworkSidebar skill list and the Settings SkillsForm get a client-side filter built on the existing `parseKeywords`/`matchesKeywords` helpers (`web/src/lib/keywordFilter.ts`). Re-derived the SkillsForm.tsx anchors on the skill enable/disable page (the header note, the `getManageableSkills` call, the optimistic-flip and checkbox ranges, the presentation range) and corrected the CoworkSidebar `SIDEBAR_SECTIONS_KEY` anchor on the per-chat MCP toggle page (`:66` -> `:84`; it was already stale before this change). ([concepts/skill-enable-disable.md](/concepts/skill-enable-disable.md), [concepts/per-chat-mcp-toggle.md](/concepts/per-chat-mcp-toggle.md))
* **Creation**: Concept page for the Files-tab Finder-style inline rename (web/desktop): a second plain click on an already-selected file name (`isInlineRenameClick`, tree and columns views) turns the row into `InlineRenameInput`. Covers the exclusions (directories, modifiers, double-click, icon and padding clicks), Enter and blur commit, Escape cancel with the `doneRef` double-commit guard, the shared `performRename` request path also used by the context-menu Rename dialog, the unchanged server endpoint, and the verification gaps (columns-view gesture, committed rename in a browser, WKWebView shell, remote projects). ([concepts/files-tab-inline-rename.md](/concepts/files-tab-inline-rename.md))
* **Creation**: As-built Concept page for the completed root-elevation feature on `feature/root-elevation` (HEAD `25eb349…`, 2026-11-18): the masked, human-only password flow for LLM-issued leading `sudo`/`su` bash commands. Records the locked decisions and 2026-11-17 spikes (SUDO_ASKPASS/fd rejected; PTY + term.MakeRaw approved), the implemented architecture by file path + symbol (leading-elevation detection and inline-secret HardDeny, omitempty wire fields with a write-only resolve password and a struct-shape test pinning no secret on PermissionResponse, a per-Agent single-use zeroing ElevationManager, a getter closure plus independent elevation flag in the tool context, PTY execution with echo suppressed and secret scrubbing, native Windows UAC, masked TUI/web dialogs with the web rehydration field mapping, remote-tunnel routing, sub-agent in-process handoff, and headless/auto-continue/Pulse/3-strike hardening), the invariants, the verification results, and the residual limits. Cross-links the existing design spec and plan. ([concepts/root-elevation.md](/concepts/root-elevation.md))
* **Update**: Re-derived the SettingsPanel.tsx anchors in the Global Skills settings form section against the real file with grep -n: skills registration `SettingsPanel.tsx:74` -> `:138`, `<SkillsForm />` render `line 201-202` -> `line 251-252`, Plugins & Local Models group `line 117-118` -> `line 137`, Theme `line 119` -> `line 140`; SkillsForm.tsx anchors and prose unchanged ([concepts/skill-enable-disable.md](/concepts/skill-enable-disable.md))
* **Update**: Preview print now inlines local `<img>` files. The html kind inlines them after DOMPurify and the markdown kind before the mermaid pass: each local image is read through `api.fetchFileRaw` and embedded as a `data:` URL (`inlineLocalImages`, `localImageDataUrl` in `web/src/components/Preview/printPreview.tsx`). An `img`/`source` srcset that names a local file is dropped; a failed read is logged and the src is kept as written. `localImagePath` moved to `web/src/components/Preview/localImagePath.ts` with no behavior change, so print does not import the viewer. The editor `PrintSource` carries optional projectRoot/projectHost, passed by `FileTabContent`. Still not handled: CSS `url(...)`, style attributes, relative link stylesheets, `@import`, markdown raw HTML, and the on-screen `HtmlViewer`. Verified by vitest only, not in Chrome, WebKit or the desktop print window. Re-derived every printPreview.tsx and MarkdownViewer.tsx anchor on the page. ([concepts/preview-print-save-as-pdf.md](/concepts/preview-print-save-as-pdf.md))
* **Update**: Markdown local-image page: print now inlines local `<img>` files as `data:` URLs through the shared `localImagePath` resolver (`inlineLocalImages` in `web/src/components/Preview/printPreview.tsx`), so the description and the print section no longer say print does not resolve images. `localImagePath` moved to `web/src/components/Preview/localImagePath.ts`; every viewer and resolver anchor on the page was re-derived by grep. Added `localImagePath.ts` to `resource`. ([concepts/markdown-local-images.md](/concepts/markdown-local-images.md))
* **Update**: Desktop save reworked to a chunked streaming bridge (2026-11-18): the SPA streams 8 MiB decoded chunks (`SAVE_CHUNK_BYTES`) via `blob.slice()` + per-chunk `arrayBuffer()` through `saveDesktopBlob` (`saveDesktopFile` deleted), and the shell appends them to a temp file — strict `seq` order, 32 MiB decoded-chunk cap checked on the base64 length before decoding, out-of-order/overrun/bad base64 aborts the session (temp deleted, native error dialog). On end it verifies received == declared size, then shows the native save panel and renames the temp file to the chosen path (sibling temp + rename across volumes). Cap raised 64 MiB to 2 GiB (`MAX_DESKTOP_SAVE_BYTES` = `desktopSaveMaxBytes`, keep-equal rule); the local fs/download cap went 512 MiB to 2 GiB (remote ssh unchanged at 100 MiB); stale save sessions (>10 min idle) are reaped by `ensurePendingSaveReaper()`. No file:line anchors existed on the page and none were added (symbol citations only). ([concepts/files-tab-clipboard.md](/concepts/files-tab-clipboard.md), [concepts/pdf-viewer-zoom-find.md](/concepts/pdf-viewer-zoom-find.md))

* **Update**: Files-tab right-click Download now streams end to end on the desktop shell: `api.fsDownloadStream` in `web/src/api/client.ts` returns the raw authenticated Response for `GET /api/fs/download` unread, and `saveResponseAsFile` in `web/src/lib/saveBlob.ts` forwards `response.body` in coalesced ~8 MiB pieces (`getReader()`) through `saveDesktopStream` (`web/src/lib/wails.ts`) when the body is a stream and `Content-Length` is a safe integer — no Blob is materialized in the webview — with a `saveDesktopBlob` fallback otherwise; `FileTree.tsx` calls `saveResponseAsFile(await api.fsDownloadStream(...))` and the old `api.fsDownload` Blob variant was removed. The desktop shell also sweeps crash-abandoned `ocode-save-*.part` temp files older than 10 minutes at app start (`reapAbandonedSaveTempFiles`, called from `ensurePendingSaveReaper`). Symbol citations only, no page anchors; `client.fsDownloadStream.test.ts` renamed accordingly. ([concepts/files-tab-clipboard.md](/concepts/files-tab-clipboard.md))

## 2026-11-17

* **Update**: Chrome mode now probes the tab title immediately on `Page.loadEventFired` (session-scoped, no frameId — the top document's load, the first moment a freshly parsed `<title>` can be read) via `Target.handleLoadEvent`, with the 2s `Target.getTargetInfo` poll kept only as a backstop; a probe at `Page.frameNavigated` was deliberately not added because it races the document's own title parse. The change gate `emitTitleIfChanged` now compares BOTH `lastTitle` and `lastURL`, so navigating between pages that share one title re-emits (the SPA's `applyNavEvent` clears `pageTitle` on every URL change), while the emitted `TitleEvent` still carries no URL. Added the live Chrome 157 measurements (static title 234ms, JS title 1.74s after `Navigate`), the `TestTarget_StopHandlersTwiceIsSafe` pin, and re-derived every `file:line` anchor on the page. ([concepts/browse-tab-titles.md](/concepts/browse-tab-titles.md))
* **Creation**: Added the embedded-browser tab-title concept: local mode's capture.js MutationObserver -> ocode:browse:title -> setPageTitle, and Chrome mode's per-target Target.getTargetInfo poll (Chrome 157 headless no longer emits targetInfoChanged for a document.title change) -> TitleEvent -> browse.Server.emitTitle -> publishBrowseTitle -> the global browse_title SSE event -> routeBusEnvelope -> setPageTitle. Covers the stale-event URL guard and why the poll omits the URL, the 2s latency and clear semantics, the raw-CDP measurement, and what to re-check if Chrome reports titles again. Cross-linked from the two browse-mode concept pages. ([concepts/browse-tab-titles.md](/concepts/browse-tab-titles.md))
* **Update**: The loopback proof now accepts a curl data value (`-d`, `--json`, `-H`, `-F` and the rest) that embeds URLs when every embedded URL is an on-host literal `http`/`https` target, so a loopback callback in a JSON body no longer falls to the outbound-network judge. Curl-only, because wget and httpie data flags take no value; the LAN carve-out is unchanged. Added a paragraph to the outbound-network concept page. ([concepts/webfetch-websearch-guardrails.md](/concepts/webfetch-websearch-guardrails.md))
* **Update**: Recorded the third loopback defect in the loopback gotcha: a `curl -d` value with an embedded loopback URL voided the proof and was escalated by Jev as `internal_target`; the rule and the failing-command table are documented there. ([gotchas/loopback-curl-shell-port-variable.md](/gotchas/loopback-curl-shell-port-variable.md))

* **Update**: Tailnet clients now get a tab origin while the Tailscale share runs with its browse mount. A non-local client's tab listener is exposed on the tailnet at one HTTPS port per tab (`tailscale serve`, serve only, never funnel); 409 remains only when no such share runs, and an expose failure is a 503. Fixed the boot race: the auto-share now starts after `StartBrowse` in `internal/desktop/boot.go`, so the browse mount is created. Recorded the unverified-live and in-memory `tabPorts` limits and the Known gap for tailnet rate limiting. Anchors re-derived by symbol. ([concepts/browse-per-tab-origin.md](/concepts/browse-per-tab-origin.md))
* **Update**: A tailnet phone's local project no longer gets the `stream_via_main` Chrome stream once the share has its browse mount; it loads in iframe mode on a tailnet tab origin. The stream is now used only when no share with a browse URL runs. Known limitation added; not verified live. ([concepts/browse-stream-via-main-origin.md](/concepts/browse-stream-via-main-origin.md))
* **Update**: skip_advisor now stands down the restored plan checkpoint too, including per-response skill detection; plan and done no longer collapse into one trigger. Sixteen code/test anchors re-derived by symbol. ([concepts/skill-frontmatter.md](/concepts/skill-frontmatter.md))
* **Update**: Three advisor checkpoints now: restored plan is an ungated first-write design review that also stands down on pending ask, while done gates the Jev-judged completion and before-commit moments. Both names survive normalization. ([concepts/pending-ask-suppresses-advisor-auto-continue-goal.md](/concepts/pending-ask-suppresses-advisor-auto-continue-goal.md))
* **Update**: Plan checkpoint exists again and stands down on a pending ask via the same blockedByPendingAsk guard; done gates only the completion and before-commit moments. All three loop checkpoints, not two, are gated. ([gotchas/advisor-checkpoints-must-wait-for-a-pending-ask.md](/gotchas/advisor-checkpoints-must-wait-for-a-pending-ask.md))
* **Update**: Restored the plan advisor checkpoint: three loop moments now, with plan an ungated first-write design review and done gating the Jev-judged completion and before-commit moments. Normalization keeps plan and done as independent triggers; every code anchor re-derived by symbol. ([concepts/advisor-gate-judge.md](/concepts/advisor-gate-judge.md))
* **Update**: Added a gotcha to the web chat transcript layout page: `ToolBlock`'s `highlight` prop is the active find query (not text already known to match), so `<mark>` appears only where the query actually occurs in the rendered command/output; grep's `formatToolArgsHint` yields a non-empty `argsHint` that suppresses the raw command block, which is why `HighlightedCode.test.tsx` (broken at HEAD after commit `784c6425` changed its fixture to `grep`) now queries `hello`, text that really renders. JSON output switches to the plaintext-with-marks fallback while a query is active. ([docs/concepts/web-chat-transcript-layout.md](/concepts/web-chat-transcript-layout.md))
* **Update**: Quick-action chips now have a third mode, `append` (Settings → Quick Actions → "Insert at cursor"): it inserts the chip message at the composer caret, replacing any selection, via the shared `insertAtCursor` helper also used by voice transcripts and LLM suggestions. `fill` still replaces the draft; `send` still dispatches. Corrected the old "quick actions never touch the composer draft" gotcha to be per-mode. The concept page had no numeric `ChatInput.tsx` anchors; the three spec anchors were re-derived by symbol to `:722-742`, `:777`, `:1782-1784`. ([docs/concepts/web-composer-quick-actions.md](/concepts/web-composer-quick-actions.md))

* **Update**: Recorded the 2026-11-17 owner reversal of two 2026-10-01 decisions: `append` is now a first-class quick-action mode (the "one-line change if the user prefers append" deferral is withdrawn) and "making `fill` append instead of replace" is no longer out of scope. `fill` itself is unchanged. The original design text is preserved with dated amendment notes. ([docs/superpowers/specs/2026-10-01-quick-actions-config-design.md](/superpowers/specs/2026-10-01-quick-actions-config-design.md))

* **Update**: Suggested responses now insert at the caret by default instead of filling the composer (2026-11-17 owner decision). They have no mode field — append is a fixed default — and still never auto-send; Q1, §9 and §11 updated with dated notes. ([docs/superpowers/specs/2026-10-20-suggested-responses-design.md](/superpowers/specs/2026-10-20-suggested-responses-design.md))

* **Creation**: Added the implementation plan for root elevation: detection/gating and omitempty wire fields on the permission ask/resolve channel, a per-Agent keyed in-memory secret store with context injection, PTY execution with echo suppressed on Unix and native elevation on Windows, masked TUI/web dialogs, remote-proxy hardening, and the end-to-end hardening sweep (Tasks 1–6). Pinned to the 2026-11-17 approved design spec. ([superpowers/plans/2026-11-17-root-elevation.md](/superpowers/plans/2026-11-17-root-elevation.md))

* **Update**: Corrected the root-elevation design spec in place: the per-session secret store is now a keyed `map[requestID][]byte` owned by the Agent (`*agent.Agent`), so the TUI (in-process) and the RC bridge reach the same per-Agent store and the secret never leaves the process that owns the tool call; parent/subagent caches are naturally isolated. Windows now sets `NeedsPassword = false` with no masked field (UAC authenticates on the secure desktop; headless/remote/service returns an unsupported error), so the forced ask is a plain confirmation. Retry cap fixed at exactly 3; resolve-endpoint anchor corrected to `:174-183` (handler `:173`). ([superpowers/specs/2026-11-17-root-elevation-design.md](/superpowers/specs/2026-11-17-root-elevation-design.md))

* **Creation**: Approved design spec for root elevation via leading `sudo`/`su` bash commands: a masked human password prompt (TUI + web/desktop; headless refuses), a per-session in-memory secret store, PTY injection with echo suppressed (`creack/pty` + `term.MakeRaw`; the SUDO_ASKPASS/fd spike was rejected), the Windows native-elevation limit, and remote/WSL routing into the execution host's memory only. Records the locked decisions, 2026-11-17 spike evidence, security invariants, failure modes and a test matrix. ([superpowers/specs/2026-11-17-root-elevation-design.md](/superpowers/specs/2026-11-17-root-elevation-design.md))

* **Update**: Excel image sizing now uses the drawing's true EMU extent instead of the two-cell anchor. `extractDrawingExtents` (`univerData.ts`) reads `<a:ext cx cy>` from `xl/drawings/*.xml` (914400 EMU = 1 inch, px = EMU/9525), skips `<a:ext uri>` creationId nodes and returns extents in picture order; `workbookFromFileBytes` writes them to `WorkbookImage.extWidth/extHeight`. `imageSizePx` now prefers the EMU extent over the two-cell `br` box, with 180x120 only as a last resort (the two-cell range is a bounding box and distorted the picture). Verified on `09nov-leave.xlsx`: logo 147.94x119.58px, signature 160x110.56px. New test pins in `univerData.test.ts` (EMU→px with creationId skip) and `SheetImageOverlay.test.tsx` (ext-over-anchor precedence); all touched anchors re-derived from source. ([concepts/excel-preview-univer-sheet.md](/concepts/excel-preview-univer-sheet.md))

* **Update**: Inbound content guardrail: per the owner's "Everything on loopback" decision, a guarded browser read whose top page is loopback and that names no `frame` is no longer judged, for text, find, snapshot, controls, console, network, pick, select and fill_form. The allowed-hosts exemption stays narrow (text, frame-less find, browser_run final page). console, network, pick, select and fill_form headers now carry the page URL for the guard; select's URL is read from the end-anchored tail of its result. Accepted residuals recorded. Anchors in content_guard_typesafe.go and browser_tool.go re-derived; the egress floor is stated as 0.8, matching `networkGuardMinConfidenceDefault`. ([concepts/inbound-content-guardrail.md](/concepts/inbound-content-guardrail.md))
* **Update**: Browser agent tools: the guard-relevant header URL is now carried by console, network, pick, select and fill_form, and the per-result rows state the loopback exemption (no `frame`). ([concepts/browser-agent-tools.md](/concepts/browser-agent-tools.md))
* **Update**: Excel preview page re-verified and extended. Every `file:line` anchor re-derived against current source (`univerData.ts`, `SheetImageOverlay.tsx`, `ExcelViewer.tsx`, `univerMount.ts`, save, FileTabContent, useEditorTabs and server anchors). Image size is now read from the two-cell (`br`) or one-cell (`ext`) anchor, with the 180x120 default only when neither exists; aspect limit recorded. New section for default-namespace drawings (`prefixDrawingNamespaces`, rewritten before ExcelJS loads; save still patches the original bytes). Invariants added: the Univer container must stay `isolate`, and the mount effect depends on `[book]` only (path-switch regression test). Browser verify recipe recorded. Bare relative anchors now name their file. ([concepts/excel-preview-univer-sheet.md](/concepts/excel-preview-univer-sheet.md))
* **Update**: Corrected the thumbnail panel title string to match `ExcelViewer.tsx` ("shown here; the grid overlay is view-only"); re-verified the `ExcelViewer.tsx:467-468` anchor (string on line 468). No other content changed. ([concepts/excel-preview-univer-sheet.md](/concepts/excel-preview-univer-sheet.md))
* **Update**: Excel save correctness: `valueWrite` now takes the original `<c>` type, so an edited numeric cell whose text parses back to a finite number (`numericFromDisplay`, commas stripped) stays numeric and an edited `t="b"` cell stays boolean; only edited DATE cells remain lossy (the loader discards the raw serial). The save dialog now states the date loss and TODO.md records numbers/bools fixed, dates still lossy. Anchors in `xlsxPackagePatch.ts`, `ExcelViewer.tsx`, `univerData.ts` and the tests re-derived from source. ([concepts/excel-preview-univer-sheet.md](/concepts/excel-preview-univer-sheet.md))
* **Update**: Hidden terminal render loop. ghostty-web re-arms its requestAnimationFrame loop forever and ocode keeps hidden panels mounted, so every hidden terminal painted each frame (about 95% of the desktop webview UI thread with seven terminals; desktop.log frontend-stall entries of 2.8 s to 18.8 s; the Go server was not the cause). Added pauseRenderLoop and resumeRenderLoop in ghosttyInternals.ts, wired to the panel's active state in TerminalPanel.tsx, with the resize re-pause invariant, the open scrolled-up full-repaint problem (needs a fork renderer change and a re-vendored tarball), and a native-sample diagnosis recipe. Fix passes the Terminal suite but is not yet verified in a rebuilt desktop app. ([concepts/web-terminal-ghostty-web.md](/concepts/web-terminal-ghostty-web.md))
* **Update**: Excel save date-edit gate and row-67 overlay test. A date-like edit (baseline vs live snapshot via `hasDateLikeEdit` in `ExcelViewer.tsx`) now forces the first-save dialog open even on an already-confirmed file, renders an "I understand edited dates become text" checkbox and disables Save through the new `ConfirmDialog.confirmDisabled` prop until it is checked — the edited-DATE type loss is now an explicit acknowledgement rather than a prose caveat. `SheetImageOverlay.test.tsx` adds the real `09nov-leave.xlsx` signature anchor (rows ~66.27–73) proving an off-screen image renders in-grid when scrolled to row 60, sized from its two-cell extent. All touched anchors re-derived from `ExcelViewer.tsx`, `ConfirmDialog.tsx` and both test files. ([concepts/excel-preview-univer-sheet.md](/concepts/excel-preview-univer-sheet.md))


## 2026-11-18

* **Creation**: New page for the UML session sub-tab: the Go AST index (`internal/umlinfo`), the fingerprint-keyed 4-entry cache with single-flight background builds, `GET /api/uml` (200/202/400/403) plus the `?host=` proxy rule that never falls back to a local index, the package-map and class-diagram views with LOD, `ocode.ui.umlViewState.v1` persistence keyed `host::root::sessionId`, the opaque `sub_tab`/`tabs.json` passthrough, and the fixed Monaco line-reveal race. Cross-linked the view-state convention from the PDF viewer page and the line-reveal fix from the chat-file-links gotcha; all anchors re-derived by symbol grep in the `uml-viewer` worktree. ([concepts/uml-viewer.md](/concepts/uml-viewer.md), [concepts/pdf-viewer-zoom-find.md](/concepts/pdf-viewer-zoom-find.md), [gotchas/chat-file-links-silent-noop.md](/gotchas/chat-file-links-silent-noop.md))

## 2026-11-16

* **Update**: Excel preview: floating drawings extracted via `getImages()`/`getImage()` and painted by a read-only, scroll/zoom-synced `SheetImageOverlay`; `viewport()` added to the Univer handle; Save now patches the original xlsx by baseline diff (`patchWorkbookBytes`) so drawings/media/styles/sharedStrings stay byte-identical, with a structural guard and explicit "Save without drawings" values-only fallback. All code anchors re-derived from current source; remaining limits recorded. ([concepts/excel-preview-univer-sheet.md](/concepts/excel-preview-univer-sheet.md))
* **Creation**: Per-tool elapsed time — agent-stamped wall clock in `handleToolCallWithImages`/`stampToolDurations`, UI-only `tool_duration_ms`, two-clock rule (committed vs live), web/TUI/Pulse renderers, ask-sentinel suppression ([concepts/tool-call-elapsed-time.md](/concepts/tool-call-elapsed-time.md))

* **Update**: Web UI Global Keyboard Shortcuts ([concepts/web-keyboard-shortcuts.md](/concepts/web-keyboard-shortcuts.md))
* **Update**: Markdown preview: persisted paper-width reading column ([concepts/preview-width.md](/concepts/preview-width.md))
* **Update**: Preview find: shared FindBar, DOM and PDF engines, iframe agent ([concepts/preview-find.md](/concepts/preview-find.md))
* **Update**: Added the ⌘P picker's lexically decided "Outside this project" row as a third entry point to the grant dialog, and recorded why it must be lexical (stat answers `exists:false` for both missing and outside-project paths) and that a pasted folder ends as an `ocode:open-folder` root switch ([concepts/file-editor-grants.md](/concepts/file-editor-grants.md), [concepts/file-search-shortest-path-ordering.md](/concepts/file-search-shortest-path-ordering.md))
* **Update**: Re-anchored `FilePicker.tsx` and `App.tsx` citations after the grant-row addition (FilePicker :90→:96, :109→:137, :142→:170, :150→:178, :151→:179, :227-239→:258-270, :242-245→:273-276, :291→:338, :326→:373; App.tsx +3/+4 from ~893 and ~2706), no other change ([concepts/file-tree-walk-budget.md](/concepts/file-tree-walk-budget.md), [concepts/web-keyboard-shortcuts.md](/concepts/web-keyboard-shortcuts.md), [concepts/inbound-content-guardrail.md](/concepts/inbound-content-guardrail.md))
* **Update**: Anchor refresh only (no content change): re-derived two `projectStore.tsx` citations after the ~16-line HYDRATE_CACHED_PROJECTS addition — `openProjectPath` :1179→:1210 and `activeTabId` :146→:147. The store now paints its last-known project list from the IndexedDB cache on cold start (`HYDRATE_CACHED_PROJECTS`, a no-op unless `loading` and `projects.length === 0`), and the sidebar keeps a loaded list visible during refreshes with a small header spinner ([concepts/git-worktrees.md](/concepts/git-worktrees.md), [concepts/session-bound-dialog-scoping.md](/concepts/session-bound-dialog-scoping.md))
* **Update**: Linker-signed Go .app can never hold a TCC grant ([gotchas/macos-linker-signed-app-tcc-grant.md](/gotchas/macos-linker-signed-app-tcc-grant.md))
* **Update**: Preview Print / Save as PDF ([concepts/preview-print-save-as-pdf.md](/concepts/preview-print-save-as-pdf.md))
* **Update**: Preview Print / Save as PDF ([concepts/preview-print-save-as-pdf.md](/concepts/preview-print-save-as-pdf.md))
* **Update**: Markdown preview: local images via /api/files/raw ([concepts/markdown-local-images.md](/concepts/markdown-local-images.md))
* **Update**: Markdown preview: local images via /api/files/raw ([concepts/markdown-local-images.md](/concepts/markdown-local-images.md))
* **Update**: Re-anchored bundle-macos.sh citations after the concurrent capture-then-match refactor and trust note (:136-148, :140-143, :147, :154, :156-157, :158-168, :170-178), re-derived the drifted create-dev-signing-cert.sh citations (:37-46, :62, :64-74, :19-29, :19-21, :48-58, :60-62) and added the `grep -q`/SIGPIPE-under-pipefail gotcha ([gotchas/macos-linker-signed-app-tcc-grant.md](/gotchas/macos-linker-signed-app-tcc-grant.md))
* **Update**: Recorded measured Verification section (same requirement across binaries, TCC grant surviving a rebuild in an isolated probe with the ad-hoc control) and corrected catalog anchors to catalog_fallback.go ([gotchas/macos-linker-signed-app-tcc-grant.md](/gotchas/macos-linker-signed-app-tcc-grant.md))
* **Update**: File-Edit Snapshot & Undo Mechanism ([file-edit-snapshot.md](/file-edit-snapshot.md))
* **Update**: Preview find: shared FindBar, DOM and PDF engines, iframe agent ([concepts/preview-find.md](/concepts/preview-find.md))
* **Update**: Markdown preview: local images via /api/files/raw ([concepts/markdown-local-images.md](/concepts/markdown-local-images.md))
* **Update**: File Editor Grants — Editor-Only Out-of-Project Access ([concepts/file-editor-grants.md](/concepts/file-editor-grants.md))
* **Update**: File-Edit Snapshot & Undo Mechanism ([file-edit-snapshot.md](/file-edit-snapshot.md))
* **Update**: File Editor Grants — Editor-Only Out-of-Project Access ([concepts/file-editor-grants.md](/concepts/file-editor-grants.md))
* **Update**: File Editor Grants — Editor-Only Out-of-Project Access ([concepts/file-editor-grants.md](/concepts/file-editor-grants.md))
* **Update**: Web UI Global Keyboard Shortcuts ([concepts/web-keyboard-shortcuts.md](/concepts/web-keyboard-shortcuts.md))
* **Update**: Markdown preview: persisted paper-width reading column ([concepts/preview-width.md](/concepts/preview-width.md))
* **Update**: Preview find: shared FindBar, DOM and PDF engines, iframe agent ([concepts/preview-find.md](/concepts/preview-find.md))
* **Update**: File-Edit Snapshot & Undo Mechanism ([file-edit-snapshot.md](/file-edit-snapshot.md))
* **Update**: Corrected bundle-macos.sh line anchors after the macOS-only guard added 7 lines near the top, and noted the new off-macOS refusal ([gotchas/macos-linker-signed-app-tcc-grant.md](/gotchas/macos-linker-signed-app-tcc-grant.md))
* **Update**: Linker-signed Go .app can never hold a TCC grant ([gotchas/macos-linker-signed-app-tcc-grant.md](/gotchas/macos-linker-signed-app-tcc-grant.md))
* **Update**: Inbound Content Guardrail ([concepts/inbound-content-guardrail.md](/concepts/inbound-content-guardrail.md))
* **Update**: Outbound-Network Guardrail ([concepts/webfetch-websearch-guardrails.md](/concepts/webfetch-websearch-guardrails.md))
* **Update**: Outbound-Network Guardrail ([concepts/webfetch-websearch-guardrails.md](/concepts/webfetch-websearch-guardrails.md))
* **Update**: Outbound-Network Guardrail ([concepts/webfetch-websearch-guardrails.md](/concepts/webfetch-websearch-guardrails.md))
* **Update**: Scheduled Jobs / Cron Dispatch ([scheduled-jobs.md](/scheduled-jobs.md))
* **Update**: Compaction Config: First-Token/Idle Timeouts, Operation Cap, and Timeout Classification ([concepts/compaction-config.md](/concepts/compaction-config.md))
* **Update**: Compaction Config: First-Token/Idle Timeouts, Operation Cap, and Timeout Classification ([concepts/compaction-config.md](/concepts/compaction-config.md))
* **Update**: browser_run step loop (Jev picks each control) ([concepts/browser-run-jev-step-loop.md](/concepts/browser-run-jev-step-loop.md))
* **Update**: browser_run step loop (Jev picks each control) ([concepts/browser-run-jev-step-loop.md](/concepts/browser-run-jev-step-loop.md))
* **Update**: Sandbox Permission Mode ([concepts/sandbox-permission-mode.md](/concepts/sandbox-permission-mode.md))
* **Update**: Sandbox Permission Mode ([concepts/sandbox-permission-mode.md](/concepts/sandbox-permission-mode.md))
* **Update**: MCP Apps widgets and plugin MCP servers ([concepts/mcp-apps-widgets.md](/concepts/mcp-apps-widgets.md))
* **Update**: Ask-dialog "Explain this call" digest ([concepts/ask-dialog-tool-call-digest.md](/concepts/ask-dialog-tool-call-digest.md))
* **Update**: Ask-dialog "Explain this call" digest ([concepts/ask-dialog-tool-call-digest.md](/concepts/ask-dialog-tool-call-digest.md))
* **Update**: Auto-Permission Dependency Binaries — Policy Decision ([gotchas/auto-permission-dependency-bin-policy.md](/gotchas/auto-permission-dependency-bin-policy.md))
* **Update**: Auto-Permission Dependency Binaries — Policy Decision ([gotchas/auto-permission-dependency-bin-policy.md](/gotchas/auto-permission-dependency-bin-policy.md))
* **Update**: File-Edit Snapshot & Undo Mechanism ([file-edit-snapshot.md](/file-edit-snapshot.md))
* **Update**: File-Edit Snapshot & Undo Mechanism ([file-edit-snapshot.md](/file-edit-snapshot.md))
* **Update**: Regenerated docs/index.md from doc frontmatter (OKF rules, existing index conventions): no entries added or removed; the 20 docs/superpowers/plans entries re-sorted by title within # superpowers; (deprecated) labels at end of line; Unclassified kept for frontmatter-less superpowers docs ([index.md](/index.md))
* **Update**: Outbound-Network Guardrail: security fix. The caller-named `base_url` of `imagegen`, `videogen` and `audiogen` is now an egress target (`extractEgressTargets`, shared `urlEgressTarget`); scope heading, table, test count (59) and `network_guard_typesafe(_test).go` anchors re-derived ([concepts/webfetch-websearch-guardrails.md](/concepts/webfetch-websearch-guardrails.md))
* **Update**: Generative media tools: security fix. A `base_url` with no `api_key` is refused before key lookup, so the stored key never reaches a caller-named endpoint; credentialed requests refuse redirects; key lookup corrected to `auth.ResolveKey("google")`; the `fetchBytes` redirect gap is recorded as open ([concepts/generative-media-tools.md](/concepts/generative-media-tools.md))
* **Update**: Sandbox Permission Mode: two `network_guard_typesafe.go` anchors re-derived after the 2026-11-16 guard change ([concepts/sandbox-permission-mode.md](/concepts/sandbox-permission-mode.md))

* **Update**: Browser tool waits for the page load. `open` (with a URL), `navigate`, `back`, `forward` and `reload` now wait through `Target.AwaitLoad` (DocumentID change, then Settle; 5 s bound for reload, 1 s otherwise) and report a `loadResult` line instead of "The load is not awaited"; htrcli surface gaps recorded. `navigate` with no tab opens one at its URL. Corrected the ref eviction description to per-insert LRU (`RefTable.Mint`) and removed the nonexistent `activate` action from the not-scanned list ([concepts/browser-agent-tools.md](/concepts/browser-agent-tools.md))
* **Update**: Re-derived the `controls` and `browser_tool.go` / `pagedom_controls.go` anchors shifted by the await-load change (browserReadOnlyActions 222 to 228, controls case 446 to 457, browserControlsOutput 1078 to 1124, controls 1096 to 1142, Controls 202 to 249, controlsWhereExpr 261 to 308, controlLayout 294 to 341, placeControls 327 to 374, controlsWalker.add 408 to 455, controlsFingerprint 451 to 498); no prose change ([concepts/browser-run-jev-step-loop.md](/concepts/browser-run-jev-step-loop.md))
* **Update**: Foreground bash stop result: a cancel is no longer reported as a timeout ("Command was stopped after ..."), and both stop results list the processes still alive in the command's process group (`liveGroupProcesses`, `ps -axww`; Windows lists none). Corrected the claim that a timeout promotes the command to background (it stops it). Re-derived the `exec.go` anchor to the background branch ([gotchas/foreground-bash-parent-death-protection.md](/gotchas/foreground-bash-parent-death-protection.md))

* **Update**: Foreground bash gotcha: the promotion trigger is the TUI ctrl+b key (`RequestBackgroundLatest` is the only sender of `bgRequestCh`), not the model; the description and tags now cover the stop result ([gotchas/foreground-bash-parent-death-protection.md](/gotchas/foreground-bash-parent-death-protection.md))
* **Update**: Re-derived the two `browser_tool.go` anchors in the tool Description and Definition, shifted by the htr surface interface addition (Description 251 to 257, Definition 257 to 263); no other change ([concepts/prompt-cache-stability.md](/concepts/prompt-cache-stability.md))
* **Update**: Removed the `activate` action from the tab-management list in the guardrail's not-scanned note; no such browser action exists (`browserActions`, `internal/server/browser_tool.go`) ([concepts/inbound-content-guardrail.md](/concepts/inbound-content-guardrail.md))
* **Update**: Browser load wait re-verified against the 2026-11-16 review. `AwaitLoad` returns `cdp.LoadState` (`LoadPending`, `LoadSettled`, `LoadNoNewDocument`, `LoadUnconfirmed`) instead of a bool. An unchanged document id is `LoadNoNewDocument` for every action, reload included, and the old page is never settled. The htrcli surface reports `LoadUnconfirmed` on readyState complete, so its earlier "can report finished on the old document" gap is closed. Documented the five `loadResult` texts, the shared `loadAction` pre-read (2 s bound: a failed read still runs the navigation and reports the state as unreadable; a done action context returns the error with nothing sent), the `navigate` no-tab fallback (lists the client's tabs first, opens a tab only when there are none), and removed the seconds promise from the tool contract ([concepts/browser-agent-tools.md](/concepts/browser-agent-tools.md))
* **Update**: Foreground bash stop branch re-verified: the order is freeze (SIGSTOP), list (`ps -axww -o pid=,ppid=,pgid=,etime=,args=`, 1 s), then kill. The list filter is by parentage, and the poll sleeps are respelled `sleep 0.50` / `sleep 0.20` so a user's `sleep 0.5` stays listed. `cmd.Cancel` is a no-op on the registry path, so the stop branch owns the whole-group kill. Recorded the busybox `ps` limit and the unverified bwrap case. Anchor `exec.go:170` to `:171` ([gotchas/foreground-bash-parent-death-protection.md](/gotchas/foreground-bash-parent-death-protection.md))
* **Update**: Re-derived the `browser_tool.go`, `pagedom_controls.go` and `permissions.go` anchors on the browser_run step-loop page after the await-load and stop-branch changes (controls case 457 to 472, browserControlsOutput 1124 to 1140, controls 1142 to 1158, browserReadOnlyActions 228 to 232, browserActions 58 to 62, Controls 249 to 273, controlsWhereExpr 308 to 332, controlLayout 341 to 365, placeControls 374 to 398, controlsWalker.add 455 to 479, controlsFingerprint 498 to 522; the observe set 6652 to 6678 was pre-existing drift). The agent.go and browser_run.go anchors there were not re-verified ([concepts/browser-run-jev-step-loop.md](/concepts/browser-run-jev-step-loop.md))
* **Update**: Re-derived the two `browser_tool.go` anchors in the `browser` tool Definition and Description (Definition 263 to 267, Description 257 to 260) ([concepts/prompt-cache-stability.md](/concepts/prompt-cache-stability.md))
* **Update**: Re-derived the foreground `exec.go` anchor after the stop-branch change (`exec.go:222` to `:228`, the `buildBashCmd` call) ([gotchas/sandbox-git-push-ssh-agent-tty.md](/gotchas/sandbox-git-push-ssh-agent-tty.md))
* **Update**: Browser load result for the htrcli surface: `DocumentID` is now the page URL, so `AwaitLoad` settles a navigate, back or forward once the URL changes (5 s bound) and reports only reload and same-URL navigations as unconfirmed (1 s same-URL wait). Every load result now ends with a location sentence from `tabLocation` that carries only the scheme and host of the current URL, never its path, query or fragment; the invariant is on the browser tool page and pinned by `TestBrowserTool_NavigateReportsTheTabLocation` ([concepts/browser-agent-tools.md](/concepts/browser-agent-tools.md))
* **Update**: Guardrail coverage: `open`, `navigate`, `back`, `forward` and `reload` results stay outside `browserGuardedOutputs`; their only page-derived text is the scheme-and-host location sentence, and the path stays guarded through `action=tabs` ([concepts/inbound-content-guardrail.md](/concepts/inbound-content-guardrail.md))
* **Update**: Re-derived the `browser_tool.go` anchors shifted by the location-sentence change: `browserControlsOutput` 1140 to 1208 (line 287 and the code map), `controls` 1158 to 1226; the `controls` case (472), `browserActions` (62) and `browserReadOnlyActions` (232) did not move ([concepts/browser-run-jev-step-loop.md](/concepts/browser-run-jev-step-loop.md))
* **Update**: Re-derived the `browser_tool.go` anchors in the browser_run design spec: `browserActions` 57 to 62, `browserSurface` 74 to 79, `browserArgs` 152 to 171, `ExecuteCtx` 332 to 361, `perform` 405 to 464, `PageContext` 559 to 621 ([superpowers/specs/2026-11-01-browser-run-jev-step-loop-design.md](/superpowers/specs/2026-11-01-browser-run-jev-step-loop-design.md))

* **Creation**: Mobile companion design draft (NOT implemented, pending user approval): a second loopback companion listener (`127.0.0.1:4097`) in the same ocode process sharing session state, with the main listener, desktop SPA, full API and launch token never tunneled; mobile-first entry at the companion origin root; pairing code to an HttpOnly `__Host-ocode_companion` cookie (no login ticket, no token in a URL or localStorage); local-only deny-by-default scoped API/SSE that rejects `host`/`hosts`, `pulse_` and `yolo` sessions and filters by device project scope; low-risk-only approvals in 1a, desktop-only high risk, WebAuthn step-up in 1b; durable ask journal with restart rehydration; Web Push/VAPID instead of FCM; TWA/Play conditional on a single hosted origin. Normative spec is `docs/superpowers/specs/2026-11-16-ocode-mobile-companion-design.md` (commit f3b3f3c5, spec only) ([concepts/mobile-companion.md](/concepts/mobile-companion.md))

* **Update**: Browser tool `frames` and `then_text`. `frames` (1 to 4 offsets, 0 to 5000 ms) on `click`, `key`, `type`, `fill` and `screenshot` returns one view-only contact sheet (`browser_frames.go`); `then_text` on `click`, `key`, `type` and `fill` waits for the text, then takes a normal screenshot. Added the Timed views section, the `checkViews` refusals, the sheet rule (`b.sheet`, never `b.shot`), the limits, the code map and the tests; the coordinate and guard rules are unchanged ([concepts/browser-agent-tools.md](/concepts/browser-agent-tools.md))
* **Update**: Browser tool schema record: the wait-for-load and navigate sentences (commit `a2633873`) and the `frames`/`then_text` properties and the `timeout` description change are recorded as one-time re-caches. Re-derived the `browser_tool.go` anchors in the record: `Description()` 260 to 266, the neutral `description` property 267 to 273 ([concepts/prompt-cache-stability.md](/concepts/prompt-cache-stability.md))
* **Update**: Re-derived the `browser_tool.go` anchors shifted by the frames change: `browserReadOnlyActions` 232 to 238, the `controls` case 472 to 491, `browserControlsOutput` 1208 to 1308, `controls` 1226 to 1326 ([concepts/browser-run-jev-step-loop.md](/concepts/browser-run-jev-step-loop.md))
* **Update**: Re-derived the `browser_tool.go` anchors in the browser_run design spec: `ExecuteCtx` 361 to 376, `perform` 464 to 483, `PageContext` 621 to 715; `browserActions` (62), `browserSurface` (79) and `browserArgs` (171) unchanged. The section heading now records the 2026-11-16 re-derivation ([superpowers/specs/2026-11-01-browser-run-jev-step-loop-design.md](/superpowers/specs/2026-11-01-browser-run-jev-step-loop-design.md))

## 2026-11-15

* **Update**: Markdown preview: local images via /api/files/raw — raw HTML is parsed and sanitized (rehype-raw + rehype-sanitize), local fetches are lazy behind a 400px IntersectionObserver, percent-encoded names are decoded, and protocol-relative sources stay external; MarkdownViewer line anchors re-derived ([concepts/markdown-local-images.md](/concepts/markdown-local-images.md))
* **Update**: Preview Print / Save as PDF — markdown print gets neither the viewer's components override nor its rehype plugins, so raw HTML prints as escaped text; MarkdownViewer line anchors re-derived ([concepts/preview-print-save-as-pdf.md](/concepts/preview-print-save-as-pdf.md))
* **Update**: A dead editor grant no longer dead-ends a reveal ([concepts/file-editor-grants.md](/concepts/file-editor-grants.md))
* **Update**: Files-tab "Show in file tree" hands an out-of-tree path to the OS file manager; absolute paths unanchored, root-relative with their root ([gotchas/files-tab-os-native-reveal.md](/gotchas/files-tab-os-native-reveal.md))
* **Creation**: Markdown preview: local images via /api/files/raw ([concepts/markdown-local-images.md](/concepts/markdown-local-images.md))
* **Update**: Preview Print / Save as PDF: the viewer now resolves local markdown images; print still does not ([concepts/preview-print-save-as-pdf.md](/concepts/preview-print-save-as-pdf.md))
* **Update**: Preview find: shared FindBar, DOM and PDF engines, iframe agent — MarkdownViewer line anchors re-derived ([concepts/preview-find.md](/concepts/preview-find.md))
* **Update**: Markdown preview: persisted paper-width reading column — MarkdownViewer line anchors re-derived ([concepts/preview-width.md](/concepts/preview-width.md))
* **Update**: Web UI Global Keyboard Shortcuts — MarkdownViewer find-listener anchor re-derived ([concepts/web-keyboard-shortcuts.md](/concepts/web-keyboard-shortcuts.md))
* **Update**: Streamable HTTP MCP (type "http") and the ElevenLabs preset ([concepts/mcp-streamable-http-elevenlabs.md](/concepts/mcp-streamable-http-elevenlabs.md))
* **Update**: Streamable HTTP MCP (type "http") and the ElevenLabs preset ([concepts/mcp-streamable-http-elevenlabs.md](/concepts/mcp-streamable-http-elevenlabs.md))
* **Update**: Session-scoped Files-tab unlock (design) ([superpowers/specs/2026-11-15-session-scoped-files-tab-unlock-design.md](/superpowers/specs/2026-11-15-session-scoped-files-tab-unlock-design.md))
* **Creation**: Session-scoped Files-tab unlock (design) ([superpowers/specs/2026-11-15-session-scoped-files-tab-unlock-design.md](/superpowers/specs/2026-11-15-session-scoped-files-tab-unlock-design.md))
* **Update**: Streamable HTTP MCP (type "http") and the ElevenLabs preset ([concepts/mcp-streamable-http-elevenlabs.md](/concepts/mcp-streamable-http-elevenlabs.md))
* **Update**: Streamable HTTP MCP (type "http") and the ElevenLabs preset ([concepts/mcp-streamable-http-elevenlabs.md](/concepts/mcp-streamable-http-elevenlabs.md))
* **Update**: Generative media tools (imagegen, videogen, audiogen) ([concepts/generative-media-tools.md](/concepts/generative-media-tools.md))
* **Update**: Remote MCP OAuth Compatibility ([concepts/remote-mcp-oauth-compat.md](/concepts/remote-mcp-oauth-compat.md))
* **Creation**: Streamable HTTP MCP (type "http") and the ElevenLabs preset ([concepts/mcp-streamable-http-elevenlabs.md](/concepts/mcp-streamable-http-elevenlabs.md))
* **Creation**: Generative media tools (imagegen, videogen, audiogen) ([concepts/generative-media-tools.md](/concepts/generative-media-tools.md))
* **Update**: Speech summary is fail-open: skip while the turn holds as.mu, cancellable summariser, unlocked config write ([gotchas/speech-summary-turn-lock-wait.md](/gotchas/speech-summary-turn-lock-wait.md))
* **Update**: Advisor gate judge (advisor_gate) ([concepts/advisor-gate-judge.md](/concepts/advisor-gate-judge.md))
* **Update**: SKILL.md Frontmatter Fields ([concepts/skill-frontmatter.md](/concepts/skill-frontmatter.md))
* **Update**: Advisor checkpoints must wait for a pending permission/question ask ([gotchas/advisor-checkpoints-must-wait-for-a-pending-ask.md](/gotchas/advisor-checkpoints-must-wait-for-a-pending-ask.md))
* **Update**: Pending ask suppresses advisor, auto-continue, and goal-resume ([concepts/pending-ask-suppresses-advisor-auto-continue-goal.md](/concepts/pending-ask-suppresses-advisor-auto-continue-goal.md))
* **Update**: Inbound Content Guardrail ([concepts/inbound-content-guardrail.md](/concepts/inbound-content-guardrail.md))
* **Update**: Auto-Permission Enforced Categories ([concepts/auto-permission-enforced-categories.md](/concepts/auto-permission-enforced-categories.md))
* **Update**: Ask-dialog "Explain this call" digest ([concepts/ask-dialog-tool-call-digest.md](/concepts/ask-dialog-tool-call-digest.md))
* **Update**: MCP Apps widgets and plugin MCP servers ([concepts/mcp-apps-widgets.md](/concepts/mcp-apps-widgets.md))
* **Update**: Scheduled Jobs / Cron Dispatch ([scheduled-jobs.md](/scheduled-jobs.md))
* **Update**: browser_run step loop (Jev picks each control) ([concepts/browser-run-jev-step-loop.md](/concepts/browser-run-jev-step-loop.md))
* **Update**: Outbound-Network Guardrail ([concepts/webfetch-websearch-guardrails.md](/concepts/webfetch-websearch-guardrails.md))
* **Creation**: Session model switch during a paused ask ([concepts/session-model-switch-during-paused-ask.md](/concepts/session-model-switch-during-paused-ask.md))
* **Update**: Per-Chat MCP Toggle ([concepts/per-chat-mcp-toggle.md](/concepts/per-chat-mcp-toggle.md))
* **Update**: Auto-Permission Dependency Binaries — Policy Decision ([gotchas/auto-permission-dependency-bin-policy.md](/gotchas/auto-permission-dependency-bin-policy.md))
* **Update**: Auto-permission enforced categories: file changes are ordinary in the Jev rubric (restore/checkout/reset stay denied), the `cd` fold accepts quoted newlines, and a folded `cd` moves the working directory in `project_context` ([concepts/auto-permission-enforced-categories.md](/concepts/auto-permission-enforced-categories.md))
* **Update**: Messaging connectors — WhatsApp/Telegram/Messenger accounts, storage, tools, webhook and sync ([concepts/messaging-connectors.md](/concepts/messaging-connectors.md))
* **Creation**: Advisor gate judge (advisor_gate) ([concepts/advisor-gate-judge.md](/concepts/advisor-gate-judge.md))
* **Update**: SKILL.md Frontmatter Fields: plan checkpoint removed, done checkpoint and before-commit gate stand down, code anchors re-derived ([concepts/skill-frontmatter.md](/concepts/skill-frontmatter.md))
* **Update**: Pending ask suppresses advisor checkpoints: plan checkpoint removed, done checkpoint and before-commit gate both stand down ([concepts/pending-ask-suppresses-advisor-auto-continue-goal.md](/concepts/pending-ask-suppresses-advisor-auto-continue-goal.md))
* **Update**: Advisor checkpoints must wait for a pending ask: plan checkpoint removed, done checkpoint and before-commit gate both stand down ([gotchas/advisor-checkpoints-must-wait-for-a-pending-ask.md](/gotchas/advisor-checkpoints-must-wait-for-a-pending-ask.md))
* **Update**: Permissions allowed-hosts list takes URL prefixes (`example.com/docs`, `*.example.com/api/v1`); a webfetch or browser URL it covers skips the permission prompt, the outbound-network guard and the inbound content guard, while bash and websearch requests are still judged. Reverses the 2026-11-08 choice that the judge still sees allowed hosts ([concepts/permissions-allowed-hosts.md](/concepts/permissions-allowed-hosts.md), [concepts/webfetch-websearch-guardrails.md](/concepts/webfetch-websearch-guardrails.md), [concepts/inbound-content-guardrail.md](/concepts/inbound-content-guardrail.md), [concepts/browser-agent-tools.md](/concepts/browser-agent-tools.md), [gotchas/network-guard-rubric-hesitates-on-unfamiliar-hosts.md](/gotchas/network-guard-rubric-hesitates-on-unfamiliar-hosts.md))

## 2026-11-14

* **Update**: Outbound-Network Guardrail ([concepts/webfetch-websearch-guardrails.md](/concepts/webfetch-websearch-guardrails.md))
* **Update**: Per-Chat MCP Toggle ([concepts/per-chat-mcp-toggle.md](/concepts/per-chat-mcp-toggle.md))
## 2026-11-12

* **Creation**: MCP Apps widgets and plugin MCP servers ([concepts/mcp-apps-widgets.md](/concepts/mcp-apps-widgets.md))
* **Update**: Plugin MCP servers: web-server merge scope, project-local toggle answer and TUI/web refusal scope corrected ([plugins.md](/plugins.md))

## 2026-11-11

* **Update**: Code-Search Relevance Judge (v1) ([superpowers/specs/2026-09-28-code-search-relevance-judge-design.md](/superpowers/specs/2026-09-28-code-search-relevance-judge-design.md))
* **Update**: Code-Search Relevance Judge ([concepts/code-search-relevance-judge.md](/concepts/code-search-relevance-judge.md))
* **Update**: Messaging connectors — WhatsApp/Telegram accounts, storage, tools, HTTP surface and sync ([concepts/messaging-connectors.md](/concepts/messaging-connectors.md))
* **Update**: Messaging connectors — WhatsApp/Telegram accounts, storage, tools and HTTP surface ([concepts/messaging-connectors.md](/concepts/messaging-connectors.md))
* **Creation**: Messaging connectors — WhatsApp/Telegram accounts, storage, tools and HTTP surface ([concepts/messaging-connectors.md](/concepts/messaging-connectors.md))
* **Update**: SQLite Browser / DB IDE (preview pane) ([concepts/sqlite-browser.md](/concepts/sqlite-browser.md))
* **Update**: Pulse assistant — overview chat that reads and, with approval, acts on sessions (server side) ([concepts/pulse-assistant.md](/concepts/pulse-assistant.md))
* **Update**: Inter-session communication — shared tools and HTTP surface for agents to inspect and drive other sessions ([concepts/inter-session-communication.md](/concepts/inter-session-communication.md))
## 2026-11-10

* **Update**: DB Connector: per-project config (.ocode/db.json) + SSH tunnel ([concepts/db-connector-ssh-tunnel.md](/concepts/db-connector-ssh-tunnel.md))
* **Update**: Prompt Cache Stability ([concepts/prompt-cache-stability.md](/concepts/prompt-cache-stability.md))
* **Update**: Browser agent tool — agents see and drive the browser tabs of their session's machine ([concepts/browser-agent-tools.md](/concepts/browser-agent-tools.md))
* **Creation**: DB Connector: per-project config (.ocode/db.json) + SSH tunnel ([concepts/db-connector-ssh-tunnel.md](/concepts/db-connector-ssh-tunnel.md))
* **Update**: File search result ordering: shortest path first ([concepts/file-search-shortest-path-ordering.md](/concepts/file-search-shortest-path-ordering.md))
* **Update**: File tree walk: node budget, symlink budget, and truncation ([concepts/file-tree-walk-budget.md](/concepts/file-tree-walk-budget.md))
* **Creation**: Remote (SSH/WSL) Files-tab content search: one grep invocation ([concepts/remote-files-content-search.md](/concepts/remote-files-content-search.md))
* **Update**: Chat file links with relative or ~ hrefs silently did nothing ([gotchas/chat-file-links-silent-noop.md](/gotchas/chat-file-links-silent-noop.md))
* **Update**: File Editor Grants — Editor-Only Out-of-Project Access ([concepts/file-editor-grants.md](/concepts/file-editor-grants.md))
## 2026-11-09

* **Update**: Browser agent tool — agents see and drive the Chrome tabs of their session's machine ([concepts/browser-agent-tools.md](/concepts/browser-agent-tools.md))
* **Update**: Inbound Content Guardrail ([concepts/inbound-content-guardrail.md](/concepts/inbound-content-guardrail.md))
* **Update**: Compaction Config: First-Token/Idle Timeouts, Operation Cap, and Timeout Classification ([concepts/compaction-config.md](/concepts/compaction-config.md))
* **Update**: Local Speech-to-Text (In-App Dictation) — Design ([superpowers/specs/2026-09-21-speech-to-text-design.md](/superpowers/specs/2026-09-21-speech-to-text-design.md))
* **Update**: Auto-share-on-start (tailnet-only boot exposure) ([concepts/auto-share-on-start.md](/concepts/auto-share-on-start.md))
* **Update**: Publish / Share Project as a Browser Artifact — Design ([superpowers/specs/2026-11-07-publish-share-project-design.md](/superpowers/specs/2026-11-07-publish-share-project-design.md))
* **Update**: Persistent per-session shell for `!` commands — Implementation Plan ([superpowers/plans/2026-09-21-persistent-shell-session.md](/superpowers/plans/2026-09-21-persistent-shell-session.md))
* **Update**: Part 06 — RC-Bridge First-Class + Legacy Cleanup ([superpowers/plans/2026-08-12-multiproject-event-architecture/06-rc-first-class-cleanup.md](/superpowers/plans/2026-08-12-multiproject-event-architecture/06-rc-first-class-cleanup.md))
* **Update**: Deferred, durable message rewind for ocode Web/Desktop ([superpowers/specs/2026-09-25-deferred-session-rewind-design.md](/superpowers/specs/2026-09-25-deferred-session-rewind-design.md))
* **Update**: Goal Condition Loop (/goal) ([concepts/goal-condition-loop.md](/concepts/goal-condition-loop.md))
* **Update**: Excel preview and Save: Univer sheet over ExcelJS and SheetJS ([concepts/excel-preview-univer-sheet.md](/concepts/excel-preview-univer-sheet.md))
* **Update**: Web/Desktop Server Locking and Liveness Rules ([concepts/web-server-locking-and-liveness-rules.md](/concepts/web-server-locking-and-liveness-rules.md))
* **Update**: Embedded HTR Extension and Managed Daemon Design ([superpowers/specs/2026-09-09-embedded-htr-extension-design.md](/superpowers/specs/2026-09-09-embedded-htr-extension-design.md))
* **Update**: Port forwards Disable/Enable: URL composed past query, supervisor retained-terminal collision, and dead-forward liveness/restart monitor ([gotchas/port-forwards-url-composition-and-supervisor-restart.md](/gotchas/port-forwards-url-composition-and-supervisor-restart.md))
* **Update**: Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing + Local Media Streaming) ([gotchas/files-tab-preview-only-routing.md](/gotchas/files-tab-preview-only-routing.md))
* **Update**: Office Add-in Path Presets — Save-Time OS Access Request ([concepts/office-addin-path-presets.md](/concepts/office-addin-path-presets.md))
* **Update**: "Add to .gitignore" — toplevel targeting, C-quoting, and append-not-truncate ([gotchas/git-ignore-toplevel-and-quoting.md](/gotchas/git-ignore-toplevel-and-quoting.md))
* **Update**: Compaction Cancellation: User-Initiated Cancel of an In-Flight Pass ([concepts/compaction-cancellation.md](/concepts/compaction-cancellation.md))
* **Update**: Git worktrees: list, open, create, remove (web Git tab) ([concepts/git-worktrees.md](/concepts/git-worktrees.md))
* **Update**: Ask-dialog "Explain this call" digest ([concepts/ask-dialog-tool-call-digest.md](/concepts/ask-dialog-tool-call-digest.md))
* **Update**: Concurrent session writers — conflict semantics and recovery ([gotchas/session-writers-conflict-recovery.md](/gotchas/session-writers-conflict-recovery.md))
* **Update**: Artifact publishing / sharing (static snapshots, Mode A + Mode B client) ([concepts/artifact-publishing.md](/concepts/artifact-publishing.md))
* **Update**: Web Git history graph ([concepts/web-git-history-graph.md](/concepts/web-git-history-graph.md))
* **Update**: Web/Desktop Terminal on ghostty-web (libghostty-vt) ([concepts/web-terminal-ghostty-web.md](/concepts/web-terminal-ghostty-web.md))
* **Update**: browser `tabs` is no longer judged by the inbound content guard. The host's strip listing has no wrapper line, so the judge flagged the empty-strip line as tool_coercion and stopped browser tasks before their first read. Page text behind a tab is still scanned; a page-authored tab title now reaches the model unscanned ([concepts/inbound-content-guardrail.md](/concepts/inbound-content-guardrail.md), [concepts/browser-agent-tools.md](/concepts/browser-agent-tools.md))
* **Update**: browser guard set narrowed to six actions (`tabs`, `pick`, `select` and `fill_form` are no longer judged); guarded browser headers carry the page URL (` at <url>`, written only when the reads before and after agree); local-network pages are not judged for `text`, `snapshot` and `find` without a frame, or `controls`; `network` is judged in full; `browser_run` is judged as a labels view. Console per-entry origin rule: the guard drops a console entry only when its recorded origin is local-network; the CDP origin capture and the server `(origin …)` writer are in the working tree, uncommitted. Security review dispositions recorded ([concepts/inbound-content-guardrail.md](/concepts/inbound-content-guardrail.md))
* **Update**: browser guard scan table and header examples: `pick`, `select` and `fill_form` rows removed, the page-URL header and local-network rules added ([concepts/browser-agent-tools.md](/concepts/browser-agent-tools.md))
* **Update**: `browser_run` judged view and final-page exemption, and the browser result anchors re-derived ([concepts/browser-run-jev-step-loop.md](/concepts/browser-run-jev-step-loop.md))

## 2026-11-08
* **Creation**: SKILL.md Frontmatter Fields ([concepts/skill-frontmatter.md](/concepts/skill-frontmatter.md))
* **Update**: In-app Word (.docx) and PDF editors ([concepts/in-app-docx-pdf-editors.md](/concepts/in-app-docx-pdf-editors.md))
* **Update**: Artifact publishing / sharing (static snapshots, Mode A + Mode B client) ([concepts/artifact-publishing.md](/concepts/artifact-publishing.md))
* **Update**: Word save verified end-to-end through the real `/api/files/raw` handler on a no-auth scratch server; URL-fetched image embedded (`word/media/opencode-image-1.png`) and fetch-back re-imported intact; inline images still do not paint in the OSS Univer 1.1.0 editor ([docs/concepts/in-app-docx-pdf-editors.md](/concepts/in-app-docx-pdf-editors.md))
* **Update**: Word images now embed from http(s)/blob URLs (bounded fetch, magic-byte check) and resolvable UUIDs in addition to base64, with per-image failure reporting; live probe shows native tables render but inline images are not painted by OSS Univer 1.1.0 ([docs/concepts/in-app-docx-pdf-editors.md](/concepts/in-app-docx-pdf-editors.md))
* **Creation**: Permissions allowed-hosts list (`permissions.allowed_hosts`): config and replace-not-merge load, normalisation and wildcard matching, the permissions-hosts endpoints, the Settings UI, the Allow it grants in `Decide` for webfetch and browser navigate or open with its precedence, and what the outbound guard still judges ([concepts/permissions-allowed-hosts.md](/concepts/permissions-allowed-hosts.md))
* **Creation**: Jev guard hesitates on unfamiliar hosts: the rubric tied allow to host reputation, and a non-empty allowed list read as look-alikes; measured confidences and the rubric rules that replaced it ([gotchas/network-guard-rubric-hesitates-on-unfamiliar-hosts.md](/gotchas/network-guard-rubric-hesitates-on-unfamiliar-hosts.md))
* **Update**: Outbound-Network Guardrail: four tools (browser navigate and open added), the allow or escalate rubric, the 7-entry concern catalog with `destructive_action`, the `network_guard` judge slot and prompt switch, the secrets opt-out honoured by the guard, the `allowed_hosts` state, and the clip-before-mask gap found on the extraction path ([concepts/webfetch-websearch-guardrails.md](/concepts/webfetch-websearch-guardrails.md))
* **Update**: Auto-permission enforced categories: the outbound-network guard is the fourth consumer of the `secrets` opt-out; anchors re-derived ([concepts/auto-permission-enforced-categories.md](/concepts/auto-permission-enforced-categories.md))
* **Update**: Browser agent tool: `navigate` and `open` to a host on `permissions.allowed_hosts` is an allow in `Decide`; the egress guard judges a non-loopback target rather than escalating it outright ([concepts/browser-agent-tools.md](/concepts/browser-agent-tools.md))
* **Update**: Pasted images are now written into the saved package (word/media part + relationship + content-type default) instead of blocking the save; only base64 sources are supported ([docs/concepts/in-app-docx-pdf-editors.md](/concepts/in-app-docx-pdf-editors.md))
* **Update**: Inline Word images are documented as native Univer drawings (custom-block sentinel + drawings/drawingsOrder, base64 source), with resize persisted via `wp:extent` and pasted images refusing the save; anchored/VML/mixed image paragraphs remain preserved raw blocks ([docs/concepts/in-app-docx-pdf-editors.md](/concepts/in-app-docx-pdf-editors.md))

* **Update**: Artifact publishing: a hub publish uploads before the local commit and undoes a first publish on a later failure; hub unpublish and delete run first; the hub-token list read is batched ([concepts/artifact-publishing.md](/concepts/artifact-publishing.md))
* **Creation**: Hub 404 means "gone" only with code not_found ([gotchas/artifact-hub-404-needs-not-found-code.md](/gotchas/artifact-hub-404-needs-not-found-code.md))
* **Update**: In-app Word editor: paragraph-level section breaks preserved, body sectPr taken as the last child, terminator anchoring convention, table cells with pictures kept verbatim, image meta reused after save; code anchors re-derived ([concepts/in-app-docx-pdf-editors.md](/concepts/in-app-docx-pdf-editors.md))
* **Update**: Web server project scoping: a terminal's relative cwd is joined onto the project root, never resolved through os.Getwd ([concepts/web-server-project-scoping.md](/concepts/web-server-project-scoping.md))
* **Update**: Sandbox permission mode: the self-escalation guard asks for a loopback request to any `/api/config/*` path, matches the cleaned, lowercased raw path, asks outright on any `%`, and checks every command on a compound line; anchors re-derived ([concepts/sandbox-permission-mode.md](/concepts/sandbox-permission-mode.md))
* **Update**: Web fetch and websearch guardrails: the `secrets` waiver is blocked deterministically on a truncated value, the target cap, a bash upload or expansion, or any internal host on the line; anchors re-derived ([concepts/webfetch-websearch-guardrails.md](/concepts/webfetch-websearch-guardrails.md))
* **Update**: Scheduled jobs, browser_run step loop and dependency-bin policy: `permissions.go` and `network_guard_typesafe.go` anchors re-derived after both files moved ([scheduled-jobs.md](/scheduled-jobs.md), [concepts/browser-run-jev-step-loop.md](/concepts/browser-run-jev-step-loop.md), [gotchas/auto-permission-dependency-bin-policy.md](/gotchas/auto-permission-dependency-bin-policy.md))
* **Update**: Sandbox permission mode: the escalation rule now also asks when a network client has a whole-word shell expansion as a positional argument, even beside a literal URL, and when a loopback URL has glob or expansion characters in its path, host or userinfo (a port expansion is exempt only if the shell proves it numeric); anchors into `permissions.go` re-derived ([concepts/sandbox-permission-mode.md](/concepts/sandbox-permission-mode.md))
* **Update**: Web fetch and websearch guardrails: the `secrets` waiver is blocked outright when a bash target carries a destination-shaping option in any spelling, or a host token holds a glob or expansion character; anchors into `permissions.go` and `network_guard_typesafe.go` re-derived ([concepts/webfetch-websearch-guardrails.md](/concepts/webfetch-websearch-guardrails.md))
* **Update**: Scheduled jobs, browser_run step loop and dependency-bin policy: `permissions.go` anchors re-derived after the option tables and expansion helpers moved the file again ([scheduled-jobs.md](/scheduled-jobs.md), [concepts/browser-run-jev-step-loop.md](/concepts/browser-run-jev-step-loop.md), [gotchas/auto-permission-dependency-bin-policy.md](/gotchas/auto-permission-dependency-bin-policy.md))
* **Update**: Sandbox permission mode: option arity is now per client (curl, wget and httpie tables), and an unknown client never skips a following expansion; `permissions.go` anchors on the sandbox, webfetch, scheduled-jobs, dependency-bin and browser_run pages re-derived after the httpie table moved the file ([concepts/sandbox-permission-mode.md](/concepts/sandbox-permission-mode.md))
## 2026-11-07

* **Update**: In-app Word (.docx) and PDF editors ([concepts/in-app-docx-pdf-editors.md](/concepts/in-app-docx-pdf-editors.md))
* **Update**: Artifact publishing / sharing (static snapshots, Mode A) ([concepts/artifact-publishing.md](/concepts/artifact-publishing.md))
* **Creation**: Artifact publishing / sharing (static snapshots, Mode A) ([concepts/artifact-publishing.md](/concepts/artifact-publishing.md))
* **Creation**: Artifact publishing / sharing (static snapshots, Mode A) ([docs/concepts/artifact-publishing.md](/docs/concepts/artifact-publishing.md))
* **Creation**: In-app Word (.docx) and PDF editors ([concepts/in-app-docx-pdf-editors.md](/concepts/in-app-docx-pdf-editors.md))
* **Creation**: Alien Discovery Skill — Coherent-but-Unavailable Combinations (skills/alien-discovery: Jev pair/coherence stats, IPv6 TypeSafe stall gotcha, 2026-11-07 holdout result) ([concepts/alien-discovery-skill.md](/concepts/alien-discovery-skill.md))
* **Update**: Reader Mode and Viewer Context: the preview record is pushed to the server, read mid-turn by the new `viewer_state` tool, and driven by the new `viewer_control` tool; support matrix per viewer ([concepts/reader-mode-viewer-context.md](/concepts/reader-mode-viewer-context.md))
* **Creation**: useViewerControl seeding, live buffer and one-directive-per-tick rules ([gotchas/viewer-control-directive-seeding-live-buffer.md](/gotchas/viewer-control-directive-seeding-live-buffer.md))
* **Update**: Auto-continue question judge: structured question asks are answered from the transcript only when it determines the answer; `answer_question` chain key; Action Set and Design Constraints updated ([concepts/server-auto-continue.md](/concepts/server-auto-continue.md))
* **Update**: Auto-permission default floor corrected from 0.85 to 0.80 to match `autoJudgeMinConfidenceDefault` ([concepts/auto-permission-enforced-categories.md](/concepts/auto-permission-enforced-categories.md))
* **Update**: Pending-ask suppression now documents the auto-continue question judge exception: the judge may answer a structured question ask before the suppression applies, and defers leave it pending ([concepts/pending-ask-suppresses-advisor-auto-continue-goal.md](/concepts/pending-ask-suppresses-advisor-auto-continue-goal.md))
* **Update**: Auto-continue question judge: choice keys are `opt0`, `opt1`, … with the label mapped back by `questionChoiceLabel` (previously raw label text, so a backend could reject every prompt); `questionAlreadyAsked` only reads answers to `question` tool calls; `tool.QuestionOptionIsCustom` is the one custom-option definition; runTurn broadcasts `messages` after an auto-answer; the answered-path failure branches are log-only; anchors re-derived ([concepts/server-auto-continue.md](/concepts/server-auto-continue.md))
* **Update**: Web/desktop locking: `findPendingSession` waits a bounded 3 s on `TryLock` and returns busy, so question answer, question dismiss and permission resolve answer 503 + Retry-After instead of 404/409 while the question judge holds `as.mu` ([concepts/web-server-locking-and-liveness-rules.md](/concepts/web-server-locking-and-liveness-rules.md))
* **Update**: Pending-ask answer lock wait is bounded (3 s, then 503), and the child-permission gotcha no longer calls the wait blocking ([concepts/question-dismiss-reopen-answer.md](/concepts/question-dismiss-reopen-answer.md), [gotchas/pending-ask-recovery-live-session-state.md](/gotchas/pending-ask-recovery-live-session-state.md))
* **Update**: useViewerControl anchors by server index (`windowStartServerIndex` plus local index) instead of Message identity, so a scroll-back prepend after a turn-end commit no longer re-applies old directives; section 3 rewritten, including the adoptTranscript rule for an unknown window position ([gotchas/viewer-control-directive-seeding-live-buffer.md](/gotchas/viewer-control-directive-seeding-live-buffer.md))
* **Update**: Viewer controller registry is a per-key stack (top active, unregister restores the one beneath); `useViewerControllerWhen` is gone; Monaco stacks on top only while Edit as text is on ([concepts/reader-mode-viewer-context.md](/concepts/reader-mode-viewer-context.md))
* **Update**: Excel viewer: `activateRange` now validates the range before switching sheets (`invalid range`), `watchUserEdits`, save and raw-save anchors re-derived, and the raw-save extension list now includes `.docx` and `.pdf` ([concepts/excel-preview-univer-sheet.md](/concepts/excel-preview-univer-sheet.md))

* **Creation**: Inter-session communication — shared tools and HTTP surface for agents to inspect and drive other sessions ([concepts/inter-session-communication.md](/concepts/inter-session-communication.md))
* **Update**: Pulse assistant — overview chat that reads and, with approval, acts on sessions (server side) ([concepts/pulse-assistant.md](/concepts/pulse-assistant.md))

## 2026-11-06

* **Update**: Office Add-in Path Presets — Save-Time OS Access Request ([concepts/office-addin-path-presets.md](/concepts/office-addin-path-presets.md))
* **Update**: File Editor Grants — Editor-Only Out-of-Project Access ([concepts/file-editor-grants.md](/concepts/file-editor-grants.md))
* **Creation**: Office Add-in Path Presets — Save-Time OS Access Request ([concepts/office-addin-path-presets.md](/concepts/office-addin-path-presets.md))
* **Update**: Linker-signed Go .app can never hold a TCC grant ([gotchas/macos-linker-signed-app-tcc-grant.md](/gotchas/macos-linker-signed-app-tcc-grant.md))
* **Update**: Computer use ([computer-use.md](/computer-use.md))
* **Creation**: Linker-signed Go .app can never hold a TCC grant ([gotchas/macos-linker-signed-app-tcc-grant.md](/gotchas/macos-linker-signed-app-tcc-grant.md))
* **Update**: DB Connector (P1) — 2026-10-08, status 2026-10-09 ([concepts/db-connector.md](/concepts/db-connector.md))
* **Update**: SQLite Browser / DB IDE (preview pane) ([concepts/sqlite-browser.md](/concepts/sqlite-browser.md))
* **Update**: Preview Print / Save as PDF ([concepts/preview-print-save-as-pdf.md](/concepts/preview-print-save-as-pdf.md))
* **Update**: Plugin System ([plugins.md](/plugins.md))
* **Update**: Excel preview and Save: Univer sheet over ExcelJS and SheetJS ([concepts/excel-preview-univer-sheet.md](/concepts/excel-preview-univer-sheet.md))

* **Update**: desktop print window ready check now polls for `window._wails.invoke` (every 30 ms, 15 s bound, sends ready once) instead of a one-shot check on `load`, which never found the Wails bridge on macOS and left the print panel unopened; added the gotcha note against reverting to a load check ([concepts/preview-print-save-as-pdf.md](/concepts/preview-print-save-as-pdf.md))
* **Update**: SQL through a well-known client is judged by the statement; project query helpers are judged from source ([concepts/auto-permission-enforced-categories.md](/concepts/auto-permission-enforced-categories.md))
* **Update**: SQL judge rule added to the eval: should-allow 66% to 68%, zero hand-written leaks, per-case results ([concepts/auto-permission-judge-eval.md](/concepts/auto-permission-judge-eval.md))

* **Creation**: Reader mode and viewer context: the chat's side preview fills the session area with the chat as a narrow column, and the viewer's document, location and selection or visible text ride with the next message. Covers the no-remount layout invariant, the reader gate, the contract module and what each viewer reports ([concepts/reader-mode-viewer-context.md](/concepts/reader-mode-viewer-context.md))
* **Creation**: Preview selection listener bound once on mount never attached when the viewer rendered late, so the Copy / Ask LLM toolbar was missing on markdown previews; the effect now re-binds every render ([gotchas/preview-selection-mouseup-listener-mount-only.md](/gotchas/preview-selection-mouseup-listener-mount-only.md))
* **Update**: Side pane state key: the reader flag sits beside `collapsed` under the same per-session key, and reader mode is chat-only ([gotchas/side-pane-statekey-convention.md](/gotchas/side-pane-statekey-convention.md))
* **Update**: Web chat transcript layout: the always-present row wrapper and the optional reader header inside the ask surface ([concepts/web-chat-transcript-layout.md](/concepts/web-chat-transcript-layout.md))

## 2026-11-05

* **Update**: Excel preview and Save: Univer sheet over ExcelJS and SheetJS ([concepts/excel-preview-univer-sheet.md](/concepts/excel-preview-univer-sheet.md))
* **Update**: Excel viewer Save hash contract fixed: the viewer now sends `rawContentHash` (lowercase hex SHA-256 of the exact bytes, via `@noble/hashes`) instead of the djb2 text hash, so Save no longer 409s on an unchanged existing file. The open-409 known gap and the djb2 description are removed, the hash contract is stated, and every anchor on the page was re-derived ([concepts/excel-preview-univer-sheet.md](/concepts/excel-preview-univer-sheet.md))
* **Update**: Preview Print / Save as PDF ([concepts/preview-print-save-as-pdf.md](/concepts/preview-print-save-as-pdf.md))
* **Update**: Browser DevConsole network capture (local vs CDP) ([concepts/browser-dev-console-network-capture.md](/concepts/browser-dev-console-network-capture.md))
* **Update**: Embedded Browser — Per-Tab Root-Mounted Origin for Root-Basepath SPAs ([concepts/browse-per-tab-origin.md](/concepts/browse-per-tab-origin.md))
* **Update**: browser DevConsole network capture re-verified against capture.js: the fetch body is a separate `ocode:browse:networkbody` message while the XHR body stays inline on its row; body gating (streaming types, finite content-length versus capped stream read), the shared request-body renderer, the Resource Timing dedupe records, body retention in browserStore, and the 2026-11-05 decision not to redact body text (sensitive headers are still redacted at capture) are written out; anchors re-derived ([concepts/browser-dev-console-network-capture.md](/concepts/browser-dev-console-network-capture.md))
* **Update**: file tree walk budget: every `handler_files.go` anchor re-derived after the file moved (maxTreeNodes now 1996, buildFileTree 2049, truncation 160, visit order 2113-2131, display sort 2218); `remoteWalkTree` now at line 229 ([concepts/file-tree-walk-budget.md](/concepts/file-tree-walk-budget.md))
* **Update**: Excel preview is now editable for uncontrolled `.xlsx` with no capped sheet: Save with a first-save confirmation, Files-tab Save and Cmd/Ctrl+S entry points, values-only serialization, and the `PUT /api/files/raw` contract (SHA-256 `expected_hash`, `__missing__`, one-time `.bak`, symlink and containment checks, remote `cat >` write). Open bug recorded: the viewer sends the djb2 text hash, so Save returns 409 on every existing file. The `.xlsm` routing gap is restated as stale ([concepts/excel-preview-univer-sheet.md](/concepts/excel-preview-univer-sheet.md))
* **Update**: Pulse — cross-project live-sessions dashboard ([concepts/pulse-dashboard.md](/concepts/pulse-dashboard.md))
* **Update**: Speech-to-text (voice input) ([concepts/speech-to-text.md](/concepts/speech-to-text.md))
* **Update**: Desktop quit guard for unsaved drafts (pending + failed) + sticky-port fallback no longer re-saves ([gotchas/desktop-quit-guard-and-sticky-port-fallback.md](/gotchas/desktop-quit-guard-and-sticky-port-fallback.md))
* **Update**: Desktop webview uses HTTPS + HTTP/2 to escape the six-connection cap ([gotchas/desktop-webview-http2.md](/gotchas/desktop-webview-http2.md))
* **Creation**: Browser DevConsole network capture (local vs CDP) ([concepts/browser-dev-console-network-capture.md](/concepts/browser-dev-console-network-capture.md))
* **Update**: phone typing dead after a tab switch: ghostty-web's `Terminal.focus()` focuses the contenteditable host, whose `beforeinput` it cancels, so soft-keyboard text never reached the pty unless the canvas itself was tapped; every ocode focus call now goes through `focusTerminalInput` (`terminalFocus.ts`), which focuses the hidden textarea; gotcha bullet and file-table row added ([concepts/web-terminal-ghostty-web.md](/concepts/web-terminal-ghostty-web.md))
* **Update**: restored the dropped `status: active` frontmatter key after the v3.1.0 doc pass; body unchanged ([concepts/browse-per-tab-origin.md](/concepts/browse-per-tab-origin.md))
* **Update**: session writers: an answer to a dismissed question now rewrites the stored row. `rewriteAskResultRow` accepted only ask sentinels, so after a dismissed question was reopened and answered the dismissal text stayed on disk while memory held the answer, and the chat reset to the question on every turn. New 2026-11-05 incident with a desktop.log and sqlite diagnostic recipe; the stale anchors on the page were re-derived ([gotchas/session-writers-conflict-recovery.md](/gotchas/session-writers-conflict-recovery.md))
* **Update**: pending-ask recovery page: duplicate frontmatter block removed (one block, `status: active` kept); two stale anchors re-derived; no statement made stale by the dismissed-question fix ([gotchas/pending-ask-recovery-live-session-state.md](/gotchas/pending-ask-recovery-live-session-state.md))
* **Update**: web ask dialogs (202 + background continuation): every anchor re-derived after the dismissed-question fix shifted `handler_questions.go`, and the permission, TypeScript and test lines had drifted too; the defer chain now names the deferred memory-maintenance flush; timestamp bumped; no statement made stale by the fix ([gotchas/web-ask-dialog-resolved-before-continuation.md](/gotchas/web-ask-dialog-resolved-before-continuation.md))
* **Update**: pending-ask recovery page: frontmatter description replaced with one that names the covered triggers (the surviving block carried a changelog sentence) ([gotchas/pending-ask-recovery-live-session-state.md](/gotchas/pending-ask-recovery-live-session-state.md))
* **Update**: Dismiss-then-answer section: before 2026-11-05 an answer to a reopened dismissal never reached disk (rewriteAskResultRow only accepted ask sentinels), so the chat reset; fixed, linked to the incident; isQuestionDismissed reference now tool.QuestionDismissed ([concepts/question-dismiss-reopen-answer.md](/concepts/question-dismiss-reopen-answer.md))
* **Update**: Code anchors re-derived after line shifts; no statements changed ([concepts/compaction-config.md](/concepts/compaction-config.md))
* **Update**: Code anchors re-derived after line shifts; no statements changed ([gotchas/derived-title-label-must-be-bounded.md](/gotchas/derived-title-label-must-be-bounded.md))
* **Update**: Code anchors re-derived after line shifts; no statements changed ([gotchas/chat-model-follows-last-switched-model.md](/gotchas/chat-model-follows-last-switched-model.md))
* **Update**: Dismissed-question incident: dedupe follow-up closed (isQuestionDismissed and dismissedQuestion are now tool.QuestionDismissed); anchors re-derived ([gotchas/session-writers-conflict-recovery.md](/gotchas/session-writers-conflict-recovery.md))
* **Update**: 2026-11-05 changes. Excel Save: first-save confirm now resolves with the user's answer (shared pending request, unmount settles false), Files-tab Save and Cmd/Ctrl+S both go through `requestSave`, the close dialog awaits it, and `setTabDirty` is pure. Remote raw save is now atomic (`remoteWriteFileAtomic`: mktemp, cp -p, size check, mv -f; one-time `.bak` unchanged). Deferred memory maintenance gets a waiter (`awaitDeferredMemoryMaintenance`). Question dismiss logs `session`, `request_id`, `pulse`, `remote` (no User-Agent or Referer). `browserStore` `retainBodies` anchors moved and an equivalence test was added; body-retention and redaction decisions unchanged. All moved anchors re-derived. Tests-only TUI nested-subagent change: no doc change. ([concepts/excel-preview-univer-sheet.md](/concepts/excel-preview-univer-sheet.md); [gotchas/web-ask-dialog-resolved-before-continuation.md](/gotchas/web-ask-dialog-resolved-before-continuation.md); [concepts/compaction-cancellation.md](/concepts/compaction-cancellation.md); [gotchas/session-writers-conflict-recovery.md](/gotchas/session-writers-conflict-recovery.md); [gotchas/question-answer-transcript-echo.md](/gotchas/question-answer-transcript-echo.md); [concepts/browser-dev-console-network-capture.md](/concepts/browser-dev-console-network-capture.md))
* **Update**: question dismiss log line documented as it is emitted: slog record through the stdlib log writer into desktop.log, attributes session, request_id, pulse, remote ([gotchas/session-writers-conflict-recovery.md](/gotchas/session-writers-conflict-recovery.md))
* **Update**: review follow-ups round 2: remote `.gitignore` write keeps existing content (regression test `TestRemoteGitIgnoreKeepsExistingContent`, `internal/server/handler_git_ignore_test.go`); raw save fsyncs the temp file before rename (`internal/server/handler_files.go`); `LoadContext` logs `memory.ResolvePaths` errors and still omits the fragment (`internal/agent/context.go`); Univer `syncOnly` commands no longer leak the in-flight counter (`web/src/components/Preview/univerMount.ts`); two TUI permission tests isolated from the shared config (`internal/tui/model_test.go`, `internal/tui/sandbox_mode_test.go`); open items recorded in `TODO.md` under "review follow-ups round 2"
* **Update**: AIHubMix test cache leak marked resolved: `resetAIHubMixCache(t)` snapshots and restores the live cache in `t.Cleanup` (`internal/agent/models_registry_aihubmix_test.go:11-25`); the original "no cleanup" text is kept as history ([gotchas/aihubmix-test-cache-leak.md](/gotchas/aihubmix-test-cache-leak.md))
* **Update**: web typecheck wording: `tsgo --noEmit` presented as the current command replaced with `bun check .` (the `typecheck` script in `web/package.json`); dated verification records left as written ([gotchas/pdf-preview-webkit-async-iterator.md](/gotchas/pdf-preview-webkit-async-iterator.md); [gotchas/files-tab-preview-only-routing.md](/gotchas/files-tab-preview-only-routing.md))
* **Update**: Excel preview page: Known data loss note added (the loader keeps display text only; `excelJsCell` and the KNOWN LOSS assertions; linked to TODO.md); anchors re-derived for the `.bak` block, the mode block, the rename (now `handler_files.go:1938`) and `watchUserEdits` (the syncOnly check is at `univerMount.ts:124`) ([concepts/excel-preview-univer-sheet.md](/concepts/excel-preview-univer-sheet.md))
* **Update**: anchors re-derived after drift: compaction cancel page (routes, `compact.go` pass registry and operation context, `HandleCompactSession` branches, `runCompact` and `recordCompactOutcome`, client `cancelCompaction`, `IsCompacting`) ([concepts/compaction-cancellation.md](/concepts/compaction-cancellation.md)); `HandleDismissQuestion` anchored at `handler_questions.go:123` with the log call at `:140` ([gotchas/session-writers-conflict-recovery.md](/gotchas/session-writers-conflict-recovery.md)); web-ask and question-echo pages checked, no anchor off; DevConsole bare anchors (`:43`, `:46`, `:88`) resolve to `useBrowserMessages.ts`, not `capture.js`

## 2026-11-04

* **Update**: soft-keyboard Backspace: ghostty-web drops it on both keydown (keyCode 229) and beforeinput (`deleteContentBackward` maps to empty), so the earlier claim that Backspace works through beforeinput is removed; the uncommitted `softKeyboardBackspace` fix and its possible double-send are recorded as unverified on device; re-derived the TerminalPanel.tsx, terminalSoftKeyboard.ts and handler_terminal.go anchors ([concepts/web-terminal-ghostty-web.md](/concepts/web-terminal-ghostty-web.md))

* **Update**: PDF desktop print refreshed to the current path: pages render at `PDF_PRINT_SCALE` (2, 144 dpi) instead of 1.5, with the 8 MiB bridge cap hit sooner; more than 50 pages throws instead of truncating; Print shows busy and transient failed states instead of the inline `setError` line; offscreen pages are released in `finally`; abandoned prints log `console.warn`; `printDocument` moved to `printDocument.ts`; all PdfViewer anchors re-derived ([concepts/pdf-viewer-zoom-find.md](/concepts/pdf-viewer-zoom-find.md))

* **Update**: preview print: `printDocument` now lives in `printDocument.ts` (split from printPreview.tsx so the PDF chunk stays small); mermaid prints under `strict` with theme restore in `finally` and shares `runMermaid` with the viewer; excel keeps blank rows and empty-cell heights; pptx renders sequentially with a 200-slide cap; image MIME parity test added ([concepts/preview-print-save-as-pdf.md](/concepts/preview-print-save-as-pdf.md))

* **Update**: preview find anchors re-derived after PdfViewer.tsx and previewKind.ts moved (Find button, FindBar, keydown gate, overlay, FileTabContent Print button, `.mdx` mapping) ([concepts/preview-find.md](/concepts/preview-find.md))

* **Update**: outbound-network guardrail bash extraction now walks every parsed command on the line (compounds, pipes and `$(...)` substitutions) and emits one target per network command instead of only the first command. A mixed loopback+remote line yields two targets; parse errors keep the old single split; the `/dev/tcp` redirect gap is recorded as predating this change; anchors re-derived; 32 tests ([concepts/webfetch-websearch-guardrails.md](/concepts/webfetch-websearch-guardrails.md))

* **Update**: inbound content guard loopback exemption is now proven per network command across the whole line, so a loopback-only compound is exempt and a compound with a remote fetch is scanned; cites the spurious 0.48 ask on a loopback smoke test; stale anchors re-derived after the 2026-10-28 rubric change ([concepts/inbound-content-guardrail.md](/concepts/inbound-content-guardrail.md))

## 2026-11-03

* **Update**: Horizontal `fill` alignment renders left in the Excel preview, now an approved known gap (user approved 2026-11-03; Univer has no fill alignment). Supersedes the "not yet approved" wording in the earlier 2026-11-03 entry ([concepts/excel-preview-univer-sheet.md](/concepts/excel-preview-univer-sheet.md))
* **Update**: Excel preview theme fallback (Office default palette when a workbook has no theme part) recorded as user-approved 2026-11-03; horizontal `fill` alignment rendering left recorded as a known gap, not yet approved ([concepts/excel-preview-univer-sheet.md](/concepts/excel-preview-univer-sheet.md))
* **Update**: DB Environment Templates (.env / .env.local for connections and SQLite paths) ([concepts/db-environment-templates.md](/concepts/db-environment-templates.md))
* **Update**: DB Connector (P1) — 2026-10-08, status 2026-10-09 ([concepts/db-connector.md](/concepts/db-connector.md))
* **Creation**: VS Code themes and file icon themes ([concepts/vscode-themes-and-icons.md](/concepts/vscode-themes-and-icons.md))
* **Update**: Remote git commit/stash messages must be shell-quoted (remoteGitCommand contract) ([gotchas/remote-git-shell-quoting.md](/gotchas/remote-git-shell-quoting.md))
* **Update**: Git stash UI: list, per-file restore, delete ([concepts/git-stash-ui.md](/concepts/git-stash-ui.md))
* **Update**: Git tab "git add failed: pathspec '…' did not match" — C-quoted names from non-`-z` listings ([gotchas/git-status-c-quoted-paths.md](/gotchas/git-status-c-quoted-paths.md))
* **Update**: Git conflicts and halted-operation recovery (web Git tab) ([concepts/git-conflicts-and-operations.md](/concepts/git-conflicts-and-operations.md))
* **Update**: Remote server version policy: an older live server is kept and flagged outdated, upgraded only by explicit restart ([concepts/remote-persistent-sessions-terminals.md](/concepts/remote-persistent-sessions-terminals.md))
* **Update**: Web UI Global Keyboard Shortcuts ([concepts/web-keyboard-shortcuts.md](/concepts/web-keyboard-shortcuts.md))
* **Update**: Markdown preview: persisted paper-width reading column ([concepts/preview-width.md](/concepts/preview-width.md))
* **Update**: PDF viewer zoom, Space navigation, and find ([concepts/pdf-viewer-zoom-find.md](/concepts/pdf-viewer-zoom-find.md))
* **Update**: Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing + Local Media Streaming) ([gotchas/files-tab-preview-only-routing.md](/gotchas/files-tab-preview-only-routing.md))
* **Creation**: In-preview find (Markdown, Word, HTML, PDF) ([concepts/preview-find.md](/concepts/preview-find.md))
* **Update**: Preview find: shared FindBar, DOM and PDF engines, iframe agent ([concepts/preview-find.md](/concepts/preview-find.md))
* **Update**: System prompt overrides — per-prompt enable switch and custom instructions ([concepts/system-prompt-overrides.md](/concepts/system-prompt-overrides.md))
* **Update**: Server-Side Auto-Continue Loop: withheld messages (relevance veto, 8-batch ceiling, budget fit) are reported to the verdict as `transcript_selection`; each selection batch is retried once; the reserved `auto_continue_select` slot is removed ([concepts/server-auto-continue.md](/concepts/server-auto-continue.md))
* **Update**: Auto-continue selection and print follow-ups: selection scores on the verdict `client` and cancels on Stop; the score cache scope includes `last_reply`; `ResetSelectScores` also runs at session load and `/reset-id`; PDF viewer print timers and `afterprint` cleanup, print errors logged, popup-blocked download revokes its object URL; macOS desktop print uses the user's saved print settings ([concepts/server-auto-continue.md](/concepts/server-auto-continue.md), [concepts/pdf-viewer-zoom-find.md](/concepts/pdf-viewer-zoom-find.md), [concepts/preview-print-save-as-pdf.md](/concepts/preview-print-save-as-pdf.md))
* **Update**: Preview-only kinds route outside-root paths to the grant dialog; known gaps ([concepts/file-editor-grants.md](/concepts/file-editor-grants.md))
* **Update**: Outside-root preview-only paths must reach the grant dialog ([gotchas/files-tab-preview-only-routing.md](/gotchas/files-tab-preview-only-routing.md))
* **Update**: File editor grants: `GET /api/files/access` containment probe, grant prompt on the sidebar and session Preview tab, `grantedFrom` re-anchor, re-derived anchors ([concepts/file-editor-grants.md](/concepts/file-editor-grants.md))
* **Update**: Preview-only routing: outside-root refusal now comes from the server access probe instead of a lexical check ([gotchas/files-tab-preview-only-routing.md](/gotchas/files-tab-preview-only-routing.md))
* **Update**: Markdown preview: re-derived `PreviewHost.tsx` and `PreviewTabPage.tsx` width-menu anchors ([concepts/preview-width.md](/concepts/preview-width.md))
* **Update**: Web Ctrl/Cmd+P FilePicker pins an exact typed path (file or folder) first, via `GET /api/files/stat` independent of the tree index ([concepts/web-keyboard-shortcuts.md](/concepts/web-keyboard-shortcuts.md))
* **Update**: File search ordering: web Ctrl+P exact direct-path hit is pinned above the ranking ([concepts/file-search-shortest-path-ordering.md](/concepts/file-search-shortest-path-ordering.md))
* **Creation**: Resize hook drag direction depends on pane side ([gotchas/resize-hook-drag-sign-per-side.md](/gotchas/resize-hook-drag-sign-per-side.md))
* **Update**: Remote server version policy: `EnsureRemoteServer` returns `(state, reused, err)`; `shouldReplaceRemote` is now `isOlderRemote` (flag only, never replaces or kills) ([concepts/remote-persistent-sessions-terminals.md](/concepts/remote-persistent-sessions-terminals.md))
* **Update**: Web Ctrl/Cmd+P direct hit: `GET /api/files/stat` returns the canonical tree-spelled `path`, a bounded remote stat (`remoteStatTimeout`), 502 on transport failure, and the picker keys the hit by query and project+host; stale anchors and the App.tsx/FileEditor.tsx line references corrected ([concepts/web-keyboard-shortcuts.md](/concepts/web-keyboard-shortcuts.md))
* **Update**: File search ordering: re-derived FilePicker.tsx and handler_files.go anchors ([concepts/file-search-shortest-path-ordering.md](/concepts/file-search-shortest-path-ordering.md))
* **Update**: File tree walk budget: re-derived handler_files.go and FilePicker.tsx anchors, FileTree.tsx hint line ([concepts/file-tree-walk-budget.md](/concepts/file-tree-walk-budget.md))
* **Creation**: Excel preview: read-only Univer sheet over ExcelJS (.xlsx) and SheetJS (csv/tsv/.xls) conversion, limits, and the .xlsm routing gap ([concepts/excel-preview-univer-sheet.md](/concepts/excel-preview-univer-sheet.md))
* **Update**: Multi-Use Preview design spec: the PreviewSurface line names Univer and ExcelJS/SheetJS instead of SheetJS for .xlsx ([superpowers/specs/2026-09-10-preview-multipurpose-design.md](/superpowers/specs/2026-09-10-preview-multipurpose-design.md))

## 2026-11-02

* **Update**: Desktop print: macOS system print panel via `ocode_printWindow` (portrait, 36pt margins) replaces Wails landscape `windowPrint`; PDF popup-tab timer fires only if onload never ran ([concepts/preview-print-save-as-pdf.md](/concepts/preview-print-save-as-pdf.md), [concepts/pdf-viewer-zoom-find.md](/concepts/pdf-viewer-zoom-find.md))
* **Update**: Preview Print / Save as PDF: toolbar Print back to native system print, in-app print dialog removed, PDF viewer print chain ([concepts/preview-print-save-as-pdf.md](/concepts/preview-print-save-as-pdf.md))
* **Update**: browser_run screenshot view: set-of-marks screenshot for pages the text view cannot judge, text-entry controls masked before the image, TypeSafe image assets, scroll offset as progress, 64 px viewport floor ([concepts/browser-run-jev-step-loop.md](/concepts/browser-run-jev-step-loop.md))
* **Update**: PDF viewer zoom, Space navigation, and find ([concepts/pdf-viewer-zoom-find.md](/concepts/pdf-viewer-zoom-find.md))
* **Update**: Preview Print / Save as PDF ([concepts/preview-print-save-as-pdf.md](/concepts/preview-print-save-as-pdf.md))
* **Update**: browser_run step loop: "Routing the model to browser_run" (skill description, webfetch JavaScript-shell hint, prompt fragment when a browser tool is bound) ([concepts/browser-run-jev-step-loop.md](/concepts/browser-run-jev-step-loop.md))
* **Update**: Browser tool on a remote host: htrcli surface maps pick, upload, frames, scroll, typeless type, console and network, with a 120 s network capture window per tab ([concepts/browser-agent-tools.md](/concepts/browser-agent-tools.md))
* **Update**: Browser tool on a remote host drives the client's browser through its htrcli daemon, with identity-checked element refs and explicit errors for the verbs the daemon cannot answer ([concepts/browser-agent-tools.md](/concepts/browser-agent-tools.md))
* **Update**: Web/Desktop Terminal on ghostty-web (libghostty-vt) ([concepts/web-terminal-ghostty-web.md](/concepts/web-terminal-ghostty-web.md))
* **Creation**: Web Git history graph (History sub-view, commit graph, ref-action menu) ([concepts/web-git-history-graph.md](/concepts/web-git-history-graph.md))
* **Update**: Web Git panel: cross-reference to the History sub-view ([concepts/web-git-panel-sections.md](/concepts/web-git-panel-sections.md))
* **Update**: Reopen a locally hidden question dialog: 2026-11-02 amendment makes a dismissed question reopenable and answerable ([superpowers/specs/2026-09-25-reopen-hidden-question-dialog-design.md](/superpowers/specs/2026-09-25-reopen-hidden-question-dialog-design.md))
* **Update**: Interrupted Turn Notice: a dismissed tail can be answered from its card ([concepts/interrupted-turn-notice.md](/concepts/interrupted-turn-notice.md))
* **Creation**: Question dismiss, reopen and answer: the three question states, transition gates and client-local reopen ([concepts/question-dismiss-reopen-answer.md](/concepts/question-dismiss-reopen-answer.md))
* **Creation**: Embedded Browser — Per-Tab Root-Mounted Origin for Root-Basepath SPAs ([concepts/browse-per-tab-origin.md](/concepts/browse-per-tab-origin.md))
* **Update**: Embedded Browser — Chrome Mode Streams: cross-reference to the per-tab origin ([concepts/browse-stream-via-main-origin.md](/concepts/browse-stream-via-main-origin.md))
* **Update**: Embedded Browser — Root-Relative Redirect: cross-reference, the 307 now serves prefix-mode clients only ([concepts/browse-root-relative-redirect.md](/concepts/browse-root-relative-redirect.md))
* **Update**: Web/Desktop Server Project Scoping: embedded-browser paragraph notes the per-tab origin for local clients ([concepts/web-server-project-scoping.md](/concepts/web-server-project-scoping.md))
* **Update**: Per-tab browse origin plan 04: remote binding in the request header, marker in the reply ([plans/2026-11-02-browse-per-tab-origin/04-remote.md](/plans/2026-11-02-browse-per-tab-origin/04-remote.md))
* **Update**: Per-tab browse origin plan 05: shipped POST /api/browse/tab-origin contract and 409 handling ([plans/2026-11-02-browse-per-tab-origin/05-spa-and-persistence.md](/plans/2026-11-02-browse-per-tab-origin/05-spa-and-persistence.md))
* **Update**: Auto-continue selection scores with the verdict's own decider (no selection slot; ten `judgeSlot`s), caps batches at 8 with one retry per batch, reports every withheld message to the verdict as `transcript_selection`, and keeps its score cache scoped to `original_request + last_reply` so a new reply rescores ([concepts/server-auto-continue.md](/concepts/server-auto-continue.md))

## 2026-11-01

* **Creation**: Preview Print / Save as PDF: Print button for markdown, html, text and json previews. Content-built document; browser route through a sandboxed iframe; desktop route through a per-window print with the print-window bridge invariant ([concepts/preview-print-save-as-pdf.md](/concepts/preview-print-save-as-pdf.md))

* **Update**: Inbound Content Guardrail ([concepts/inbound-content-guardrail.md](/concepts/inbound-content-guardrail.md))
* **Update**: Web chat transcript layout: agent-run tool answers with empty content are kept, so a finished call no longer shows running ([concepts/web-chat-transcript-layout.md](/concepts/web-chat-transcript-layout.md))
* **Update**: Session-tagged snapshot gotcha: line anchors re-derived across handler, agent, discovery_glue and TUI model ([gotchas/session-snapshot-stale-derived-fields.md](/gotchas/session-snapshot-stale-derived-fields.md))
* **Update**: Discovery Corpus Cache: discovery_glue.go and md_discovery.go line anchors re-derived after the toolsMu edits ([concepts/discovery-corpus-cache.md](/concepts/discovery-corpus-cache.md))
* **Update**: Discovery MCP Tool Gating: a malformed pinned_mcp_servers value now fails the session build, and pinned server names are validated at chat creation (HTTP 400/500) ([concepts/discovery-mcp-tool-gating.md](/concepts/discovery-mcp-tool-gating.md))
* **Creation**: MCP tool maps are read under toolsMu, never bare: the invariant, the fatal concurrent-map trigger, the race test, and the open GetToolDefinitions loop ([gotchas/mcp-maps-read-under-toolsmu.md](/gotchas/mcp-maps-read-under-toolsmu.md))
* **Update**: browser_run step loop: a stale ref now costs one step and the next decision uses a fresh listing; the (already filled) hint reads the unredacted caller copy; over-budget element trimming is a binary search; line anchors re-derived ([concepts/browser-run-jev-step-loop.md](/concepts/browser-run-jev-step-loop.md))
* **Update**: browser_run step loop (Jev picks each control) ([concepts/browser-run-jev-step-loop.md](/concepts/browser-run-jev-step-loop.md))
* **Update**: Auto-Permission Enforced Categories ([concepts/auto-permission-enforced-categories.md](/concepts/auto-permission-enforced-categories.md))
* **Update**: Web/Desktop UI Parity — Current Status ([web-desktop-parity-todo.md](/web-desktop-parity-todo.md))
* **Update**: Web Chat Transcript Layout — Flex Column and Shrink-Weight Invariant ([concepts/web-chat-transcript-layout.md](/concepts/web-chat-transcript-layout.md))
* **Deprecation**: Chat Verbosity Display — Design Spec: Accidental near-duplicate: a doc_write was issued with the wrong path (specs/ instead of superpowers/specs/) and a truncated body. The authoritative page is superpowers/specs/2026-09-24-chat-verbosity-display-design.md. ([specs/2026-09-24-chat-verbosity-display-design.md](/specs/2026-09-24-chat-verbosity-display-design.md))
* **Creation**: Chat Verbosity Display — Design Spec ([specs/2026-09-24-chat-verbosity-display-design.md](/specs/2026-09-24-chat-verbosity-display-design.md))
* **Update**: Discovery MCP Tool Gating: server-group sibling attach and per-session pinned servers ([concepts/discovery-mcp-tool-gating.md](/concepts/discovery-mcp-tool-gating.md))
* **Update**: Discovery TypeSafe Relevance Judge: a veto beats the server-group sibling expansion ([concepts/discovery-typesafe-judge.md](/concepts/discovery-typesafe-judge.md))
* **Update**: Auto-Permission Enforced Categories: MCP tool description and risk reach the judge ([concepts/auto-permission-enforced-categories.md](/concepts/auto-permission-enforced-categories.md))

* **Creation**: Chat file links with relative or `~` hrefs silently did nothing ([gotchas/chat-file-links-silent-noop.md](/gotchas/chat-file-links-silent-noop.md))
* **Update**: File Editor Grants: home-directory `~` links open in the Files tab ([concepts/file-editor-grants.md](/concepts/file-editor-grants.md))
* **Creation**: browser_run step loop (Jev picks each control) ([concepts/browser-run-jev-step-loop.md](/concepts/browser-run-jev-step-loop.md))
* **Update**: Browser agent tool: `controls` action, guarded index, and a browser_run section ([concepts/browser-agent-tools.md](/concepts/browser-agent-tools.md))
* **Update**: browser_run: target and value gate on choice probability; value asked in its own request; guard judges a plain view of browser results; live test recorded ([concepts/browser-run-jev-step-loop.md](/concepts/browser-run-jev-step-loop.md))
* **Update**: Inbound Content Guardrail: what the judge sees for browser results; cited anchors re-derived ([concepts/inbound-content-guardrail.md](/concepts/inbound-content-guardrail.md))

## 2026-10-31

* **Creation**: Markdown preview: persisted paper-width reading column ([concepts/preview-width.md](/concepts/preview-width.md))
* **Update**: Data Storage Layout ([concepts/data-storage-layout.md](/concepts/data-storage-layout.md))
* **Creation**: Embedded Browser — Chrome Mode Streams Over the Main Origin for Non-Local Clients ([concepts/browse-stream-via-main-origin.md](/concepts/browse-stream-via-main-origin.md))
* **Update**: xlsx probe recalculates through Microsoft Excel (macOS gotchas) ([gotchas/xlsx-probe-excel-recalc-backend.md](/gotchas/xlsx-probe-excel-recalc-backend.md))
* **Update**: Web Chat Transcript Layout — Flex Column and Minimum-Height Invariant ([concepts/web-chat-transcript-layout.md](/concepts/web-chat-transcript-layout.md))
* **Creation**: xlsx probe recalculates through Microsoft Excel (macOS gotchas, backend status, closed-book baseline) ([gotchas/xlsx-probe-excel-recalc-backend.md](/gotchas/xlsx-probe-excel-recalc-backend.md))
* **Update**: Web chat transcript layout — flex column and minimum-height invariant ([concepts/web-chat-transcript-layout.md](/concepts/web-chat-transcript-layout.md))
* **Creation**: Web chat transcript layout — flex column and minimum-height invariant ([concepts/web-chat-transcript-layout.md](/concepts/web-chat-transcript-layout.md))
* **Update**: Git conflicts and halted-operation recovery (web Git tab) ([concepts/git-conflicts-and-operations.md](/concepts/git-conflicts-and-operations.md))
* **Update**: Web UI Mobile Layout Breakage (≤767px) ([gotchas/web-ui-mobile-layout-breakage.md](/gotchas/web-ui-mobile-layout-breakage.md))
* **Update**: Web/Desktop Terminal on ghostty-web (libghostty-vt) ([concepts/web-terminal-ghostty-web.md](/concepts/web-terminal-ghostty-web.md))
* **Update**: Web Git panel: stacked section heights ([concepts/web-git-panel-sections.md](/concepts/web-git-panel-sections.md))
* **Creation**: Web Git panel: stacked section heights ([concepts/web-git-panel-sections.md](/concepts/web-git-panel-sections.md))
* **Update**: Git worktrees: project list folds linked worktrees under their main checkout as a sub tree; sessions stay per checkout ([concepts/git-worktrees.md](/concepts/git-worktrees.md))
* **Update**: Data Storage Layout: each linked worktree has its own project slug and session dir; only the project list groups them ([concepts/data-storage-layout.md](/concepts/data-storage-layout.md))
* **Update**: Amendment set — Data Storage Layout records the `servers/` registry as advisory-locked, registered from `Server.Serve`, with no stop command for unverified siblings ([concepts/data-storage-layout.md](/concepts/data-storage-layout.md)); markdown preview width now uses a CLDR letter-territory list with the kind gate replacing a `prose` resolver option, fluid padding, a self-resolving Auto label, and an `aria-pressed` control ([concepts/preview-width.md](/concepts/preview-width.md)); embedded browser chrome sets `stream_via_main` only when no reachable shared origin exists, adds `detached` + Take over and `browseCdpWsConnection(stateKey, grant, host)` with new tests ([concepts/browse-stream-via-main-origin.md](/concepts/browse-stream-via-main-origin.md)); web chat transcript layout rewritten around the header/body split, a `flex-[1_1_10rem]` transcript, no viewport floor, and a non-yielding BtwPanel, so its index title now reads Shrink-Weight Invariant ([concepts/web-chat-transcript-layout.md](/concepts/web-chat-transcript-layout.md)); line anchors re-derived on concepts/auto-share-on-start, concepts/compaction-cancellation, concepts/per-chat-mcp-toggle and six gotchas (chat-model-follows-last-switched-model, files-tab-preview-only-routing, main-model-pick-also-sets-global-default, port-forwards-url-composition-and-supervisor-restart, remote-project-path-trust-boundary, session-snapshot-stale-derived-fields).

## 2026-10-30

* **Update**: Terminal voice input (mic button in the web Terminal panel) ([concepts/terminal-voice-input.md](/concepts/terminal-voice-input.md))
* **Update**: Local Speech-to-Text Runs in a Resident Worker ([architecture/stt-resident-worker.md](/architecture/stt-resident-worker.md))
* **Update**: Speech-to-text (voice input) ([concepts/speech-to-text.md](/concepts/speech-to-text.md))
* **Creation**: Remote fs actions on a "~"-rooted project: root-joined paths and $HOME quoting ([gotchas/remote-fs-tilde-root-pathing.md](/gotchas/remote-fs-tilde-root-pathing.md))

## 2026-10-29

* **Update**: Terminal appearance: following Ghostty config in a browser terminal ([concepts/terminal-appearance-ghostty.md](/concepts/terminal-appearance-ghostty.md))
* **Update**: Remote Persistent Sessions and Terminals ([concepts/remote-persistent-sessions-terminals.md](/concepts/remote-persistent-sessions-terminals.md))
* **Update**: Persistent Todo Plan (todowrite / todoread / todo_update) ([concepts/persistent-todo-plan.md](/concepts/persistent-todo-plan.md))
* **Update**: Web/Desktop Terminal on ghostty-web (libghostty-vt) ([concepts/web-terminal-ghostty-web.md](/concepts/web-terminal-ghostty-web.md))
* **Creation**: Terminal appearance: following Ghostty config in a browser terminal ([concepts/terminal-appearance-ghostty.md](/concepts/terminal-appearance-ghostty.md))

## 2026-10-28

* **Update**: Remote Persistent Sessions and Terminals ([concepts/remote-persistent-sessions-terminals.md](/concepts/remote-persistent-sessions-terminals.md))
* **Creation**: Web/Desktop Terminal on ghostty-web (libghostty-vt) ([concepts/web-terminal-ghostty-web.md](/concepts/web-terminal-ghostty-web.md))
* **Update**: Main model pick is per chat; only no-session/draft picks set the global default ([gotchas/main-model-pick-also-sets-global-default.md](/gotchas/main-model-pick-also-sets-global-default.md))
* **Creation**: Main model pick is per chat; only no-session/draft picks set the global default ([docs/gotchas/main-model-pick-also-sets-global-default.md](/docs/gotchas/main-model-pick-also-sets-global-default.md))
* **Update**: Chat model followed last switched global model (fixed) ([gotchas/chat-model-follows-last-switched-model.md](/gotchas/chat-model-follows-last-switched-model.md))
* **Creation**: Chat model followed last switched global model (fixed) ([gotchas/chat-model-follows-last-switched-model.md](/gotchas/chat-model-follows-last-switched-model.md))
* **Creation**:  ([concepts/git-pull-strategy.md](/concepts/git-pull-strategy.md))
## 2026-10-27

* **Update**: Files Tab Clipboard (Cut / Copy / Paste) ([concepts/files-tab-clipboard.md](/concepts/files-tab-clipboard.md))
* **Creation**: Files Tab Clipboard (Cut / Copy / Paste) ([concepts/files-tab-clipboard.md](/concepts/files-tab-clipboard.md))
## 2026-10-25

* **Update**: Local embed server (llama-server) fails with "Exec format error" ([gotchas/local-embed-server-exec-format-error.md](/gotchas/local-embed-server-exec-format-error.md))
## 2026-10-22

* **Creation**: File Editor Grants — Editor-Only Out-of-Project Access ([docs/concepts/file-editor-grants.md](/docs/concepts/file-editor-grants.md))
## 2026-10-21

* **Creation**: Bash Prefix Always-Allow Rules ([concepts/bash-prefix-always-allow.md](/concepts/bash-prefix-always-allow.md))
* **Creation**: Reopen a locally hidden permission dialog ([superpowers/specs/2026-10-21-reopen-hidden-permission-dialog-design.md](/superpowers/specs/2026-10-21-reopen-hidden-permission-dialog-design.md))
* **Creation**: Ask-dialog "Explain this call" digest ([concepts/ask-dialog-tool-call-digest.md](/concepts/ask-dialog-tool-call-digest.md))
* **Update**: DB Environment Templates (.env / .env.local for connections and SQLite paths) ([concepts/db-environment-templates.md](/concepts/db-environment-templates.md))
* **Creation**: Pending ask suppresses advisor, auto-continue, and goal-resume ([concepts/pending-ask-suppresses-advisor-auto-continue-goal.md](/concepts/pending-ask-suppresses-advisor-auto-continue-goal.md))
## 2026-10-20

* **Update**: Per-Project Skill Enable/Disable ([concepts/skill-enable-disable.md](/concepts/skill-enable-disable.md))
* **Creation**: Per-Project Skill Enable/Disable ([concepts/skill-enable-disable.md](/concepts/skill-enable-disable.md))
* **Creation**: Local embed server (llama-server) fails with "Exec format error" ([gotchas/local-embed-server-exec-format-error.md](/gotchas/local-embed-server-exec-format-error.md))
* **Creation**: A received TLS alert is not a net.Error: bad record MAC hard-failed an LLM turn ([gotchas/llm-tls-bad-record-mac-retry.md](/gotchas/llm-tls-bad-record-mac-retry.md))
* **Update**: Prompt Cache Stability ([concepts/prompt-cache-stability.md](/concepts/prompt-cache-stability.md))
* **Update**: File search result ordering: shortest path first ([concepts/file-search-shortest-path-ordering.md](/concepts/file-search-shortest-path-ordering.md))
* **Creation**: File tree walk: node budget, symlink budget, and truncation ([concepts/file-tree-walk-budget.md](/concepts/file-tree-walk-budget.md))
## 2026-10-17

* **Update**: Server-Side Auto-Continue Loop ([concepts/server-auto-continue.md](/concepts/server-auto-continue.md))
* **Update**: Web/Desktop UI Parity — Current Status ([web-desktop-parity-todo.md](/web-desktop-parity-todo.md))
* **Update**: Pulse assistant — overview chat that reads and, with approval, acts on sessions (server side) ([concepts/pulse-assistant.md](/concepts/pulse-assistant.md))
## 2026-10-16

* **Update**: TTS speech playback ([tts-speech-playback.md](/tts-speech-playback.md))
## 2026-10-15

* **Creation**: Git worktrees: list, open, create, remove (web Git tab) ([concepts/git-worktrees.md](/concepts/git-worktrees.md))
* **Update**: re-derived line anchors on bundle pages that cite the Git tab, web client, App and server files (Git tab, web-keyboard-shortcuts, web-tab-loading-indicators, web-chat-input-history, compaction, auto-share, port-forwards, files-tab and others)

## 2026-10-12

* **Update**: Git conflicts and halted-operation recovery (web Git tab) ([concepts/git-conflicts-and-operations.md](/concepts/git-conflicts-and-operations.md))













































## 2026-10-09

* **Update**: Chat Verbosity Display — Design Spec ([superpowers/specs/2026-09-24-chat-verbosity-display-design.md](/superpowers/specs/2026-09-24-chat-verbosity-display-design.md))
* **Update**: Web UI Global Keyboard Shortcuts ([concepts/web-keyboard-shortcuts.md](/concepts/web-keyboard-shortcuts.md))
* **Update**: Reopen a locally hidden question dialog ([superpowers/specs/2026-09-25-reopen-hidden-question-dialog-design.md](/superpowers/specs/2026-09-25-reopen-hidden-question-dialog-design.md))
## 2026-10-07

* **Update**: TUI User Interaction: Slash Command Queuing ([concepts/tui-slash-command-queuing.md](/concepts/tui-slash-command-queuing.md))
* **Update**: Auto-share-on-start (tailnet-only boot exposure) ([concepts/auto-share-on-start.md](/concepts/auto-share-on-start.md))
* **Update**: SQLite Browser / DB IDE (preview pane) ([concepts/sqlite-browser.md](/concepts/sqlite-browser.md))
* **Creation**: "Add to .gitignore" — toplevel targeting, C-quoting, and append-not-truncate ([gotchas/git-ignore-toplevel-and-quoting.md](/gotchas/git-ignore-toplevel-and-quoting.md))
* **Update**: Auto-Permission Judge — Credential Material Is Withheld from the Judge Context ([gotchas/auto-permission-judge-withholds-credentials.md](/gotchas/auto-permission-judge-withholds-credentials.md))
* **Update**: Desktop subprocess PATH trap — bare CLI names fail under Finder/Dock-launched .app ([gotchas/desktop-subprocess-path-trap.md](/gotchas/desktop-subprocess-path-trap.md))
* **Update**: File-Edit Snapshot &amp; Undo Mechanism ([file-edit-snapshot.md](/file-edit-snapshot.md))
* **Update**: A derived title/label must be bounded before it is persisted or rendered ([gotchas/derived-title-label-must-be-bounded.md](/gotchas/derived-title-label-must-be-bounded.md))
* **Creation**: SQLite Browser / DB IDE (preview pane) ([concepts/sqlite-browser.md](/concepts/sqlite-browser.md))
## 2026-10-06

* **Update**: Cross-process session activity sync (revision revalidation) ([concepts/cross-process-session-sync.md](/concepts/cross-process-session-sync.md))
* **Creation**: A derived title/label must be bounded before it is persisted or rendered ([gotchas/derived-title-label-must-be-bounded.md](/gotchas/derived-title-label-must-be-bounded.md))
* **Update**: Desktop durable share token (two-credential model) ([concepts/desktop-share-token.md](/concepts/desktop-share-token.md))
* **Update**: Auto-share-on-start (tailnet-only boot exposure) ([concepts/auto-share-on-start.md](/concepts/auto-share-on-start.md))
## 2026-10-05

* **Update**: Clef decision-judge backend + per-judge model selection — design ([superpowers/specs/2026-10-03-clef-judge-backend-design.md](/superpowers/specs/2026-10-03-clef-judge-backend-design.md))
* **Update**: Code-Search Relevance Judge ([concepts/code-search-relevance-judge.md](/concepts/code-search-relevance-judge.md))
## 2026-10-04

* **Update**: Pulse — cross-project live-sessions dashboard ([concepts/pulse-dashboard.md](/concepts/pulse-dashboard.md))
* **Update**: Server-Side Auto-Continue Loop ([concepts/server-auto-continue.md](/concepts/server-auto-continue.md))
* **Update**: Auto-Permission Enforced Categories ([concepts/auto-permission-enforced-categories.md](/concepts/auto-permission-enforced-categories.md))
* **Update**: Doc Search Relevance Judge ([concepts/doc-search-relevance-judge.md](/concepts/doc-search-relevance-judge.md))
* **Update**: Discovery TypeSafe Relevance Judge ([concepts/discovery-typesafe-judge.md](/concepts/discovery-typesafe-judge.md))
* **Creation**: Auto-share-on-start (tailnet-only boot exposure) ([concepts/auto-share-on-start.md](/concepts/auto-share-on-start.md))
* **Update**: TUI User Interaction: Slash Command Queuing ([concepts/tui-slash-command-queuing.md](/concepts/tui-slash-command-queuing.md))
* **Update**: 'TUI User Interaction: Slash Command Queuing' ([concepts/tui-slash-command-queuing.md](/concepts/tui-slash-command-queuing.md))
* **Update**: Compaction Config: First-Token/Idle Timeouts, Operation Cap, and Timeout Classification ([concepts/compaction-config.md](/concepts/compaction-config.md))
## 2026-10-03

* **Update**: Inbound Content Guardrail ([concepts/inbound-content-guardrail.md](/concepts/inbound-content-guardrail.md))
* **Update**: Shared HTR daemon (htrcli serve) ([concepts/htr-shared-daemon.md](/concepts/htr-shared-daemon.md))
* **Update**: Pending ask recovery from live session state (sentinel-less transcript) ([gotchas/pending-ask-recovery-live-session-state.md](/gotchas/pending-ask-recovery-live-session-state.md))
* **Update**: Advisor checkpoints must wait for a pending permission/question ask ([gotchas/advisor-checkpoints-must-wait-for-a-pending-ask.md](/gotchas/advisor-checkpoints-must-wait-for-a-pending-ask.md))
## 2026-10-02

* **Update**: Inbound Content Guardrail ([concepts/inbound-content-guardrail.md](/concepts/inbound-content-guardrail.md))
* **Creation**: Settings → Connectors (web/desktop) ([concepts/web-connector-settings.md](/concepts/web-connector-settings.md))
* **Creation**: Inbound Content Guardrail ([concepts/inbound-content-guardrail.md](/concepts/inbound-content-guardrail.md))
* **Update**: Auto-Permission Enforced Categories ([concepts/auto-permission-enforced-categories.md](/concepts/auto-permission-enforced-categories.md))
* **Update**: Prompt Cache Stability ([concepts/prompt-cache-stability.md](/concepts/prompt-cache-stability.md))
* **Update**: Web/Desktop "Connectors" settings — TUI /connect parity — design ([superpowers/specs/2026-10-01-web-connector-settings-design.md](/superpowers/specs/2026-10-01-web-connector-settings-design.md))
* **Update**: Design Spec: Reliable Large-Context Compaction (web/desktop /compact) ([superpowers/specs/2026-09-25-web-compact-large-context-reliability-design.md](/superpowers/specs/2026-09-25-web-compact-large-context-reliability-design.md))
* **Update**: Compaction Config: First-Token/Idle Timeouts, Operation Cap, and Timeout Classification ([concepts/compaction-config.md](/concepts/compaction-config.md))
* **Creation**: Shared HTR daemon (htrcli serve) ([concepts/htr-shared-daemon.md](/concepts/htr-shared-daemon.md))
* **Creation**: Advisor checkpoints must wait for a pending permission/question ask ([gotchas/advisor-checkpoints-must-wait-for-a-pending-ask.md](/gotchas/advisor-checkpoints-must-wait-for-a-pending-ask.md))
* **Update**: Local Model Instance Manager Implementation Plan ([superpowers/plans/2026-08-01-local-model-instance-manager.md](/superpowers/plans/2026-08-01-local-model-instance-manager.md))
* **Update**: Discovery Corpus Cache ([concepts/discovery-corpus-cache.md](/concepts/discovery-corpus-cache.md))
* **Update**: Web/Desktop Connectors settings — implementation plan ([superpowers/plans/2026-10-01-web-connector-settings.md](/superpowers/plans/2026-10-01-web-connector-settings.md))
* **Update**: Loopback curl with a shell-variable port asked as exfiltration — and the 127. host-match hole beside it ([gotchas/loopback-curl-shell-port-variable.md](/gotchas/loopback-curl-shell-port-variable.md))
* **Update**: Discovery TypeSafe Relevance Judge ([concepts/discovery-typesafe-judge.md](/concepts/discovery-typesafe-judge.md))
* **Update**: Discovery MCP Tool Gating ([concepts/discovery-mcp-tool-gating.md](/concepts/discovery-mcp-tool-gating.md))
* **Update**: Session-tagged snapshot: override base fields but recompute ALL derived fields ([gotchas/session-snapshot-stale-derived-fields.md](/gotchas/session-snapshot-stale-derived-fields.md))
## 2026-10-01

* **Update**: Loopback curl with a shell-variable port asked as exfiltration — and the 127. prefix host-match hole ([gotchas/loopback-curl-shell-port-variable.md](/gotchas/loopback-curl-shell-port-variable.md))
* **Creation**: Loopback curl with a shell-variable port asked as exfiltration — and the 127. prefix host-match hole ([gotchas/loopback-curl-shell-port-variable.md](/gotchas/loopback-curl-shell-port-variable.md))
* **Update**: Compaction Config: First-Token/Idle Timeouts, Operation Cap, and Timeout Classification ([concepts/compaction-config.md](/concepts/compaction-config.md))
* **Update**: Design Spec: Reliable Large-Context Compaction (web/desktop /compact) ([superpowers/specs/2026-09-25-web-compact-large-context-reliability-design.md](/superpowers/specs/2026-09-25-web-compact-large-context-reliability-design.md))
* **Update**: Cross-Client Compaction Indicator — Design Spec ([superpowers/specs/2026-09-25-cross-client-compaction-indicator-design.md](/superpowers/specs/2026-09-25-cross-client-compaction-indicator-design.md))
* **Update**: Compaction Cancellation: User-Initiated Cancel of an In-Flight Pass ([concepts/compaction-cancellation.md](/concepts/compaction-cancellation.md))
* **Creation**: Desktop durable share token (two-credential model) ([concepts/desktop-share-token.md](/concepts/desktop-share-token.md))
* **Creation**: Compaction Cancellation: User-Initiated Cancel of an In-Flight Pass ([concepts/compaction-cancellation.md](/concepts/compaction-cancellation.md))
* **Update**: Changes Tab ([changes-tab.md](/changes-tab.md))
* **Update**: Web Chat Composer Input History (↑/↓ Navigation) ([concepts/web-chat-input-history.md](/concepts/web-chat-input-history.md))
* **Update**: Web ask dialogs: 202 + background continuation (broadcast *_resolved before Step) ([gotchas/web-ask-dialog-resolved-before-continuation.md](/gotchas/web-ask-dialog-resolved-before-continuation.md))
* **Update**: Prompt Cache Stability ([concepts/prompt-cache-stability.md](/concepts/prompt-cache-stability.md))
* **Update**: Pulse — cross-project live-sessions dashboard ([concepts/pulse-dashboard.md](/concepts/pulse-dashboard.md))
* **Update**: Discovery TypeSafe Relevance Judge ([concepts/discovery-typesafe-judge.md](/concepts/discovery-typesafe-judge.md))
## 2026-09-30

* **Update**: Scheduled Jobs / Cron Dispatch ([scheduled-jobs.md](/scheduled-jobs.md))
* **Creation**: Environment Prompt ([concepts/environment-prompt.md](/concepts/environment-prompt.md))
* **Update**: Sandbox Permission Mode ([concepts/sandbox-permission-mode.md](/concepts/sandbox-permission-mode.md))
* **Creation**: TUI User Interaction: Slash Command Queuing ([concepts/tui-slash-command-queuing.md](/concepts/tui-slash-command-queuing.md))
* **Creation**: Knowledge System (OKF Bundle): Agents, Tools and Maintenance ([concepts/okf-knowledge-system.md](/concepts/okf-knowledge-system.md))
* **Creation**: Prompt Cache Stability ([concepts/prompt-cache-stability.md](/concepts/prompt-cache-stability.md))
* **Creation**: Data Storage Layout ([concepts/data-storage-layout.md](/concepts/data-storage-layout.md))
* **Creation**: Backend and Sync URL Split (2026-09-03) ([concepts/backend-sync-url-split.md](/concepts/backend-sync-url-split.md))
* **Creation**: Web/Desktop Context Gauge Resolution ([concepts/web-context-gauge-resolution.md](/concepts/web-context-gauge-resolution.md))
* **Creation**: Web/Desktop Server Project Scoping ([concepts/web-server-project-scoping.md](/concepts/web-server-project-scoping.md))
* **Creation**: Web/Desktop Server Locking and Liveness Rules ([concepts/web-server-locking-and-liveness-rules.md](/concepts/web-server-locking-and-liveness-rules.md))
* **Creation**: In-batch Task DAG (id / depends_on) ([concepts/task-dag.md](/concepts/task-dag.md))
* **Creation**: Sub-agent Transcripts: OnSubAgentMessage and Child Sessions ([concepts/subagent-transcripts-child-sessions.md](/concepts/subagent-transcripts-child-sessions.md))
* **Creation**: Persistent Todo Plan (todowrite / todoread / todo_update) ([concepts/persistent-todo-plan.md](/concepts/persistent-todo-plan.md))
* **Creation**: Task Output Contracts (expected_output) ([concepts/task-output-contracts.md](/concepts/task-output-contracts.md))
* **Creation**: Sidebar row stopPropagation breaks mobile drawer dismiss ([gotchas/sidebar-row-propagation-breaks-drawer-dismiss.md](/gotchas/sidebar-row-propagation-breaks-drawer-dismiss.md))
* **Update**:  ([superpowers/plans/2026-09-18-remote-persistent-sessions-terminals/04-frontend-sidebar-inventory.md](/superpowers/plans/2026-09-18-remote-persistent-sessions-terminals/04-frontend-sidebar-inventory.md))
* **Update**: Remote Persistent Sessions and Terminals Design ([superpowers/specs/2026-09-18-remote-persistent-sessions-terminals-design.md](/superpowers/specs/2026-09-18-remote-persistent-sessions-terminals-design.md))
* **Update**: Remote Persistent Sessions and Terminals ([concepts/remote-persistent-sessions-terminals.md](/concepts/remote-persistent-sessions-terminals.md))
* **Update**: Port forwards Disable/Enable: URL composed past query, supervisor retained-terminal collision, and dead-forward liveness/restart monitor ([gotchas/port-forwards-url-composition-and-supervisor-restart.md](/gotchas/port-forwards-url-composition-and-supervisor-restart.md))
* **Update**: Compaction Config: First-Token/Idle Timeouts, Operation Cap, and Timeout Classification ([concepts/compaction-config.md](/concepts/compaction-config.md))
* **Creation**: A module-scope model/corpus fetch turns a "local" Python engine into a networked one ([gotchas/python-module-scope-network-fetch.md](/gotchas/python-module-scope-network-fetch.md))
* **Update**: Speech playback ([tts-speech-playback.md](/tts-speech-playback.md))
* **Update**: Web/Desktop UI Parity — Current Status ([web-desktop-parity-todo.md](/web-desktop-parity-todo.md))
* **Creation**: Remote session listing 404s for tilde-keyed projects ([gotchas/remote-session-list-tilde-404.md](/gotchas/remote-session-list-tilde-404.md))
* **Update**: Outbound-Network Guardrail ([concepts/webfetch-websearch-guardrails.md](/concepts/webfetch-websearch-guardrails.md))
* **Creation**: Outbound-Network Guardrail ([concepts/webfetch-websearch-guardrails.md](/concepts/webfetch-websearch-guardrails.md))
## 2026-09-29

* **Update**: Auto-Permission Enforced Categories ([concepts/auto-permission-enforced-categories.md](/concepts/auto-permission-enforced-categories.md))
* **Update**: Speech playback ([tts-speech-playback.md](/tts-speech-playback.md))
* **Creation**: User-Message Jump (Alt+↑ / Alt+↓) ([concepts/user-message-jump.md](/concepts/user-message-jump.md))
* **Update**: Cowork Sidebar LSP Status Overflow (horizontal scrollbar from an unshrinkable cell) ([gotchas/cowork-sidebar-lsp-status-overflow.md](/gotchas/cowork-sidebar-lsp-status-overflow.md))
* **Update**: Web UI Mobile Layout Breakage (≤767px)" ([gotchas/web-ui-mobile-layout-breakage.md](/gotchas/web-ui-mobile-layout-breakage.md))
* **Creation**: Cowork Sidebar LSP Status Overflow (horizontal scrollbar from an unshrinkable cell) ([gotchas/cowork-sidebar-lsp-status-overflow.md](/gotchas/cowork-sidebar-lsp-status-overflow.md))
* **Creation**: Remote connection identity is user+host+port, never Target.String() ([gotchas/remote-connection-identity-includes-port.md](/gotchas/remote-connection-identity-includes-port.md))
* **Creation**: A failed git probe is not the same answer as "not a repository" ([gotchas/git-probe-failure-is-not-a-non-repo.md](/gotchas/git-probe-failure-is-not-a-non-repo.md))
* **Update**: opencode-go per-model protocol routing & Anthropic tool schema flatness ([gotchas/opencode-go-per-model-protocol-and-anthropic-tool-schemas.md](/gotchas/opencode-go-per-model-protocol-and-anthropic-tool-schemas.md))
* **Update**: Stack Detection ([okf/_schema/stack-detection.md](/okf/_schema/stack-detection.md))
* **Update**: Advisor Claude Code CLI backend on web/desktop ([concepts/advisor-claude-code-backend.md](/concepts/advisor-claude-code-backend.md))
* **Update**: Chat transcript scroll-bounce: clamped pin vs. user intent ([gotchas/chat-scroll-bounce-clamped-pin-vs-user-intent.md](/gotchas/chat-scroll-bounce-clamped-pin-vs-user-intent.md))
* **Update**: Code-Search Relevance Judge Implementation Plan ([superpowers/plans/2026-09-28-code-search-relevance-judge.md](/superpowers/plans/2026-09-28-code-search-relevance-judge.md))
* **Update**: Code-Search Relevance Judge ([concepts/code-search-relevance-judge.md](/concepts/code-search-relevance-judge.md))
* **Deprecation**: Write semantics test: Temporary scratch document used to test doc_write replace-vs-merge semantics during the Pulse entry-point doc update (2026-09-29); not real knowledge. ([scratch/write-semantics-test.md](/scratch/write-semantics-test.md))
* **Update**: Pulse — cross-project live sessions dashboard ([superpowers/specs/2026-09-24-pulse-dashboard-design.md](/superpowers/specs/2026-09-24-pulse-dashboard-design.md))
* **Update**: Pulse — cross-project live-sessions dashboard ([concepts/pulse-dashboard.md](/concepts/pulse-dashboard.md))
* **Update**: Write semantics test ([scratch/write-semantics-test.md](/scratch/write-semantics-test.md))
* **Update**: Auto-Permission Judge — Credential Material Is Withheld from the Judge Context ([gotchas/auto-permission-judge-withholds-credentials.md](/gotchas/auto-permission-judge-withholds-credentials.md))
* **Creation**: Write semantics test ([scratch/write-semantics-test.md](/scratch/write-semantics-test.md))
* **Creation**: Chat transcript scroll-bounce: clamped pin vs. user intent ([gotchas/chat-scroll-bounce-clamped-pin-vs-user-intent.md](/gotchas/chat-scroll-bounce-clamped-pin-vs-user-intent.md))
* **Update**: TTS Speech Playback Design Specification ([superpowers/specs/2026-09-09-tts-speech-playback-design.md](/superpowers/specs/2026-09-09-tts-speech-playback-design.md))
* **Update**: Speech summary is fail-open: skip while the turn holds as.mu, cancellable summariser, unlocked config write ([gotchas/speech-summary-turn-lock-wait.md](/gotchas/speech-summary-turn-lock-wait.md))
* **Update**: Mutation-check: mutants must compile; filelock non-positive timeout = 10s default; harness backup/trap/preflight hygiene ([gotchas/mutation-check-mutants-must-compile.md](/gotchas/mutation-check-mutants-must-compile.md))
* **Update**: TUI copy over SSH never reaches the local clipboard — route every copy through OSC 52 (`copyToClipboard`) ([gotchas/tui-clipboard-remote-ssh-osc52.md](/gotchas/tui-clipboard-remote-ssh-osc52.md))
* **Creation**: TUI copy over SSH never reaches the local clipboard — route every copy through OSC 52 (`copyToClipboard`) ([gotchas/tui-clipboard-remote-ssh-osc52.md](/gotchas/tui-clipboard-remote-ssh-osc52.md))
* **Creation**: Mutation-check false verdicts: compile-broken mutants and equivalent mutants ([gotchas/mutation-check-false-caught.md](/gotchas/mutation-check-false-caught.md))
* **Update**: Knowledge Bundle System ([knowledge-bundle.md](/knowledge-bundle.md))
* **Update**: Discovery MCP Tool Gating ([concepts/discovery-mcp-tool-gating.md](/concepts/discovery-mcp-tool-gating.md))
* **Update**: Discovery Corpus Cache ([concepts/discovery-corpus-cache.md](/concepts/discovery-corpus-cache.md))
* **Update**: Embedded HTR Extension and Managed Daemon Design ([superpowers/specs/2026-09-09-embedded-htr-extension-design.md](/superpowers/specs/2026-09-09-embedded-htr-extension-design.md))
* **Update**: Delayed Chat Input Consolidation ([superpowers/specs/2026-09-09-chat-input-consolidation-design.md](/superpowers/specs/2026-09-09-chat-input-consolidation-design.md))
* **Update**: TUI Input Queueing Behind a ! Shell Command — Gates That Only Knew About Streaming ([gotchas/tui-input-queueing-behind-shell-command.md](/gotchas/tui-input-queueing-behind-shell-command.md))
* **Update**: Mutation-check: mutants must compile; filelock non-positive timeout = 10s default ([gotchas/mutation-check-mutants-must-compile.md](/gotchas/mutation-check-mutants-must-compile.md))
* **Creation**: Discovery Corpus Cache ([concepts/discovery-corpus-cache.md](/concepts/discovery-corpus-cache.md))
* **Creation**: Mutation-check: mutants must compile; filelock non-positive timeout = 10s default ([gotchas/mutation-check-mutants-must-compile.md](/gotchas/mutation-check-mutants-must-compile.md))
* **Update**: Discovery TypeSafe Relevance Judge ([concepts/discovery-typesafe-judge.md](/concepts/discovery-typesafe-judge.md))
* **Creation**: TUI Input Queueing Behind a `!` Shell Command — Gates That Only Knew About Streaming ([gotchas/tui-input-queueing-behind-shell-command.md](/gotchas/tui-input-queueing-behind-shell-command.md))
* **Update**: Doc Search Relevance Judge ([concepts/doc-search-relevance-judge.md](/concepts/doc-search-relevance-judge.md))
## 2026-09-28

* **Update**: Auto-Permission Dependency Binaries — Policy Decision ([gotchas/auto-permission-dependency-bin-policy.md](/gotchas/auto-permission-dependency-bin-policy.md))
* **Update**: Code-Search Relevance Judge ([concepts/code-search-relevance-judge.md](/concepts/code-search-relevance-judge.md))
* **Update**: Discovery TypeSafe Relevance Judge ([concepts/discovery-typesafe-judge.md](/concepts/discovery-typesafe-judge.md))
* **Update**: Doc Search Relevance Judge ([concepts/doc-search-relevance-judge.md](/concepts/doc-search-relevance-judge.md))
* **Creation**: Code-Search Relevance Judge ([concepts/code-search-relevance-judge.md](/concepts/code-search-relevance-judge.md))
* **Update**: Moved: speech-summary turn-lock gotcha lives at gotchas/speech-summary-turn-lock-wait.md ([docs/gotchas/speech-summary-turn-lock-wait.md](/docs/gotchas/speech-summary-turn-lock-wait.md))
* **Update**: TTS Speech Playback Design Specification ([superpowers/specs/2026-09-09-tts-speech-playback-design.md](/superpowers/specs/2026-09-09-tts-speech-playback-design.md))
* **Update**: Speech playback ([tts-speech-playback.md](/tts-speech-playback.md))
* **Creation**: Speech summary is fail-open: skip while the turn holds as.mu, cancellable summariser, unlocked config write ([gotchas/speech-summary-turn-lock-wait.md](/gotchas/speech-summary-turn-lock-wait.md))
* **Deprecation**: Speech summary is fail-open: skip while the turn holds as.mu, cancellable summariser, unlocked config write: Written with a wrong bundle-relative path (docs/gotchas/... instead of gotchas/...), landing in docs/docs/. Superseded by gotchas/speech-summary-turn-lock-wait.md. ([docs/gotchas/speech-summary-turn-lock-wait.md](/docs/gotchas/speech-summary-turn-lock-wait.md))
* **Creation**: Speech summary is fail-open: skip while the turn holds as.mu, cancellable summariser, unlocked config write ([docs/gotchas/speech-summary-turn-lock-wait.md](/docs/gotchas/speech-summary-turn-lock-wait.md))
* **Creation**: Monaco's WebKit clipboard workaround spams NotAllowedError and lags every keystroke ([gotchas/monaco-webkit-clipboard-workaround.md](/gotchas/monaco-webkit-clipboard-workaround.md))
* **Creation**: Session-scoped config helpers must pass the session's remote host ([gotchas/remote-session-config-host-routing.md](/gotchas/remote-session-config-host-routing.md))
* **Creation**: Desktop webview has no Wails event system — shell→page signals must use ExecJS + DOM CustomEvent ([gotchas/desktop-shell-webview-no-wails-events.md](/gotchas/desktop-shell-webview-no-wails-events.md))
* **Update**:  ([superpowers/plans/2026-09-24-pulse-dashboard/07-pulse-card.md](/superpowers/plans/2026-09-24-pulse-dashboard/07-pulse-card.md))
* **Update**:  ([superpowers/plans/2026-09-24-pulse-dashboard/06-jump-helper.md](/superpowers/plans/2026-09-24-pulse-dashboard/06-jump-helper.md))
* **Update**:  ([superpowers/plans/2026-09-24-pulse-dashboard/05-pulse-store.md](/superpowers/plans/2026-09-24-pulse-dashboard/05-pulse-store.md))
* **Update**:  ([superpowers/plans/2026-09-24-pulse-dashboard/04-pulse-endpoint.md](/superpowers/plans/2026-09-24-pulse-dashboard/04-pulse-endpoint.md))
* **Update**:  ([superpowers/plans/2026-09-24-pulse-dashboard/03-pulse-rows.md](/superpowers/plans/2026-09-24-pulse-dashboard/03-pulse-rows.md))
* **Update**:  ([superpowers/plans/2026-09-24-pulse-dashboard/10-docs.md](/superpowers/plans/2026-09-24-pulse-dashboard/10-docs.md))
* **Update**:  ([superpowers/plans/2026-09-24-pulse-dashboard/09-desktop-open-pulse.md](/superpowers/plans/2026-09-24-pulse-dashboard/09-desktop-open-pulse.md))
* **Update**:  ([superpowers/plans/2026-09-24-pulse-dashboard/08-pulse-view-entry.md](/superpowers/plans/2026-09-24-pulse-dashboard/08-pulse-view-entry.md))
* **Update**:  ([superpowers/plans/2026-09-24-pulse-dashboard/zz-checkbox-probe.md](/superpowers/plans/2026-09-24-pulse-dashboard/zz-checkbox-probe.md))
* **Creation**:  ([superpowers/plans/2026-09-24-pulse-dashboard/zz-checkbox-probe.md](/superpowers/plans/2026-09-24-pulse-dashboard/zz-checkbox-probe.md))
* **Update**:  ([superpowers/plans/2026-09-24-pulse-dashboard/02-last-turn-error.md](/superpowers/plans/2026-09-24-pulse-dashboard/02-last-turn-error.md))
* **Update**: Part 01 — Per-session todo reader and todo_updated event ([superpowers/plans/2026-09-24-pulse-dashboard/01-todo-reader-and-event.md](/superpowers/plans/2026-09-24-pulse-dashboard/01-todo-reader-and-event.md))
* **Update**: Pulse — cross-project live sessions dashboard ([superpowers/specs/2026-09-24-pulse-dashboard-design.md](/superpowers/specs/2026-09-24-pulse-dashboard-design.md))
* **Creation**: Pulse — cross-project live-sessions dashboard ([concepts/pulse-dashboard.md](/concepts/pulse-dashboard.md))
* **Creation**: Code-Search Relevance Judge Implementation Plan ([superpowers/plans/2026-09-28-code-search-relevance-judge.md](/superpowers/plans/2026-09-28-code-search-relevance-judge.md))
* **Creation**: Code-Search Relevance Judge (v1) ([superpowers/specs/2026-09-28-code-search-relevance-judge-design.md](/superpowers/specs/2026-09-28-code-search-relevance-judge-design.md))
* **Update**: Port forwards Disable/Enable: URL composed past query, supervisor retained-terminal collision, and dead-forward liveness/restart monitor ([gotchas/port-forwards-url-composition-and-supervisor-restart.md](/gotchas/port-forwards-url-composition-and-supervisor-restart.md))
* **Update**: Duplicate Project as Remote (SSH/WSL) ([superpowers/specs/2026-09-28-duplicate-project-as-remote-design.md](/superpowers/specs/2026-09-28-duplicate-project-as-remote-design.md))
* **Update**: Remote Project Paths Must Not Enter the Local Filesystem Trust Boundary ([gotchas/remote-project-path-trust-boundary.md](/gotchas/remote-project-path-trust-boundary.md))
* **Creation**: Duplicate Project as Remote (SSH/WSL) ([superpowers/specs/2026-09-28-duplicate-project-as-remote-design.md](/superpowers/specs/2026-09-28-duplicate-project-as-remote-design.md))
## 2026-09-27

* **Update**: Compaction Config: First-Token/Idle Timeouts, Operation Cap, and Timeout Classification ([concepts/compaction-config.md](/concepts/compaction-config.md))
## 2026-09-26

* **Update**: Git conflicts and halted-operation recovery (web Git tab) ([concepts/git-conflicts-and-operations.md](/concepts/git-conflicts-and-operations.md))
* **Update**: Discovery MCP Tool Gating ([concepts/discovery-mcp-tool-gating.md](/concepts/discovery-mcp-tool-gating.md))
* **Update**: Discovery TypeSafe Relevance Judge ([concepts/discovery-typesafe-judge.md](/concepts/discovery-typesafe-judge.md))
* **Creation**: Discovery MCP Tool Gating ([concepts/discovery-mcp-tool-gating.md](/concepts/discovery-mcp-tool-gating.md))
* **Update**: Shared Confirm Dialog Implementation Plan ([superpowers/plans/2026-09-25-shared-confirm-dialog.md](/superpowers/plans/2026-09-25-shared-confirm-dialog.md))
* **Update**: Host and project scoping for web session/project reads ([concepts/web-session-host-scoping.md](/concepts/web-session-host-scoping.md))
* **Update**: Chat Verbosity Display — Design Spec ([superpowers/specs/2026-09-24-chat-verbosity-display-design.md](/superpowers/specs/2026-09-24-chat-verbosity-display-design.md))
## 2026-09-25

* **Update**: Cross-Client Compaction Indicator — Design Spec ([superpowers/specs/2026-09-25-cross-client-compaction-indicator-design.md](/superpowers/specs/2026-09-25-cross-client-compaction-indicator-design.md))
* **Update**: Chat Verbosity Display — Design Spec ([superpowers/specs/2026-09-24-chat-verbosity-display-design.md](/superpowers/specs/2026-09-24-chat-verbosity-display-design.md))
* **Update**: Git conflicts and halted-operation recovery (web Git tab) ([concepts/git-conflicts-and-operations.md](/concepts/git-conflicts-and-operations.md))
* **Creation**: Shared Confirm Dialog Implementation Plan ([superpowers/plans/2026-09-25-shared-confirm-dialog.md](/superpowers/plans/2026-09-25-shared-confirm-dialog.md))
* **Update**: Last-dispatched model in the status bar — design ([superpowers/specs/2026-09-25-last-dispatched-model-status-design.md](/superpowers/specs/2026-09-25-last-dispatched-model-status-design.md))
* **Creation**: Shared confirm dialog + confirmation coverage for destructive sidebar/actions ([superpowers/specs/2026-09-25-shared-confirm-dialog-design.md](/superpowers/specs/2026-09-25-shared-confirm-dialog-design.md))
* **Creation**: Web List-Dialog Keyboard Navigation ([concepts/web-list-dialog-keyboard-navigation.md](/concepts/web-list-dialog-keyboard-navigation.md))
* **Update**: Design Spec: Reliable Large-Context Compaction (web/desktop /compact) ([superpowers/specs/2026-09-25-web-compact-large-context-reliability-design.md](/superpowers/specs/2026-09-25-web-compact-large-context-reliability-design.md))
* **Update**: Compaction Config: First-Token/Idle Timeouts, Operation Cap, and Timeout Classification ([concepts/compaction-config.md](/concepts/compaction-config.md))
* **Creation**: Git conflicts and halted-operation recovery (web Git tab) ([concepts/git-conflicts-and-operations.md](/concepts/git-conflicts-and-operations.md))
* **Update**: TUI wheel scroll over chat composer design ([superpowers/specs/2026-09-25-tui-wheel-scroll-over-composer-design.md](/superpowers/specs/2026-09-25-tui-wheel-scroll-over-composer-design.md))
* **Update**: Reopen a locally hidden question dialog ([superpowers/specs/2026-09-25-reopen-hidden-question-dialog-design.md](/superpowers/specs/2026-09-25-reopen-hidden-question-dialog-design.md))
* **Update**: Deferred, durable message rewind for ocode Web/Desktop ([superpowers/specs/2026-09-25-deferred-session-rewind-design.md](/superpowers/specs/2026-09-25-deferred-session-rewind-design.md))
* **Update**: List Dialog Keyboard Navigation — Design Spec ([superpowers/specs/2026-09-25-list-dialog-keyboard-navigation-design.md](/superpowers/specs/2026-09-25-list-dialog-keyboard-navigation-design.md))
* **Creation**: Reopen a locally hidden question dialog ([superpowers/specs/2026-09-25-reopen-hidden-question-dialog-design.md](/superpowers/specs/2026-09-25-reopen-hidden-question-dialog-design.md))
* **Creation**: Compaction Config: First-Token/Idle Timeouts, Operation Cap, and Timeout Classification ([concepts/compaction-config.md](/concepts/compaction-config.md))
* **Creation**: Last-dispatched model in the status bar — design ([superpowers/specs/2026-09-25-last-dispatched-model-status-design.md](/superpowers/specs/2026-09-25-last-dispatched-model-status-design.md))
* **Creation**: Cross-Client Compaction Indicator — Design Spec ([superpowers/specs/2026-09-25-cross-client-compaction-indicator-design.md](/superpowers/specs/2026-09-25-cross-client-compaction-indicator-design.md))
* **Update**: Per-Chat MCP Toggle ([concepts/per-chat-mcp-toggle.md](/concepts/per-chat-mcp-toggle.md))
* **Update**: Zoho MCP OAuth Compatibility — Design Spec ([superpowers/specs/2026-09-25-zoho-mcp-oauth-compatibility-design.md](/superpowers/specs/2026-09-25-zoho-mcp-oauth-compatibility-design.md))
* **Creation**: Design Spec: Reliable Large-Context Compaction (web/desktop /compact) ([superpowers/specs/2026-09-25-web-compact-large-context-reliability-design.md](/superpowers/specs/2026-09-25-web-compact-large-context-reliability-design.md))
* **Update**: Remote MCP OAuth Compatibility ([concepts/remote-mcp-oauth-compat.md](/concepts/remote-mcp-oauth-compat.md))
* **Creation**: Remote MCP OAuth Compatibility ([concepts/remote-mcp-oauth-compat.md](/concepts/remote-mcp-oauth-compat.md))
* **Update**: Web ask dialogs: 202 + background continuation (broadcast *_resolved before Step) ([gotchas/web-ask-dialog-resolved-before-continuation.md](/gotchas/web-ask-dialog-resolved-before-continuation.md))
* **Update**: Part 03 — Async Bootstrap, Turn State Machine, Reconcile & Status Endpoints ([superpowers/plans/2026-08-12-multiproject-event-architecture/03-async-bootstrap-turn-state.md](/superpowers/plans/2026-08-12-multiproject-event-architecture/03-async-bootstrap-turn-state.md))
* **Creation**: TUI wheel scroll over chat composer design ([superpowers/specs/2026-09-25-tui-wheel-scroll-over-composer-design.md](/superpowers/specs/2026-09-25-tui-wheel-scroll-over-composer-design.md))
* **Creation**: Web Chat Message Copy Menu — Design ([superpowers/specs/2026-09-25-web-chat-message-copy-design.md](/superpowers/specs/2026-09-25-web-chat-message-copy-design.md))
* **Creation**: Deferred, durable message rewind for ocode Web/Desktop ([superpowers/specs/2026-09-25-deferred-session-rewind-design.md](/superpowers/specs/2026-09-25-deferred-session-rewind-design.md))
* **Creation**: Zoho MCP OAuth Compatibility — Design Spec ([superpowers/specs/2026-09-25-zoho-mcp-oauth-compatibility-design.md](/superpowers/specs/2026-09-25-zoho-mcp-oauth-compatibility-design.md))
* **Creation**: List Dialog Keyboard Navigation — Design Spec ([superpowers/specs/2026-09-25-list-dialog-keyboard-navigation-design.md](/superpowers/specs/2026-09-25-list-dialog-keyboard-navigation-design.md))
* **Update**: Desktop quit guard for unsaved drafts (pending + failed) + sticky-port fallback no longer re-saves ([gotchas/desktop-quit-guard-and-sticky-port-fallback.md](/gotchas/desktop-quit-guard-and-sticky-port-fallback.md))
* **Creation**: Desktop quit guard for unsaved drafts + sticky-port fallback no longer re-saves ([gotchas/desktop-quit-guard-and-sticky-port-fallback.md](/gotchas/desktop-quit-guard-and-sticky-port-fallback.md))
## 2026-09-24

* **Creation**: Makefile Version Bump Targets (up-patch / up-minor) — Design Spec ([superpowers/specs/2026-09-24-make-version-bump-targets-design.md](/superpowers/specs/2026-09-24-make-version-bump-targets-design.md))
* **Update**: Plan: Tab Loading Indicators ([superpowers/plans/2026-09-24-tab-loading-indicators.md](/superpowers/plans/2026-09-24-tab-loading-indicators.md))
* **Update**: Web tab loading indicators ([concepts/web-tab-loading-indicators.md](/concepts/web-tab-loading-indicators.md))
* **Update**: Chat Verbosity Display — Design Spec ([superpowers/specs/2026-09-24-chat-verbosity-display-design.md](/superpowers/specs/2026-09-24-chat-verbosity-display-design.md))
* **Creation**: Web tab loading indicators ([concepts/web-tab-loading-indicators.md](/concepts/web-tab-loading-indicators.md))
* **Creation**: Plan: Tab Loading Indicators ([superpowers/plans/2026-09-24-tab-loading-indicators.md](/superpowers/plans/2026-09-24-tab-loading-indicators.md))
* **Creation**: Chat Verbosity Display — Design Spec ([superpowers/specs/2026-09-24-chat-verbosity-display-design.md](/superpowers/specs/2026-09-24-chat-verbosity-display-design.md))
* **Update**: Tab Loading Indicators — Design Spec ([superpowers/specs/2026-09-24-tab-loading-indicators-design.md](/superpowers/specs/2026-09-24-tab-loading-indicators-design.md))
* **Update**:  ([superpowers/plans/2026-09-24-browser-password-vault-phase1/10-docs-gates.md](/superpowers/plans/2026-09-24-browser-password-vault-phase1/10-docs-gates.md))
* **Update**:  ([superpowers/plans/2026-09-24-browser-password-vault-phase1/09-settings-group.md](/superpowers/plans/2026-09-24-browser-password-vault-phase1/09-settings-group.md))
* **Update**:  ([superpowers/plans/2026-09-24-browser-password-vault-phase1/08-vault-form.md](/superpowers/plans/2026-09-24-browser-password-vault-phase1/08-vault-form.md))
* **Update**:  ([superpowers/plans/2026-09-24-browser-password-vault-phase1/07-web-api.md](/superpowers/plans/2026-09-24-browser-password-vault-phase1/07-web-api.md))
* **Update**:  ([superpowers/plans/2026-09-24-browser-password-vault-phase1/06-handler-api.md](/superpowers/plans/2026-09-24-browser-password-vault-phase1/06-handler-api.md))
* **Update**:  ([superpowers/plans/2026-09-24-browser-password-vault-phase1/05-generator.md](/superpowers/plans/2026-09-24-browser-password-vault-phase1/05-generator.md))
* **Update**:  ([superpowers/plans/2026-09-24-browser-password-vault-phase1/04-url-match.md](/superpowers/plans/2026-09-24-browser-password-vault-phase1/04-url-match.md))
* **Update**:  ([superpowers/plans/2026-09-24-browser-password-vault-phase1/03-vault-api.md](/superpowers/plans/2026-09-24-browser-password-vault-phase1/03-vault-api.md))
* **Update**:  ([superpowers/plans/2026-09-24-browser-password-vault-phase1/02-store-lock.md](/superpowers/plans/2026-09-24-browser-password-vault-phase1/02-store-lock.md))
* **Update**:  ([superpowers/plans/2026-09-24-browser-password-vault-phase1/01-crypto.md](/superpowers/plans/2026-09-24-browser-password-vault-phase1/01-crypto.md))
* **Creation**: Tab Loading Indicators — Design Spec ([superpowers/specs/2026-09-24-tab-loading-indicators-design.md](/superpowers/specs/2026-09-24-tab-loading-indicators-design.md))
* **Creation**: React 19 upgrade — act() timing in tests and the skipLibCheck dependency ([gotchas/react-19-upgrade-test-timing-and-typings.md](/gotchas/react-19-upgrade-test-timing-and-typings.md))
* **Update**: Browser Password Vault — Phase 1 Implementation Plan ([superpowers/plans/2026-09-24-browser-password-vault-phase1/INDEX.md](/superpowers/plans/2026-09-24-browser-password-vault-phase1/INDEX.md))
* **Creation**: Web "All sessions" dialog slow to open (render bottleneck, child filtering + pagination) ([gotchas/web-all-sessions-dialog-slow.md](/gotchas/web-all-sessions-dialog-slow.md))
* **Creation**: Per-Chat MCP Toggle ([concepts/per-chat-mcp-toggle.md](/concepts/per-chat-mcp-toggle.md))
* **Update**: Per-session LLM spend accumulator ([gotchas/per-session-llm-spend-accumulator.md](/gotchas/per-session-llm-spend-accumulator.md))
* **Creation**: Side Pane StateKey Convention: Per-Session Scoping ([gotchas/side-pane-statekey-convention.md](/gotchas/side-pane-statekey-convention.md))
* **Update**: Web UI Mobile Layout Breakage (≤767px)" ([gotchas/web-ui-mobile-layout-breakage.md](/gotchas/web-ui-mobile-layout-breakage.md))
* **Update**: Project scoping is visibility, not mounting — React key remounts ([gotchas/project-scope-is-mounting-not-visibility.md](/gotchas/project-scope-is-mounting-not-visibility.md))
* **Deprecation**: Browser Panel Close Reopens Deleted State: Superseded by per-session side-pane scoping (2026-09-24). The cross-session propagation effect in App.tsx (which called browserActions.open(sideStateKey) for a new session when the old one was open) and the panelClosedByUser ref that caused the close-then-reopen bug were both removed. Pane open/collapsed state now lives under each session's own side:chat:<id> key in localStorage ocode.ui.sidebarPreview.v2; closing the pane in one chat never affects another chat, and switching to a different chat shows that chat's own (likely closed) pane. See gotchas/project-scope-is-mounting-not-visibility.md for the current mechanism. ([gotchas/browser-panel-close-reopens-state.md](/gotchas/browser-panel-close-reopens-state.md))
* **Update**: Embedded Browser Password Vault — Design ([superpowers/specs/2026-09-24-browser-password-vault-design.md](/superpowers/specs/2026-09-24-browser-password-vault-design.md))
* **Creation**: Per-session LLM spend accumulator ([docs/gotchas/per-session-llm-spend-accumulator.md](/docs/gotchas/per-session-llm-spend-accumulator.md))
* **Update**: Interrupted Turn Notice ([concepts/interrupted-turn-notice.md](/concepts/interrupted-turn-notice.md))
* **Update**: Auto-permission settings not applied to a running chat ([gotchas/auto-permission-settings-not-applied-to-live-chat.md](/gotchas/auto-permission-settings-not-applied-to-live-chat.md))
* **Creation**: Auto-permission settings not applied to a running chat ([gotchas/auto-permission-settings-not-applied-to-live-chat.md](/gotchas/auto-permission-settings-not-applied-to-live-chat.md))
* **Creation**: Advisor Claude Code CLI backend on web/desktop ([docs/concepts/advisor-claude-code-backend.md](/docs/concepts/advisor-claude-code-backend.md))

## 2026-09-23

* **Creation**: Embedded HTR Extension and Managed Daemon Design ([docs/superpowers/specs/2026-09-09-embedded-htr-extension-design.md](/docs/superpowers/specs/2026-09-09-embedded-htr-extension-design.md))
* **Update**: Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing + Local Media Streaming) ([gotchas/files-tab-preview-only-routing.md](/gotchas/files-tab-preview-only-routing.md))
* **Update**: Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing + Local Media Streaming) ([docs/gotchas/files-tab-preview-only-routing.md](/docs/gotchas/files-tab-preview-only-routing.md))
* **Creation**: Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing + Local Media Streaming) ([docs/gotchas/files-tab-preview-only-routing.md](/docs/gotchas/files-tab-preview-only-routing.md))
* **Update**: Multi-Use Preview — Design Spec (Draft, 2026-09-10) ([superpowers/specs/2026-09-10-preview-multipurpose-design.md](/superpowers/specs/2026-09-10-preview-multipurpose-design.md))
* **Update**: MDX Preview: Rendered as Markdown, Never Evaluated ([gotchas/mdx-preview-not-evaluated.md](/gotchas/mdx-preview-not-evaluated.md))
* **Deprecation**: Files Tab Auto-Previews Binary/Office Formats (Preview-Only Routing): Superseded by docs/gotchas/files-tab-preview-only-routing.md — duplicate created during prior edit. ([gotchas/files-tab-preview-only-routing copy.md](/gotchas/files-tab-preview-only-routing copy.md))
* **Creation**: "git commit failed: exit status 1" with no reason (git explains on stdout, not stderr) ([gotchas/git-commit-opaque-exit-status.md](/gotchas/git-commit-opaque-exit-status.md))
* **Update**: Desktop/Web Terminal Wheel Scroll Chains to the App Page (xterm.js Escape Gestures) ([gotchas/terminal-wheel-scroll-chaining.md](/gotchas/terminal-wheel-scroll-chaining.md))
* **Creation**: Desktop/Web Terminal Wheel Scroll Chains to the App Page (xterm.js Escape Gestures) ([gotchas/terminal-wheel-scroll-chaining.md](/gotchas/terminal-wheel-scroll-chaining.md))
* **Update**: Web UI Mobile Layout Breakage (≤767px)" ([gotchas/web-ui-mobile-layout-breakage.md](/gotchas/web-ui-mobile-layout-breakage.md))
* **Update**: Web UI Global Keyboard Shortcuts ([concepts/web-keyboard-shortcuts.md](/concepts/web-keyboard-shortcuts.md))
* **Creation**: Web UI Mobile Layout Breakage (≤767px) ([docs/gotchas/web-ui-mobile-layout-breakage.md](/docs/gotchas/web-ui-mobile-layout-breakage.md))
* **Update**: Web UI Mobile Layout Breakage (≤767px) ([gotchas/web-ui-mobile-layout-breakage.md](/gotchas/web-ui-mobile-layout-breakage.md))
* **Update**: Linux Sandbox Lockout: Confiner Shell Path, /dev/null, and Binary Dispatch ([gotchas/linux-sandbox-lockout-confiner-shell-devnull-dispatch.md](/gotchas/linux-sandbox-lockout-confiner-shell-devnull-dispatch.md))
* **Update**: Sandbox Writable-Root Must Exist on Disk ([gotchas/sandbox-writable-root-must-exist.md](/gotchas/sandbox-writable-root-must-exist.md))
* **Update**: Seatbelt Profile Test Coverage Gap ([gotchas/seatbelt-profile-test-coverage-gap.md](/gotchas/seatbelt-profile-test-coverage-gap.md))

## 2026-09-22

* **Update**: opencode-go per-model protocol routing & Anthropic tool schema flatness ([gotchas/opencode-go-per-model-protocol-and-anthropic-tool-schemas.md](/gotchas/opencode-go-per-model-protocol-and-anthropic-tool-schemas.md))
* **Creation**: opencode-go per-model protocol routing & Anthropic tool schema flatness ([docs/gotchas/opencode-go-per-model-protocol-and-anthropic-tool-schemas.md](/docs/gotchas/opencode-go-per-model-protocol-and-anthropic-tool-schemas.md))
* **Update**: Remote git commit/stash messages must be shell-quoted (remoteGitCommand contract) ([gotchas/remote-git-shell-quoting.md](/gotchas/remote-git-shell-quoting.md))
* **Creation**: Remote git commit/stash messages must be shell-quoted (remoteGitCommand contract) ([gotchas/remote-git-shell-quoting.md](/gotchas/remote-git-shell-quoting.md))
* **Update**: Shared tabs.json is written by every ocode server process — whole-map replace dropped projects ([gotchas/shared-tabs-json-multi-writer-clobber.md](/gotchas/shared-tabs-json-multi-writer-clobber.md))
* **Creation**: Shared tabs.json is written by every ocode server process — whole-map replace dropped projects ([gotchas/shared-tabs-json-multi-writer-clobber.md](/gotchas/shared-tabs-json-multi-writer-clobber.md))
* **Update**: TUI: skipLLM is not a render gate — fake-agent and cron replies vanish on fresh sessions ([gotchas/tui-skipllm-is-not-a-render-gate.md](/gotchas/tui-skipllm-is-not-a-render-gate.md))
* **Creation**: TUI: skipLLM is not a render gate — fake-agent and cron replies vanish on fresh sessions ([gotchas/tui-skipllm-is-not-a-render-gate.md](/gotchas/tui-skipllm-is-not-a-render-gate.md))
* **Creation**: File search result ordering: shortest path first ([concepts/file-search-shortest-path-ordering.md](/concepts/file-search-shortest-path-ordering.md))
* **Update**: Web Chat Composer Input History (↑/↓ Navigation) ([concepts/web-chat-input-history.md](/concepts/web-chat-input-history.md))
* **Update**: Web UI Global Keyboard Shortcuts ([concepts/web-keyboard-shortcuts.md](/concepts/web-keyboard-shortcuts.md))
* **Creation**: Web Chat Composer Input History (↑/↓ Navigation) ([concepts/web-chat-input-history.md](/concepts/web-chat-input-history.md))
* **Update**: Interrupted Turn Notice ([concepts/interrupted-turn-notice.md](/concepts/interrupted-turn-notice.md))
* **Creation**: Interrupted Turn Notice ([concepts/interrupted-turn-notice.md](/concepts/interrupted-turn-notice.md))
* **Creation**: MDX Preview: Rendered as Markdown, Never Evaluated ([gotchas/mdx-preview-not-evaluated.md](/gotchas/mdx-preview-not-evaluated.md))
* **Update**: Multi-Use Preview — Design Spec (Draft, 2026-09-10) ([superpowers/specs/2026-09-10-preview-multipurpose-design.md](/superpowers/specs/2026-09-10-preview-multipurpose-design.md))
* **Creation**: Main model pick also sets the global default ([gotchas/main-model-pick-also-sets-global-default.md](/gotchas/main-model-pick-also-sets-global-default.md))
* **Update**: Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing + Local Media Streaming) ([gotchas/files-tab-preview-only-routing.md](/gotchas/files-tab-preview-only-routing.md))
* **Deprecation**: Main model pick also sets the global default: Superseded — created at wrong depth (docs/docs/) by doc_write which prepends docs/; correct file written at docs/gotchas/ ([docs/gotchas/main-model-pick-also-sets-global-default.md](/docs/gotchas/main-model-pick-also-sets-global-default.md))
* **Creation**: Main model pick also sets the global default ([docs/gotchas/main-model-pick-also-sets-global-default.md](/docs/gotchas/main-model-pick-also-sets-global-default.md))
## 2026-09-21

* **Update**: Auto-Permission Enforced Categories ([concepts/auto-permission-enforced-categories.md](/concepts/auto-permission-enforced-categories.md))
* **Creation**: Persistent per-session shell for exclamation-mark commands ([concepts/persistent-shell-session.md](/concepts/persistent-shell-session.md))
* **Update**: Git stash UI: list, per-file restore, delete ([concepts/git-stash-ui.md](/concepts/git-stash-ui.md))
* **Update**: Web Composer Quick-Actions Strip ([concepts/web-composer-quick-actions.md](/concepts/web-composer-quick-actions.md))
* **Creation**: Session-tagged snapshot: override base fields but recompute ALL derived fields ([gotchas/session-snapshot-stale-derived-fields.md](/gotchas/session-snapshot-stale-derived-fields.md))
* **Deprecation**: Session-tagged snapshot: override base fields but recompute ALL derived fields: Created at wrong path (docs/docs/... due to double-prefix). Superseded by the correctly placed version at gotchas/session-snapshot-stale-derived-fields.md ([docs/gotchas/session-snapshot-stale-derived-fields.md](/docs/gotchas/session-snapshot-stale-derived-fields.md))
* **Creation**: Session-tagged snapshot: override base fields but recompute ALL derived fields ([docs/gotchas/session-snapshot-stale-derived-fields.md](/docs/gotchas/session-snapshot-stale-derived-fields.md))
* **Update**: Desktop subprocess PATH trap — bare CLI names fail under Finder/Dock-launched .app ([gotchas/desktop-subprocess-path-trap.md](/gotchas/desktop-subprocess-path-trap.md))
* **Update**: Advisor Claude Code CLI backend on web/desktop ([concepts/advisor-claude-code-backend.md](/concepts/advisor-claude-code-backend.md))
* **Creation**: Advisor Claude Code CLI backend on web/desktop ([concepts/advisor-claude-code-backend.md](/concepts/advisor-claude-code-backend.md))
* **Creation**: Desktop subprocess PATH trap — bare CLI names fail under Finder/Dock-launched .app ([gotchas/desktop-subprocess-path-trap.md](/gotchas/desktop-subprocess-path-trap.md))
* **Creation**: Auto-Permission Enforced Categories ([concepts/auto-permission-enforced-categories.md](/concepts/auto-permission-enforced-categories.md))
* **Update**: Remote Persistent Sessions and Terminals ([concepts/remote-persistent-sessions-terminals.md](/concepts/remote-persistent-sessions-terminals.md))
* **Creation**: Session-bound dialogs scope to their chat session ([concepts/session-bound-dialog-scoping.md](/concepts/session-bound-dialog-scoping.md))
* **Creation**: Terminal find bar stuck on "No matches" ([gotchas/terminal-find-stuck-no-matches.md](/gotchas/terminal-find-stuck-no-matches.md))
* **Update**: Web UI Global Keyboard Shortcuts ([concepts/web-keyboard-shortcuts.md](/concepts/web-keyboard-shortcuts.md))
* **Update**: Web UI Mobile Layout Breakage (≤767px) ([gotchas/web-ui-mobile-layout-breakage.md](/gotchas/web-ui-mobile-layout-breakage.md))
* **Creation**: Auto-Permission Judge — Credential Material Is Withheld from the Judge Context ([gotchas/auto-permission-judge-withholds-credentials.md](/gotchas/auto-permission-judge-withholds-credentials.md))
* **Creation**: Web UI Global Keyboard Shortcuts ([concepts/web-keyboard-shortcuts.md](/concepts/web-keyboard-shortcuts.md))
* **Update**: Server-Side Auto-Continue Loop ([concepts/server-auto-continue.md](/concepts/server-auto-continue.md))
* **Creation**: Remote SSH chat ignored the desktop active profile — proxy silent on window→profile mapping ([gotchas/remote-ssh-chat-profile-not-applied.md](/gotchas/remote-ssh-chat-profile-not-applied.md))
* **Update**: Concurrent session writers — conflict semantics and recovery ([gotchas/session-writers-conflict-recovery.md](/gotchas/session-writers-conflict-recovery.md))
* **Update**: Cross-process session activity sync (revision revalidation) ([concepts/cross-process-session-sync.md](/concepts/cross-process-session-sync.md))
* **Creation**: Cross-process session activity sync (revision revalidation) ([concepts/cross-process-session-sync.md](/concepts/cross-process-session-sync.md))
* **Creation**: Web Composer Quick-Actions Strip ([concepts/web-composer-quick-actions.md](/concepts/web-composer-quick-actions.md))
* **Update**: Files tab: directory expansion persists across project switches — but only after the fix ([gotchas/files-tab-tree-expansion-persistence.md](/gotchas/files-tab-tree-expansion-persistence.md))
* **Creation**: Files tab: directory expansion persists across project switches — but only after the fix ([gotchas/files-tab-tree-expansion-persistence.md](/gotchas/files-tab-tree-expansion-persistence.md))
* **Creation**: Git stash UI: list, per-file restore, delete ([concepts/git-stash-ui.md](/concepts/git-stash-ui.md))
* **Update**: Local Speech-to-Text (In-App Dictation) — Design ([superpowers/specs/2026-09-21-speech-to-text-design.md](/superpowers/specs/2026-09-21-speech-to-text-design.md))
* **Creation**: Speech-to-Text (Dictation) Design ([superpowers/specs/2026-09-21-speech-to-text-design.md](/superpowers/specs/2026-09-21-speech-to-text-design.md))
* **Update**: Git index.lock contention in ocode git subprocesses ([gotchas/git-index-lock-contention.md](/gotchas/git-index-lock-contention.md))
* **Creation**: Git index.lock contention in ocode git subprocesses ([gotchas/git-index-lock-contention.md](/gotchas/git-index-lock-contention.md))
## 2026-09-20

* **Creation**: Radix modal focus trap silently breaks the execCommand clipboard fallback ([gotchas/radix-modal-focus-trap-breaks-execCommand-clipboard.md](/gotchas/radix-modal-focus-trap-breaks-execCommand-clipboard.md))
* **Update**: Profile switch does not affect an already-open chat session (window-id divergence) ([gotchas/profile-switch-window-id-divergence.md](/gotchas/profile-switch-window-id-divergence.md))
* **Creation**: Profile switch does not affect an already-open chat session (window-id divergence) ([gotchas/profile-switch-window-id-divergence.md](/gotchas/profile-switch-window-id-divergence.md))
* **Update**: Web/Desktop Chat Went Stale Because a Dead SSE Body Never Errors ([gotchas/web-sse-stream-silent-death-liveness.md](/gotchas/web-sse-stream-silent-death-liveness.md))
* **Update**: Part 05 — Frontend Status on Activation + Streaming Watchdog ([superpowers/plans/2026-08-12-multiproject-event-architecture/05-frontend-status-streaming.md](/superpowers/plans/2026-08-12-multiproject-event-architecture/05-frontend-status-streaming.md))
* **Update**: Part 03 — Async Bootstrap, Turn State Machine, Reconcile & Status Endpoints ([superpowers/plans/2026-08-12-multiproject-event-architecture/03-async-bootstrap-turn-state.md](/superpowers/plans/2026-08-12-multiproject-event-architecture/03-async-bootstrap-turn-state.md))
* **Update**: Session Re-key via /reset-id ([docs/concepts/session-rekey-reset-id.md](/docs/concepts/session-rekey-reset-id.md))
* **Creation**: Session Re-key via /reset-id ([docs/concepts/session-rekey-reset-id.md](/docs/concepts/session-rekey-reset-id.md))
* **Update**: Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing + Local Media Streaming) ([gotchas/files-tab-preview-only-routing.md](/gotchas/files-tab-preview-only-routing.md))
* **Update**: Project scoping is visibility, not mounting — React key remounts ([gotchas/project-scope-is-mounting-not-visibility.md](/gotchas/project-scope-is-mounting-not-visibility.md))
* **Update**: PDF viewer zoom, Space navigation, and find ([concepts/pdf-viewer-zoom-find.md](/concepts/pdf-viewer-zoom-find.md))
* **Creation**: Project scoping is visibility, not mounting — React key remounts ([gotchas/project-scope-is-mounting-not-visibility.md](/gotchas/project-scope-is-mounting-not-visibility.md))
* **Update**: Concurrent session writers — conflict semantics and recovery ([gotchas/session-writers-conflict-recovery.md](/gotchas/session-writers-conflict-recovery.md))
* **Deprecation**: Concurrent session writers — conflict semantics and recovery: Path was incorrect — doc_write paths are relative to docs/ root. This file was created by mistake when the path docs/gotchas/session-writers-conflict-recovery.md was used (resolving to docs/docs/...); the correct location is gotchas/session-writers-conflict-recovery.md. Superseded by the update to that path. ([docs/gotchas/session-writers-conflict-recovery.md](/docs/gotchas/session-writers-conflict-recovery.md))
* **Creation**: Concurrent session writers — conflict semantics and recovery ([docs/gotchas/session-writers-conflict-recovery.md](/docs/gotchas/session-writers-conflict-recovery.md))
* **Update**: PDF viewer zoom, Space navigation, and find ([docs/concepts/pdf-viewer-zoom-find.md](/docs/concepts/pdf-viewer-zoom-find.md))
* **Creation**: PDF viewer zoom, Space navigation, and find ([docs/concepts/pdf-viewer-zoom-find.md](/docs/concepts/pdf-viewer-zoom-find.md))
* **Creation**: PDF viewer zoom, Space navigation, and find ([concepts/pdf-viewer-zoom-find.md](/concepts/pdf-viewer-zoom-find.md))
## 2026-09-19

* **Deprecation**: PDF viewer zoom, Space navigation, and find: Path was incorrect — doc_write paths are relative to docs/ root. Replaced by concepts/pdf-viewer-zoom-find.md. ([docs/concepts/pdf-viewer-zoom-find.md](/docs/concepts/pdf-viewer-zoom-find.md))
* **Creation**: PDF viewer zoom, Space navigation, and find ([concepts/pdf-viewer-zoom-find.md](/concepts/pdf-viewer-zoom-find.md))
* **Creation**: PDF viewer zoom, Space navigation, and find ([docs/concepts/pdf-viewer-zoom-find.md](/docs/concepts/pdf-viewer-zoom-find.md))
* **Update**: Web UI Mobile Layout Breakage (≤767px) ([gotchas/web-ui-mobile-layout-breakage.md](/gotchas/web-ui-mobile-layout-breakage.md))
* **Creation**: Host and project scoping for web session/project reads ([concepts/web-session-host-scoping.md](/concepts/web-session-host-scoping.md))
* **Creation**: Per-session LLM spend accumulator ([gotchas/per-session-llm-spend-accumulator.md](/gotchas/per-session-llm-spend-accumulator.md))
* **Creation**: Web UI Mobile Layout Breakage (≤767px) ([gotchas/web-ui-mobile-layout-breakage.md](/gotchas/web-ui-mobile-layout-breakage.md))
* **Update**: Web model picker must open from the cached model list, not a live refresh ([gotchas/web-model-picker-cached-not-live.md](/gotchas/web-model-picker-cached-not-live.md))
* **Creation**: Web model picker must open from the cached model list, not a live refresh ([docs/gotchas/web-model-picker-cached-not-live.md](/docs/gotchas/web-model-picker-cached-not-live.md))
* **Creation**: Web Ask Dialog LLM Context Preview ([concepts/web-ask-dialog-llm-context.md](/concepts/web-ask-dialog-llm-context.md))
* **Creation**: After Compaction: Publish Status Snapshot + Record Estimate or Context Gauge Goes Stale ([gotchas/compaction-context-gauge-stale-after-splice.md](/gotchas/compaction-context-gauge-stale-after-splice.md))
* **Creation**: Web Compact Feedback — Final Implementation ([superpowers/specs/2026-09-19-web-compact-feedback-final.md](/superpowers/specs/2026-09-19-web-compact-feedback-final.md))
* **Deprecation**: Web Compact Feedback Design: Superseded by the actual implementation which changed the design significantly: (1) backend now publishes a status snapshot after compaction via publishTurnStatusSnapshot so the Context gauge stays accurate, (2) the "complete" compaction state was removed — only "active" and "error" states remain, (3) the compaction notice moved from a composer bar into the transcript as a persisted [ocode:compaction-summary] system message rendered by CompactionNotice.tsx, (4) clearCompaction was added to compactionState.ts. See superseding spec and gotcha below. ([superpowers/specs/2026-09-18-web-compact-feedback-design.md](/superpowers/specs/2026-09-18-web-compact-feedback-design.md))
* **Update**: Files-Tab OS-Native Reveal (Open/Show in Finder/Explorer/File Manager) ([gotchas/files-tab-os-native-reveal.md](/gotchas/files-tab-os-native-reveal.md))
* **Creation**: Files-Tab OS-Native Reveal (Open/Show in Finder/Explorer/File Manager) ([gotchas/files-tab-os-native-reveal.md](/gotchas/files-tab-os-native-reveal.md))
* **Update**: Discovery TypeSafe Relevance Judge ([concepts/discovery-typesafe-judge.md](/concepts/discovery-typesafe-judge.md))
* **Update**: Remote Persistent Sessions and Terminals Design ([superpowers/specs/2026-09-18-remote-persistent-sessions-terminals-design.md](/superpowers/specs/2026-09-18-remote-persistent-sessions-terminals-design.md))
* **Creation**: Terminal close semantics and OSC title source ([gotchas/terminal-close-and-osc-title.md](/gotchas/terminal-close-and-osc-title.md))
* **Creation**: Doc Search Relevance Judge ([concepts/doc-search-relevance-judge.md](/concepts/doc-search-relevance-judge.md))
## 2026-09-18

* **Update**: ChatPanel & LogPanel Autoscroll Bounce / Freeze ([gotchas/autoscroll-bounce.md](/gotchas/autoscroll-bounce.md))
* **Update**: ChatPanel Autoscroll Bounce / Freeze ([gotchas/autoscroll-bounce.md](/gotchas/autoscroll-bounce.md))
* **Update**: Discovery TypeSafe Relevance Judge ([concepts/discovery-typesafe-judge.md](/concepts/discovery-typesafe-judge.md))
* **Creation**: Discovery Web Surfaces ([concepts/discovery-web-surfaces.md](/concepts/discovery-web-surfaces.md))
* **Creation**: ONNX Runtime POSIX telemetry writes `:memory:.ses` into process cwd ([gotchas/onnx-runtime-telemetry-memory-ses.md](/gotchas/onnx-runtime-telemetry-memory-ses.md))
* **Deprecation**: Remote SSH terminal 502s from provisioning and path bugs: Superseded: doc tools take bundle-relative paths, so this write landed at docs/docs/gotchas/remote-terminal-502-provisioning.md instead of docs/gotchas/.... The correct doc is at gotchas/remote-terminal-502-provisioning.md (bundle-relative). ([docs/gotchas/remote-terminal-502-provisioning.md](/docs/gotchas/remote-terminal-502-provisioning.md))
* **Creation**: Remote SSH terminal 502s from provisioning and path bugs ([gotchas/remote-terminal-502-provisioning.md](/gotchas/remote-terminal-502-provisioning.md))
* **Creation**: Remote SSH terminal 502s from provisioning and path bugs ([docs/gotchas/remote-terminal-502-provisioning.md](/docs/gotchas/remote-terminal-502-provisioning.md))
* **Creation**: Project/Endpoint Isolation — one project must never halt another ([gotchas/project-endpoint-isolation.md](/gotchas/project-endpoint-isolation.md))
* **Update**: opencode-go per-model protocol routing & Anthropic tool schema flatness ([gotchas/opencode-go-per-model-protocol-and-anthropic-tool-schemas.md](/gotchas/opencode-go-per-model-protocol-and-anthropic-tool-schemas.md))
* **Update**: Server-Side Auto-Continue Loop ([concepts/server-auto-continue.md](/concepts/server-auto-continue.md))
* **Update**: Speech playback ([tts-speech-playback.md](/tts-speech-playback.md))
* **Creation**: espeak-ng N_PATH_HOME buffer truncates long data paths → exit(1) ([gotchas/kokoro-espeak-ng-path-limit.md](/gotchas/kokoro-espeak-ng-path-limit.md))
* **Update**: Sandbox Permission Mode ([concepts/sandbox-permission-mode.md](/concepts/sandbox-permission-mode.md))
* **Creation**: Remote Persistent Sessions and Terminals ([concepts/remote-persistent-sessions-terminals.md](/concepts/remote-persistent-sessions-terminals.md))
* **Deprecation**: System Permissions Settings Section (macOS TCC + cross-platform): Document was created at the wrong path (docs/docs/...). The correct canonical location is superpowers/specs/2026-09-18-system-permissions-design.md. Content has been moved; this copy is superseded. ([docs/superpowers/specs/2026-09-18-system-permissions-design.md](/docs/superpowers/specs/2026-09-18-system-permissions-design.md))
* **Creation**: System Permissions Settings Section (macOS TCC + cross-platform) ([superpowers/specs/2026-09-18-system-permissions-design.md](/superpowers/specs/2026-09-18-system-permissions-design.md))
* **Creation**: Discovery TypeSafe Relevance Judge ([concepts/discovery-typesafe-judge.md](/concepts/discovery-typesafe-judge.md))
* **Update**: Web Compact Feedback Design ([superpowers/specs/2026-09-18-web-compact-feedback-design.md](/superpowers/specs/2026-09-18-web-compact-feedback-design.md))
* **Creation**: Web Compact Feedback Design ([superpowers/specs/2026-09-18-web-compact-feedback-design.md](/superpowers/specs/2026-09-18-web-compact-feedback-design.md))
## 2026-09-17

* **Creation**: Auto-continue turn transcript rebase: capture base length before the loop ([gotchas/auto-continue-turn-transcript-rebase.md](/gotchas/auto-continue-turn-transcript-rebase.md))
* **Creation**: Server-Side Auto-Continue Loop ([concepts/server-auto-continue.md](/concepts/server-auto-continue.md))
* **Update**: Question-answer transcript echo: client must mirror server payload shape ([gotchas/question-answer-transcript-echo.md](/gotchas/question-answer-transcript-echo.md))
* **Update**: Computer use ([computer-use.md](/computer-use.md))
* **Update**: Remote Project Paths Must Not Enter the Local Filesystem Trust Boundary ([gotchas/remote-project-path-trust-boundary.md](/gotchas/remote-project-path-trust-boundary.md))
* **Update**: Terminal Shells Survive Page Reload (Detach / Reattach) ([architecture/terminal-detach-reattach.md](/architecture/terminal-detach-reattach.md))
* **Update**: Multi-Use Preview — Design Spec (Draft, 2026-09-10) ([superpowers/specs/2026-09-10-preview-multipurpose-design.md](/superpowers/specs/2026-09-10-preview-multipurpose-design.md))
* **Update**: Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing + Local Media Streaming) ([gotchas/files-tab-preview-only-routing.md](/gotchas/files-tab-preview-only-routing.md))
* **Update**: Auto-Permission — Interpreter Scripts in Compound Commands ([gotchas/auto-permission-interpreter-scripts-in-compound-commands.md](/gotchas/auto-permission-interpreter-scripts-in-compound-commands.md))
* **Creation**: Bash Control-Flow Loops Are Not Commands ([gotchas/bash-control-flow-loops-are-not-commands.md](/gotchas/bash-control-flow-loops-are-not-commands.md))
* **Update**: Speech playback ([tts-speech-playback.md](/tts-speech-playback.md))
* **Update**: ChatPanel Autoscroll Bounce/Freeze ([gotchas/autoscroll-bounce.md](/gotchas/autoscroll-bounce.md))
* **Creation**: Pinned pip requirement sets need an upper Python bound, not just a minimum ([gotchas/tts-pinned-python-upper-bound-needed.md](/gotchas/tts-pinned-python-upper-bound-needed.md))
* **Update**: Pending ask recovery from live session state (sentinel-less transcript) ([gotchas/pending-ask-recovery-live-session-state.md](/gotchas/pending-ask-recovery-live-session-state.md))
* **Creation**: opencode-go per-model protocol routing & Anthropic tool schema flatness ([gotchas/opencode-go-per-model-protocol-and-anthropic-tool-schemas.md](/gotchas/opencode-go-per-model-protocol-and-anthropic-tool-schemas.md))
* **Creation**: Linux Sandbox Lockout: Confiner Shell Path, /dev/null, and Binary Dispatch ([gotchas/linux-sandbox-lockout-confiner-shell-devnull-dispatch.md](/gotchas/linux-sandbox-lockout-confiner-shell-devnull-dispatch.md))
* **Creation**: Pending ask recovery from live session state (sentinel-less transcript) ([docs/gotchas/pending-ask-recovery-live-session-state.md](/docs/gotchas/pending-ask-recovery-live-session-state.md))
* **Creation**: Sandbox Permission Mode ([concepts/sandbox-permission-mode.md](/concepts/sandbox-permission-mode.md))
* **Update**: Sandbox git push — SSH agent inheritance and fail-closed TTY prompts ([gotchas/sandbox-git-push-ssh-agent-tty.md](/gotchas/sandbox-git-push-ssh-agent-tty.md))
* **Creation**: Port forwards Disable/Enable: URL composed past query + supervisor retained-terminal collision ([gotchas/port-forwards-url-composition-and-supervisor-restart.md](/gotchas/port-forwards-url-composition-and-supervisor-restart.md))
## 2026-09-16

* **Creation**: PDF preview fails on WebKit with `undefined is not a function (near '...e of t...')` ([gotchas/pdf-preview-webkit-async-iterator.md](/gotchas/pdf-preview-webkit-async-iterator.md))
* **Update**: Speech playback ([tts-speech-playback.md](/tts-speech-playback.md))
* **Creation**: Speech Rendered Text Extraction — DOM, Not Markdown Source ([gotchas/speech-rendered-text-extraction.md](/gotchas/speech-rendered-text-extraction.md))
* **Update**: TTS Speech Playback Design Specification ([superpowers/specs/2026-09-09-tts-speech-playback-design.md](/superpowers/specs/2026-09-09-tts-speech-playback-design.md))
* **Update**: Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing + Local Media Streaming) ([gotchas/files-tab-preview-only-routing.md](/gotchas/files-tab-preview-only-routing.md))
* **Creation**: Remote-project port forwards in the web/desktop UI ([superpowers/specs/2026-09-16-remote-project-port-forwards-design.md](/superpowers/specs/2026-09-16-remote-project-port-forwards-design.md))
* **Update**: Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing) ([gotchas/files-tab-preview-only-routing.md](/gotchas/files-tab-preview-only-routing.md))
* **Update**: Multi-Use Preview — Design Spec (Draft, 2026-09-10) ([superpowers/specs/2026-09-10-preview-multipurpose-design.md](/superpowers/specs/2026-09-10-preview-multipurpose-design.md))
* **Deprecation**: Files Tab Auto-Previews Binary/Office Formats (Preview-Only Routing): Misplaced by a path error: doc tools take bundle-relative paths (root is docs/), so this write landed at docs/docs/gotchas/... instead of docs/gotchas/.... Superseded by the correct doc at gotchas/files-tab-preview-only-routing.md; safe to remove via /docs cleanup. ([docs/gotchas/files-tab-preview-only-routing.md](/docs/gotchas/files-tab-preview-only-routing.md))
* **Creation**: Files Tab Auto-Previews Binary/Office Formats (Preview-Only Routing) ([gotchas/files-tab-preview-only-routing.md](/gotchas/files-tab-preview-only-routing.md))
* **Creation**: Files Tab Auto-Previews Binary/Office Formats (Preview-Only Routing) ([docs/gotchas/files-tab-preview-only-routing.md](/docs/gotchas/files-tab-preview-only-routing.md))
* **Creation**: Git action errors "come out then disappear by themselves" — TUI truncation, sticky status, and background-refresh error clearing ([gotchas/git-action-errors-disappear.md](/gotchas/git-action-errors-disappear.md))
## 2026-09-14

* **Creation**: TUI Leaves Mouse Tracking On After Silent Exit — Diagnose via tui-crash.log ([gotchas/tui-mouse-garbage-after-idle-crash-log.md](/gotchas/tui-mouse-garbage-after-idle-crash-log.md))
* **Creation**: Auto-Permission — Interpreter Network Effects Denied Loopback and host:port Targets ([gotchas/auto-permission-interpreter-network-loopback.md](/gotchas/auto-permission-interpreter-network-loopback.md))
* **Creation**: Remote Terminal Custom Port Omitted from WebSocket ([gotchas/remote-terminal-custom-port-omitted.md](/gotchas/remote-terminal-custom-port-omitted.md))
* **Creation**: Browser Panel Close Reopens Deleted State ([gotchas/browser-panel-close-reopens-state.md](/gotchas/browser-panel-close-reopens-state.md))
* **Update**: Remote Project Paths Must Not Enter the Local Filesystem Trust Boundary ([gotchas/remote-project-path-trust-boundary.md](/gotchas/remote-project-path-trust-boundary.md))
* **Creation**: Remote Project Paths Must Not Enter the Local Filesystem Trust Boundary ([gotchas/remote-project-path-trust-boundary.md](/gotchas/remote-project-path-trust-boundary.md))
* **Creation**: LSP Diagnostics Marked Reported Before Emission ([gotchas/lsp-diagnostics-marked-reported-before-emission.md](/gotchas/lsp-diagnostics-marked-reported-before-emission.md))
* **Creation**: TUI Selection Context Lost on Double Preparation ([gotchas/tui-selection-context-lost-on-double-preparation.md](/gotchas/tui-selection-context-lost-on-double-preparation.md))
## 2026-09-13

* **Update**: AGENTS.md Data Storage / README API list — open session tabs now server-side via bulk `GET/PUT /api/tabs` (`tabs.json`), replacing per-origin `localStorage`; fixes empty tab bar on shared Tailscale URL
* **Update**: Computer use ([docs/computer-use.md](/docs/computer-use.md))
## 2026-09-11

* **Creation**: TTS License Acceptance Must Validate the Exact License Text Hash ([gotchas/tts-license-acceptance-hash-validation.md](/gotchas/tts-license-acceptance-hash-validation.md))
## 2026-09-09

* **Update**: TUI Sidebar Title Expand/Collapse Design ([docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md](/docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md))
* **Deprecation**: TUI Sidebar Title Expand/Collapse Design: Resetting to active status per user redesign decision ([docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md](/docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md))
* **Creation**: TUI Sidebar Title Expand/Collapse Design ([docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md](/docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md))
* **Update**: TUI Sidebar Title Expand/Collapse Design ([superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md](/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md))
* **Deprecation**: TUI Sidebar Title Expand/Collapse Design: User decided: expansion is transient; /new, /clear, /session load, and active-session replacement all reset to collapsed; no session JSON persistence. Contradictory phrases about session JSON restoration need updating. ([superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md](/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md))
* **Update**: TTS Speech Playback Design Specification ([superpowers/specs/2026-09-09-tts-speech-playback-design.md](/superpowers/specs/2026-09-09-tts-speech-playback-design.md))
* **Fix**: TTS Speech Playback Design Specification deprecation corrected - removed `status: deprecated` and `deprecated_reason` from frontmatter; restored to active status. Index and log updated. ([superpowers/specs/2026-09-09-tts-speech-playback-design.md](/superpowers/specs/2026-09-09-tts-speech-playback-design.md))
* **Update**: TTS Speech Playback Design Specification ([docs/superpowers/specs/2026-09-09-tts-speech-playback-design.md](/docs/superpowers/specs/2026-09-09-tts-speech-playback-design.md))

* **Creation**: TTS Speech Playback Design Specification ([docs/superpowers/specs/2026-09-09-tts-speech-playback-design.md](/docs/superpowers/specs/2026-09-09-tts-speech-playback-design.md))
* **Update**:  ([docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md](/docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md))
* **Update**: TUI Sidebar Title Expand/Collapse Design (Updated) ([docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md](/docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md))
* **Creation**: TUI Sidebar Title Expand/Collapse Design (Updated) ([docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md](/docs/superpowers/specs/2026-09-09-tui-sidebar-title-expand-design.md))
* **Creation**: Chrome Tab Hang — Unbounded CDP Calls, JS Dialogs, Invisible Popups ([gotchas/chrome-tab-hang-unbounded-cdp-call.md](/gotchas/chrome-tab-hang-unbounded-cdp-call.md))
* **Update**: Browser Chrome CDP design ([docs/superpowers/specs/2026-08-31-browser-chrome-cdp-design.md](/docs/superpowers/specs/2026-08-31-browser-chrome-cdp-design.md)) — Conn.Call default deadline, JS dialog auto-accept, popup/middle-click discovery via Target.setDiscoverTargets
* **Update**: Remote SSH Phase 2 — web ([docs/superpowers/specs/2026-08-29-remote-ssh/03-phase2-web.md](/docs/superpowers/specs/2026-08-29-remote-ssh/03-phase2-web.md)) — browse origin port now recorded in serve.json (`browsePort`) and forwarded by the SSH tunnel on the same port number; browser panel enabled in remote (SSH + WSL) sessions

## 2026-09-08

* **Creation**: Terminal Shells Survive Page Reload (Detach / Reattach) ([docs/architecture/terminal-detach-reattach.md](/docs/architecture/terminal-detach-reattach.md))
* **Update**: Changes Tab ([docs/changes-tab.md](/docs/changes-tab.md)) — bash recorder: per-call baseline, ns mtime, same-size edits no longer dropped; sub-agents share the parent registry; agent rebuilds/session loads re-bind the snapshot store via Store.SwitchSession
* **Creation**: Permission Evaluation and Unknown Tool Guard ([gotchas/permission-evaluation-and-unknown-tool-guard.md](/gotchas/permission-evaluation-and-unknown-tool-guard.md))
* **Update**: Session Storage Critical Issues ([docs/gotchas/session-storage-critical-issues.md](/docs/gotchas/session-storage-critical-issues.md))
* **Creation**: Test Update Doc ([docs/gotchas/test-update.md](/docs/gotchas/test-update.md))
* **Creation**: Session Storage Critical Issues ([docs/gotchas/session-storage-critical-issues.md](/docs/gotchas/session-storage-critical-issues.md))
* **Creation**: Web ask dialogs: broadcast resolved before continuation ([docs/gotchas/web-ask-dialog-resolved-before-continuation.md](/docs/gotchas/web-ask-dialog-resolved-before-continuation.md)) — question_resolved now fires before agent.Step so the QuestionDialog closes on answer
* **Deprecation**: Terminal History Persistence and Restore: Replaced by corrected version at terminal-history-persistence-and-restore.md per user instructions ([docs/docs/terminal-history-persistence-and-restore.md](/docs/docs/terminal-history-persistence-and-restore.md))
* **Update**: Terminal History Persistence and Restore ([docs/terminal-history-persistence-and-restore.md](/docs/terminal-history-persistence-and-restore.md))
* **Creation**: Terminal History Persistence and Restore ([docs/terminal-history-persistence-and-restore.md](/docs/terminal-history-persistence-and-restore.md))
* **Update**: Terminal History Persistence and Restore ([docs/docs/terminal-history-persistence-and-restore.md](/docs/docs/terminal-history-persistence-and-restore.md))
* **Deprecation**: Terminal History Persistence and Restore: Replacing with corrected version per user instructions ([docs/docs/terminal-history-persistence-and-restore.md](/docs/docs/terminal-history-persistence-and-restore.md))
* **Creation**: Terminal History Persistence and Restore ([docs/docs/terminal-history-persistence-and-restore.md](/docs/docs/terminal-history-persistence-and-restore.md))
* **Deprecation**: Permission Evaluation and Unknown Tool Guard: Accidental deprecation from earlier call — document was just updated with new content, not deprecated. Re-verified: doc_content is current and enhanced. ([docs/gotchas/permission-evaluation-and-unknown-tool-guard.md](/docs/gotchas/permission-evaluation-and-unknown-tool-guard.md))
* **Update**: Permission Evaluation and Unknown Tool Guard ([docs/gotchas/permission-evaluation-and-unknown-tool-guard.md](/docs/gotchas/permission-evaluation-and-unknown-tool-guard.md))
* **Deprecation**: Permission Evaluation and Unknown Tool Guard: Already updated — no further action needed ([docs/gotchas/permission-evaluation-and-unknown-tool-guard.md](/docs/gotchas/permission-evaluation-and-unknown-tool-guard.md))
* **Creation**: Permission Evaluation and Unknown Tool Guard ([docs/gotchas/permission-evaluation-and-unknown-tool-guard.md](/docs/gotchas/permission-evaluation-and-unknown-tool-guard.md))
* **Update**: Permission Evaluation and Unknown Tool Guard ([gotchas/permission-evaluation-and-unknown-tool-guard.md](/gotchas/permission-evaluation-and-unknown-tool-guard.md))
## 2026-09-06

* **Creation**: Worktree-Based Parallel Feature Development ([architecture/worktree-based-parallel-feature-development.md](/architecture/worktree-based-parallel-feature-development.md))
* **Creation**: Sandbox git push — SSH agent inheritance and fail-closed TTY prompts ([gotchas/sandbox-git-push-ssh-agent-tty.md](/gotchas/sandbox-git-push-ssh-agent-tty.md))
## 2026-09-05

* **Update**: Auto-Permission Prompt Prose Code Audit Gap ([gotchas/auto-permission-prompt-prose-code-audit-gap.md](/gotchas/auto-permission-prompt-prose-code-audit-gap.md))
* **Update**: Seatbelt Profile Test Coverage Gap ([gotchas/seatbelt-profile-test-coverage-gap.md](/gotchas/seatbelt-profile-test-coverage-gap.md))
* **Update**: Version-Changelog Mismatch ([gotchas/version-changelog-mismatch.md](/gotchas/version-changelog-mismatch.md))
* **Creation**: Auto-Permission Prompt Prose Code Audit Gap ([gotchas/auto-permission-prompt-prose-code-audit-gap.md](/gotchas/auto-permission-prompt-prose-code-audit-gap.md))
* **Creation**: Seatbelt Profile Test Coverage Gap ([gotchas/seatbelt-profile-test-coverage-gap.md](/gotchas/seatbelt-profile-test-coverage-gap.md))
* **Creation**: Version-Changelog Mismatch ([gotchas/version-changelog-mismatch.md](/gotchas/version-changelog-mismatch.md))
* **Creation**: Auto-Permission Dependency Binaries — Policy Decision ([gotchas/auto-permission-dependency-bin-policy.md](/gotchas/auto-permission-dependency-bin-policy.md))
* **Creation**: Auto-Permission Prompt — Load-Semantics Flip ([gotchas/auto-permission-prompt-load-semantics-flip.md](/gotchas/auto-permission-prompt-load-semantics-flip.md))
## 2026-09-03

* **Creation**: Session Storage Critical Issues ([gotchas/session-storage-critical-issues.md](/gotchas/session-storage-critical-issues.md))
* **Creation**: Sandbox Writable-Root Must Exist on Disk ([gotchas/sandbox-writable-root-must-exist.md](/gotchas/sandbox-writable-root-must-exist.md))
## 2026-09-02

* **Creation**: Radix Select Empty String Sentinel ([gotchas/radix-select-empty-string-sentinel.md](/gotchas/radix-select-empty-string-sentinel.md))
* **Creation**: Browser Panel — Documentation vs. Landed Behavior Mismatch ([gotchas/browser-panel-transition-inconsistency.md](/gotchas/browser-panel-transition-inconsistency.md))
* **Creation**: OKF Naming Convention Enforcement ([okf/_schema/naming-convention-enforcement.md](/okf/_schema/naming-convention-enforcement.md))
* **Creation**: Auto-Permission Prompt — TOCTOU Install Race ([gotchas/auto-permission-prompt-atomic-race.md](/gotchas/auto-permission-prompt-atomic-race.md))
## 2026-09-01

* **Creation**: Shell Sandbox Is Write-Integrity Only, Not Confidentiality ([architecture/shell-sandbox-integrity-only-mode.md](/architecture/shell-sandbox-integrity-only-mode.md))
* **Creation**: PATH Shadowing Can Bypass Sandbox Discovery ([gotchas/shell-sandbox-path-shadowing.md](/gotchas/shell-sandbox-path-shadowing.md))
* **Creation**: Shell Execution Must Set cmd.Dir to Agent Workdir ([gotchas/shell-sandbox-working-directory-not-set.md](/gotchas/shell-sandbox-working-directory-not-set.md))
* **Creation**: BashTool Dynamic Permission Manager Resolution ([gotchas/shell-sandbox-dynamic-bash-permissions.md](/gotchas/shell-sandbox-dynamic-bash-permissions.md))
* **Creation**: Writable-Root Validation Prevents Confinement Defeat ([gotchas/shell-sandbox-writable-root-validation.md](/gotchas/shell-sandbox-writable-root-validation.md))
* **Creation**: Shell Sandbox Backend Availability Status ([architecture/shell-sandbox-backend-status.md](/architecture/shell-sandbox-backend-status.md))
## 2026-08-31

* **Creation**: Concurrent File Editing Risk — Multiple Writers in Same Checkout ([gotchas/concurrent-file-editing-risk.md](/gotchas/concurrent-file-editing-risk.md))
* **Creation**: Auto-Permission — Judge Must See the Session WorkDir, Not the Process CWD ([gotchas/auto-permission-judge-process-cwd.md](/gotchas/auto-permission-judge-process-cwd.md))
* **Update**: Plugin Auto-Permission — Arbitrary Execution Risk ([gotchas/plugin-auto-permission-security.md](/gotchas/plugin-auto-permission-security.md))
* **Creation**: Desktop Single Instance Lock ([architecture/desktop-single-instance.md](/architecture/desktop-single-instance.md))
* **Creation**: Terminal Shells Survive Page Reload (Detach / Reattach) ([architecture/terminal-detach-reattach.md](/architecture/terminal-detach-reattach.md))
* **Update**: V1 Connection Cap Exclusion — Embedded Browser Panel ([architecture/v1-connection-cap-exclusion.md](/architecture/v1-connection-cap-exclusion.md))
* **Creation**: Auto-Permission — Interpreter Scripts in Compound Commands ([gotchas/auto-permission-interpreter-scripts-in-compound-commands.md](/gotchas/auto-permission-interpreter-scripts-in-compound-commands.md))
* **Creation**: Embedded Browser — WebSocket Proxy 404 ([gotchas/embedded-browser-websocket-proxy-404.md](/gotchas/embedded-browser-websocket-proxy-404.md))
* **Creation**: V1 Connection Cap Exclusion — Embedded Browser Panel ([architecture/v1-connection-cap-exclusion.md](/architecture/v1-connection-cap-exclusion.md))
## 2026-08-30

* **Creation**: Journal DB Connection Pool Leak ([gotchas/journal-db-connection-pool-leak.md](/gotchas/journal-db-connection-pool-leak.md))
* **Creation**: Git ext:: Transport Auto-Permission Bypass ([gotchas/git-ext-transport-auto-allow-bypass.md](/gotchas/git-ext-transport-auto-allow-bypass.md))
## 2026-08-28

* **Update**: Plugin Auto-Permission — Arbitrary Execution Risk ([gotchas/plugin-auto-permission-security.md](/gotchas/plugin-auto-permission-security.md))
* **Creation**: Chat Input — Queued Messages Lost on Submission Failure ([gotchas/chat-input-message-loss.md](/gotchas/chat-input-message-loss.md))
* **Creation**: ocode debug CLI Reference ([guides/debug-cli.md](/guides/debug-cli.md))
* **Update**: Plugin Removal — Root Directory Deletion Risk ([gotchas/plugin-removal-root-deletion.md](/gotchas/plugin-removal-root-deletion.md))
* **Update**: Debug Instrumentation Ships Unconditionally ([gotchas/debug-instrumentation-ships-unconditionally.md](/gotchas/debug-instrumentation-ships-unconditionally.md))
* **Update**: Plugin Install Rollback Bug ([gotchas/plugin-install-rollback-bug.md](/gotchas/plugin-install-rollback-bug.md))
* **Creation**: FilePicker.test.tsx Stale Build Status — Corrected ([gotchas/filepicker-stale-todo.md](/gotchas/filepicker-stale-todo.md))
* **Creation**: Debug Instrumentation Ships Unconditionally ([gotchas/debug-instrumentation-ships-unconditionally.md](/gotchas/debug-instrumentation-ships-unconditionally.md))
* **Creation**: Plugin Auto-Permission — Arbitrary Execution Risk ([gotchas/plugin-auto-permission-security.md](/gotchas/plugin-auto-permission-security.md))
* **Creation**: Plugin Removal — Root Directory Deletion Risk ([gotchas/plugin-removal-root-deletion.md](/gotchas/plugin-removal-root-deletion.md))
* **Creation**: Plugin Install Rollback Bug ([gotchas/plugin-install-rollback-bug.md](/gotchas/plugin-install-rollback-bug.md))
## 2026-08-27

* **Update**: Plugin System ([plugins.md](/plugins.md))
* **Creation**: Symlink Escape in Plugin Removal Validation ([gotchas/plugin-removal-symlink-escape.md](/gotchas/plugin-removal-symlink-escape.md))
## 2026-08-26

* **Update**: Foreground Bash Commands — Parent-Death Protection Before Start ([gotchas/foreground-bash-parent-death-protection.md](/gotchas/foreground-bash-parent-death-protection.md))
* **Creation**: Bubble Tea Cleanup Race — Orphaned Goroutines on Shutdown ([gotchas/bubbletea-cleanup-race.md](/gotchas/bubbletea-cleanup-race.md))
* **Creation**: Foreground Bash Commands — Parent-Death Protection Before Start ([gotchas/foreground-bash-parent-death-protection.md](/gotchas/foreground-bash-parent-death-protection.md))
* **Creation**: Agent Replacement — Input Queuing & Stream Event Epochs ([gotchas/agent-replacement-input-queuing.md](/gotchas/agent-replacement-input-queuing.md))
* **Creation**: AIHubMix Test — Global Cache State Leakage ([gotchas/aihubmix-test-cache-leak.md](/gotchas/aihubmix-test-cache-leak.md))
* **Creation**: Local Model Limiter — Stale-Slot Reclamation Race ([gotchas/local-model-limiter-stale-slot-race.md](/gotchas/local-model-limiter-stale-slot-race.md))
* **Creation**: Local Model Auto-Start Hijacks the Controlling Terminal ([gotchas/local-model-tty-hijack.md](/gotchas/local-model-tty-hijack.md))
* **Creation**: run_in_background Bash Commands Orphaned on Force-Killed ocode ([gotchas/background-bash-orphan-on-force-kill.md](/gotchas/background-bash-orphan-on-force-kill.md))
## 2026-08-20

* **Creation**: Shared LSP Broker Server-State and Routing Gotchas ([architecture/lsp-broker-shared-server-gotchas.md](/architecture/lsp-broker-shared-server-gotchas.md))
## 2026-07-22

* **Creation**: Skill Tool Test Fixture Gap — expectedBuiltinTools Missing load_skill ([gotchas/skill-tool-test-fixture-gap.md](/gotchas/skill-tool-test-fixture-gap.md))
## 2026-07-11

* **Creation**: Subagent Feedback-Loop Guard (task tool) ([gotchas/subagent-feedback-loop-guard.md](/gotchas/subagent-feedback-loop-guard.md))
## 2026-07-09

* **Creation**: Sidebar TUI/Web Parity Gaps ([architecture/sidebar-tui-parity-gaps.md](/architecture/sidebar-tui-parity-gaps.md))
* **Creation**: ChatPanel Autoscroll Bounce/Freeze ([gotchas/autoscroll-bounce.md](/gotchas/autoscroll-bounce.md))
## 2026-07-08

* **Creation**: File-Edit Snapshot & Undo Mechanism ([file-edit-snapshot.md](/file-edit-snapshot.md))
## 2026-07-06

* **Update**: Session Title Generation & UI Update Root Cause Analysis ([title-generation-analysis.md](/title-generation-analysis.md))
* **Update**: Plugin System ([plugins.md](/plugins.md))
* **Update**: Zed-compatible ACP Mode Specification ([acp-zed-spec.md](/acp-zed-spec.md))
* **Update**: Using ocode with Zed ([zed.md](/zed.md))
* **Creation**: Knowledge Bundle System ([knowledge-bundle.md](/knowledge-bundle.md))
