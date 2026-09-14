---
type: Gotcha
title: TUI Selection Context Lost on Double Preparation
description: 'Confirmed regression: Agent.Step re-prepares TUI-prepared messages with an empty selection and removes the existing [ocode:selection] context.'
resource: internal/tui/model.go; internal/agent/prompt.go; internal/agent/agent.go
tags:
  - tui
  - selection
  - agent
  - prompt
  - regression
  - gotcha
timestamp: 2026-09-14T01:56:29Z
---
# TUI Selection Context Lost on Double Preparation

**Type:** Gotcha  
**Description:** `Agent.Step` re-prepares messages after the TUI has already appended its `[ocode:selection]` context, so explicitly selected file and line context can be silently removed from the model request.

## Symptom

A user selects files or a line range in the TUI and submits a prompt. The TUI initially prepares the outgoing messages with a user-role `[ocode:selection]` block, but the model request may contain no selected-file or selected-line context. There is no user-visible error; the model simply receives less context than the UI indicates.

## Cause

The TUI prepares messages in `internal/tui/model.go` through `prepareAgentMessages`, passing `m.buildSelectionContext()` to `Agent.PrepareMessages`. `PrepareMessages` intentionally removes any existing `[ocode:selection]` marker before appending the current selection, making repeated preparation idempotent for a supplied selection value (`internal/agent/prompt.go`).

Later, `Agent.Step` calls `a.PrepareMessages(messages, "")` (`internal/agent/agent.go`). That second preparation treats the empty selection as authoritative and strips the already-prepared selection marker. The resulting request therefore omits the context the TUI had explicitly selected.

## Invariant

Message preparation must be idempotent across the TUI-to-agent boundary without deleting a selection that was already prepared for the current turn. Either:

- avoid preparing the same message list twice, or
- make preparation distinguish “no new selection supplied” from “clear the current selection” and preserve an existing `[ocode:selection]` block in the former case.

Any fix must retain the cache-stability rule: selection is a volatile user-role tail block, not a system-role message.

## Verification

The relevant paths are:

- `internal/tui/model.go`: `prepareAgentMessages` adds the selection context.
- `internal/agent/prompt.go`: `PrepareMessages` strips `[ocode:selection]` before adding a supplied selection.
- `internal/agent/agent.go`: `Step` calls `PrepareMessages(messages, "")` again before the client request.

Add an end-to-end capture-client test that starts with TUI-prepared messages and verifies the client request still contains the selected file/line context after `Agent.Step`.
