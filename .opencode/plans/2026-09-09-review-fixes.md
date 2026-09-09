# Design: Sandbox docs fix + Dependency-output doc note

Status: Draft (pending user approval before edits)

## Design approval needed
- Confirm: proceed with both sections? Or only one?
- Confirm: use the precise warning text (integrity-only, not confidentiality)?
- Confirm: do NOT manually edit `docs/index.md`; only edit server response fields + design comments in code/docs source.

## 1. Sandbox persistence documentation fix (HIGH — design/policy gap)
- Modify `HandleGetPermissionModeConfig` (`internal/server/handler_config.go`) to include `"note": "sandbox = integrity-only write confinement; reads, network egress, and execution remain open — NOT secret/confidentiality protection"` in response when mode == `sandbox`.
- Modify `HandleSetPermissionModeConfig` same — include `"note"` in the `200` response when mode == `sandbox`.
- Update any server-side response schema expectations (tests). Add/update regression test verifying the `note` field appears for sandbox mode (e.g., in `permissions_mode_test.go` or a new targeted test).
- Do NOT edit `docs/index.md` (generated; use docs-workflow for durable docs if needed).
- This is an API response schema addition (additive public field); document it explicitly in comments above the DTO/handler.

## 2. Dependency-output injection trust-boundary documentation (HIGH — design gap)
- Add design comment above `buildPredecessorContext` (`internal/agent/task_dag.go`, ~line 740) describing exactly: predecessor output is bounded by `TruncateToolResult` (disk-cached, bounded length) but is NOT sanitized or escaped; it is treated as trusted session work-product within the agent session boundary; a malicious predecessor can influence the child's behavior. Reference the influence risk directly. Do NOT confuse with the shared-notes bus mechanism.
- Optionally add brief design note in `docs/superpowers/plans/PLAN-agent-crew/02-task-dag.md` (not `docs/index.md`).
- No behavior/code change beyond comments + optional doc source update.

## Implementation (after approval)
1. Edit `handler_config.go`: add `"note"` to `permissionModeConfigDTO` or response map for sandbox mode.
2. Update `task_contract_dispatch_test.go` or `permissions_mode_test.go` for regression coverage of new response field + sandbox behavior.
3. Edit `task_dag.go`: add design comment above `buildPredecessorContext`.
4. Optionally edit `PLAN-agent-crew/02-task-dag.md` source doc.
5. Run `gofmt`, targeted tests (`go test ./internal/agent -run 'TaskStatus|AgentStatus'` and `go test ./internal/server -run 'PermissionMode'`), verify build.
