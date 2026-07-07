# QA Testing Checklist

Generic checks to apply to every page and feature. Domain-specific scenarios should be derived from the codebase during Phase 2 planning.

Severity tags: `[C]` = CRITICAL, `[H]` = HIGH, `[M]` = MEDIUM, `[L]` = LOW. Test in severity order.

## Per-Page Checks

### Rendering
- [ ] `[C]` Page loads without 500/error
- [ ] `[C]` No blank white screen or stuck "Loading..." (wait 10s+)
- [ ] `[H]` No `[object Object]`, `undefined`, `NaN`, or `null` rendered as text
- [ ] `[M]` Images load or show proper placeholders (no broken icons)
- [ ] `[L]` No layout shift after content loads
- [ ] `[L]` No unstyled flash (FOUC)

### Data Integrity
- [ ] `[C]` Lists show items when data exists (not empty when they shouldn't be)
- [ ] `[C]` Counts/totals are mathematically accurate
- [ ] `[H]` Numbers display correctly (currency symbols, no raw floats, no NaN)
- [ ] `[H]` Filters return correct results
- [ ] `[H]` Sort order is correct (numeric sort, not string sort on numbers)
- [ ] `[M]` Dates are formatted (not raw ISO strings)
- [ ] `[M]` Pagination shows correct total and page count
- [ ] `[M]` Search returns relevant results

### Navigation
- [ ] `[C]` Auth guards redirect to login with return URL
- [ ] `[H]` All links point to valid routes (no 404s)
- [ ] `[H]` After login, redirects back to intended page
- [ ] `[H]` Logout clears session and redirects
- [ ] `[M]` Back button works without breaking state
- [ ] `[M]` Deep links work (can bookmark and revisit)
- [ ] `[L]` Breadcrumbs are accurate (if present)

### Forms
- [ ] `[C]` Double-click submit doesn't create duplicates
- [ ] `[C]` Server-side errors display to user (not swallowed)
- [ ] `[H]` Required field validation shows readable messages
- [ ] `[H]` Form preserves input on validation failure
- [ ] `[M]` Submit button shows loading state during request
- [ ] `[M]` Loading state resets on error (not stuck)
- [ ] `[M]` Success shows confirmation and redirects correctly

### Empty & Error States
- [ ] `[H]` Empty lists show friendly message (not blank table)
- [ ] `[H]` Network errors show user-friendly message
- [ ] `[M]` 404 page is styled with navigation back
- [ ] `[M]` Permission denied shows appropriate message
- [ ] `[M]` Expired/invalid tokens redirect to login with explanation

### Responsive (Mobile 375px)
- [ ] `[H]` No horizontal scroll / overflow
- [ ] `[H]` Navigation accessible (hamburger menu works)
- [ ] `[M]` Text readable without zooming
- [ ] `[M]` Touch targets >= 44px
- [ ] `[M]` Tables scroll horizontally or stack vertically
- [ ] `[L]` Modals/dialogs fit viewport

### Console & Network
- [ ] `[C]` No uncaught JavaScript errors
- [ ] `[C]` No unexpected 4xx/5xx API responses
- [ ] `[H]` No CORS errors
- [ ] `[H]` No failed network requests
- [ ] `[L]` No framework warnings (React, Vue, etc.)

## CRUD Lifecycle (Per Entity)

For EVERY entity type discovered in the app:

- [ ] `[C]` **CREATE**: Valid data → success feedback → appears in list
- [ ] `[H]` **CREATE**: Empty required fields → validation errors shown
- [ ] `[H]` **CREATE**: Duplicate/conflict data → appropriate error
- [ ] `[C]` **READ (List)**: Items display with correct data
- [ ] `[H]` **READ (List)**: Search/filter returns correct results
- [ ] `[M]` **READ (List)**: Empty state shows helpful message
- [ ] `[H]` **READ (Detail)**: All fields display correctly
- [ ] `[H]` **READ (Detail)**: Related data loads (associations, nested items)
- [ ] `[C]` **EDIT**: Changes persist after save
- [ ] `[H]` **EDIT**: Form pre-fills with current values
- [ ] `[H]` **EDIT**: Validation works same as create
- [ ] `[C]` **DELETE**: Entity removed from list after confirm
- [ ] `[H]` **DELETE**: Confirmation dialog appears
- [ ] `[H]` **DELETE**: Cascade effects handled (related data cleaned up)

## Multi-Step Flows

For every flow spanning multiple pages/steps:

- [ ] `[C]` Complete happy path start to finish
- [ ] `[C]` Validation per step prevents bad data propagating
- [ ] `[H]` Back navigation preserves data
- [ ] `[H]` Cannot skip required steps
- [ ] `[M]` Browser refresh mid-flow handled gracefully
- [ ] `[M]` Final confirmation shows all entered data correctly
- [ ] `[M]` Post-completion: entity appears in all relevant views

## Permissions & Authorization

- [ ] `[C]` Unauthenticated users redirected to login
- [ ] `[C]` Direct URL access as wrong role shows 403/redirect
- [ ] `[H]` Role-based UI: restricted buttons/links hidden for wrong roles
- [ ] `[H]` Owner-only actions restricted to creator/owner
- [ ] `[H]` After role change, old permissions no longer work

## Edge Cases (apply to every scope)

### Data Input
- [ ] `[C]` Special characters in text fields: `<script>alert(1)</script>`, `"quotes"`, `O'Brien`
- [ ] `[H]` Emoji in text fields: `🎉🔥`
- [ ] `[H]` Very long strings (500+ chars) — truncated or breaks layout?
- [ ] `[H]` Numeric boundaries: 0, negative, decimal, very large numbers
- [ ] `[M]` Date boundaries: today, far past, far future
- [ ] `[M]` Empty/zero state: no items, fresh account

### Interaction
- [ ] `[C]` Double-click/rapid submit — creates duplicates?
- [ ] `[H]` Submit with all optional fields empty
- [ ] `[H]` Browser back button after form submission
- [ ] `[M]` Navigate away during async operation, then return
- [ ] `[M]` Refresh page mid-flow — state preserved or clean restart?
- [ ] `[M]` Same entity open in two tabs, edit in both

### Stale State
- [ ] `[H]` Action on already-deleted entity — graceful error?
- [ ] `[H]` Action on entity in wrong state (e.g., approve already-approved)
- [ ] `[M]` Cancel mid-flow — no partial state saved

## Memory Leaks

Run these checks after repeated navigation cycles or CRUD operations within a scope:

- [ ] `[H]` Heap usage does not grow unbounded after navigating away and back to the same page 10+ times
- [ ] `[H]` No detached DOM nodes accumulating (elements removed from DOM but still referenced in JS)
- [ ] `[H]` Event listeners cleaned up on component unmount (no duplicate handlers after re-mount)
- [ ] `[H]` `setInterval` / `setTimeout` cleared on unmount (no orphaned timers firing after navigation)
- [ ] `[H]` WebSocket / SSE connections closed on page exit (no stale connections stacking)
- [ ] `[M]` Large data (images, blobs, files) released after use (`URL.revokeObjectURL`, nulling refs)
- [ ] `[M]` AbortController used for fetch calls — pending requests cancelled on unmount
- [ ] `[M]` No growing array of subscription callbacks (e.g., store subscriptions not unsubscribed)
- [ ] `[L]` Console logs don't reference large objects that prevent GC

## Security Spot Checks
- [ ] `[C]` XSS: User-generated content is escaped (try `<script>` in forms)
- [ ] `[C]` Auth: Can't access other users' data by changing URL IDs
- [ ] `[H]` No sensitive data in URL params
