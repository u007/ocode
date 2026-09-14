---
type: Gotcha
title: Auto-Permission — Interpreter Network Effects Denied Loopback and host:port Targets
description: verifyInterpreterEffects looked up model-reported network targets ("127.0.0.1:8765", "https://api.example.com/v1") verbatim in the webfetch domain allowlist, so loopback and port-qualified hosts were always denied even when the subprocess rule and webfetch policy would allow them. Fixed by normalizing to bare hostname and exempting loopback.
tags:
  - auto-permission
  - gotcha
  - permissions
  - network
timestamp: 2026-09-14T00:00:00Z
---
## Symptom

LLM permission judge returns `allow` for a script that talks to a local server, but the deterministic verifier still auto-denies:

```
⛔ Auto-denied by LLM permission model:
network target not allowed by policy: 127.0.0.1:8765
```

## Root cause

`Agent.verifyInterpreterEffects` (`internal/agent/permission_interpreter.go`) checked `resp.Effects.Network` with a literal map lookup `pm.webfetchDomains[host] != PermissionAllow`. The judge reports whatever the source contains — `host:port`, full URLs, bracketed IPv6 — none of which match a bare-domain allowlist key. There was also no loopback exemption, even though the adjacent subprocess rule (`subprocessTargetsLocalhost`) already treats loopback as local.

## Fix

Each network target is reduced with `normalizeNetworkEffectHost` (strip scheme/userinfo/path/port/brackets), loopback (`isLocalhostDomain`) is skipped, and only the bare domain is consulted against `webfetchDomains`. Regression cases live in `TestVerifyInterpreterEffects` ("loopback network targets auto-allow", "allowed webfetch domain with port auto-allows").

## Rule

Any new deterministic check that consumes model-reported hosts must normalize before policy lookup and keep loopback semantics identical to the subprocess rule.
