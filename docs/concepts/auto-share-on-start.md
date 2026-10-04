---
type: Concept
title: Auto-share-on-start (tailnet-only boot exposure)
description: Desktop feature that starts the tailscale serve exposure at boot so the instance is reachable on the tailnet without opening the Share dialog. Opt-in, serve-only, fail-safe-off.
tags:
  - desktop
  - tailscale
  - auto-share
  - serve
  - boot
  - config
timestamp: 2026-10-04T11:36:33Z
---
# Auto-share-on-start (tailnet-only boot exposure)

When enabled, the desktop app starts the tailscale `serve` exposure automatically at
boot, so the instance is reachable on your tailnet without opening the Share dialog.
Takes effect on the next launch.

## Config key

`auto_share_on_start` — a top-level bool in `ocodeconfig.json`. **DEFAULT OFF**
(explicit opt-in). Defined at `internal/config/ocodeconfig.go:896`
(`AutoShareOnStart bool`).

Deliberately **NOT** part of `ProfileDelta`: boot-time network exposure is a
property of the machine, not of a model/provider profile, so switching profiles
must never publish or un-publish the instance.

Persisted via `config.SaveAutoShareOnStart` (`internal/config/ocodeconfig.go:4091`),
a targeted load-modify-write that never clobbers concurrent writers' unrelated keys.

## Serve-only, never funnel

Auto-share uses `tailscale.StartServeExpose` (`internal/tailscale/tailscale.go:262`),
which creates a **tailnet-only** `serve` mount. The manual Share dialog keeps
`StartExpose`, which tries `funnel` (public internet) first, because that click is
an explicit informed request. Auto-share fires unattended, so it must stay inside
the tailnet.

There is no funnel fallback on the auto-share path: if `serve` exposes nothing,
`StartServeExpose` returns empty rather than a bare `DNSName` guess, because an
unproven URL is worse than an honest "unavailable" when there is no dialog to show
a hint next to it. Pinned by
`TestStartServeExposeUnavailableWhenServeFails`
(`internal/tailscale/serve_expose_test.go:115`) and
`TestStartServeExposeNeverFunnels` (`internal/tailscale/serve_expose_test.go:89`).

## One exposure slot

`tailscaleShare` (`internal/server/tailscale_share.go:29`) caches one exposure per
process behind its own mutex. `ensureServe` (auto-share,
`internal/server/tailscale_share.go:88`) and `ensure` (Share dialog,
`internal/server/tailscale_share.go:130`) share the slot and the first caller wins.

This is load-bearing, not an optimisation: `tailscale serve --bg --set-path /desktop`
is a single **global** mount per node, so a second exposure would silently
overwrite the first one's target. Consequence, deliberate: once auto-share warms the
cache, the Share dialog reports the tailnet URL instead of trying funnel.

The stable mount path is `desktopTailscalePath = "desktop"`
(`internal/server/tailscale_share.go:23`), so the public URL becomes
`https://<tailnet-host>/desktop/`.

`Server.StartAutoShare` (`internal/server/tailscale_share.go:117`) is the boot
entry point. It is best-effort: an unavailable tailscale is reported via the
returned hint, never raised.

## Reads never start an exposure

`HandleGetAutoShareConfig` (`internal/server/handler_config.go:2851`) reads
through `tailscaleShare.peek` (`internal/server/tailscale_share.go:203`) via the
injected `Handler.tailscaleShareSnapshot` seam, so merely opening Settings can
never publish the instance. Only the boot hook and the Share dialog may start one.

The seam also keeps `h.mu` (a map lock, per the web-server locking rules) off any
tailscale work. Wired at `internal/server/server.go:188`
(`h.tailscaleShareSnapshot = s.tsShare.peek`).

## Token reuse

The auto-share URL carries the **durable share token** from `desktop-share-token`,
not the per-launch token, so links survive a restart. See
`concepts/desktop-share-token.md` for the two-credential model and revocation
semantics.

## Config endpoints

Both behind `authMiddleware` (the toggle changes network exposure, so an
unauthenticated route would let any tailnet peer turn sharing on):

| Method | Path | Handler |
|--------|------|---------|
| GET | `/api/config/ocode/auto-share` | `HandleGetAutoShareConfig` (`internal/server/handler_config.go:2851`) |
| PUT | `/api/config/ocode/auto-share` | `HandleSetAutoShareConfig` (`internal/server/handler_config.go:2878`) |

Routes registered at `internal/server/server.go:479-480`; wrappers at
`internal/server/server.go:2316` and `:2320`.

Response shape: `{enabled, available, url?, hint?}` (`autoShareResponse`,
`internal/server/handler_config.go:2840`). Reads are side-effect free and safe to
poll.

## Boot trigger

The gate is `startAutoShareIfEnabled()` (`internal/desktop/boot.go:215`), called
from `StartServer` at `internal/desktop/boot.go:176`. It consults
`autoShareEnabledAtBoot()` (`internal/desktop/boot.go:247`) and returns **before**
launching anything when the config does not opt in, which is what makes
"no config => zero spawns" a testable property. The exposure itself runs under
`crashguard.Go` (`internal/desktop/boot.go:219`) so a panic cannot kill the
process; `tailscale serve` waits up to 2s, and blocking `StartServer` would
delay the window appearing.

The outcome is logged (active URL / setup hint / tailscale absent) at
`internal/desktop/boot.go:221-229`.

Only the local branch has it: `startRemoteServer` (`internal/desktop/boot.go:422`)
binds `127.0.0.1` and returns at `internal/desktop/boot.go:84` before the
auto-share code runs.

### Caveat — the boot read is deliberately fail-safe-off

`config.LoadOcodeConfigCopy` is a **strict** loader: it returns an error for
corrupt JSON, truncated JSON, a wrong-typed value, and unreadable paths.
`autoShareEnabledAtBoot` treats ANY such error as OFF, so a malformed
`ocodeconfig.json` can never be the reason the instance is published. The trade-off:
the error is only logged (`internal/desktop/boot.go:235`), never surfaced in the UI
— a deliberate narrowing of the fail-fast rule, scoped to this single read. Do NOT
propose changing the shared loader.

## Teardown

`Server.Shutdown` calls `tsShare.cleanup()`
(`internal/server/server.go:1745` → `internal/server/tailscale_share.go:149`),
which kills the process and removes **only** the `--set-path /desktop` mount —
never a global reset, which would tear down TUI `/rc` sessions on the same node.

## UI

- **Web:** Settings → "Auto Share" (`web/src/components/Settings/AutoShareForm.tsx:13`,
  group registered in `web/src/components/Settings/SettingsPanel.tsx:174`).
- **TUI:** `/auto-share [on|off|status]` (`internal/tui/commands.go:148`,
  handler at `internal/tui/commands.go:1138`). Both write the same config key; the
  `status` verb is read-only and an unrecognised argument prints usage without
  changing the setting.

## Deadline

`Expose` (`internal/tailscale/tailscale.go:170`) uses `exec.CommandContext` with
a 2s `exposeTimeout` (`internal/tailscale/tailscale.go:25`), so a hung tailscale
CLI is killed rather than leaking a child.

## Testing notes

**Tailscale exposure tests must neutralize the package-level `knownCandidates` slice
as well as PATH.** `findCLIImpl` (`internal/tailscale/tailscale.go:43`) falls back
to absolute paths (`/usr/local/bin/tailscale` and friends, defined at
`internal/tailscale/tailscale.go:32`) when PATH lookup fails, so a PATH-only
isolation silently shells out to the REAL binary and mutates the developer's live
node-wide serve config. The test helper at
`internal/tailscale/serve_expose_test.go:19` sets `knownCandidates = nil` and
restores it via `t.Cleanup`.

**Config tests must isolate `HOME` + `XDG_CONFIG_HOME`.** `OPENCODE_CONFIG_DIR`
alone is overridden by `internal/config`'s own `TestMain`. Include a control
assertion proving the loader actually read what the test wrote (see
`internal/config/auto_share_test.go:29` `TestSaveAutoShareOnStartRoundTrips`,
which saves, reloads, and asserts the value round-tripped).

**Fail-safe-off tests** live at `internal/desktop/auto_share_test.go` — they
write corrupt/truncated/unreadable config and assert `autoShareEnabledAtBoot()`
returns false, guarding the strict-loader contract end-to-end.

**Route auth tests** at `internal/server/handler_auto_share_routes_test.go:71`
assert that both GET and PUT reject unauthenticated requests with 401.

**Exposure-slot tests** at `internal/server/tailscale_auto_share_test.go` verify
that auto-share and the Share dialog share one slot: the first caller wins, and a
second exposure attempt is never made.
