# Plan Part 2: Dialog Integrations

## Scope

Integrate the shared list navigation into the custom web dialogs without
changing their existing selection or persistence contracts.

## Files

- `web/src/components/Layout/ModelDialog.tsx`
- `web/src/components/Layout/ModelDialog.test.tsx`
- `web/src/components/Layout/SessionDialog.tsx`
- `web/src/components/Layout/SessionDialog.test.tsx`
- `web/src/components/Layout/DirectoryBrowser.tsx` and its focused tests
- `web/src/components/Chat/QuestionDialog.tsx`
- `web/src/components/Chat/QuestionDialog.test.tsx`

## Order of work

1. Read the current working-tree diff for every target file before editing;
   `QuestionDialog.tsx` and its test already contain unrelated concurrent work.
2. Add failing keyboard tests for ModelDialog, run them, then implement its row
   identity/order integration.
3. Add failing keyboard tests for SessionDialog, including the explicit Load
   more boundary, then implement its visible-slice handoff.
4. Add failing keyboard tests for DirectoryBrowser, then implement row Enter
   confirmation while preserving double-click navigation and selected-path input
   behavior.
5. Extract a QuestionDialog option-list child so each question has an independent
   navigation scope; add failing single- and multi-select keyboard tests before
   changing the component.
6. Run all affected dialog tests and typecheck after each integration.

## Required behavior

### ModelDialog

Search ArrowDown enters the first rendered row; arrows move by rendered order;
Enter uses the existing `handleSelect` path and closes. Search/provider changes
reset the cursor. Favorite stars remain independent. Preserve Recently Used,
Favorites, provider grouping, configured-only loading, refresh, and the 500-row
provider cap. Use section-qualified row IDs so repeated models are distinct.

### SessionDialog

Search ArrowDown enters the first visible session; Enter opens it. At the last
visible row, ArrowDown calls the existing `loadMore` and focuses the first
newly rendered session after the synchronous `visibleCount` update. Preserve
background revalidation clamping, prefetch, middle-click, and nested close
behavior. Test through the explicit Load more fallback when
`IntersectionObserver` is absent.

### DirectoryBrowser

Arrow navigation moves the active folder. Enter on a focused row selects and
confirms that folder; double-click still navigates into it. The selected-path
input's existing Enter-confirm behavior must not also handle row-originated
Enter.

### QuestionDialog

Each question gets its own active row. Arrows move within that question;
Enter/Space toggle the focused option; the custom-text option is reachable.
No row activation submits. Preserve radio semantics for single-select and
checkbox semantics for `q.multiple`, plus the explicit Submit button.

## Accessibility/focus checks

Use real focus rather than `aria-activedescendant`. Add listbox/option semantics
where compatible with existing button structure. Verify that Radix focus traps
and `onOpenAutoFocus` still work: add a regression assertion that Tab remains
inside the dialog after list navigation.

## Validation

Run the focused test files, the affected-area suites, `pnpm --dir web typecheck`,
and later the full web test/build gates. Do not delete or weaken existing tests.
