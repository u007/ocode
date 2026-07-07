# QA Templates & Code

## Browser Automation

Use the **agent-browser** skill for all browser interaction. Core workflow:

```bash
agent-browser open <url>                    # Navigate to page
agent-browser snapshot -i                   # Get interactive elements with refs
agent-browser click @e1                     # Click element by ref
agent-browser fill @e2 "text"               # Fill input by ref
agent-browser screenshot qa-screenshots/... # Capture screenshot
agent-browser console                       # View console messages
agent-browser errors                        # View page errors
agent-browser network requests              # View tracked network requests
agent-browser network requests --filter api # Filter to API calls
agent-browser set viewport 1440 900         # Desktop viewport
agent-browser set viewport 375 812          # Mobile viewport
agent-browser close                         # Close browser
```

After every page load or action, run `agent-browser console` and `agent-browser errors` to check for issues, and `agent-browser network requests` to check for failed API calls.

## Playwright Spec Template

Specs live in `e2e/{scope}.spec.ts`. Run with `npx playwright test e2e/{scope}.spec.ts`.

### Setup (once per project)

```bash
npm install -D @playwright/test
npx playwright install chromium
```

`playwright.config.ts` minimal config:
```ts
import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './e2e',
  use: {
    baseURL: process.env.BASE_URL ?? 'http://localhost:3000',
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
  },
});
```

### Spec structure

```ts
// e2e/{scope}.spec.ts
import { test, expect, Page } from '@playwright/test';

// ── shared setup ─────────────────────────────────────────────────────────────

test.beforeEach(async ({ page }) => {
  // seed state or log in if needed
  await page.goto('/login');
  await page.getByLabel('Email').fill('test@example.com');
  await page.getByLabel('Password').fill('password');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/dashboard/);
});

// ── happy path ────────────────────────────────────────────────────────────────

test('creates a {entity} successfully', async ({ page }) => {
  await page.goto('/datasets/new');

  await page.getByLabel('Name').fill('Test Dataset');
  await page.getByRole('button', { name: 'Create' }).click();

  // assert redirect + success state
  await expect(page).toHaveURL(/datasets\/\d+/);
  await expect(page.getByRole('heading')).toContainText('Test Dataset');
  await expect(page.getByText('Dataset created')).toBeVisible();

  await page.screenshot({ path: 'qa-screenshots/e2e-datasets-create-pass.png' });
});

// ── validation / error state ──────────────────────────────────────────────────

test('shows validation error on empty submit', async ({ page }) => {
  await page.goto('/datasets/new');
  await page.getByRole('button', { name: 'Create' }).click();

  await expect(page.getByText('Name is required')).toBeVisible();
  await page.screenshot({ path: 'qa-screenshots/e2e-datasets-create-validation.png' });
});

// ── full lifecycle ─────────────────────────────────────────────────────────────

test('create → edit → delete lifecycle', async ({ page }) => {
  // CREATE
  await page.goto('/datasets/new');
  await page.getByLabel('Name').fill('Lifecycle Test');
  await page.getByRole('button', { name: 'Create' }).click();
  await expect(page).toHaveURL(/datasets\/\d+/);

  // EDIT
  await page.getByRole('button', { name: 'Edit' }).click();
  await page.getByLabel('Name').fill('Lifecycle Test Edited');
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByRole('heading')).toContainText('Lifecycle Test Edited');

  // DELETE
  await page.getByRole('button', { name: 'Delete' }).click();
  await page.getByRole('button', { name: 'Confirm' }).click();
  await expect(page).toHaveURL(/datasets$/);
  await expect(page.getByText('Lifecycle Test Edited')).not.toBeVisible();
});
```

### Locator priority (most → least stable)

```ts
page.getByRole('button', { name: 'Submit' })     // 1. ARIA role + name — most stable
page.getByLabel('Email')                          // 2. form label association
page.getByText('Welcome back')                    // 3. visible text
page.getByPlaceholder('Enter your email')         // 4. placeholder
page.locator('[data-testid="submit-btn"]')         // 5. explicit test ID
page.locator('.submit-button')                    // ❌ avoid — CSS breaks on refactor
page.locator('button:nth-child(3)')               // ❌ never — positional, always breaks
```

### Common assertion patterns

```ts
// URL
await expect(page).toHaveURL(/dashboard/);
await expect(page).toHaveURL('http://localhost:3000/settings');

// Visibility
await expect(page.getByText('Success')).toBeVisible();
await expect(page.getByText('Error')).not.toBeVisible();

// Content
await expect(page.getByRole('heading')).toContainText('My Dataset');
await expect(page.getByRole('list')).toContainText('Item 1');

// Count
await expect(page.getByRole('listitem')).toHaveCount(3);

// Input value
await expect(page.getByLabel('Name')).toHaveValue('Test');

// Network (intercept before action)
const responsePromise = page.waitForResponse('**/api/datasets');
await page.getByRole('button', { name: 'Save' }).click();
const response = await responsePromise;
expect(response.status()).toBe(200);
```

### Naming convention

```
e2e/{scope}.spec.ts                                    # spec file
qa-screenshots/e2e-{scope}-{scenario}-{state}.png      # manual screenshots
playwright-report/                                     # auto-generated HTML report
```

Examples:
- `e2e/auth.spec.ts` — login, logout, register, forgot password
- `e2e/datasets.spec.ts` — create, view, edit, delete datasets
- `e2e/team-management.spec.ts` — invite, remove, role changes

## Screenshot Naming

```
qa-screenshots/qa-{scope}-{scenario}-{state}.png
```

## QA Report Template

Maintain `QA_REPORT.md` in the app root:

```markdown
# QA Report
**Date:** YYYY-MM-DD | **Environment:** localhost:PORT | **Data:** [seed method]

## Summary
- **Scopes tested:** N | **Scenarios:** N | **Issues:** N (X critical, Y high, Z medium, W low) | **Fixed:** N

## Results by Scope

### Scope: [Feature Area]
| # | Scenario | Status | Screenshot | Issues |
|---|----------|--------|------------|--------|
| 1 | [description] | pass/fail | [link](qa-screenshots/...) | — |

## Remaining Issues
| # | Severity | Scope | Description | Screenshot |
|---|----------|-------|-------------|------------|
| 1 | CRITICAL | [scope] | [what's broken] | [link](qa-screenshots/...) |

## Fixes Applied
### N. [SEVERITY] Short description
- **File:** `path/to/file.tsx:line`
- **Root cause:** Why it happened
- **Fix:** What was changed
- **Before/After:** [link](qa-screenshots/before.png) → [link](qa-screenshots/after.png)

## Files Modified
| File | Change |
|------|--------|

## Console & Network Errors
| Page | Error | Severity | Fixed? |
|------|-------|----------|--------|
```

## PM Verdict Template

Add to end of QA report after all testing is complete:

```markdown
## Product Quality Assessment
### Overall: [SHIP / SHIP WITH CAVEATS / DO NOT SHIP]

### What works well
### Critical blockers (must fix before any user sees this)
### High-priority issues (fix this week)
### Polish items (not urgent)
### Missing features users would expect
### UX red flags
```

## Common Bug Patterns

| Pattern | Example | Fix |
|---------|---------|-----|
| Wrong field name | `data.orderTotal` vs `data.total` | Check API response shape |
| Missing include | API returns `null` for relation | Add `include`/`join` to query |
| Stuck loading | Unhandled async error | Add error handling to async call |
| Hydration mismatch | Server/client render different HTML | Check conditional rendering |
| CORS error | Frontend can't reach API | Check proxy/CORS config |
| Stale cache | Old data after mutation | Invalidate query cache |
| Memory leak: detached DOM | Components removed but refs held in closures | Null refs in cleanup, use WeakRef |
| Memory leak: orphaned timer | `setInterval` not cleared on unmount | Return cleanup from `useEffect` |
| Memory leak: event listener | Global listener added without removal | Add `removeEventListener` in cleanup |
| Memory leak: stale closure | Callback captures old state/large objects | Use refs or memoize dependencies |
| Memory leak: uncancelled fetch | Pending requests after navigation | Use `AbortController` signal |
| Memory leak: blob/object URL | `URL.createObjectURL` never revoked | Call `URL.revokeObjectURL` after use |
