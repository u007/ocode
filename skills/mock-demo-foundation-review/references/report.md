# Report Template

Use this structure. Keep every section as short as the findings allow; do not pad. Each
finding cites where it was seen.

```markdown
# Foundation Review: <product / mock name>

**Verdict:** Build on it | Fix first | Re-architect
**Why in two sentences:** <the single biggest reason, then the second>

## 1. What was reviewed
<one paragraph: material received, what was driven or read, what was missing>

## 2. Implied data model
| Entity | Natural key | Owner column | Relationships | Lifecycle states | Gaps |
|--------|-------------|--------------|---------------|------------------|------|

<2 to 5 lines on where the UI and any existing schema disagree>

## 3. Findings
| # | Severity | Lens | Finding (where seen) | Why it costs later | Fix | Effort now | Effort after launch |
|---|----------|------|----------------------|--------------------|-----|------------|---------------------|

Severity: blocker (must change before the first real row) · major (change before
launch) · minor (backlog). Effort in days; the "after launch" column is the point.

## 4. Lens summaries
**Tech lead:** <3 lines, includes the slowest implied query and the fix>
**Project manager:** <3 lines, includes the before-first-row list>
**CIO:** <3 lines, includes the actor × action matrix gaps and audit coverage>
**Data analyst:** <3 lines, includes the first-five-reports result>

## 5. Foundation contract
The rules the build must satisfy regardless of release size. Checkable, not aspirational.
- [ ] Every entity has a surrogate id and a named natural key with a DB unique constraint
- [ ] Every row has an owner column, created/updated at/by, and soft delete where referenced
- [ ] Every reference is a foreign key; no entity is linked by text
- [ ] Every enum / lookup is a native enum, CHECK or lookup table; no free-text dimensions
- [ ] Every column has the narrowest correct type; every text column has a length limit
- [ ] Every field passes the type matrix: typed input → client check → server check → typed column
- [ ] Every validation rule exists server-side; the form mirrors it
- [ ] Seeds are idempotent (lookup, then insert or update); reference and demo data are separate sets
- [ ] Demo seeds cover every screen, status, role and relationship; one command loads them
- [ ] Every edit carries a version; every create is idempotent
- [ ] Imports and APIs use the same write path and the same rules as the form
- [ ] Each rule, enum and entity shape is defined once in code and imported; no string-literal status values at use sites
- [ ] No secrets or environment URLs in source; one config module; no swallowed errors
- [ ] Every list is server-side sorted and paginated; every filter and sort column is indexed
- [ ] Unbounded tables (events, logs, audit) have a retention and partition plan before launch
- [ ] Money in minor units + currency; timestamps in UTC; dates as dates
- [ ] A merge path exists for every keyed entity
- [ ] Permissions are an enumerated set in code; roles are scoped membership rows; deny by default
- [ ] Every query is tenant-scoped from one enforced place; every mutation checks the target row
- [ ] Permission, access and money changes land in an append-only audit table with the actor
<add or remove lines to match the product; delete any line the product truly never needs>

## 6. Sequenced next steps
1. Before first row: <blockers>
2. Before launch: <majors>
3. Backlog: <minors>

## 7. Open decisions
| Decision the mock silently made | Options | Recommended | Owner |
|---------------------------------|---------|-------------|-------|
```

## Writing rules

- Lead with the verdict. A reader who stops after the first three lines knows what to do.
- Name the screen, field or file for every finding. No finding without a location.
- "Effort after launch" is an estimate with reasoning (rows to backfill, systems to
  dual-run, reports to restate), not a guess.
- Keep the foundation contract to rules that are expensive to retrofit. If a line is
  cheap to add later, it is backlog, not contract.
- Do not recommend technology choices unless the mock already made one that is a
  finding.
