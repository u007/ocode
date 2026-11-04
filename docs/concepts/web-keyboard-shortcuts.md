---
type: Concept
title: Web UI Global Keyboard Shortcuts
description: 'Canonical reference for the ocode web/desktop keyboard shortcuts: app-level bindings in useKeyboard, the local chat find bar, the per-surface preview find exception (PDF/Markdown/Word/HTML), and the browser/desktop caveats.'
resource: "web/src/hooks/useKeyboard.ts; web/src/App.tsx:1014-1093; web/src/components/Preview/findDom.ts"
tags:
  - web
  - keyboard
  - shortcuts
  - ui
  - reference
  - find
  - preview
timestamp: 2026-11-03T15:20:26Z
---
# Web UI Global Keyboard Shortcuts

Canonical reference for the ocode web/desktop keyboard bindings. The source of truth is the code (anchors re-verified 2026-11-03); this doc records the contract and the caveats.

## Where they live

- **Dispatch:** `web/src/hooks/useKeyboard.ts` — a single `window` `keydown` listener created by `useKeyboard` (`useKeyboard.ts:33`) with one `useEffect` and an empty dep array; handlers are read through a ref so re-renders never rebind. This is the only place **app-level/global** shortcuts may be registered (the surface-local find listeners are the single documented exception, below).
- **Wiring/semantics:** `web/src/App.tsx:1014-1093` — an `openNewChat` helper (`:1014-1018`) plus the top-level `useKeyboard({...})` call (`:1020-1093`), which supplies `onNewSession`, `onNewTerminal`, `onCommandPalette`, `onFilePicker`, `onSave`, `onEscape`, `onCloseSession`, `onCloseBrowserTab`, and `focusedKind` / `activeBrowserId`.
- **Component-local (not global):** chat find (`⌘/Ctrl+F`) lives inside `web/src/components/Chat/ChatPanel.tsx:1261-1270`, guarded to the visible chat tab (`offsetParent === null` → ignore). The `/search`, `/find` commands open the same bar via the `ocode:open-chat-search` window `CustomEvent` (`ChatPanel.tsx:1285`).
- **Surface-local (not global):** in-document find in the PDF / Markdown / Word / HTML previews is owned by each viewer (`FindBar` + a window keydown gated to that surface). See the exception caveat and [preview-find.md](preview-find.md).

## Bindings

| Shortcut | Action |
|---|---|
| `⌘K` / `Ctrl+K` | Open CommandPalette (`onCommandPalette`) |
| `⌘P` / `Ctrl+P` | Open FilePicker (`onFilePicker`) |
| `⌘S` / `Ctrl+S` | Save the visible editor tab (`onSave`) — also exposed as a **header Save button** (`<Save />`, `aria-label="Save file"`, `FileEditor.tsx:948-960`) for touch devices that have no keyboard; both paths invoke the same `saveEditorTab` call (`App.tsx:1056` keyboard path and `App.tsx:1866-1868` header button) |
| `⌘N` / `Ctrl+N` | New chat — reveals the merged **Sessions** view on the chat half, then opens (or reuses the blank) chat tab (`onNewSession` → `openNewChat`). Identical to the tab-bar "new chat" button |
| `⌘T` / `Ctrl+T` | New terminal on the merged **Sessions** view; on any other view the same new-chat reveal (`onNewTerminal`) |
| `⌘W` / `Ctrl+W` | Close the frontmost thing (desktop shell only — see caveats) |
| `Escape` | Close the CommandPalette / FilePicker (`onEscape`) |
| `⌘F` / `Ctrl+F` | In-chat find bar (ChatPanel-local, not the global hook) |
| `⌘F` / `Ctrl+F` (preview) | In-document find in the active PDF/Markdown/Word/HTML preview — the shared `FindBar` (surface-local, not the global hook). Claimed only by the surface the reader is in |
| `F3` / `Shift+F3` | Next / previous match while a preview find bar is open (also walks PDF matches) |
| `Escape` (preview) | Close the preview find bar when it is open |
| `⌘⇧A` / `Ctrl+Shift+A` | Toggle the Pulse assistant window (`onToggleAssistant` → `toggleAssistantWindow`, `components/Pulse/pulseAssistantPrefs.ts`). Closed opens; a minimised window restores; otherwise closes. The same toggle is the top bar's "Toggle assistant" button |

`e.preventDefault()` is called on every bound combo so the webview's own handling (e.g. browser find) does not also fire.

## Semantics of ⌘N, ⌘T and ⌘W

**⌘N** (`App.tsx:1032-1037`, helper at `:1014-1018`): `openNewChat()` = `setActiveView("sessions"); setFocusedKind("chat"); openNewSessionTab(isNewSessionTabEmpty(activeTabId))`. It reveals the merged Sessions view on the chat half and opens (or reuses the blank) chat tab, so it works from any view (Files/Settings/…) exactly like the tab bar's "new chat" button (`UnifiedTabBar.tsx:1044`). Before this change ⌘N only touched the tab stores, so pressed from a non-Sessions view the new tab landed behind the current view and the shortcut looked inert; the reveal is the fix. It is the sibling of ⌘T for a new terminal.

**⌘T** (`App.tsx:1038-1050`): when `activeView === "sessions"` it focuses the terminal kind and calls `terminalRefs.current.get(proj)?.openTerminal()` — terminal handles are **project-scoped**, looked up by the active project path. On any other view it calls the same `openNewChat()` helper (it previously called `openNewSessionTab(...)` directly, i.e. added a chat tab without revealing it — now consistent with ⌘N).

**⌘W** (`App.tsx:1059-1088`, `useKeyboard.ts:59-72`): closes whatever is frontmost, mirroring the tab bar's X:
1. Sessions view + terminal focused → `closeActiveTerminal()` (returns early on success), else bail.
2. Files view → `requestCloseTab(visibleActiveEditorTabId)`.
3. Sessions view + chat focused → the full session teardown: `closeSessionBackend` → `closeSessionTab` → `cancelLiveDeltas` → `clearQueue` → `clearInputHistory` → `dispatch({type:"RESET"})`.
4. Browser tab focused (`focusedKind === "browser"` and `activeBrowserId` set) → `onCloseBrowserTab(id)`, which strips identity/page state and calls `browserActions.close(...)` (`useKeyboard.ts:68-69`).

## Semantics of ⌘P (direct path hit)

**⌘P** (`onFilePicker`) opens `FilePicker`. Besides the ranked list, the picker pins an **exact typed path** first, folder or file:

- The typed text is stat'd only when it is a single token (no whitespace; trailing `/` stripped). The `directHit` effect in `FilePicker.tsx` waits 120 ms, then calls `GET /api/files/stat?path=…&project_root=…[&host=…]`. An existing path is listed ahead of the ranking.
- The stat is independent of the `GET /api/files/tree?depth=0` index, so the hit appears while the tree is still loading or truncated.
- `HandleFileStat` (`internal/server/handler_files.go`, route registered in `internal/server/server.go`) applies the same editor containment as `/api/files/access`. A missing or out-of-scope path returns `200 {"exists":false,"is_dir":false}`, not an error.
- An existing path returns `path` too: the repo-relative spelling `/api/files/tree` uses (`./`, doubled and trailing slashes normalised). The picker dedupes against tree rows and opens that path, so a typed `./src/a.go` and the tree's `src/a.go` are one tab. An absolute spelling through a symlink alias has no in-root spelling and returns `exists:false`.
- The hit is held as `{query, scope}` state (`directHitScope` in `FilePicker.tsx`), where scope is the project path plus host. It is drawn only while the typed text and scope still match, so a row for an earlier query or another project/host never renders; typing hides it at once.
- The picker treats a non-OK stat response as no hit and logs it with `console.error`. An aborted request (a superseded keystroke) is not logged.
- Remote projects run one ssh command (`DIR` / `FILE` / `NONE`) after `remoteWorkForFileRead` and `remoteRawContainment`, bounded by `remoteStatTimeout` (5 s, a slot wait included). A transport failure (ssh will not start, the link dies, or the bound expires) returns `502`; any other remote error returns `500`; a bad host or project root returns `400`. A stat cancelled by the client is logged at debug only.
- A folder hit is pinned only when the picker has `onOpenFolder`. Choosing it calls `revealPathInTree` in `App.tsx`, which switches to the Files view and expands the tree to that path. A file hit goes to `onOpenFile`.

Tests: `web/src/components/Files/FilePicker.directHit.test.tsx`. Go: `TestFileStat_*` in `internal/server/handler_file_grants_test.go` (local only; the remote branch is untested).

## Caveats (the load-bearing part)

- **⌘W is desktop-shell only.** `useKeyboard.ts:59` gates it on `isDesktopShell()` (Wails runtime present). In a plain browser the OS/webview consumes ⌘W to close the browser tab and it cannot be intercepted, so binding it would double-close. See the hook's header comment.
- **Ctrl+W inside the embedded terminal is not stolen.** `useKeyboard.ts:59-72` (guard at `:65`): if `!e.metaKey` and the event target is inside `.terminal-host`, the handler returns — Ctrl+W is readline's "delete previous word" while typing in the pty. Cmd+W (`metaKey`) is never sent to the pty, so it still closes the frontmost tab even when the terminal has focus (regression: `web/src/hooks/useKeyboard.test.ts` "closes via Cmd+W even when the embedded terminal has focus").
- **⌘N and ⌘T have no native desktop menu accelerator.** The Wails webview receives the keys and `useKeyboard` handles them; `buildAppMenu` (`cmd/ocode-desktop/main.go:993`) binds only `CmdOrCtrl+,` (Settings, `main.go:1003`), `CmdOrCtrl+Shift+J` (Dashboard, `main.go:1013`) and `CmdOrCtrl+q` (Quit, `main.go:1032`). The Edit menu is the Wails role `menu.AddRole(application.EditMenu)` (`main.go:1060`), whose accelerators are the standard ones (⌘Z / ⇧⌘Z, ⌘X, ⌘C, ⌘V, ⌘⇧⌥V, Backspace, ⌘A, plus the Speech submenu) — none is ⌘N or ⌘T. Adding/removing these bindings is hook-only.
- **⌘N / ⌘T are browser-chrome shortcuts in a plain browser tab.** `Ctrl+N` is "new window" and `Ctrl+T` is "new tab", both on Chrome's **non-overridable** list (`Ctrl+T`, `Ctrl+N`, `Ctrl+W`/`F4`, `Ctrl+Shift+T`, `Ctrl+Shift+N`, `Ctrl+Tab`, `F12`/`Ctrl+Shift+I`, `Ctrl+Shift+C`, `Ctrl+U`, `Ctrl+Shift+Del`), so `e.preventDefault()` cannot reliably claim them there. Contrast omnibox-family keys such as `Ctrl+K`, which pages *are* allowed to take (GitHub overrides it) — that is why ⌘K works in a browser while ⌘N/⌘T may not. The desktop shell is unaffected (its webview has no browser chrome), and outside it the reliable entry point is the tab-bar buttons.
- **Save has a touch-path button.** The Files-tab editor header renders a Save button (lucide `Save`, `aria-label="Save file"`, `FileEditor.tsx:948-960`) when `onSave` is provided and `dirty` is true — disabled otherwise. This is the only save mechanism on touch devices (no keyboard). Read-only surfaces (`TextViewer`/`MarkdownViewer`) pass no `onSave`, so no button renders there. Save failures surface via `reportActionError(…, "Save file")` → `ActionErrorToast`; a 409 conflict also sets the in-editor external-change banner.
- **⌘K does not reach the palette from the focused terminal.** `TerminalPanel.tsx` lets ⌘W/⌘N/⌘T/⌘P/⌘S/⌘J (`APP_META_SHORTCUT_KEYS`, `TerminalPanel.tsx:129`) and ⌘⇧A (`APP_META_SHIFT_SHORTCUT_KEY`, `:131`) bubble to this hook from the terminal (`:1283`), but deliberately not ⌘K, so the terminal keeps that key. The table's ⌘K row applies outside the terminal. The terminal's list is hand-synced with this file: when a Cmd binding is added here, add it there too (⌘J is in the terminal list because its Pulse toggle works from the terminal).
- **No raw listeners in child components — one deliberate, gated exception.** Registering bindings outside `useKeyboard` bypasses the desktop-shell and terminal guards above, so the rule stands for every **app-level** shortcut. The sole exception is the per-surface find listeners in the four preview viewers (`MarkdownViewer.tsx:216-217`, `DocxViewer.tsx:63`, `HtmlViewer.tsx:40-41`, `PdfViewer.tsx:896`): they bind `window` keydown for Ctrl/⌘F, F3/Shift+F3 and Esc, but each refuses to act unless the reader is inside that preview, so no one surface can swallow a key meant for another. Two gate shapes:
  - **PDF gates on its `active` prop** (`if (!active) return` at `PdfViewer.tsx:896`): only the visible viewer attaches the listener, so N mounted-but-hidden PDF panes cannot all flip `find.active` or race on F3.
  - **Markdown / Word / HTML gate on `previewWantsFindShortcut(root)`** (`findDom.ts:175`): `offsetParent === null` → refuse (`findDom.ts:178`, rejects a CSS-hidden keep-alive pane), otherwise claim only when focus is inside, a selection is anchored inside, or `root.matches(":hover")`.

  All four also skip editable targets (`isEditableTarget`, `findDom.ts:158`, so an input keeps native find) and handle F3/Esc only while their bar is open. This gate is what keeps **one** Ctrl/⌘F from opening both the ChatPanel find bar and a preview find bar — precedence comes from the gate, never from listener registration order. A new preview kind must reuse `previewWantsFindShortcut` (or an equivalent visibility gate), not claim the key unconditionally. Details: [preview-find.md](preview-find.md).
- **Keep `skills/ocode-web/SKILL.md` §10 in sync.** That table lists the same bindings (⌘K, ⌘P, ⌘S, ⌘N, ⌘T, ⌘W, Escape, plus the note that ⌘, opens Settings via the native menu) — when a binding changes, update both this doc and the skill.

## Tests

- `web/src/hooks/useKeyboard.test.ts` — Cmd+W / Ctrl+W dispatch in the desktop shell, no binding in a plain browser, Ctrl+W not stolen inside the embedded terminal, Cmd+W with terminal focus, plain W ignored, and ⌘N still fires `onNewSession`. Unchanged by the ⌘N-reveal work (the hook itself was not modified).
- `web/src/App.newChatShortcut.test.tsx` — the App-level wiring: from a restored Files view, ⌘/Ctrl+N switches to the Sessions view on the chat half and clears the no-open-sessions empty state (a tab now exists); a second press reuses the blank tab rather than stacking (exactly one chat pane); with the terminal half active it moves focus back to chat; and ⌘/Ctrl+T outside Sessions performs the same reveal. Mutation-verified by temporary revert: dropping `setActiveView("sessions")` failed 3 of the 4, dropping `setFocusedKind("chat")` failed the terminal-half case, and dropping the `reuseIfEmpty` argument failed the reuse case.
- `web/src/hooks/useKeyboard.browser.test.ts` — ⌘W closes the focused browser tab, falls back to session close when chat is focused or no active browser id.
- `web/src/components/Files/FileEditor.save.test.tsx` — header Save button: renders disabled when not dirty, enabled and calls `onSave` when dirty, omitted entirely when `onSave` is undefined (read-only surfaces).
- `web/src/App.editorTabScope.test.tsx` — "wires a Save handler and the dirty flag into each editor pane".
- `web/src/components/Preview/findDom.test.ts` — index/match/range math plus the `previewWantsFindShortcut` gate (focus, hover, selection, hidden via `offsetParent`).
- `web/src/components/Preview/{MarkdownViewer,DocxViewer,HtmlViewer,PdfViewer}.find.test.tsx` — the four viewers' FindBar wiring and their shortcut gating; `htmlFindAgent.test.ts` covers the serialised iframe agent.

## Composer (chat input)

The composer (`web/src/components/Chat/ChatInput.tsx`) has its own key handling inside `handleKeyDown` — these are **not** global bindings (they only fire when the textarea is focused) and are deliberately excluded from `useKeyboard`.

| Shortcut | Action |
|---|---|
| `Enter` | Send the draft (`handleSend`) |
| `Shift+Enter` | Newline in the composer (does not send) |
| `↑` / `↓` | Walk sent-input history (see [web-chat-input-history.md](web-chat-input-history.md)) |
| `/` | Open slash menu (command autocomplete) |
| `!` | Shell command (sent as `!shell …`); excluded from history recall |

**Caret gate for ↑/↓:** plain ↑ first enters the history walk **only** when the caret is on the first line (no newline above it) and there is no slash menu open. Once inside the walk (`historyIndexRef !== -1`), arrows keep walking regardless of caret position. Modifier keys (Shift/Alt/Meta/Ctrl) on arrow presses are ignored by the history handler, so ⌘↑ etc. fall through to the browser/default.
