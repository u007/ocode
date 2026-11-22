---
type: Concept
title: "Upstash Box browser: spike findings (2026-11-21)"
description: Measured facts from a 2026-11-21 spike of Upstash Box as a remote Chromium (retention fixed at creation, the 3-day ttl cap, external CDP access, cookie and localStorage seeding limits), the Box REST API verified against the SDK source, and the design consequences. Phases 1 and 2 of the design are committed (f96e2da9); phases 3 to 5 are implemented and uncommitted. The phase 3, phase 4 (login-state) and phase 5 (retained box) live findings are in the last sections before Cleanup.
resource: https://upstash.com/docs/box/overall/ephemeral-box
tags: [browser, upstash, box, spike, cloud-sandbox]
timestamp: 2026-11-22T13:48:30+08:00
status: active
---

# Upstash Box browser: spike findings (2026-11-21)

> This page records measured facts that the design depends on. It does not describe ocode behaviour.
>
> Status of the design: phases 1 and 2 are committed (f96e2da9). Phases 3 to 5 are implemented and uncommitted: the box is wired into the browser tools, so a session created with an Upstash account can use one, login states are saved and restored, and `retain` keeps a persistent box per login state.

## Scope and caveats

- One box, one sample per timing. Timings were taken through the Upstash MCP server from a Mac in Malaysia, so each one includes MCP and network overhead. Read them as rough orders of magnitude, not benchmarks.
- Test box: small size, runtime `node`, ephemeral, browser enabled, ttl 1800 s, on 2026-11-21.
- Prices are in [upstash-firecrawl-browser-pricing.md](upstash-firecrawl-browser-pricing.md). The design built on these facts is [the Upstash Box browser backend spec](../superpowers/specs/2026-11-21-upstash-box-browser-backend-design.md).

## Lifecycle and retention

- An ephemeral box accepts `browser=true`. Creation returned a ready box (status `idle`) in about 3 s.
- The create call set a label, and the create response returned it. Listing by label is offered by the Upstash MCP tool but was not exercised.
- Maximum ttl is 259200 s (3 days), per the Upstash MCP tool schema for create.
- Pausing an ephemeral box is refused. `POST /v2/box/{id}/pause` returns HTTP 400 with `{"error":"Ephemeral boxes cannot be paused"}`. Ephemeral versus persistent is therefore fixed at creation.
- No way to extend ttl was found. The MCP `box_manage` tool has no update action, the box CLI (`@upstash/box-cli` 0.4.1) has no ttl or extend command, and the docs lookup returned nothing. Record this as "not found", not as "impossible".
- Docs conflict, not tested: the ephemeral-box docs table lists `snapshot()` and `fromSnapshot()` as supported on EphemeralBox, while the prose on the same page says ephemeral boxes have no snapshots.
- From the ephemeral-box docs, not tested: EphemeralBox has no agent runs, no git, and no pause or resume.

## Browser inside the box

- First page load, including Chromium start, took about 12 s (`goto https://example.com`).
- Runs as user `boxuser` with `HOME=/home/boxuser` on Debian 12. Chromium 156.0.8078.79.
- The Box runner launches Chromium with `--remote-debugging-port=9222`, `--user-data-dir=/tmp/box-gui/chromium-profile`, `--no-sandbox`, and two runner extensions under `/opt/runner/`.
- The Chromium profile is under `/tmp`, not under the persisted workspace `/workspace/home`. Whether it survives a pause was tested in phase 5 (see the last section).
- The User-Agent reports an ordinary `Chrome/156.0.0.0` on X11 Linux x86_64, not `HeadlessChrome`.

## CDP from inside the box

- `http://127.0.0.1:9222` needs no token. `/json/version` and `/json` both work.
- Cookies: `Storage.setCookies` and `Storage.getCookies` on the browser-level websocket both work, including an HttpOnly Secure cookie.
- localStorage for an origin with an open frame: `Runtime.evaluate`, `DOMStorage.setDOMStorageItem` and `DOMStorage.getDOMStorageItems` all work.
- localStorage for an origin with no open frame: `DOMStorage.setDOMStorageItem` fails with `Frame not found for the given storage id`.
- Seeding through `Page.addScriptToEvaluateOnNewDocument` works. The script writes localStorage when `location.origin` matches, and the value was present after navigating to that origin.

## External CDP from outside the box

- The URL comes from `box browser cdp-url` and needs a Box API key. Its shape is `wss://<box-id>-9223.preview.box.upstash.com?token=<token>`.
- It is a single browser-level websocket. From the Mac, connecting took about 1.2 s and one `Runtime.evaluate` round trip took about 340 ms.
- Working from the Mac: `Browser.getVersion`, `Storage.getCookies`, `Target.getTargets`, and `Target.attachToTarget` with flatten.
- An HTTP GET of `/json/version` on that host without the token returns 401. There is no HTTP `/json` discovery from outside the box.
- This worked on an ephemeral box, although the ephemeral-box docs page says ephemeral boxes have no public URLs.

## Box REST API, verified against the SDK source

Source: `@upstash/box` 0.8.1, `src/client.ts` and `src/types.ts`, read 2026-11-21. The public docs did not cover these details, and they correct earlier guesses.

- **Base URL:** `https://us-east-1.box.upstash.com`. The SDK also reads `UPSTASH_BOX_BASE_URL`.
- **Auth:** the header `X-Box-Api-Key` carries the key. Not `Authorization: Bearer`.
- **Create:** `POST /v2/box`. Body keys are snake_case: `name`, `labels`, `size`, `runtime`, `browser`, `ephemeral`, `ttl`, `keep_alive`, `network_policy`, `env_vars`, `attach_headers`. A `custom` network policy sends `mode` plus `allowed_domains`, `allowed_cidrs` and `denied_cidrs`. Other modes send only `mode`.
- **Create status:** create may return status `creating`. The SDK polls `GET /v2/box/{id}` until the status changes, for up to 300 s.
- **List:** `GET /v2/box`, with optional `?label=`. The response is a bare array. The SDK has no paging parameters.
- **Get, delete, pause, resume:** `GET /v2/box/{id}`, `DELETE /v2/box/{id}`, `POST /v2/box/{id}/pause`, `POST /v2/box/{id}/resume`.
- **CDP URL:** `POST /v2/box/{id}/browser/connect`. The response key is `cdp_url`.
- **Errors:** error bodies are `{"error": "<message>"}`.
- **Box record (`BoxData`):** always has `id`, `status`, `created_at` and `updated_at`. `name`, `labels`, `size`, `keep_alive`, `browser` and `network_policy` are optional. `ephemeral` and `expires_at` exist only on ephemeral boxes.
- **Network policy modes:** `allow-all`, `deny-all`, `custom`.

## Design consequences

These are the consequences the design was written on. Phases 3 to 5 are built on them. The phase 3 and phase 4 live findings, in the last sections, are measured against the built code.

1. Retention must be chosen when the box is created. An ephemeral box cannot be converted or paused.
2. Cookies can be restored at box start. localStorage must be restored lazily per origin, through an injected new-document script.
3. Box keeps its Chromium profile under `/tmp`. The profile survives a pause (phase 5), but Chromium restarts on resume and every tab is gone. A login-state store outside the box stays the source of truth.
4. A client outside the box must speak browser-level CDP over one token-bearing websocket, using `Target.attachToTarget`. There is no HTTP `/json` discovery from outside. htrcli's `--cdp` transport today probes a local port, so it cannot attach to this URL unchanged. htrcli's source is outside this repo.
5. About 340 ms per command from Malaysia makes multi-step tasks slower than local Chrome. Running the driver inside the box removes that delay.

## Not tested or not found

- Whether ttl can be extended after creation (not found in the MCP tool, the box CLI or the docs).
- Which snapshot claim is right (the docs conflict, see above).
- Stability of the Box REST API and of the external CDP URL on ephemeral boxes. The docs make no promise on either.

## Phase 3 live findings (2026-11-21)

Measured with the gated live tests against a real Upstash account:
`TestLiveBoxBrowser` and `TestLiveBoxNetworkPolicy` (`internal/upstashbox/live_test.go`)
and `TestLiveBrowserToolOnBox` (`internal/server/browser_boxes_live_test.go`). Each
test is skipped unless `UPSTASH_BOX_API_KEY` is set. One box per timing, so the numbers
are rough. No full agent turn was run.

- **Soft delete.** After `DELETE`, a `GET` of the box returns HTTP 200 with status
  `deleted` (`upstashbox.StatusDeleted`), and the list omits it.
- **Pausing an ephemeral box** is refused with HTTP 400, as recorded in the lifecycle
  section above. Retained boxes (phase 5) depend on this.
- **Timings.** Create about 2 s (the MCP-measured figure above was about 3 s). First CDP
  attach about 5 s. The `start` action through the `browser` tool, end to end, about 8 s.
- **Request blocking in attach mode.** The egress rule blocks requests to `127.0.0.1:9222`
  and `169.254.169.254`. The blocked page lands on `chrome-error://chromewebdata/`, and no
  failure NavEvent is emitted. A blocked navigation is therefore visible only in where the
  page lands, not in an event.
- **Closing the attach-mode Manager** (`cdp.Manager.Close`) closes its tabs and shared
  context and leaves the box's browser running.
- **Network policy.** A `custom` policy with only `DeniedCIDRs` is accepted by the API,
  but no page loads. It works only together with `AllowedDomains`. Decision made in phase 3:
  no network policy is set on the box. The spec's private-range policy (section 4.4, part
  3) is not implemented, and the question is open in spec section 5.

## Phase 4 live findings (2026-11-22)

Measured against real boxes with the gated live tests `TestLiveBoxLoginStateRoundTrip`
(`internal/upstashbox/live_test.go`) and `TestLiveBrowserToolLoginState`
(`internal/server/browser_boxes_live_test.go`). Each is skipped unless `UPSTASH_BOX_API_KEY`
is set.

- **Round trip across a teardown.** `terminate` saved the login state. A new box, started
  with that state, had the saved cookie and the saved localStorage item.
- **Hold.** A second session that asked for the same login state was refused. The error
  named the holding session, and no second box was created.
- **localStorage seed, live page.** The restored item was present on the page. The page's
  own later value survived a reload. After `localStorage.clear()` and a reload, the seed was
  not applied again. No marker key was visible to the page.

## Phase 5 live findings (2026-11-22)

Measured against a real Upstash account with a persistent box. The
round trip ran through the gated live test `TestLiveBrowserToolRetainedBox`
(`internal/server/browser_boxes_live_test.go`), which is skipped unless `UPSTASH_BOX_API_KEY`
is set. The cookie items are raw box checks, not ocode runs.

- **Create.** `POST /v2/box` without `ephemeral` and `ttl` makes a persistent box. Status goes
  `creating` to `idle`. The record has no `expires_at`. At size small, `storage_gb` is 10. It
  works on the current plan.
- **Pause.** `POST /v2/box/{id}/pause` returns 200 with `{"message":"box paused"}`. Status is
  `paused` in about 2 s.
- **Resume.** `POST /v2/box/{id}/browser/connect` on a paused box resumes it (about 6.5 s) and
  returns a CDP URL. No explicit resume call is needed.
- **Chromium after resume.** It is a new process, so every tab is gone. The profile at
  `/tmp/box-gui/chromium-profile` lives on the box's overlay disk and survives the pause. This
  resolves the earlier open question about the `/tmp` profile.
- **Cookies.** A cookie set 10 s before the pause was lost. One set 75 s before survived. The
  cause is Chromium's cookie flush interval.
- **Round trip through ocode.** `terminate` left the box paused. A start with the same login
  state resumed the same box id, and the saved cookie was present.

## Cleanup

The test box and the temporary API key were deleted. The account had zero boxes afterwards.
