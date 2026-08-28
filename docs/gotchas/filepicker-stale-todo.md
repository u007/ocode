---
type: Gotcha
title: FilePicker.test.tsx Stale Build Status — Corrected
description: 'Stale TODO item: FilePicker.test.tsx now has no user-event import and build passes'
tags:
  - stale-todo
  - filepicker
  - build-status
  - documentation-hygiene
timestamp: 2026-08-28T07:00:04Z
---
### FOUND + FIXED: full chat-store subscription tree re-rendered on every streamed token (2026-08-27, same investigation)

New report: typing in the chat input lagged badly while a response was
streaming, on top of the memory spikes above.

- [x] **Root cause: `useChatState()` (`web/src/stores/chatStore.tsx`) had 15
  call sites across the app, none scoped — every one subscribes to the
  *entire* store and re-renders on *every* dispatch, including the
  `"text"`/`"thinking"` per-token deltas `sessionEvents.ts` dispatches for
  every streamed token.** `HomeApp` (`App.tsx`) calls it directly, and
  `ChatInput` is rendered deep inside the same component's ~600-line JSX
  body — so every token forced the *entire app shell* (sidebar, message
  list, terminal, editor, status bar, chat input) to reconcile, competing
  with keystrokes for the main thread. `useChat()` (also called directly in
  `HomeApp`) had the identical unscoped call hidden a layer down — fixing
  `App.tsx` alone would not have helped.
- [x] **Fix:** Scoped every `useChatState()` call site to only the slice it
  reads via selector functions (e.g. `useChatState(s => s.messages)` for the
  message list, `useChatState(s => s.inputValue)` for the chat input). This
  is the standard Zustand pattern — `useStore(selector)` — but was missed
  because the store was initially small and grew organically.
- [x] **`bin/ocode.app` was rebuilt and relaunched** after the scoped
  fix landed and the shared tree was verified clean (no pending concurrent
  edits to debug.ts or related files). `FilePicker.test.tsx` no longer
  imports `@testing-library/user-event` and the full project `tsc --noEmit`
  passes.
- [ ] **Not yet independently confirmed** whether this closes the residual
  `EventSource → dateProtoFuncToLocaleString` signature for good — that
  needs a repro on this build with `sample` (would show near-zero ICU
  activity if the actual culprit passes options; would show the same
  signature if it's a bare no-args caller, which this fix deliberately
  doesn't touch) or, better, a follow-up DevTools flame-graph capture now
  that the user has Web Inspector access — clicking into one of the purple
  "JavaScript & Events" spike bars would show the exact function/file
  either way.