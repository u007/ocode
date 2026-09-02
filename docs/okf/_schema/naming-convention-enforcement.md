---
type: Schema
title: OKF Naming Convention Enforcement
description: OKF naming conventions for question IDs, bundle entries, and index format to maintain bundle integrity.
tags:
  - okf
  - schema
  - naming-convention
  - bundle-integrity
timestamp: 2026-09-02T07:06:56Z
---
## OKF Naming Convention Enforcement

Consistent naming for question IDs and bundle entries prevents migration
issues, broken cross-references, and silent data loss during bundle
upgrades. This schema defines the conventions that every OKF bundle entry
must follow.

### Question ID format

Question IDs are the stable identifiers used across scores, answers, and
derived skill files. They must:

1. Use lowercase-kebab-case: `my-question-id`
2. Contain only `[a-z0-9-]` — no underscores, spaces, uppercase, or special
   characters.
3. Be globally unique within a question domain (e.g. within `conduct/`,
   within `csharp/`, etc.).
4. Not be reused after deprecation — once a question ID is retired, it is
   dead forever in that domain.

### Bundle entry naming

Files in the OKF bundle follow a strict hierarchy:

```
okf/<domain>/questions.md          — canonical question list
okf/<domain>/scores/<model>.md    — raw model scores per question
okf/<domain>/answers/<model>.md   — full answer transcripts
okf/<domain>/derived/<domain>.<model>.SKILL.md  — corrective skills
```

Rules:

1. **Domain directory** — lowercase, single word or hyphenated: `conduct`,
   `nextjs`, `dotnet`. No nesting beyond one level.
2. **Model filenames** — the model id with `/` replaced by `__` (double
   underscore): `tencent__hy3.md`, `mimo-v2.5.md`. The double underscore
   is the filesystem-safe encoding of the provider/model separator.
3. **Derived skill filenames** — `<domain>.<model>.SKILL.md`. The domain
   prefix is redundant but disambiguates when skills from different domains
   are loaded together.
4. **Score filenames** — `<model>.md` under `scores/`. No other files in
   `scores/` except the index `README.md`.

### Index entry format

`docs/index.md` entries must match the file's actual path and frontmatter:

```markdown
- [Display Title](relative/path.md) - Description from frontmatter.
```

Entries are sorted by their path within each section. Sections are sorted
alphabetically. The `log.md` is auto-generated and must never be edited
manually.

### Migration considerations

- Renaming a question ID breaks every score file, answer file, and derived
  skill that references it. Treat question IDs as immutable once shipped.
- Adding a new domain directory requires updating `docs/index.md` with a
  new section header.
- Model filename changes (`/` → `__`) are a one-way encoding; never
  decode back to `/` in filenames.