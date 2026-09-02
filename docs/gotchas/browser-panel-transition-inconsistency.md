---
type: Gotcha
title: Browser Panel — Documentation vs. Landed Behavior Mismatch
description: Inconsistency between browser panel docs (showConnecting overlay) and actual implementation (indeterminate progress bar + everLoaded flag).
tags:
  - browser-panel
  - gotcha
  - documentation
  - inconsistency
timestamp: 2026-09-02T07:06:56Z
---
## Browser Panel Transition Inconsistency

The browser panel documentation and the actual landed implementation
diverge on how the connecting/loading state is presented to the user.

### What the docs say

The spec (`superpowers/specs/2026-08-30-embedded-browser-panel-design.md`)
and the plan (`superpowers/plans/2026-08-30-embedded-browser-panel/09-browser-panel-component.md`)
describe a `showConnecting` overlay that appears while the page is loading
and is dismissed once navigation completes.

### What actually shipped

The landed implementation uses:

1. **An indeterminate progress bar** at the top of the browser panel (not
   a full overlay) — visible during navigation, hidden when idle.
2. **An `everLoaded` flag** that tracks whether at least one page has
   successfully loaded. Before the first load, the panel shows an empty
   state rather than the progress bar.
3. **No `showConnecting` variable** in the final code — the concept was
   replaced by the progress bar + `everLoaded` combination.

### Impact

- The `showConnecting` overlay is referenced in at least one doc
  (`docs/changes-tab.md` cross-references) and in the spec, but does not
  exist in the codebase.
- Developers reading the spec will look for a variable that does not
  exist, slowing onboarding and review.
- The actual UX (progress bar) is arguably better than the spec's
  overlay, but the docs have not been updated to reflect this.

### Recommended fix

Update the spec and plan documents to describe the indeterminate progress
bar and `everLoaded` pattern, removing references to the `showConnecting`
overlay. Cross-reference docs that mention the overlay should be updated
or annotated.

### Files

- `web/src/components/Browser/BrowserPanel.tsx` — `everLoaded`, progress
  bar implementation
- `superpowers/specs/2026-08-30-embedded-browser-panel-design.md` — spec
  referencing `showConnecting`
- `superpowers/plans/2026-08-30-embedded-browser-panel/09-browser-panel-component.md`
  — plan referencing `showConnecting`

### Related

- Code review findings for browser/Git frontend (2026-09-01) noted this
  divergence as an IMPORTANT item.