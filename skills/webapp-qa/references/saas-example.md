# Domain Example: SaaS Dashboard QA Scopes

Illustration of expected depth for a SaaS/project management app. For your app, derive equivalent scopes from the codebase.

Severity tags: `[C]` = CRITICAL, `[H]` = HIGH, `[M]` = MEDIUM, `[L]` = LOW.

## Scope: Authentication & Onboarding — Priority: CRITICAL
- `[C]` Signup — creates account, sends verification email / logs in
- `[H]` Signup — duplicate email, weak password, empty fields show errors
- `[C]` Login — valid credentials logs in, redirects to dashboard
- `[H]` Login — wrong password shows clear error
- `[C]` Logout — clears session, protected pages inaccessible
- `[C]` OAuth login — redirects to provider, returns with session
- `[H]` Onboarding flow — new user sees setup wizard, can complete or skip
- `[M]` Password reset — request, email received, reset works, old password invalid

## Scope: Dashboard & Analytics — Priority: HIGH
- `[C]` Dashboard loads with real data (not zeros when data exists)
- `[H]` Metrics match underlying data (spot-check a count against list view)
- `[H]` Charts render with correct axes, labels, data points
- `[M]` Date range picker — changes data, default is sensible
- `[M]` Dashboard with no data — shows empty state, not broken charts
- `[L]` Dashboard responsive — charts readable on mobile

## Scope: Project/Workspace CRUD — Priority: CRITICAL
- `[C]` Create project — name, description → appears in project list
- `[C]` Edit project — changes persist, list updates
- `[C]` Delete project — confirmation, removed from list, cascade (tasks/members gone)
- `[H]` Project detail — shows all fields, related tasks, members
- `[H]` Duplicate project name — error or allowed with disambiguation
- `[M]` Project settings — editable by owner/admin only

## Scope: Task/Item Management — Priority: CRITICAL
- `[C]` Create task — title, description, assignee, due date → appears in list/board
- `[C]` Edit task — changes persist, reflected in all views (list, board, detail)
- `[C]` Delete task — confirmation, removed from all views
- `[C]` Status transitions — drag on board or dropdown: todo → in progress → done
- `[H]` Invalid transitions blocked (if applicable)
- `[H]` Assign/unassign user — assignee shown on card, user's task list updates
- `[H]` Due date — past due shows warning styling
- `[M]` Comments on task — add, edit, delete, shows author and timestamp
- `[M]` File attachments — upload, download, delete
- `[M]` Filters — by status, assignee, date, label
- `[M]` Sort — by date, priority, name
- `[L]` Bulk actions — select multiple, change status, delete

## Scope: Team & Permissions — Priority: CRITICAL
- `[C]` Invite member — email sent or link generated, member joins
- `[C]` Role assignment — admin/member/viewer, each sees correct UI
- `[C]` Permission boundaries — viewer can't edit, member can't delete project
- `[H]` Remove member — loses access immediately
- `[H]` Transfer ownership — new owner gets full control
- `[M]` Team-scoped data — members only see their team's projects/tasks
- `[M]` Cross-team isolation — can't access another team's resources via URL

## Scope: Settings & Billing — Priority: HIGH
- `[H]` Profile edit — name, avatar, email change works
- `[H]` Notification preferences — toggle on/off, changes persist
- `[C]` Plan/billing page — shows current plan, usage
- `[C]` Upgrade/downgrade — plan changes reflected, features unlock/lock
- `[H]` Payment method — add, update, remove
- `[M]` Invoice history — shows past charges with correct amounts

## Scope: Notifications & Real-time — Priority: MEDIUM
- `[H]` Notifications appear for relevant events (assignment, comment, mention)
- `[M]` Mark as read — count updates
- `[M]` Click notification — navigates to correct entity
- `[M]` Real-time updates — another user's change appears without refresh (if applicable)

## Scope: Search & Navigation — Priority: HIGH
- `[H]` Global search — returns projects, tasks, users
- `[H]` Search with no results — shows helpful empty state
- `[M]` Search with special characters — doesn't break
- `[H]` Sidebar navigation — all links work, active state correct
- `[M]` Keyboard shortcuts — documented shortcuts work (if applicable)
