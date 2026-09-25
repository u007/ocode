# Plan Part 3: Selector Popovers

## Scope

Add the same keyboard movement behavior to the two hand-rolled, non-Radix list
popovers while keeping their existing API and selection semantics.

## Files

- `web/src/components/Layout/ReasoningLevelSelector.tsx`
- `web/src/components/Layout/ReasoningLevelSelector.test.tsx`
- `web/src/components/ProfileSwitcher.tsx`
- Add a focused ProfileSwitcher test if none exists.

## Order of work

1. Read the current files and tests, including any concurrent changes.
2. Add failing tests for open focus, Arrow movement, activation, Escape close and
   focus restoration, and Tab close.
3. Implement ReasoningLevelSelector using the shared hook and its existing
   session/global persistence handlers.
4. Extract ProfileSwitcher's profile request into a named handler, then add the
   shared navigation and explicit non-option handling for “Manage profiles…”.
5. Rerun popover tests and typecheck.

## Required behavior

### ReasoningLevelSelector

The current level is focused when the popover opens. Arrow keys move through
`REASONING_LEVELS`; Enter/Space selects the focused level through the existing
session-vs-global API path and closes the popover. Escape closes and restores
trigger focus. Tab closes without adding a focus trap.

### ProfileSwitcher

The active default/profile is focused when the popover opens. The primary list
contains the default option followed by profiles. Arrow keys move through that
list; Enter/Space uses the same request handler as mouse selection and closes.
The active-profile API, window ID, labels, and override counts remain unchanged.
Escape closes and restores trigger focus. Tab closes. “Manage profiles…” is
not a primary navigation option and must not be selected by arrows or Enter.

## Error handling

Do not introduce empty catches in touched code. Log/report failed profile loads
or updates using the project's existing error conventions while preserving the
current UI behavior.

## Validation

Assert `document.activeElement`, include the shared `scrollIntoView` test stub,
run the focused tests, and perform a small browser/desktop smoke check for
focus and Escape behavior.
