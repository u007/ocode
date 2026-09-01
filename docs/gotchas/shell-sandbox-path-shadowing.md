---
type: Gotcha
title: PATH Shadowing Can Bypass Sandbox Discovery
description: 'Gotcha: PATH-based sandbox-exec/bwrap discovery can be shadowed by user-writable executables, requiring hardening to prevent security bypasses.'
tags:
  - gotcha
  - sandbox
  - security
  - shell
  - path
timestamp: 2026-08-31T17:26:30Z
---
# PATH-Based Sandbox Discovery Can Be Shadowed

## The Problem

Sandbox backends are discovered via `PATH` lookup: `which sandbox-exec`, `which bwrap`. If a user (or a malicious script) places a binary named `sandbox-exec` or `bwrap` earlier in `PATH`, the wrong binary is executed — potentially one that does no confinement at all, silently defeating the sandbox.

## Attack Vector

1. User has `~/bin` in PATH before `/usr/bin`.
2. Attacker (or a compromised dependency) writes `~/bin/bwrap` that is a no-op shell script.
3. ocode's sandbox probe finds `~/bin/bwrap` → status `available`.
4. Sandbox "confinement" runs via the fake binary → no actual restriction.

## Required Hardening

1. **Resolve to absolute paths at probe time**: don't just check `which`, resolve the full path and store it.
2. **Validate the binary**: check that the resolved path is in a trusted prefix (`/usr/bin`, `/usr/local/bin`, `/snap/bin`, system paths). Reject user-writable directories.
3. **Hash-check optional but recommended**: for high-security modes, verify the binary's hash against a known-good value.
4. **Log the resolved path**: always log which binary is being used so users can audit.

## Gotcha: PATH Can Change Between Probe and Execution

The probe runs at session start. If `PATH` is modified between probe and a later execution (e.g. a bash command prepends `~/bin`), the stored absolute path still points to the right binary — this is why storing the absolute path at probe time is critical, not re-resolving at execution time.