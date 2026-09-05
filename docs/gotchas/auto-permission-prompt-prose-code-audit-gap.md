---
type: Gotcha
title: Auto-Permission Prompt Prose Code Audit Gap
description: The bundled gatekeeper prompt v1.9.0 enumerated non-existent git config forms (e.g., --set) and omitted real ones. Resolved in v1.9.2: prose synced with the –c dangerous-key list and a code backstop (`gitConfigWriteArgs` in IsHarmfulBashCommand, internal/agent/permissions.go) hard-blocks git config writes; ~25 regression cases in TestIsHarmfulBashCommand_GitConfigWrites (internal/agent/permissions_test.go). Prose and code must be audited together.
tags:
  - auto-permission
  - gotcha
  - prompt
  - security
  - git
timestamp: 2026-09-05T02:15:35Z
---
## Resolved

- **Gap:** The bundled gatekeeper prompt v1.9.0 enumerated non-existent git config forms (e.g., --set) and omitted existing ones (e.g., git config <name> <value>), creating a security gap in command allowlisting.
- **Fix:** In v1.9.2, prose synced with the –c dangerous-key list and a code backstop was added: `gitConfigWriteArgs` in `IsHarmfulBashCommand` (`internal/agent/permissions.go`) hard-blocks git config writes; ~25 regression cases verified in `TestIsHarmfulBashCommand_GitConfigWrites` (`internal/agent/permissions_test.go`). Prose and code must be audited together.
- **Date:** 2026-09-05
