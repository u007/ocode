---
name: mock-demo-foundation-review
description: Four-lens review (tech lead, PM, CIO, data analyst) of a UI mock, prototype, demo or early front end to decide if it is a foundation that lasts or will force a re-architecture. Derives the implied data model, then audits identity and duplicate prevention, referential integrity, column types/lengths/enums, form-to-schema parity and input-type validation, idempotent seeds and demo coverage, audit trail, DB performance (indexes, pagination, growth), security and RBAC, reporting readiness, and a lenient DRY / best-practices pass over demo code (only shortcuts the real build would copy). Produces a Build-on / Fix-first / Re-architect verdict, ranked findings and a foundation contract. Use when asked to "review this mock", "evaluate the demo", "is this UI ready to build on", "check the forms", "will this data model last", "find duplicate-data risks", "review as tech lead / CIO / PM / analyst", or when a Figma, screenshots, prototype URL or demo codebase is shared before real implementation starts.
when_to_use: When the user asks to review, evaluate or audit a UI mock, prototype, demo or early front end before real implementation; asks whether a data model, forms or seeds will last; asks for duplicate-data, DB performance, security, RBAC, DRY or code-quality risks in a demo; or asks for a tech lead / PM / CIO / data analyst review of screens, Figma, screenshots, a prototype URL or a demo codebase.
---

# Mock & Demo Foundation Review

Sit as a four-person panel. Each lens asks different questions; the verdict is one. The
bar is not "does the demo look good" but "if we build the real system on exactly this,
what breaks in month twelve and what does it cost to fix then versus now".

## Workflow

### 1. Inventory what was shared

Read everything before judging: screenshots, Figma, prototype URL, demo code, PRD, schema.
When a running demo exists, drive it (webapp-qa / htrcli) and submit every form once with
good data and once with bad data. When only code exists, read routes, components, forms,
mock data and any schema or migration files.

Produce a flat inventory:

- **Screens**: name, purpose, entry points
- **Forms**: every field, type, required?, validation seen, what it writes
- **Lists / tables**: columns shown, sort, filter, pagination, row actions
- **Entities implied**: every noun the UI creates, edits, links or counts
- **Flows**: create → edit → delete → search → report, per entity

If the material is too thin to do this, say so and ask for the specific missing piece.
Do not invent screens.

### 2. Derive the implied data model

The mock already has a schema; it is just not written down. Write it down:

- One table per entity, with the columns the forms imply
- Primary identity per entity: what makes two records "the same thing"?
- Relationships: which foreign keys the links, dropdowns and nested lists imply
- Lifecycle: which status values and transitions the buttons imply
- Ownership: which tenant / org / user owns each row

Read [references/data-integrity.md](references/data-integrity.md) and audit the derived
model against it. Every gap becomes a finding. Pay most attention to duplicate prevention:
a mock that lets the user type a customer name into a free-text box has already decided
that there will be five spellings of the same customer.

### 3. Audit the forms, lists and seeds

Read [references/ui-forms.md](references/ui-forms.md). Check every form and list in the
inventory against it. Form-to-schema parity is the core check: a field the UI collects but
the model cannot store, or a column the model needs but no form sets, is a finding. Run
every field through the type matrix in its §1 (input control → client check → server
check → column type) and fail the row on any mismatch.

Read [references/seeds.md](references/seeds.md). Check that seeds are idempotent
(re-runnable without duplicates, look-up-then-insert-or-update, never a blanket upsert),
that reference data and demo data are separate, and that the demo seed set covers every
screen, status, role and relationship in the inventory.

### 4. Audit performance and security

Read [references/db-performance.md](references/db-performance.md): write down the query
every screen implies and check what makes it fast at ten million rows, not ten. The ask
is simple indexing done right (foreign keys, unique keys, filter and sort columns,
tenant-leading composites), not a performance programme; recommend only what a named
query needs.

Read [references/security-rbac.md](references/security-rbac.md): derive the actor ×
action matrix the screens imply, then check the permission model, row scoping,
enforcement placement and audit trail. A mock with a role switcher in the header has
already made authorization decisions; write them down as open decisions.

When demo code exists, read [references/code-quality.md](references/code-quality.md) and
run its DRY and best-practices pass. The bar is lenient: a demo earns its shortcuts, and
only a shortcut the next five screens would copy (duplicated rules, string-literal
enums, per-screen data access, swallowed errors, secrets in source) is a finding. Grade
it per that reference; a duplicated foundation rule is graded under the reference that
owns the rule, not here. Top five compounding items at most; never style.

### 5. Run the four lenses

Read [references/lenses.md](references/lenses.md). Answer each lens's questions against the
inventory and model. Record only findings backed by something in the material (a screen, a
field, a line of code). "Auth looks weak" is not a finding; "the edit form has no version
field, so two users saving the same record silently overwrite each other" is.

### 6. Decide and report

Follow the template in [references/report.md](references/report.md). The verdict is one of:

- **Build on it**: the implied model and flows are sound; findings are additive
- **Fix first**: the foundation holds but listed blockers must change before the first
  real table is created (typically identity, uniqueness, ownership, audit)
- **Re-architect**: the mock encodes a model that cannot carry the stated goal; building
  it would create data that has to be migrated or thrown away

The report always ends with the **foundation contract**: the short list of rules the build
must satisfy regardless of how simple the first release is. Keep it to the rules whose
absence is expensive to retrofit. Everything else is backlog.

## Rules of judgment

- **Simple is fine; fragile is not.** A single table with the right keys, constraints and
  audit columns is a foundation. Twelve microservices with free-text identity are not.
  Never recommend complexity as insurance.
- **Retrofit cost decides severity.** Rank by how hard the fix is after real data exists:
  identity and uniqueness > ownership, tenancy and permission model > referential
  integrity > audit and timestamps > table grain and partitioning > validation parity >
  reporting fields > indexes > UX polish.
- **The database enforces, the UI assists.** Any rule only the form enforces does not
  exist (imports, APIs, a second client, and a retried request all bypass it).
- **Mock data is evidence.** Hard-coded sample rows show the author's mental model; look
  for duplicates, missing ids, inconsistent casing, and fields with no home.
- **Respect the stated scope.** If the user says the first release is a single-user tool,
  do not demand multi-tenancy today; demand that an `owner_id`-shaped column exists so it
  can be added without a rewrite. Flag, suggest, never veto a stated requirement.
- **Count honestly.** Three real findings are three findings, not ten padded ones.
- **Demo code is graded on what compounds.** Duplication and shortcuts are findings only
  when the real build would copy them; one working shortcut with one use site is fine.
  DRY is about rules, enums and data access agreeing, not about line count.
