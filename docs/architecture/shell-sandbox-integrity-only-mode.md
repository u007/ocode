---
type: Decision
title: Shell Sandbox Is Write-Integrity Only, Not Confidentiality
description: Decision to document the sandbox as integrity-only/write-confined with explicit confidentiality limitations, since global reads and network egress remain open.
tags:
  - architecture
  - sandbox
  - security
  - documentation
timestamp: 2026-08-31T17:26:30Z
---
# Sandbox as Integrity-Only / Write-Confined Mode

## Decision

The ocode shell sandbox is a **write-integrity boundary**, not a confidentiality boundary. Document this explicitly and warn users accordingly.

## What the Sandbox Restricts

- **File writes**: confined to declared writable roots (project dir, tmp, cache).
- **Process spawning**: may limit which binaries can execute.
- **Network**: may restrict outbound connections (depends on backend).

## What the Sandbox Does NOT Restrict

- **File reads**: the sandbox allows reading the entire filesystem by default. This means `cat ~/.ssh/id_rsa`, `cat ~/.config/opencode/auth.json`, and `cat /etc/shadow` all succeed from within the sandbox.
- **Network egress**: unless explicitly restricted, the sandbox allows outbound TCP connections — a sandboxed process can exfiltrate any file it can read.
- **Environment variables**: secrets in env vars are fully visible.

## Why This Design

A true confidentiality sandbox (no read access outside project) would break most development workflows — compilers read system headers, package managers fetch from the network, debuggers inspect other processes. The pragmatic choice is write-confinement: prevent accidental or malicious modification of files outside the project, while allowing reads for toolchain compatibility.

## Required Warnings

1. **Session start**: if sandbox mode is active, log a one-line notice: `"Sandbox mode: write-confined only. Auth tokens, SSH keys, and system files remain readable."`
2. **API status response**: the `features` array must NOT include `confidentiality` unless the backend actually enforces read restrictions.
3. **Documentation**: every reference to "sandbox" in user-facing docs must include the caveat that it is write-integrity only, not a security sandbox in the traditional sense.

## Gotcha: auth.json Is Readable

`~/.local/share/opencode/auth.json` contains provider API keys. A sandboxed process can read this file and exfiltrate it over the network. Users who want true isolation must combine the sandbox with network restrictions (bwrap `--unshare-net` or equivalent). The default sandbox mode does NOT provide this.