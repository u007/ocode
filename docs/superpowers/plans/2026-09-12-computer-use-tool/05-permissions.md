# Part 05: Permission classification for computer actions

**Files:**
- Modify: `internal/agent/permissions.go` (`NewPermissionManager` default rule lists around lines 1299-1305; `Decide` immediately before the generic `level := pm.Check(toolName)` tail around line 1645; `isReadOnlyTool` around line 4103)
- Create: `internal/agent/permissions_computer_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `Decide("computer", args)` semantics below; unexported helper `computerAction(args json.RawMessage) string`.

## Behaviour

- Default rule: `computer` is added to the `PermissionAsk` list in `NewPermissionManager` (the `SetRule(..., PermissionAsk)` loop that holds `delete`, `bash`, `webfetch`).
- In `Decide`, before the generic tail: when `toolName == "computer"`, read `action` from args. If it is `screenshot`, `cursor_position` or `wait`, return `PermissionAllow` with a debug line `Decide ALLOW (computer observe): action=...`. Otherwise fall through to the generic tail so the tool rule applies: `Ask` by default, `Allow` once the user chose "always" (which persists `tool.computer` through the existing `PermissionScopeTool` path — no new persistence code).
- The Ask request for input actions carries `Rule: "tool.computer"` and `Command` set to a one-line summary such as `left_click at 412,300` (build it from the args so the dialog shows what will happen).
- `isReadOnlyTool` is NOT extended: in locked mode `computer` is denied, including screenshots.

## Steps

- [ ] **Step 1: Write failing tests** in `permissions_computer_test.go`:
  - `TestPermissions_ComputerDefaultAsk`: `NewPermissionManager().Check("computer") == PermissionAsk`.
  - `TestPermissions_ComputerObserveActionsAllowed`: table over `screenshot`, `cursor_position`, `wait` → `Decide` level Allow.
  - `TestPermissions_ComputerInputActionsAsk`: `left_click`, `type`, `key`, `scroll` → level Ask, `Request.Rule == "tool.computer"`, `Request.Command` contains the action name.
  - `TestPermissions_ComputerAlwaysPersists`: after `SetRule("computer", PermissionAllow)`, `left_click` → Allow.
  - `TestPermissions_ComputerLockedDenied`: `SetMode(PermissionModeLocked)`, `screenshot` → Deny.

- [ ] **Step 2: Run** `go test ./internal/agent -run 'Permissions_Computer' -v`. Expected: FAIL.

- [ ] **Step 3: Implement.**

- [ ] **Step 4: Run** `go test ./internal/agent -run 'Permissions'`. Expected: PASS, including existing permission tests.

- [ ] **Step 5: Commit** `git add internal/agent/permissions.go internal/agent/permissions_computer_test.go && git commit -m "feat(permissions): computer tool asks for input actions, allows observation"`.
