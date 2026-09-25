# List-Dialog Keyboard Navigation Plan: Index

## Goal

Make web/desktop list popups keyboard navigable, prioritizing the Select Model
 dialog, while preserving existing model/session/question behavior.

## Ordered work

1. `01-helper-hook.md` — tests first, then the pure movement helper and shared
   React hook.
2. `02-dialogs.md` — tests first, then ModelDialog, SessionDialog,
   DirectoryBrowser, and QuestionDialog integrations.
3. `03-popovers.md` — tests first, then ReasoningLevelSelector and
   ProfileSwitcher integrations.
4. `04-docs.md` — concept documentation, skill guidance, index, and deferred
   surface tracking.

## Global guardrails

- Target the web/desktop React app only; do not change the TUI ListBox plan or
  TUI model picker.
- Do not rewrite cmdk surfaces (`FilePicker`, `CommandPalette`) or native Radix
  `Select` menus.
- Do not add multi-select to ModelDialog or other single-value settings.
- Keep keyboard handlers local to dialogs/popovers; do not add global window
  listeners.
- Preserve cached model loading, model row caps, session windowing, Radix focus
  traps, and explicit QuestionDialog submission.
- Do not stash, reset, or overwrite unrelated concurrent working-tree changes.
- Read the current diff before editing any file with concurrent WIP.

## Definition of done

- Focused and affected-area web tests pass.
- Typecheck and production web build pass.
- `git diff --check` is clean.
- Manual desktop/web smoke checks cover model selection, filtering, session
  load-more, question multi-select, and popover Escape/focus behavior.
- Documentation and deferred-surface records are complete.
