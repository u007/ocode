---
version: 1.0.0
name: webapp-qa
description: Thorough QA testing of web applications with browser automation screenshots. Use when the user asks to QA, test, or verify a web app UI, find and fix bugs, do a visual walkthrough, or check pages for issues. Triggers include "QA the app", "test the UI", "check all pages", "find bugs", "visual walkthrough", "screenshot the pages", or any request to systematically test a web frontend.
---

# Web Application QA Testing

Systematic end-to-end QA of any web application. Test full user flows, not just page renders.

## Core Principle

You are a **Product Manager who also does QA**. Understand what the app does, derive test scopes from the codebase, and test every feature through its complete lifecycle — happy paths, edge cases, error states, and cross-feature integrations.

### Triage: Not All Bugs Are Equal

| Severity | Definition | Examples |
|----------|-----------|----------|
| **CRITICAL** | Data loss, security hole, crash, or total feature failure | Double-charge on submit, auth bypass, 500 on core flow |
| **HIGH** | Feature broken or unusable but no data loss | Form won't submit, search returns wrong results |
| **MEDIUM** | Degraded UX — feature works but poorly | Missing loading states, bad error messages |
| **LOW** | Cosmetic — no functional impact | FOUC, minor alignment, fallback font flash |

**Test in severity order.** CRITICAL and HIGH first. Report highest-severity findings first.

---

## Phase 1: Discovery & Setup

- Glob route files (`src/routes/**`, `src/pages/**`, `app/**/page.tsx`) to build the site map
- Identify public, authenticated, and admin pages
- Check servers needed (frontend, backend, microservices) and DB status
- Find and run seed scripts (`**/seed*.{ts,js}`, `**/fixtures/**`, check `package.json` for seed commands)
- If no seed exists, create data through the app/API (inability to create data = bug)
- **Check for existing `QA_REPORT.md`** — if it exists, read it first (see Resumption Rule below)
- Check `qa-screenshots/` for comparison baselines

### Resumption Rule: Fix Known Issues First

If `QA_REPORT.md` already exists with unfixed issues, **prioritize fixing those before discovering new ones** (unless the user asks otherwise). Work through the existing report's unfixed items in severity order (CRITICAL → HIGH → MEDIUM → LOW), verify each fix with before/after screenshots, and update the report. Only proceed to new test scopes after the backlog is clear.

---

## Phase 2: Scope-Based Test Planning

**Analyze the app before visiting pages.**

1. **Read routes, schema, and API endpoints** to understand what features exist
2. **Group by domain, not by page** — a "scope" is a feature area (e.g., "Team Management", "Invoicing")
3. **For each scope, ask:** What can users create, read, update, delete? What states can entities be in? What multi-step flows exist? What permissions apply? What cross-feature integrations exist?
4. **Assign priority** (CRITICAL/HIGH/MEDIUM/LOW) to each scope based on business impact
5. **Test flows, not pages** — complete lifecycle: `CREATE → VERIFY → EDIT → VERIFY → DELETE → VERIFY`
6. **Use the [checklist](references/checklist.md)** for specific verification items per scope (CRUD, flows, permissions, rendering, forms, responsive, security)

### Plan Output Format

```markdown
### Scope: [Feature Area]
**Priority:** CRITICAL / HIGH / MEDIUM / LOW
**Prerequisites:** [Data needed, auth state, prior steps]

**Scenarios:**
1. [Happy path] — [action and expected result]
2. [Variation] — [different input/state]
3. [Edge case] — [boundary condition]
4. [Error case] — [invalid input and expected handling]
5. [Cross-feature] — [integration with other scope]
```

---

## Phase 3: Execute Tests

1. Use the **agent-browser** skill for all browser interaction — see [command reference](references/templates.md#browser-automation)
2. **Always `agent-browser snapshot -i` after navigating or after any action that changes the DOM** — refs (`@e1`, `@e2`) go stale after DOM changes, so re-snapshot before interacting with new elements
3. After every page load or action, run `agent-browser console`, `agent-browser errors`, and `agent-browser network requests` to catch issues
4. **Memory leak checks:** After repeated navigation or CRUD cycles, run `agent-browser eval "performance.memory ? JSON.stringify(performance.memory) : 'N/A'"` to track heap growth. Compare heap snapshots before and after repeated actions — sustained growth indicates a leak. Check for detached DOM nodes, uncleared intervals/timeouts, and event listeners that survive component unmounts
5. Follow plan scope by scope, severity order
6. Screenshot every meaningful state change (after actions, not just page loads)
7. Read and analyze every screenshot — don't just collect
8. Test desktop and mobile using `agent-browser set viewport 1440 900` / `agent-browser set viewport 375 812`
9. When something fails, investigate immediately (Phase 4)
10. **Note every flow you exercise** — the exact steps, what you asserted, and what selectors/text uniquely identified each element. This is the raw material for Phase 6 Playwright specs.

---

## Phase 4: Debug Issues

Trace through layers: **UI** (wrong field names, null checks) → **API** (curl endpoint directly) → **DB** (query data/relations) → **Schema** (ORM field names). See [common bug patterns](references/templates.md#common-bug-patterns).

## Phase 5: Fix and Verify

Fix → re-run exact scenario → before/after screenshots → check for new console errors → re-test adjacent scenarios.

## Phase 6: Playwright Regression Specs

**agent-browser is the discovery tool. Playwright is the regression artifact.**

For every scope tested, write `e2e/{scope}.spec.ts`. See [Playwright spec template](references/templates.md#playwright-spec-template).

**Why Playwright, not agent-browser scripts:**
- agent-browser refs (`@e1`, `@e2`) are snapshot-relative — they shift on any DOM change, silently breaking scripts
- Playwright locators (`getByRole`, `getByText`, `data-testid`) are stable by design
- Built-in auto-wait and retry on every assertion — no manual `wait` calls needed
- Test isolation: fresh browser context per test, no state bleed
- CI-ready: `npx playwright test` with HTML reporter, failure screenshots, and trace viewer

**Workflow:**
1. Check if Playwright is installed — `cat package.json | grep playwright`. If not: `npm install -D @playwright/test && npx playwright install chromium`
2. Translate every flow you exercised with agent-browser into a `test()` block
3. Use stable locators — prefer `getByRole` > `getByText` > `data-testid` > CSS. Never use positional selectors
4. Run `npx playwright test e2e/{scope}.spec.ts` to verify specs pass
5. On failure, check the HTML report: `npx playwright show-report`

## Phase 7: QA Report

Maintain `QA_REPORT.md` in app root. See [report template](references/templates.md#qa-report-template).

## Phase 8: Cleanup

After QA and specs are written, clean up ephemeral test artifacts that shouldn't persist:

**Always clean:**
- Test data created through the UI (delete entities you created: datasets, teams, records)
- If the app has a seed/reset endpoint or CLI command, prefer that over manual deletion
- `agent-browser close` — release the browser session

**Keep:**
- `qa-screenshots/` — visual evidence for the QA report; commit these
- `e2e/*.spec.ts` — regression suite; commit these
- `QA_REPORT.md` — permanent record; commit this
- `playwright-report/` — add to `.gitignore`, regenerated on each run

**DB cleanup pattern** — if tests write to a real DB, clean up in Playwright's `afterEach`:
```ts
test.afterEach(async ({ request }) => {
  // delete via API if the app exposes a delete endpoint
  await request.delete(`/api/datasets/${createdId}`);
});
```

Or use a dedicated test DB (`TEST_DATABASE_URL`) so the whole DB can be wiped between runs.

## Phase 9: PM Verdict

Ship/no-ship assessment. See [verdict template](references/templates.md#pm-verdict-template).

---

## References

- [Per-page checklist](references/checklist.md) — severity-tagged rendering, data, nav, form, responsive, edge case, and security checks
- [Templates & code](references/templates.md) — browser commands, Playwright spec template, report template, PM verdict, bug patterns
- [Domain example: ecommerce](references/ecommerce-example.md) — scope breakdown for an online store
- [Domain example: SaaS dashboard](references/saas-example.md) — scope breakdown for a project management / SaaS app
