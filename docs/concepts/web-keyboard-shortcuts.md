---
type: Concept
title: Web UI Global Keyboard Shortcuts
description: 'Canonical reference for the ocode web/desktop global keyboard shortcuts: ⌘/Ctrl+K palette, ⌘/Ctrl+P file picker, ⌘/Ctrl+S save, ⌘/Ctrl+N new chat (reveals the Sessions chat half), ⌘/Ctrl+T new terminal, ⌘/Ctrl+W close frontmost, Escape — plus the desktop-shell and xterm caveats.'
resource: web/src/hooks/useKeyboard.ts; web/src/App.tsx:695-763
tags:
  - web
  - keyboard
  - shortcuts
  - ui
  - reference
timestamp: 2026-09-21T03:35:30Z
---
# Web UI Global Keyboard Shortcuts

Canonical reference for the ocode web/desktop keyboard bindings. The source of truth is the code (verified 2026-09-21); this doc records the contract and the caveats.

## Where they live

- **Dispatch:** `web/src/hooks/useKeyboard.ts` — a single `window` `keydown` listener with one `useEffect` and an empty dep array; handlers are read through a ref so re-renders never rebind. This is the only place global shortcuts may be registered.
- **Wiring/semantics:** `web/src/App.tsx:695-763` — an `openNewChat` helper (`:695-699`) plus the top-level `useKeyboard({...})` call (`:701-763`), which supplies `onNewSession`, `onNewTerminal`, `onCommandPalette`, `onFilePicker`, `onSave`, `onEscape`, `onCloseSession`, `onCloseBrowserTab`, and `focusedKind` / `activeBrowserId`.
- **Component-local (not global):** chat find (`⌘/Ctrl+F`) lives inside `web/src/components/Chat/ChatPanel.tsx:724`, guarded to the visible chat tab. The `/search`, `/find` commands open the same bar via the `ocode:open-chat-search` window `CustomEvent` (`ChatPanel.tsx:746`).

## Bindings

| Shortcut | Action |
|---|---|
| `⌘K` / `Ctrl+K` | Open CommandPalette (`onCommandPalette`) |
| `⌘P` / `Ctrl+P` | Open FilePicker (`onFilePicker`) |
| `⌘S` / `Ctrl+S` | Save the visible editor tab (`onSave`) |
| `⌘N` / `Ctrl+N` | New chat — reveals the merged **Sessions** view on the chat half, then opens (or reuses the blank) chat tab (`onNewSession` → `openNewChat`). Identical to the tab-bar "new chat" button |
| `⌘T` / `Ctrl+T` | New terminal on the merged **Sessions** view; on any other view the same new-chat reveal (`onNewTerminal`) |
| `⌘W` / `Ctrl+W` | Close the frontmost thing (desktop shell only — see caveats) |
| `Escape` | Close the CommandPalette / FilePicker (`onEscape`) |
| `⌘F` / `Ctrl+F` | In-chat find bar (ChatPanel-local, not the global hook) |

`e.preventDefault()` is called on every bound combo so the webview's own handling (e.g. browser find) does not also fire.

## Semantics of ⌘N, ⌘T and ⌘W

**⌘N** (`App.tsx:709-714`, helper at `:695-699`): `openNewChat()` = `setActiveView("sessions"); setFocusedKind("chat"); openNewSessionTab(isNewSessionTabEmpty(activeTabId))`. It reveals the merged Sessions view on the chat half and opens (or reuses the blank) chat tab, so it works from any view (Files/Settings/…) exactly like the tab bar's "new chat" button (`UnifiedTabBar.tsx:551-554`). Before this change ⌘N only touched the tab stores, so pressed from a non-Sessions view the new tab landed behind the current view and the shortcut looked inert; the reveal is the fix. It is the sibling of ⌘T for a new terminal.

**⌘T** (`App.tsx:715-728`): when `activeView === "sessions"` it focuses the terminal kind and calls `terminalRefs.current.get(projectPath)?.openTerminal()` — terminal handles are **project-scoped**, looked up by the active project path. On any other view it calls the same `openNewChat()` helper (it previously called `openNewSessionTab(...)` directly, i.e. added a chat tab without revealing it — now consistent with ⌘N).

**⌘W** (`App.tsx:736-758`, `useKeyboard.ts:55-69`): closes whatever is frontmost, mirroring the tab bar's X:
1. Sessions view + terminal focused → `closeActiveTerminal()` (returns early on success), else bail.
2. Files view → `requestCloseTab(visibleActiveEditorTabId)`.
3. Sessions view + chat focused → the full session teardown: `closeSessionBackend` → `closeSessionTab` → `cancelLiveDeltas` → `clearQueue` → `dispatch({type:"RESET"})`.
4. Browser tab focused (`focusedKind === "browser"` and `activeBrowserId` set) → `onCloseBrowserTab(id)`, which strips identity/page state and calls `browserActions.close(...)`.

## Caveats (the load-bearing part)

- **⌘W is desktop-shell only.** `useKeyboard.ts:55` gates it on `isDesktopShell()` (Wails runtime present). In a plain browser the OS/webview consumes ⌘W to close the browser tab and it cannot be intercepted, so binding it would double-close. See the hook's header comment.
- **Ctrl+W inside the embedded terminal is not stolen.** `useKeyboard.ts:55-69` (return at `:61`): if `!e.metaKey` and the event target is inside `.xterm`, the handler returns — Ctrl+W is readline's "delete previous word" while typing in the pty. Cmd+W (`metaKey`) is never sent to the pty, so it still closes the frontmost tab even when the terminal has focus (regression: `web/src/hooks/useKeyboard.test.ts` "closes via Cmd+W even when the embedded terminal has focus").
- **⌘N and ⌘T have no native desktop menu accelerator.** The Wails webview receives the keys and `useKeyboard` handles them; `buildAppMenu` (`cmd/ocode-desktop/main.go:542`) binds only `CmdOrCtrl+,` (Settings) and `CmdOrCtrl+Shift+S` (Share). The Edit menu is the Wails role `menu.AddRole(application.EditMenu)`, whose accelerators are the standard ones (⌘Z / ⇧⌘Z, ⌘X, ⌘C, ⌘V, ⌘⇧⌥V, Backspace, ⌘A, plus the Speech submenu) — none is ⌘N or ⌘T. Adding/removing these bindings is hook-only.
- **⌘N / ⌘T are browser-chrome shortcuts in a plain browser tab.** `Ctrl+N` is "new window" and `Ctrl+T` "new tab", both on Chrome's **non-overridable** list (`Ctrl+T`, `Ctrl+N`, `Ctrl+W`/`F4`, `Ctrl+Shift+T`, `Ctrl+Shift+N`, `Ctrl+Tab`, `F12`/`Ctrl+Shift+I`, `Ctrl+Shift+C`, `Ctrl+U`, `Ctrl+Shift+Del`), so `e.preventDefault()` cannot reliably claim them there. Contrast omnibox-family keys such as `Ctrl+K`, which pages *are* allowed to take (GitHub overrides it) — that is why ⌘K works in a browser while ⌘N/⌘T may not. The desktop shell is unaffected (its webview has no browser chrome), and outside it the reliable entry point is the tab-bar buttons.
- **No raw listeners in child components.** Registering bindings outside `useKeyboard` bypasses the desktop-shell and xterm guards above.
- **Keep `skills/ocode-web/SKILL.md` §10 in sync.** That table lists the same bindings (⌘K, ⌘P, ⌘S, ⌘N, ⌘T, ⌘W, Escape, plus the note that ⌘, opens Settings via the native menu) — when a binding changes, update both this doc and the skill.

## Tests

- `web/src/hooks/useKeyboard.test.ts` — Cmd+W / Ctrl+W dispatch in the desktop shell, no binding in a plain browser, Ctrl+W not stolen inside `.xterm`, Cmd+W with terminal focus, plain W ignored, and ⌘N still fires `onNewSession`. Unchanged by the ⌘N-reveal work (the hook itself was not modified).
- `web/src/App.newChatShortcut.test.tsx` — the App-level wiring: from a restored Files view, ⌘/Ctrl+N switches to the Sessions view on the chat half and clears the no-open-sessions empty state (a tab now exists); a second press reuses the blank tab rather than stacking (exactly one chat pane); with the terminal half active it moves focus back to chat; and ⌘/Ctrl+T outside Sessions performs the same reveal. Mutation-verified by temporary revert: dropping `setActiveView("sessions")` failed 3 of the 4, dropping `setFocusedKind("chat")` failed the terminal-half case, and dropping the `reuseIfEmpty` argument failed the reuse case.
- `web/src/hooks/useKeyboard.browser.test.ts` — ⌘W closes the focused browser tab, falls back to session close when chat is focused or no active browser id.
