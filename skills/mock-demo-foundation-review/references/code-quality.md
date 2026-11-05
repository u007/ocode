# Code Quality: DRY & Best Practices (demo bar)

Apply only when demo code exists. The bar is **lenient**: a demo may cut corners, and
this checklist does not ask for production engineering. It asks one question per item:
*if the real build starts by copying this, does the shortcut compound?* A shortcut that
stays local (one file, one screen) is noted and ignored. A shortcut that every new screen
will copy is a finding.

Contents: 1 Grading · 2 DRY · 3 Best practices · 4 What not to flag

## 1. Grading

Grade each item, not the codebase:

- **Fine for demo**: local, cheap to change later, nobody will copy it. Not reported.
- **Compounds**: the next five screens will copy it, so fixing it later means fixing it
  in many places. Reported as *minor*, or *major* when it carries a data rule
  (validation, status values, ownership, money) that the database must own.
- **Blocker**: only when it hides a foundation rule from `data-integrity.md` or
  `security-rbac.md` (e.g. the only place a uniqueness or permission check exists is a
  copied `if` in three components). Grade it under that reference, not here.

Report at most the top five compounding items; everything else is one line of backlog.

## 2. DRY: duplication that compounds

Look for the same thing written more than once where the copies must agree:

- **Rules**: the same validation, status transition, permission check or price/total
  calculation in more than one component, handler or helper. Two copies drift; the
  database and one shared function should hold it.
- **Enumerations**: status, role, type or category values as string literals at each
  use site (`"active"`, `"Active"`, `'ACTIVE'`). One definition, imported everywhere;
  it is also the enum the schema will use.
- **Data access**: each screen fetches, maps and error-handles the same entity its own
  way. One client / repository per entity is enough; the shape of that one is what the
  real API will mirror.
- **Form plumbing**: copied field state, submit handlers and error display per form.
  One form helper or one library, applied the same way, is fine; five hand-rolled
  variants are a finding only if they differ in behaviour (trim, required, error text).
- **Mock data**: sample rows pasted into several components instead of one fixture
  module. Copies diverge and hide which fields are real (see `seeds.md`).
- **Layout**: copied page shells, tables and dialogs. Fine for demo unless the copies
  already differ in behaviour (sort, pagination, empty state).

Rule of three applies: a second copy is a note, a third is a finding.

## 3. Best practices: the short list that matters for a demo

- **Boundaries**: UI, data access and rules are in different places, even if each is
  one file. A component that renders, fetches, validates and computes totals will be
  copied as the pattern.
- **Config and secrets**: no credentials, API keys or environment URLs in source;
  one config module read from the environment, with an example file.
- **Errors**: failed requests are shown to the user and not swallowed; a `catch` that
  does nothing is a finding because every copy will do nothing too.
- **Identity in the UI**: rows are keyed and referenced by id, not by name, index or
  position (list keys, selection, links, dropdown values).
- **State ownership**: server-owned data is not duplicated into local state that is
  edited independently (then "saved" by overwriting).
- **Types**: entities have one declared shape (type, interface, schema) that forms,
  lists and fixtures import. Untyped `any` / dict blobs mean the schema lives nowhere.
- **Dead and stub code**: TODOs, commented blocks and stub handlers are listed in the
  report's scope-truth section (`lenses.md` §2), not treated as defects.
- **Dependencies**: demo-only packages (mock server, fake auth, in-memory store) are
  isolated behind one module so they can be swapped without touching screens.
- **Naming**: entity and field names in code match the names the UI and the derived
  model use. A `customer` that is `client` in one file and `account` in another is a
  model question, not a style one.

## 4. What not to flag

- Formatting, lint style, import order, file naming conventions.
- Missing tests, missing docs, missing CI. (Note once in scope truth if asked.)
- Framework choice, folder structure, component size, prop drilling.
- Hard-coded copy and labels, inline styles, placeholder images.
- Anything with a single use site that works. A demo earns its shortcuts; the review
  only tracks the ones the real build would inherit.
