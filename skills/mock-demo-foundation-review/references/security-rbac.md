# Security & RBAC Checklist

A mock usually has no auth, a hard-coded user, or a role switch in the header. That is
fine for a demo and a finding for a foundation, because the permission model shapes the
schema (owner columns, role tables, audit) and is the hardest thing to bolt on after
users exist. Audit what the screens imply, not what the demo enforces.

Contents: 1 Identity & authentication · 2 Authorization model · 3 RBAC design · 4 Row
and field scoping · 5 Enforcement placement · 6 Audit & accountability · 7 Data
protection · 8 Input & transport · 9 Demo signals

## 1. Identity & authentication

- One identity per human and per service; a user row with a stable id separate from
  email so an email change does not break history.
- Authentication is delegated (OIDC / SSO / a maintained auth library), not hand-rolled
  password handling in the demo backend.
- Sessions or tokens expire, can be revoked, and are bound to the user id, never to a
  role or an email.
- Service-to-service and import jobs run as named service identities with their own
  permissions and their own `created_by`.
- Invitations, password reset and email change are single-use, expiring, and logged.

## 2. Authorization model

Derive it from the UI before judging it:

- List every action the screens offer (view, create, edit, delete, approve, export,
  invite, change settings) per entity.
- List every actor the screens imply (admin, manager, member, viewer, external
  customer, support, service).
- Build the actor × action matrix. Empty cells are decisions the mock silently made;
  put them in the report's open decisions.
- Decide the scope of each grant: global, per organisation, per team, per record
  (ownership), per field. The scope decides the schema.

## 3. RBAC design

- **Roles are data, permissions are code.** Permissions are a fixed enumerated set in
  code (`invoice.approve`, `member.invite`); roles are rows that bundle permissions and
  can be renamed or added without a deploy. Hard-coded `if (user.role === 'admin')`
  scattered through handlers is a finding.
- **Roles are scoped**: a membership row `(user_id, org_id, role_id)` not a `role`
  column on the user. A user is an admin of one organisation and a viewer of another.
- **Keep the role count small and the permission set complete.** Three to six roles
  cover most products; the permission list must cover every action in §2.
- **Ownership is a permission condition, not a role**: "edit own records" is a
  permission plus a check that `owner_id = current user`, not an "owner" role.
- **Attribute conditions** (status, amount thresholds, record age) are expressed in one
  policy layer, not in each handler. If the product clearly heads toward "managers can
  approve under $10k", plan the policy function now even if the first rule is simple.
- **Deny by default.** A new permission grants nothing until a role includes it; a new
  endpoint is closed until a permission is attached.
- **Super-admin and support access are separate, logged and time-boxed**, not a role
  anyone can be given from the UI.
- **The UI reads the same permission set** to hide and disable controls; hiding is UX,
  the server decision is the rule.

## 4. Row and field scoping

- Every tenant-scoped query carries the tenant predicate from a single enforced place
  (a scoped repository, row-level security, or a query builder that refuses unscoped
  queries), never remembered per handler.
- Cross-tenant references are impossible by construction (foreign keys include the
  tenant column, or the write layer verifies both sides belong to the caller).
- Field-level restrictions (salary visible to HR only, cost visible to managers only)
  are enforced in the serializer, not by the UI omitting a column.
- Export, search, autocomplete and "related items" panels obey the same row scoping as
  the main list; these side paths are where leaks happen.
- Ids are not guessable as a security measure, but unguessable ids are not a
  substitute for the scoping check.

## 5. Enforcement placement

- The check happens in the server request path, once, before the handler runs, for
  every route including bulk, import, export and webhooks.
- Mutations verify the target row's tenant and the caller's permission on that row,
  not just the caller's role in general.
- Background jobs and scheduled tasks act as a named identity and re-check permissions
  against current state, not the state at enqueue time.
- Rate limits exist on authentication, invitations, exports and anything that sends
  email.
- A permission denied returns the same shape as not found for row-scoped reads where
  existence itself is sensitive.

## 6. Audit & accountability

- Every permission-changing action (role grant, invitation, removal, settings change)
  is in an append-only audit table with actor, target, before, after, time, and the
  request origin.
- Every mutation on money, status, access and personal data is attributable to a user
  or service identity (`created_by`, `updated_by`, audit rows).
- Impersonation and support access are audited with the real actor and the acted-as
  user.
- Audit rows are never deleted by tenant-level cleanup; retention is a separate policy.

## 7. Data protection

- Classify fields: public, internal, personal, sensitive (financial, health,
  credentials). The classification drives field scoping, logging redaction and
  retention.
- Secrets and credentials are never in rows the UI reads; integrations store tokens
  encrypted with a key outside the database.
- Personal data has a deletion or anonymization path that keeps referential integrity
  and audit history (anonymize in place, keep the id).
- Logs and error reports redact personal and sensitive fields.
- Backups inherit the same access control as the database.
- Exports are scoped, logged, and expire if delivered by link.

## 8. Input & transport

- All input is validated server-side with an allowlist shape (see `ui-forms.md` §3);
  mass assignment of arbitrary fields from the request body is a finding.
- Queries are parameterized; string-built SQL anywhere in the demo is a finding if the
  code is kept.
- Output encoding on every user-supplied string rendered in HTML; rich text goes
  through a sanitizer with an allowlist.
- File uploads: content type verified from bytes, size capped, stored outside the web
  root under a generated name, served with a safe content disposition.
- CSRF protection on cookie-authenticated mutations; CORS allowlist not wildcard when
  credentials are used.
- Transport is TLS only; security headers (CSP, frame options, referrer policy) are
  set by the server, not left to a later hardening pass.

## 9. Demo signals

Patterns that are acceptable in a mock and must appear in the foundation contract so
the real build does not inherit them:

- A role dropdown in the header that changes what the UI shows, with no server
  involvement.
- A single hard-coded user or `currentUser` constant.
- Routes that accept an `orgId` or `userId` query parameter and trust it.
- Authorization in route guards on the client only.
- `isAdmin` boolean on the user object as the whole permission model.
- Delete and export buttons that call endpoints with no permission check.
- Mock API that returns every tenant's data and lets the client filter.
- Tokens, API keys or seed credentials committed in the demo repository.

Each becomes a finding only if the code is kept; otherwise record the rule it violates
in the contract and move on.
