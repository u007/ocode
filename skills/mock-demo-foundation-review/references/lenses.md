# The Four Lenses

Answer each lens against the inventory and derived model. A lens that finds nothing says
so in one line. Findings must point at a screen, field, row of mock data, or line of
code.

Contents: 1 Tech lead · 2 Project manager · 3 CIO · 4 Data analyst · 5 How the lenses
disagree

## 1. Tech lead: will this carry real code?

- **Model fidelity**: does the UI's implied model match any schema or API already in the
  demo? Where they disagree, which one is right?
- **Rules placement**: which rules live only in the browser? Which live only in mock
  data? Which exist in the database?
- **Write path**: is there one write path per entity (service / repository / handler),
  or does each screen write its own way? Multiple write paths means multiple sets of
  rules and future divergence.
- **State**: where does the UI keep state that the server should own (filters, drafts,
  selections, computed totals)? Which of it is lost on refresh?
- **Concurrency**: what happens when two users edit the same record? When one request
  is retried?
- **Boundaries**: can a second client use the same backend? Is there a backend, or does
  the demo talk to local storage / JSON files / a mock server that will be "replaced
  later"?
- **Migrations**: are schema changes versioned files? Is the first real migration
  already shaped by the demo's shortcuts?
- **Dependencies**: which demo-only libraries (mock servers, fake auth, in-memory
  stores) are load-bearing in ways that will leak into production code?
- **Testability**: can the rules in the model be tested without the UI?
- **DRY and shape of the code**: which rules, enums, fetch paths and form plumbing are
  written more than once, and do the copies already disagree? Lenient bar, rule of
  three, checklist in `code-quality.md`; name only the shortcuts the next screens will
  copy.
- **Performance at scale**: for each screen, the implied query, its index, and what
  happens at ten million rows. Full checklist in `db-performance.md`; name the slowest
  implied query in the report.

## 2. Project manager: will this ship and keep shipping?

- **Scope truth**: which screens are real, stubbed, or decorative? Does the demo imply
  features nobody has committed to (reports, integrations, roles)?
- **Hidden work**: for each finding in the other lenses, what is the implementation cost
  now versus after launch with data in the system? Make the asymmetry visible.
- **Sequencing**: which foundation items must land before the first real row is written?
  Which can follow? Put the "before first row" list at the top of the report.
- **Decision debt**: which open questions does the mock silently answer (single vs multi
  tenant, soft vs hard delete, who owns a record)? Each needs an explicit owner and
  decision, not a default inherited from a prototype.
- **Stakeholder gaps**: who has not seen this that will have requirements for it
  (finance, support, compliance, the people who run reports)?
- **Definition of done** for the foundation: a short, checkable list, not "robust".
- **Risk register**: top three ways building on this mock causes a re-architecture, each
  with a trigger ("when we add a second customer", "when finance asks for monthly
  revenue by region").

## 3. CIO: will this survive the organisation?

- **Source of truth**: for each entity, is this system the master or a copy? If a copy,
  where does the id of the master live on the row?
- **Integration**: stable ids, timestamps and an API shape that a second system can
  consume without scraping the UI.
- **Security and access**: is authorization server-side and row-scoped? Does the mock
  show data the eventual user role should not see? Are permission-changing actions
  audited? Full checklist in `security-rbac.md`; the actor × action matrix from its §2
  belongs in the report.
- **Compliance and PII**: which fields are personal data? Is there a retention and
  erasure path that does not break referential integrity (soft delete, anonymize in
  place)?
- **Auditability**: can anyone answer "who changed this and when" for money, access and
  status changes?
- **Vendor and lock-in**: does the demo's stack (no-code backend, proprietary store,
  hosted mock API) decide the production stack by inertia? Say so if it does.
- **Operability**: backups, restore, observability and a way to fix bad data without a
  developer and a database console.
- **Total cost**: what the organisation pays if this model is wrong: data migration,
  dual-running, retraining, lost reporting history. Name it in days, not adjectives.
- **Exit path**: can the data leave the system complete and consistent (export with ids,
  relationships and history)?

## 4. Data analyst: will this answer questions?

- **Grain**: what is one row of each table? If a table mixes grains (a customer table
  that also holds their latest order), reports will double-count.
- **Keys for joins**: every relationship the analyst will need is a real foreign key.
  Free-text links make joins fuzzy forever.
- **Dimensions**: everything a report groups by is a typed, constrained value with a
  lookup table. List the dimensions the mock's screens imply (status, type, region,
  owner, channel) and check each.
- **Time**: event time versus record time; UTC; date versus timestamp; effective dating
  where "as of then" matters.
- **Completeness**: which fields are optional in the form but mandatory for the obvious
  report? Expect them to be empty.
- **Lineage**: `source`, `import_batch_id`, `created_by` so bad data can be traced to
  its entry path.
- **Duplicates**: the analyst sees every duplicate as a wrong count. Confirm the
  uniqueness design in `data-integrity.md` §2; a weak design here is a top finding.
- **Deletion and merge visibility**: counts over time need deleted and merged rows to
  remain in history.
- **Derived numbers**: which displayed totals are computed on the fly, which are stored,
  and what refreshes the stored ones? Stale cached totals become support tickets.
- **The first five reports**: name the five questions the business will ask first and
  check the model can answer each with a straightforward query. Any that cannot is a
  finding with the exact missing column or key.

## 5. How the lenses disagree, and how to resolve it

- Tech lead wants to defer; CIO wants everything auditable. Resolve by retrofit cost:
  audit columns and ids are cheap now and expensive later, so they go in; a full event
  store can wait if the audit columns exist.
- PM wants scope cut; analyst wants more fields. Resolve by making fields nullable and
  present in the model rather than collected in the form today; collect later without a
  migration.
- Everyone wants it simple. Agree: simple means few tables with strong keys, one write
  path, rules in the database. It does not mean free text and no constraints.
