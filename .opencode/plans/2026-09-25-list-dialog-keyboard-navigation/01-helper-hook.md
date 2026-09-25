# Plan Part 1: Shared List Navigation

## Scope

Create the DOM-free movement helper and the small React integration hook used by
web list popups. This part does not change any dialog behavior by itself.

## Files

- Add `web/src/lib/listNavigation.ts`.
- Add `web/src/lib/listNavigation.test.ts`.
- Add `web/src/hooks/useListNavigation.ts`.

## Order of work

1. Write failing helper tests for empty lists, first/last movement, next/previous
   movement, and clamp-without-wrap behavior.
2. Run the focused test and confirm the new cases fail for the intended reason.
3. Implement the pure helper and rerun the focused test.
4. Write failing hook/component-level tests for real focus movement, reset on a
   changed reset key, nested-action exclusion, and search-input entry/return.
5. Implement the hook with stable row identities, roving tab stops, active-row
   visibility, and `scrollIntoView({ block: "nearest" })`.
6. Rerun helper and hook tests, then typecheck.

## Constraints

- The helper must not import React or touch the DOM.
- Row identity must be supplied by the caller and remain stable across renders.
- Handled keys must prevent default and stop propagation.
- Only registered primary rows and the designated search input are navigation
  targets; nested actions are excluded.
- Keep optional behaviors small: the hook may support the SessionDialog
  load-more handoff and the QuestionDialog multi-select toggle, but callers
  that do not need them must not opt in.
- Do not add a global keyboard listener.

## Validation

Use the repository's existing Vitest conventions. Install the shared
`scrollIntoView` test stub in the test setup rather than in production code or
individual test files. Assert `document.activeElement` for focus movement and
restore any test doubles after each test.
