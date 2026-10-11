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
timestamp: 2026-10-07T06:12:29Z
---
# Auto-share-on-start (tailnet-only boot exposure)

When enabled, the desktop app starts the tailscale `serve` exposure automatically at
boot, so the instance is reachable on your tailnet without opening the Share dialog.
Takes effect on the next launch.

## Config key

`auto_share_on_start` — a top-level bool in `ocodeconfig.json`. **DEFAULT OFF**
(explicit opt-in). Defined at `internal/config/ocodeconfig.go:900`
(`AutoShareOnStart bool`).

Deliberately **NOT** part of `ProfileDelta`: boot-time network exposure is a
property of the machine, not of a model/provider profile, so switching profiles
must never publish or un-publish the instance.

Persisted via `config.SaveAutoShareOnStart` (`internal/config/ocodeconfig.go:4145`),
a targeted load-modify-write that never clobbers concurrent writers' unrelated keys.

## Serve-only, never funnel

Auto-share uses `tailscale.StartServeExpose` (`internal/tailscale/tailscale.go:323`),
which creates a **tailnet-only** `serve` mount. The manual Share dialog uses
`StartExposeWithKind` (`internal/tailscale/tailscale.go:290`), which tries `funnel`
(public internet) first, because that click is an explicit informed request.
Auto-share fires unattended, so it must stay inside the tailnet.

There is no funnel fallback on the auto-share path: if `serve` exposes nothing,
`StartServeExpose` returns empty rather than a bare `DNSName` guess, because an
unproven URL is worse than an honest "unavailable" when there is no dialog to show
a hint next to it. Pinned by
`TestStartServeExposeUnavailableWhenServeFails`
(`internal/tailscale/serve_expose_test.go:115`) and
`TestStartServeExposeNeverFunnels` (`internal/tailscale/serve_expose_test.go:89`).

## Public funnel port: 8443, not 443

Funnel mounts on a dedicated public HTTPS port, `tailscale.FunnelHTTPSPort = 8443`
(`internal/tailscale/tailscale.go:38`), not the implicit 443. `Expose`
(`internal/tailscale/tailscale.go:207`) appends `--https=8443` to every `funnel`
argv; `serve` keeps its 443 default.

Reason: Tailscale cannot expose one port as both `serve` and `funnel` — "if the
most recent command to configure the port was serve, then the port will be
completely private" — and 443 is normally already held by tailnet-only `serve`
routes (the TUI `/rc` `/ses_...` mounts). A funnel that defaulted to 443 therefore
silently stayed private, which is how a "public" share ended up reachable only over
the VPN. 8443 is funnel-eligible and keeps the public and tailnet listeners on
separate ports.

## One exposure slot

`tailscaleShare` (`internal/server/tailscale_share.go:43`) caches one exposure per
process behind its own mutex. `ensureServe` (auto-share,
`internal/server/tailscale_share.go:201`) and `ensure` (Share dialog,
`internal/server/tailscale_share.go:211`) share the slot and the first caller wins.

This is load-bearing, not an optimisation: `tailscale serve --bg --set-path /desktop`
is a single **global** mount per node, so a second exposure would silently
overwrite the first one's target. The one exception is an **upgrade**: an explicit
Share Start that finds a warm tailnet-only `serve` mount replaces it with a public
`funnel` (see below). Auto-share itself never downgrades a warm funnel back to
`serve` — it reuses whatever is cached.

The stable mount path is `desktopTailscalePath = "desktop"`
(`internal/server/tailscale_share.go:22`), so the public URL becomes
`https://<tailnet-host>/desktop/`.

`Server.StartAutoShare` (`internal/server/tailscale_share.go:280`) is the boot
entry point. It is best-effort: an unavailable tailscale is reported via the
returned hint, never raised.

### Start upgrades a warm serve mount

An explicit Share Start is an **upgrade path**, not just a cache read. When
auto-share has already warmed the single global mount with tailnet-only `serve`,
`start("full", port)` (`internal/server/tailscale_share.go:148`) detects the warm
`serve` kind and re-exposes **without** tearing the mount down first. Funnel mounts
on its own port (`FunnelHTTPSPort`), so it does not collide with serve on 443, and a
funnel attempt that proves nothing must not destroy a working share. The previous
state is kept unless the new exposure is proven; a superseded background process is
always killed. Stop later clears both listeners.

If the funnel attempt then fails, `StartExposeWithKind`
(`internal/tailscale/tailscale.go:290`) falls back to `serve` internally, so a
failed funnel remounts `serve` rather than leaving the share unmounted.

The upgrade is deliberately **one-way**: auto-share (`ensureServe`,
`internal/server/tailscale_share.go:201`) reuses whatever is cached — including a
public funnel — and never downgrades it back to `serve`, so an explicit share is
not silently withdrawn by a later boot-time auto-share call.

### Start/stop serialization (`opMu`)

`tailscaleShare` gained `opMu` (`internal/server/tailscale_share.go:49`), a mutex
held across the entire start or stop operation. This serializes start against stop,
so a Stop can never be resurrected by an in-flight start, and two starts can never
race for the one global mount. A Stop issued while a start is running waits for
that start to finish and then tears it down — bounded and correct. Status reads
(which take only `mu`) stay responsive throughout.

## Status, start, and stop endpoints

The old `GET /api/tailscale-url` (which started a funnel-first exposure on read) is
**removed**. It is replaced by three endpoints, all behind `authMiddleware`:

| Method | Path | Handler | Side effects |
|--------|------|---------|--------------|
| GET | `/api/tailscale-share` | `handleGetTailscaleShare` (`internal/server/tailscale_share.go:305`) | None — pure status read |
| POST | `/api/tailscale-share/start` | `handleStartTailscaleShare` (`internal/server/tailscale_share.go:312`) | Starts funnel-first exposure (upgrades a warm serve mount) |
| POST | `/api/tailscale-share/stop` | `handleStopTailscaleShare` (`internal/server/tailscale_share.go:329`) | Kills process + removes mount |

Routes registered at `internal/server/server.go:454-456`.

### Status shape

`GET /api/tailscale-share` returns `tailscaleShareStatus`
(`internal/server/tailscale_share.go:31`):

```json
{
  "running": true,
  "available": true,
  "url": "https://<tailnet-host>/desktop/",
  "kind": "serve",
  "hint": ""
}
```

- `running` — an exposure is PROVEN live (a URL came back from funnel or serve,
  not a bare DNS-name guess).
- `available` — tailscale is installed and could serve.
- `url` — the share URL (omitted when not running).
- `kind` — `"funnel"` (public internet) or `"serve"` (tailnet-only). Empty when
  the URL came from the unproven DNS-name fallback.
- `hint` — one-time setup hint (e.g. "serve is not enabled on your tailnet").

An unproven DNS-name fallback (empty `kind`) reports `running: false` with no URL.

### Start

`POST /api/tailscale-share/start` calls `tailscaleShare.ensure(port)`
(`internal/server/tailscale_share.go:211`), which tries `funnel` first and falls
back to `serve`. It is idempotent: a second call reuses the live exposure, with
one exception — a warm tailnet-only `serve` mount is **upgraded** to a public
`funnel` instead of reused (see "Start upgrades a warm serve mount").

### Stop

`POST /api/tailscale-share/stop` calls `tailscaleShare.stop()`
(`internal/server/tailscale_share.go:224`), which:
1. Kills the background `tailscale` process.
2. Calls `tailscale.RemoveSetPath("/desktop")`
   (`internal/tailscale/tailscale.go:339`) to remove the mount.

Killing the process alone would leave a public funnel live, so the mount removal
is essential. `RemoveSetPath` issues **two** off-commands, one per listener —
`funnel --https=8443 --set-path <p> off` and `serve --set-path <p> off`
(`internal/tailscale/tailscale.go:355-356`). A single argv would target only the
listener it names and orphan the other's mount, leaving a public funnel live after
Stop. Every helper resolves the CLI through the `tailscale.CLIPath` injection seam;
the `server`, `tui`, `desktop` and `tailscale` test binaries replace it in
`TestMain` with a resolver returning `""` (only the fake-CLI tests re-enable it), so
a test binary cannot edit the developer's live tailscale node config. Production
code never imports `testing`. `stopLocked` detaches state under `mu` and runs the
process kill and the two `off` subprocesses (each bounded by `exposeTimeout`)
after releasing it, so status reads never block behind a slow CLI. Stop never changes the persisted
`auto_share_on_start` setting —
stopping the running share must not silently change what happens on the next
launch.

## Reads never start an exposure

`HandleGetAutoShareConfig` (`internal/server/handler_config.go:2868`) reads
through `tailscaleShare.status()` (`internal/server/tailscale_share.go:126`) via
the injected `Handler.tailscaleShareSnapshot` seam, so merely opening Settings can
never publish the instance. Only the boot hook and explicit start actions may
start one.

The seam also keeps `h.mu` (a map lock, per the web-server locking rules) off any
tailscale work. Wired at `internal/server/server.go:188`
(`h.tailscaleShareSnapshot = s.tsShare.status`).

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
| GET | `/api/config/ocode/auto-share` | `HandleGetAutoShareConfig` (`internal/server/handler_config.go:2868`) |
| PUT | `/api/config/ocode/auto-share` | `HandleSetAutoShareConfig` (`internal/server/handler_config.go:2890`) |

Routes registered at `internal/server/server.go:519-520`.

Response shape: `{enabled, available, running, kind?, url?, hint?}`
(`autoShareResponse`, `internal/server/handler_config.go:2840`). The `running` and
`kind` fields were added so the Settings UI can show the live consequence of the
flag. Reads are side-effect free and safe to poll.

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
`internal/desktop/boot.go:220`.

Only the local branch has it: `startRemoteServer` (`internal/desktop/boot.go:440`)
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
(`internal/server/server.go:1820` → `internal/server/tailscale_share.go:251`),
which kills the process and removes **only** the `--set-path /desktop` mount —
never a global reset, which would tear down TUI `/rc` sessions on the same node.

## UI

- **Web Share dialog:** `ShareDialog` (`web/src/components/Layout/ShareDialog.tsx:75`)
  shows a Running/Not-sharing badge with kind, and Start/Stop buttons. Opening the
  dialog only READS status (`loadShareState` at
  `web/src/components/Layout/ShareDialog.tsx:117`); only the explicit "Copy Desktop
  URL" menu action starts a share when stopped.
- **Web Settings:** "Auto Share" (`web/src/components/Settings/AutoShareForm.tsx:17`)
  shows the same Running/Not-sharing badge + kind, and Start/Stop. The status block
  is independent of the persisted toggle.
- **TUI:** `/auto-share [on|off|status]` (`internal/tui/commands.go:148`,
  handler at `internal/tui/commands.go:1138`). Both write the same config key; the
  `status` verb is read-only and an unrecognised argument prints usage without
  changing the setting.

## Deadline

`Expose` (`internal/tailscale/tailscale.go:207`) uses `exec.CommandContext` with
a 2s `exposeTimeout` (`internal/tailscale/tailscale.go:26`), so a hung tailscale
CLI is killed rather than leaking a child.

## Testing notes

**Tailscale exposure tests must neutralize the package-level `knownCandidates` slice
as well as PATH.** `findCLIImpl` (`internal/tailscale/tailscale.go:74`) falls back
to absolute paths (`/usr/local/bin/tailscale` and friends, defined at
`internal/tailscale/tailscale.go:63`) when PATH lookup fails, so a PATH-only
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