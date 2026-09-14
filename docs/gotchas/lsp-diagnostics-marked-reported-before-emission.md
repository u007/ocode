---
type: Gotcha
title: LSP Diagnostics Marked Reported Before Emission
description: 'Confirmed regression: LSP delta injection fingerprints URIs before cap checks and emission, permanently suppressing diagnostics that were never delivered.'
resource: internal/agent/lsp_inject.go
tags:
  - lsp
  - diagnostics
  - agent
  - prompt
  - regression
  - gotcha
timestamp: 2026-09-14T01:56:42Z
---
# LSP Diagnostics Marked Reported Before Emission

**Type:** Gotcha  
**Description:** `injectLSPDelta` records each changed URI as reported before applying the diagnostics delta cap and before confirming that its diagnostics were emitted into the model request.

## Symptom

When several files have changed diagnostics, the delta reaches its line cap. Diagnostics for URIs encountered after the cap are omitted from the emitted `[ocode:lsp]` block, yet those URIs are treated as already reported. If the capped block is not delivered or emission otherwise fails, those diagnostics can remain absent from later requests even though the model never received them.

## Cause

In `internal/agent/lsp_inject.go`, `injectLSPDelta` iterates over sorted changed URIs and assigns `a.lspSeen[uri] = fingerprintDiagnostics(diags)` before checking whether `lines >= lspDeltaLineLimit` and before rendering the URI. Once the cap is reached, the loop continues and marks the remaining URIs as seen while skipping their output.

The per-URI fingerprint is intended to mean that the diagnostic set was shown to the model. Updating it before successful emission breaks that meaning. The same state is shared with `markLSPReported`, which records diagnostics only when the edit-result path actually attaches them.

## Invariant

A diagnostic fingerprint may be recorded as reported only for diagnostics that were actually included in the outgoing model message or tool result. Diagnostics skipped by the cap must remain pending so a later request can emit them. If message construction or delivery can fail, the state update must occur only after the emission path has succeeded, or the implementation must retain the URI as pending for retry.

The cap must also account for the rendered header and truncation text consistently; the limit is a presentation bound, not permission to discard unreported diagnostic state.

## Verification

The relevant path is `internal/agent/lsp_inject.go:injectLSPDelta`, where `a.lspSeen[uri]` is updated before the cap check. Existing LSP tests cover user-role delivery, but a regression test should create enough changed URIs to exceed `lspDeltaLineLimit`, verify that only the emitted URIs become seen, and then verify that a subsequent injection emits the omitted diagnostics. Include an emission-failure case if the final implementation has a fallible delivery boundary.
