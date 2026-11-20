---
type: Design
title: Upstash Box as a browser backend for ocode
description: Draft design, revision 2. Phases 1 and 2 implemented, uncommitted, not wired into the tools; phases 3 to 5 not started. An optional Upstash Box Chromium backend for the browser and browser_run tools, with a per-call backend field, an explicit start action, a websocket attach-mode driver, a lease-based sweep, login states kept on the ocode machine, a boxes panel and phased delivery.
resource: internal/browse/cdp
tags:
  - design-spec
  - browser
  - upstash
  - cloud-sandbox
  - login-state
timestamp: 2026-11-21T01:46:41+08:00
status: active
---

# Upstash Box as a browser backend for ocode

- **Date:** 2026-11-21
- **Status:** Draft, awaiting user approval. Partly implemented; see Implementation status.
- **Scope:** An optional remote headless Chromium for the `browser` and `browser_run` tools, hosted in an Upstash Box.
- **Related:** [Spike findings](../../concepts/upstash-box-browser-spike-findings.md), [Pricing](../../concepts/upstash-firecrawl-browser-pricing.md), [Browser agent tool](../../concepts/browser-agent-tools.md)
- **Revision 2 (2026-11-21):** Revised after a code-grounded review. Section 7 lists each review finding and the section that now covers it.
- **Implementation status (2026-11-21):** Phases 1 and 2 implemented, uncommitted, not wired into the browser tools. Phases 3 to 5 not started. No session can use a box yet.

## 1. Goal

Let an ocode session use a headless Chromium in an Upstash Box as an alternative to the local browser. No box keeps running by default. Login state survives box deletion.

## 2. Non-goals

- Upstash Box as a remote host for ocode or Claude Code. This has its own TODO.md nice-to-have entry dated 2026-11-21.
- Firecrawl as a browser backend. This has its own TODO.md nice-to-have entry dated 2026-11-21.
- Saving IndexedDB, service workers, or other in-page state.
- Remote-project (SSH/WSL) sessions in v1. They keep today's behaviour.
- Continuous two-way sync with the user's local browser.
- File uploads on the upstash backend in v1.
- A TUI `/box` command. The TUI's own chat agent has no browser tool. Only server sessions do.
- Restoring open tabs. Reopening tabs would navigate without a tool call.

## 3. Decisions

Decided by the user on 2026-11-21:

- **Hybrid default.** A setting picks the default browser backend: local or upstash. The model may also pick a backend per call, when the user asks for it or the task suits it.
- **No box by default.** No box runs by default. Boxes are created lazily. They are ephemeral unless retention is opted into.
- **Retention is opt-in by the model.** Retention is off by default.
- **Login states hold the logins.** Login state lives in named login states stored on the ocode machine. A login state is used whenever no retained box exists.
- **Timeouts.** 15 minutes idle. 2 hours hard cap for ephemeral boxes. 14 days unused for retained boxes. All three are editable in the settings UI.
- **Accounts.** One or more Upstash accounts can be set up in the settings UI.
- **Box management.** A way to list all boxes with status, and to stop or delete them.

On 2026-11-21 the user asked for every review finding, minor ones included, to be fixed in this spec.

## 4. Design

### 4.1 Accounts and settings

- The settings section "Upstash Box" lists accounts. Each account has:
  - a name;
  - a Box API key;
  - a default box size;
  - an idle timeout in minutes (default 15);
  - a hard cap in minutes (default 120), with an upper bound of 3 days, which is the Box maximum ttl;
  - a retained-box delete period in days (default 14).
- **Bounds.** Idle timeout from 1 to 4320 minutes, and not greater than the hard cap. Retained-box delete period from 1 to 365 days. A list page holds at most 200 accounts.
- One account is marked as the default.
- **Routes.** `/api/upstash/accounts` and `/api/upstash/settings`. They are covered by the loopback self-escalation guard (`isPermissionAPIPath` in `internal/agent/permissions.go`), so a write to them from the agent's shell asks in every mode.
- One default-backend choice: local or upstash.
- **Account selection.** A session uses the default account as it was when the session was created. The tools have no account field. The model cannot choose an account.
- **Secret storage.** The Box API key is stored in `auth.json`, an owner-only file, through the existing store in `internal/auth/store.go`. The web client sees a masked value, the same way `maskConnectCredential` in `internal/server/handler_connect.go` masks credentials. The key never goes in `ocodeconfig.json` and never reaches the web client.
- Non-secret settings are stored in `ocodeconfig.json` through a targeted load-modify-write saver. Never save an in-memory snapshot. Concurrent sessions would erase each other's changes.
- The section uses the web app's existing UI component library.
- The product adds no environment variable. The live test in 4.8 is the only reader of a Box API key from the environment.

### 4.2 Box API client

- A new internal package calls the Box HTTP API directly. It covers create, list, get, delete, pause, resume and CDP URL.
- It does not depend on the box CLI or on Node.
- Every call has a deadline. No call runs while `Handler.mu` is held.
- Errors are wrapped so that a URL carrying a token never appears in an error string or a log line. Logs carry the box id only.
- The endpoints were verified against the SDK source and are recorded on the [spike findings page](../../concepts/upstash-box-browser-spike-findings.md#box-rest-api-verified-against-the-sdk-source).
- The client never follows redirects. Go forwards a custom header such as `X-Box-Api-Key` to a redirect target on another host.
- Once the create request succeeds, the client always returns the box id, even with an error, so the caller can delete the box.
- **Labels.** The spike's create call set a label, and the response returned it. Listing by label is offered by the Upstash MCP tool but was not exercised. Confirm in phase 1.

### 4.3 Tool surface

- `browser` and `browser_run` get an optional `backend` field. It is a fixed enum: `local` or `upstash`. When it is omitted, the session default applies.
- The field is named `backend`, not `target`. `target` already names the element ref or selector on the `browser` tool (`internal/server/browser_tool.go`), and `browser_run` sets it on each sub-step (`internal/agent/browser_run.go`).
- New `browser` actions, valid only with `backend` set to `upstash`: `start` and `terminate`.
  - `start` is the only call that creates a box.
  - Any other upstash call with no running box returns an error that tells the model to call `start`.
  - `start` takes an optional `retain` boolean. It is used in phase 5.
- **Why an explicit `start`.** The permission decision (`Decide` in `internal/agent/permissions.go`) sees only the tool name and its arguments. It allows observe-type actions and navigation to allowed hosts outright. A lazy create hidden inside such a call would run with no ask.
- `browser_run` threads `backend` into every sub-step it issues. The sub-steps are built in `internal/agent/browser_run.go`. `browser_run` never calls `start` itself. With no box running, it stops with an error.
- **Tools array stability.** Tool definitions are derived from the agent's tools on every request (`internal/agent/agent.go`). The agent is rebuilt on idle eviction, on skill or plugin changes, and on `/reset-id`. So the decision "this session has the upstash fields" is recorded in creation-only session metadata, the same way as `pinned_mcp_servers`. Every rebuild reads it from there.
  - A session created before any account existed never gains the fields. A new session is needed.
  - Adding or removing an account never changes a live session's tools array.
- **Routing.** The tool already works against the `browserSurfaces` interface in `internal/server/browser_tool.go`. That interface has a Chrome implementation and an htrcli implementation. The upstash backend is a third implementation behind the same interface. A small router picks the implementation per call from `backend`.
  - Tab state keys are namespaced per backend, so the two never collide.
  - Nothing moves between backends.
- A failure on the upstash backend is returned as an error. It never falls back to the local browser.
- **Uploads.** The `upload` action and file fields in `fill_form` are refused on the upstash backend in v1. The driver passes a local file path (`internal/browse/cdp/upload_cdp.go`), and that path does not exist in the box.
- **Sub-agents.** A sub-agent may use the session's running box. Sub-agents already get their own tab state through `ForAgent` in `internal/server/browser_tool.go`, and their tabs close when their run ends. A sub-agent can never `start`, `retain` or `terminate`.
- All existing `browser_run` invariants apply unchanged:
  - sub-steps go through the normal tool-call path, never a direct call;
  - no chat model is used;
  - a mutation is never retried;
  - data values never enter a Jev request;
  - text-entry controls are masked in the screenshot view.
- The CDP URL token never enters a model request, a Jev request, a log line or the web client.

### 4.4 Driver transport

- **Finding.** The current driver cannot attach to a remote browser. `cdp.Conn` (`internal/browse/cdp/conn.go`) speaks only the `--remote-debugging-pipe` transport, with NUL-terminated frames. The pipe launch is in `internal/browse/cdp/launch.go`. The `Manager` (`internal/browse/cdp/manager.go`) owns one locally launched Chrome, with a Chrome profile directory and a supervisor. One `Manager` exists per server (`internal/browse/server.go`).
- **Design.** Add a websocket transport to `internal/browse/cdp`, beside the pipe transport. Allow a `Manager` that attaches to an existing browser instead of launching one. One such `Manager` exists per box. It has no local process, no Chrome profile directory and no supervisor.
- **Egress finding.** Today every tab's browser context is created with `proxyServer` set to ocode's loopback egress proxy (`internal/browse/cdp/manager.go`, `internal/browse/cdp/egress.go`). That proxy is the guard against requests to private addresses. In a box, the loopback address is the box itself, so every navigation would fail.
- **Egress rule for the box `Manager`.** There is no local proxy and no proxy-auth handler. Three parts replace them.
  1. The tool refuses loopback, private and link-local URLs before any `navigate` or `open` on the upstash backend. Today loopback URLs are allowed (`internal/tool/browser_url.go`). They skip the network guard (`internal/agent/network_guard_typesafe.go`) and the content guard (`content_guard_typesafe.go`). That is wrong in a box, because `127.0.0.1:9222` there is the box's token-free CDP endpoint.
  2. The box `Manager` blocks requests to loopback, private and link-local hosts with CDP request interception. A redirect or a page script cannot reach them either.
  3. The box is created with a Box network policy that denies private address ranges.
  
  Parts 2 and 3 still need verification against a real box; that is the gated live test in 4.8. The box is not on the user's network, so it cannot reach the user's LAN in any case.
- **What the egress code does (uncommitted).**
  - Attach mode fails closed. If request interception cannot be enabled on a tab, popup or child session, that target is closed or detached.
  - Allowed schemes are `http`, `https`, `ws`, `wss`, `data`, `blob` and `about`. Everything else is blocked.
  - A host name is resolved and blocked if any address is loopback, private, link-local or unspecified. A resolution failure blocks.
  - Paused requests are answered concurrently.
  - Close closes the Manager's tabs and disposes the shared context. It never closes the remote browser.
- **Residual limits.**
  - Name resolution runs on the ocode machine, not in the box. A hostile DNS server can still answer differently for the box. The full fix is the in-box driver.
  - A popup request already in flight before interception is enabled is not judged.
  - Whether CDP request interception pauses a WebSocket handshake is not verified against a live browser.
- **Contexts.** Today each tab state key gets its own browser context (`internal/browse/cdp/manager.go`). Cookies set at browser level would reach no tab, and tabs would not share logins. The box `Manager` uses one shared browser context for all tabs of the box. Login state is saved from that context and restored into it.
- **Rejected for v1: htrcli inside the box.** The htrcli daemon routes each command to the HTR browser extension (see [Browser agent tool](../../concepts/browser-agent-tools.md), "Remote host: the client's browser over htrcli"). The box's Chromium is started by the Box runner with only its own two extensions. The ephemeral-box docs say ephemeral boxes have no public URLs for a daemon port. It can be revisited as a later phase.
- **Known cost.** About 340 ms per command, measured from Malaysia.
- A standalone htrcli attach mode for the external CDP URL is a separate later phase. htrcli's source is outside this repo.

### 4.5 Lifecycle

- **Ownership.** A box manager sits at the `Handler` level and is keyed by session id. It is not hung on the agent or the tool. `Agent.Shutdown` and `closeSessionTools` (`internal/agent/agent.go`) also run on idle eviction and agent rebuilds, and `/reset-id` (`internal/server/handler_reset_id.go`) releases the agent. So eviction and rebuild never delete a box.
- **Creation.** Only `start` creates a box. It is a static permission ask with its own category, in normal and sandbox modes. The auto-permission judge is not consulted for it. Locked mode denies it. Yolo mode allows it, as it allows everything else. Sub-agents are refused.
- **Default box.** Ephemeral, with a ttl equal to the hard cap. Upstash deletes it even if ocode crashes.
- **Early deletion by ocode.** A box is deleted early on:
  - `HandleCloseSession`;
  - `Handler.Shutdown` (`internal/server/handler_shutdown.go`), which has no browser step today;
  - the idle timeout;
  - the `terminate` action.
- The server has no session-delete route today. `session.Delete` is TUI-only (`internal/tui/model.go`).
- How web `/new` and `/clear` reach the server, and whether they close the old session, was not determined in the review. The implementation plan decides it. Until then the idle timeout and the ttl cover those paths.
- **Idle timeout.** The timer counts only while the session has no active turn and no pending permission or question ask. It is paused during a turn. The hard cap still applies.
- **Re-key.** The manager's session-keyed map is added to the `/reset-id` move list (`internal/server/handler_reset_id.go`).
- **Ownership label.** Every box carries a label that names ocode and this installation.
- **Journal and sweep.**
  - Each created box is journaled in the data directory with its kind (ephemeral or retained) and the owning process's lease. The lease holds the process id, the process start time and a heartbeat time.
  - Several ocode server processes can share one data directory. No session is live at server start, because agents are built lazily. So the sweep never uses "no live session" as its test.
  - The sweep deletes only ephemeral boxes of this installation whose owning lease is dead or expired. Retained boxes are exempt from the sweep.
- **Hard cap mid-task.** The next call fails with a clear error that says the box expired. The model calls `start` again. The login state is restored. Page state is gone.
- **Teardown ordering.** Teardown takes the per-box lock, cancels the pending checkpoint, writes a final login-state save, then deletes the box. No checkpoint runs after a delete has started. Login-state files are written atomically: a temporary file, then a rename.
- **Retained box (phase 5).**
  - It is created only by `start` with `retain`. Each such call is a permission ask.
  - It needs a named login state. `retain` with login state "none" is refused, because the rule is one retained box per login state.
  - It is a persistent, non-keep-alive box.
  - On idle and on session end it is paused, not deleted.
  - It is deleted after the retained-box period unused.
  - Ephemeral versus persistent is fixed at creation, because pausing an ephemeral box is refused by the API. So opting in while an ephemeral box runs terminates that box and starts a persistent one.

### 4.6 Login states

- **Naming.** The stored unit is called a "login state", not a "profile". "Profile" already means the credential profile (`internal/server/agent_session.go`) and the Chrome profile directory of `browse/chrome-profile` (`internal/server/server.go`).
- Named login states are stored on the ocode machine, under the browse area of the global data directory, in owner-only files.
- A login state holds all cookies of the box's shared browser context, and the localStorage of each visited origin. It does not hold open tabs.
- It is saved on every clean teardown, and by a debounced checkpoint after browser calls. A ttl delete or a crash gives no warning, so the checkpoint matters.
- **Restore.**
  - Cookies are restored at `start`.
  - localStorage is restored lazily, once per origin per box, through a new-document script on the first visit to that origin. After that, the page's own values are never overwritten.
- The user assigns a login state to a session or project. The model cannot name one. "none" gives a clean browser each time.
- **One writer per login state.** A second session that tries to `start` with a login state already in use gets an error. The error names the holding session. It does not get a clean box silently.
- Login-state data never enters a provider payload, a Jev request, or sync.
- The local login state stays the source of truth for retained boxes too. Box keeps its Chromium profile under `/tmp`, and survival across a pause is untested.
- **Privacy.** While a box lives, the cookies are in the box, and all page traffic passes through Upstash.

### 4.7 Boxes panel

- Web only. A panel lists every box for each account. Columns: status, size, kind (ephemeral or retained), login state, owning session, last used, and expiry.
- The list is sorted and paginated, which is the project-wide rule for listings.
- Actions: pause, resume and delete. Delete asks for confirmation.
- The idle reaper and the sweep act only on boxes labelled for this installation. Other boxes are listed, and changed only by hand.
- The panel polls. It does not use push events.
- There is no TUI part. See the non-goal in section 2.

### 4.8 Testing

- Unit tests with a fake Box client cover:
  - `start` is the only creator;
  - the permission category for `start`;
  - sub-agent refusal;
  - the idle timer paused during a turn and during a pending ask;
  - the hard-cap expiry error;
  - the sweep deletes only dead-lease ephemeral boxes, and never retained boxes or another live process's boxes;
  - the `/reset-id` move;
  - teardown versus checkpoint ordering;
  - refusal of loopback and private URLs;
  - refusal of uploads;
  - the tools array unchanged when an account is added to a live session.
- Websocket transport tests against a local Chrome started with a debugging port.
- Login-state round trip: save, start a new browser, restore, then confirm that cookies are present in a tab, and that localStorage is restored once and not overwritten on the second load.
- One live test, gated on a Box API key environment variable, follows the repo's gated-test pattern.

### 4.9 Phases

1. Box API client, accounts and settings section, secret storage.
2. Websocket transport, attach-mode `Manager` with one shared context, egress blocking. Proven by the gated live test. No tool exposure yet.
3. `backend`, `start` and `terminate` on the tools. Handler-level box manager. Permission category. Idle timeout, hard cap, journal and sweep. Boxes panel.
4. Login states.
5. `retain`.
6. Later: htrcli attach mode; importing selected domains from the local browser into a login state; restoring open tabs; uploads; an in-box driver.

## 5. Open questions

- Whether ttl can be extended after creation. Not found in the MCP tool, the box CLI, or the docs.
- Whether Box's `/tmp` Chromium profile survives a pause.
- Whether 340 ms per command is acceptable for `browser_run`.
- Whether a Box network policy can deny private ranges while allowing everything else, and whether CDP request interception is enough for redirects.
- How web `/new` and `/clear` reach the server, and whether they close the old session.
- Sites that bind sessions to an IP address or a device may reject restored cookies. Rotating refresh tokens may log out another browser.
- Stability of the Box REST API, and of the external CDP URL on ephemeral boxes. The docs promise neither.
- The shared credential store (`internal/auth/store.go`) loads once and writes its whole cache, so a second ocode process with an older copy can erase a key saved by the first. This affects every provider credential and predates this work. The user has been asked whether to fix it.

## 6. References

- [Spike findings, 2026-11-21](../../concepts/upstash-box-browser-spike-findings.md)
- [Upstash Box and Firecrawl pricing](../../concepts/upstash-firecrawl-browser-pricing.md)
- [Browser agent tool](../../concepts/browser-agent-tools.md)
- [browser_run step loop](../../concepts/browser-run-jev-step-loop.md)
- [Prompt cache stability](../../concepts/prompt-cache-stability.md)

## 7. Review findings addressed

1. `target` clashed with the element-ref field on `browser`. Now `backend`. See 4.3.
2. The driver cannot attach to a websocket. See 4.4 (design).
3. The local egress proxy would block every navigation in a box. See 4.4 (egress finding).
4. Per-tab contexts would stop cookies from being shared. See 4.4 (contexts).
5. No hook existed for "fixed at session creation". See 4.3 (tools array stability).
6. Box creation could skip the permission ask. See 4.3 and 4.5 (creation).
7. No single teardown point existed. See 4.5 (teardown ordering).
8. The startup sweep would delete live boxes. See 4.5 (journal and sweep).
9. Loopback was trusted inside a box. See 4.4 (egress rule).
10. Uploads would break in a box. See 2 (non-goals) and 4.3 (uploads).
11. Account selection was unstated. See 4.1 (account selection).
12. The idle timeout ignored active turns. See 4.5 (idle timeout).
13. The localStorage restore was not limited to once. See 4.6 (restore).
14. Reopening tabs navigated without a tool call. Tab restore is dropped from v1. See 2 (non-goals) and 4.6.
15. A retained box with no login state had no key. See 4.5 (retained box) and 4.6.
16. The TUI `/box` command was unnecessary. See 2 (non-goals) and 4.7.
17. "Profile" clashed with existing terms. The unit is now a "login state". See 4.6 (naming).
18. The secret store was unnamed. See 4.1 (secret storage).
19. Phase 2 was too large. It is split across phases 2 and 3. See 4.9.

A second, code-level review on 2026-11-21 found 8 major and 9 minor defects in the phase 1 and 2 code. All were fixed except the credential-store issue in section 5 and the residual egress limits in 4.4.
