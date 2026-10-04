---
type: Concept
title: Shared HTR daemon (htrcli serve)
description: 'Why one shared htrcli daemon serves both ocode and the user''s extension: coordinates from ~/.htrcli/config.json, browser.htr_shared/htr_token config keys, AdoptOnly, the StartedByPID stop rule, and lifecycle limits.'
tags:
  - browser
  - htr
  - htrcli
  - daemon
  - extension
  - native-host
  - settings
  - lifecycle
timestamp: 2026-10-02T02:11:51Z
---
## Why this exists

ocode used to run a **private** `htrcli serve` on port 3846 with its own socket, a random per-launch bearer token, and a namespaced native-messaging host (`com.ocode.htrcontrol`). The user's own HTR NControl extension talks to a *different* daemon — port 3845, `com.htrcontrol.host`, `~/.htrcli/daemon.sock`. The two could never meet: different port, different bearer, different native host. Whenever ocode was running, the user's extension showed "not connected".

The fix is not to make the two daemons talk to each other; it is to stop having two. One `htrcli serve` now serves both ocode's embedded browser and the user's personal extension, and ocode learns that daemon's coordinates from htrcli's own config instead of inventing its own identity.

## The shared daemon — htrcli's coordinates, not ours

`cdp.ResolveSharedDaemon` (`internal/browse/cdp/htr_shared.go`) maps config to the one daemon ocode will ensure. In shared mode every coordinate comes from `~/.htrcli/config.json`, read as **JSON only** (`loadHTRcliConfig`):

- **port** — from the `server` URL, but only when `loopbackEndpoint` proves it is a loopback URL with an explicit port; a loopback URL without a port, or an unusable default, keeps `DefaultHTRCLIPort` (3845, htrcli's own default).
- **token** — from `token` (source `htrcli-config`), or from `browser.htr_token` when set (source `ocode-config`), or none at all.
- **binary** — from `htrcli_path`.
- **socket** — `sharedSocketPath`: `~/.htrcli/daemon.sock` on Unix, a loopback endpoint on Windows.

JSON-only is deliberate: viper accepts TOML/YAML configs that this resolver refuses to guess at, so a non-JSON config lands in AdoptOnly with a notice rather than a half-read identity.

**ocode never passes `HTR_BEARER_TOKEN`.** `sharedServeEnv` (as opposed to `privateServeEnv`, which is private-mode-only) omits it entirely: htrcli resolves its own token from its own config, so there is exactly one source of truth for the credential the daemon actually validates against. Passing one would create a second source that can silently disagree — every authenticated probe would 401 and ocode would believe it may spawn a daemon that then never looks healthy.

`resolveManagedHTROptions` (`internal/server/htr.go`) performs this resolution for every caller (browse server, Settings API, TUI) and copies `Shared.Socket`/`Shared.Port` into the options so no caller re-derives them into ocode's private namespace.

## Config keys

Both live in the `BrowserConfig` block (`internal/config/ocodeconfig.go`):

- **`browser.htr_shared`** (bool, default **`true`**) — selects the shared daemon. `false` is the rollback: the old private ocode-managed 3846 daemon, byte-for-byte unchanged, and htrcli's config is deliberately *never* consulted in that mode (a leftover config must not move the private port or supply a token). On the wire it is a `*bool` (`BrowserConfigOverride`) precisely so an explicit `false` is distinguishable from an absent key — otherwise the rollback would be unreachable once the default became `true`.
- **`browser.htr_token`** (string) — the escape hatch when ocode cannot read a token from htrcli's config. Empty means "read htrcli's config". It overrides the token *value*, not the presence of the config file — a missing config still forces AdoptOnly, which is why no notice ever offers `htr_token` as the fix for a missing file.

Both keys are **config-file-only**: `config.SaveOcodeHTRConfig` has no parameter for them, so the Settings UI (`BrowserForm`) renders them as read-only provenance rather than inputs — the form says where they are set instead of pretending to save them. Editing `ocodeconfig.json` (and reloading) is the supported path; `browser.htr_shared: false` is the documented one-line rollback.

## Resolution: the four states

`cdp.EnsureHTRServe` resolves in a fixed order (the comment calls them "the four states, in resolution order"):

1. **Own-alive** — the marker's identity answers the strict health probe ⇒ adopt what this process already started.
2. **Foreign-alive** — a daemon on the port answers with the shared token but carries no ocode marker ⇒ adopt it, and **write no owner marker**, because that marker is what authorises a stop and this daemon is the user's. This check deliberately precedes the supervisor guard: a caller without a supervisor (TUI bridge, Settings API) must still attach to a daemon the user is already running.
3. **Adopt-only refusal** — see below.
4. **Spawn** — a supervisor-owned child under `withHTRStartLock`, with both probes re-checked inside the lock so two ocode processes (or the user) racing in can't double-spawn; a daemon that appears in the gap is adopted without ever being reported as started by ocode.

## AdoptOnly — probe, never spawn

When the htrcli config is **missing, unreadable, unparseable (non-JSON), tokenless, or points at a non-loopback/unparseable `server` URL**, `ResolveSharedDaemon` sets `AdoptOnly: true`. From then on ocode probes the configured coordinates and reuses a healthy daemon, but **never spawns one** — and it says exactly why, with a notice naming the config path (e.g. "No htrcli config at ~/.htrcli/config.json. Start `htrcli serve` yourself; ocode stays adopt-only until that file exists").

Why the hard refusal rather than a best-effort spawn: ocode never passes a bearer, so a daemon spawned with no readable config comes up with no token and every authenticated probe 401s forever — ocode would spawn, then never see it become healthy. Failing loudly with the file to fix beats a spawn loop that never converges. AdoptOnly also suppresses `ensureSharedSocketDir`: ocode has no business creating directories in the user's home for a daemon it will not launch.

In the Settings UI, AdoptOnly shows up as `adopt_only` + `notice` on `htrStatusResponse` (`internal/server/handler_config.go`) so the UI can explain why Start cannot succeed instead of offering a button that never works.

## The stop rule — the part users will notice

`shouldStopSharedDaemon` (`internal/browse/cdp/htr.go`) gates every termination. All conditions must hold:

1. the owner marker names a daemon;
2. **this process spawned it** — `StartedByPID`, never `OwnerPID`: adopting rewrites `OwnerPID` and must not buy stop rights — **or the recorded spawner is no longer running**, in which case its stop rights pass to whichever ocode process asks. A live spawner's daemon is never stopped by anyone else; an unknown spawner (`0`, a pre-`StartedByPID` marker) is never inherited from;
3. the daemon is still alive (a dead marker is cleanup, not a stop decision);
4. the daemon is still attributable to ocode (marker executable + start token match, or the bearer-protected health probe answers). Inherited rights demand the executable + start token match: in shared mode the probe also answers for a daemon the user started with the same token;
5. no *other* ocode instance still holds a live lease (`activeHTRLeases`).

A `(false, nil)` answer is a **refusal, not a failure**: `StopHTRServe` reports the daemon as still running and touches nothing, leaving the marker in place so the next run sees "not mine" rather than "no daemon". Daemons from another *running* ocode server or desktop instance, and an external `htrcli serve` (adopting it writes no marker), are all left alone.

**The consequence, plainly: if ocode started the daemon, closing ocode drops your browser extension's connection.** That is intended, not a bug — the daemon ocode spawned dies with the last ocode process that spawned it, and the extension's "not connected" is the honest state. If you want the extension to outlive ocode, run `htrcli serve` yourself: ocode will adopt that daemon and never stop it.

**Orphans:** the daemon outlives its spawner when ocode is SIGKILLed, or when the spawner exits while another instance still holds a lease (TUI spawns, desktop adopts, TUI exits). Stop rights then pass on: the next ocode process to release the last lease reaps it, and its Settings Stop button works (`started_by_ocode` is the same verdict). A recycled spawner pid reads as "still running" and only delays this — the failure direction is a refusal, never a wrong kill.

## Lifecycle limits (documented, not hidden)

- **Eager start in a plain TUI session.** `model.ensureSharedHTRDaemonAsync` (`internal/tui/model.go`) kicks off the ensure at startup, at most once, on a crashguard goroutine. Why eager: a plain TUI session never reaches `StartBrowse`, so a lazy ensure has no trigger — and that session is exactly the one whose daemon would go missing. The cost: such a session still spawns a daemon that dies on exit. That is the stop rule above, not an oversight. `server.EnsureSharedHTRDaemon` performs the same `resolveManagedHTROptions` + `EnsureHTRServe` path the desktop app uses, so every caller lands on the same daemon under the same lease bookkeeping.
- **Readiness never stalls boot.** Two halves: `confirmSharedSpawnAlive` is a short *blocking* check that only catches an exec that produced a process dying on the spot (its failure path costs one reap, not the whole window); `verifySharedDaemonAsync` then confirms `/api/health` in the *background* and, on timeout, logs once and publishes a notice ("browsing continues without the HTR extension"). A slow start is not a failed start — the verifier never kills the daemon; the stop rule owns termination.
- **Death mid-session is loud, terminal, and unrecovered.** `watchHTRExit` is the single death detector: it marks the supervisor record, retracts the owner marker, clears the UI's cached health, and logs `not restarting it`. There is deliberately **no auto-restart and no fallback to a private per-session daemon** — a silent fallback would recreate the two-daemon split this feature exists to end and hide the disconnect the user must notice. Recovery is the next *explicit* ensure: the next ocode start, or the Settings start button.

## Never touches `com.htrcontrol.host`

ocode keeps its own `com.ocode.htrcontrol` native host (default `HTRNativeHostName`), so the user's personal extension's manifest is never rewritten. The invariant is guarded at three layers:

- config rejects `com.htrcontrol.host` outright — `validHTRNativeHostName` demands a `com.ocode.` prefix, pinned by `TestBrowserConfigRejectsStandaloneNativeHostName` (`internal/config/ocodeconfig_htr_test.go`);
- `ensureNativeHostManifest` refuses to register under that name — `TestNativeHostManifestDoesNotTouchStandaloneHost` (`internal/browse/cdp/htr_test.go`) also proves an existing standalone `com.htrcontrol.host.json` manifest is left untouched;
- the spawned daemon's env never carries it — `sharedServeEnv` test coverage (`htr_shared_lifecycle_test.go`) plus `TestEmbeddedExtensionNativeHostNameIsNamespaced`, which proves ocode's *own* embedded extension gets its `connectNative` name rewritten away from `com.htrcontrol.host` rather than shipping it.

## Tests

- `internal/browse/cdp/htr_shared_test.go` — resolution: AdoptOnly reasons, notices, JSON-only, loopback port, token sources.
- `internal/browse/cdp/htr_shared_stop_test.go` — the stop rule, including `TestStopHTRServeLeavesAdoptedDaemonRunning`, `TestStopRuleUsesStartedByPIDNotOwnerPID`, `TestFinalLeaseReleaseDoesNotKillAnAdoptedDaemon`, `TestFinalLeaseReleaseReapsDaemonWhoseSpawnerExited`, `TestInheritedStopRightsRefusals`.
- `internal/browse/cdp/htr_shared_lifecycle_test.go` — adoption without spawn/marker, AdoptOnly-never-spawns, shared env.
- `internal/browse/cdp/htr_shared_readiness_test.go` — readiness halves, no auto-restart, no private fallback on death.
- `internal/config/ocodeconfig_htr_test.go` — defaults (`HTRShared` true, `HTRToken` empty), `TestBrowserConfigHTRSharedExplicitFalseBeatsDefault`, native-host rejection.
- `internal/server/htr_shared_daemon_test.go`, `internal/server/handler_htr_shared_test.go` — server-side ensure and the Settings status body (`daemon_pid`, `adopt_only`, `notice`, `started_by_ocode`).
- `internal/tui/htr_startup_test.go` — eager ensure runs at most once, off-thread, with the model's supervisor.
- `web/src/components/Settings/BrowserForm.htrShared.test.tsx` — read-only config-file-only rendering.

## References

- Resolver: `cdp.ResolveSharedDaemon`, `SharedDaemon`, `HTRSharedInput`, `loadHTRcliConfig`, `loopbackEndpoint`, `sharedSocketPath` (`internal/browse/cdp/htr_shared.go`)
- Lifecycle: `EnsureHTRServe`, `adoptHTRServe`, `shouldStopSharedDaemon`, `StopHTRServe`, `htrWriteOwner`, `watchHTRExit`, `verifySharedDaemonAsync`, `confirmSharedSpawnAlive`, `sharedServeEnv` (`internal/browse/cdp/htr.go`)
- Server wiring: `resolveManagedHTROptions`, `EnsureSharedHTRDaemon`, `ensureSharedSocketDir` (`internal/server/htr.go`); `htrStatusResponse` (`internal/server/handler_config.go`)
- Config: `BrowserConfig`, `BrowserConfigOverride` (`htr_shared` `*bool`), `SaveOcodeHTRConfig` (`internal/config/ocodeconfig.go`)
- TUI startup: `model.ensureSharedHTRDaemonAsync` (`internal/tui/model.go`)
- Settings UI: `web/src/components/Settings/BrowserForm.tsx`
