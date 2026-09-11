---
type: Gotcha
title: TTS License Acceptance Must Validate the Exact License Text Hash
description: TTS license acceptance must validate engine and exact license text/hash instead of trusting client-supplied or synthetic metadata.
resource: internal/tts/supervisor.go; internal/server/handler_tts.go; web/src/components/Settings/TTSForm.tsx
tags:
  - TTS
  - license
  - security
  - acceptance
  - hash-validation
  - compliance
timestamp: 2026-09-11T07:37:45Z
---
## Gotcha

TTS license acceptance is currently recorded from client-supplied identity fields without proving that they describe the license the user accepted. The settings UI submits `license_hash: "sha256-" + engine.id` and a generated name (`<engine label> License`), rather than a cryptographic hash of the actual license text. The backend accepts and persists those values without validating them against engine license metadata.

This breaks the acceptance invariant: consent must be tied to the exact engine and the exact license text/version shown to the user. An engine identifier or arbitrary client-provided hash is not evidence that the displayed terms were accepted, and a caller could submit a mismatched license name or hash.

## Required behavior

- Keep authoritative license metadata with the engine definition: exact engine identity, license name, and license text (or a canonical source plus immutable content).
- Compute the license hash from the canonical license text using the documented algorithm and compare it server-side; do not trust `license_hash` or `license_name` from the client as authoritative.
- Require acceptance to identify the engine and match the current metadata hash/name. Reject missing, stale, or mismatched values rather than recording acceptance.
- The UI must display the same authoritative license text whose hash is submitted, and must submit the computed metadata hash—not a synthetic value derived from the engine ID.
- Preserve the engine- and license-hash-specific nature of recorded consent. A license-text change must require fresh acceptance.

## Current evidence

- `web/src/components/Settings/TTSForm.tsx` constructs the synthetic hash and license name before calling `POST /api/tts/license`.
- `internal/server/handler_tts.go` decodes `license_hash` and `license_name` and passes them through to the supervisor.
- `internal/tts/supervisor.go` stores those values in `EngineInstall` without checking them against authoritative license metadata.
- The existing state-machine design correctly treats acceptance as a separate transition, but state transition gating alone does not provide license identity or text integrity.

## Security and compliance impact

The persisted record can claim acceptance for terms other than the terms presented to the user. This weakens auditability and makes license acceptance spoofable by any caller that can reach the authenticated endpoint. Acceptance must remain explicit consent recording only; it must not imply that an unavailable engine was installed or enabled.

## Validation expectations

Cover both layers with tests: verify the UI sends the authoritative license hash and displays the matching text, and verify the backend rejects unknown engines, unknown/mismatched license names, missing hashes, synthetic or stale hashes, and hashes that do not match the canonical license text. Verify that a valid engine/text/hash tuple records acceptance and that changing the license text invalidates prior acceptance.