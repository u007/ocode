---
type: Design
title: Upstash Box as a browser backend for ocode
description: Draft design, revision 2. Phases 1 and 2 committed (f96e2da9). Phases 3 to 5 implemented, uncommitted. An optional Upstash Box Chromium backend for the browser and browser_run tools, with a per-call backend field, an explicit start action, a websocket attach-mode driver, a lease-based sweep, login states kept on the ocode machine, a boxes panel and phased delivery.
resource: internal/browse/cdp
tags:
  - design-spec
  - browser
  - upstash
  - cloud-sandbox
  - login-state
timestamp: 2026-11-22T15:12:03Z
status: active
---

# Upstash Box as a browser backend for ocode

- **Date:** 2026-11-21
- **Status:** Draft. Phases 1 and 2 committed; phases 3 to 5 implemented and uncommitted; see Implementation status.
- **Scope:** An optional remote headless Chromium for the `browser` and `browser_run` tools, hosted in an Upstash Box.
- **Related:** [Spike findings](../../concepts/upstash-box-browser-spike-findings.md), [Pricing](../../concepts/upstash-firecrawl-browser-pricing.md), [Browser agent tool](../../concepts/browser-agent-tools.md)
- **Revision 2 (2026-11-21):** Revised after a code-grounded review. Section 7 lists each review finding and the section that now covers it.
- **Implementation status (2026-11-22):** Phases 1 and 2 are committed in f96e2da9. Phases 3 to 5 are implemented and uncommitted: a session created with an Upstash account can use the box backend, its login states are saved and restored, and `retain` keeps a persistent box per login state. Section 8 has the current built and not-built lists, the known limits and the open network-policy decision.

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
  - `start` takes an optional `retain` boolean (phase 5). See 4.5, retained box.
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
  3. The box is created with a Box network policy that denies private address ranges. **Not implemented.** Phase 3 sets no network policy (decided in phase 3, see section 8). The live probe found that a `custom` policy with only `DeniedCIDRs` is accepted by the API but no page loads; it works only together with `AllowedDomains`.
  
  Part 2 is verified against a real box: attach-mode blocking stops `127.0.0.1:9222` and `169.254.169.254` (spike findings, phase 3 live findings). Without part 3, the box rests on parts 1 and 2. The box is not on the user's network, so it cannot reach the user's LAN in any case.
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
- **Creation.** Only `start` creates a box. It is a static permission ask with its own category, in normal and sandbox modes. The auto-permission judge is not consulted for it. Locked mode denies it. Yolo mode allows it, as it allows everything else. Sub-agents are refused. When the project has a login state assigned, the ask names it (` (login state "<name>" is restored)`), so the approver sees the identity the box will restore.
- **Default box.** Ephemeral, with a ttl equal to the hard cap. Upstash deletes it even if ocode crashes.
- **Early teardown by ocode.** An ephemeral box is deleted early on:
  - `HandleCloseSession`;
  - `Handler.Shutdown` (`internal/server/handler_shutdown.go`), through `shutdownBoxes`;
  - the idle timeout;
  - the `terminate` action.
  A retained box is paused on the same four triggers instead (see 4.5, retained box).
- The server has no session-delete route today. `session.Delete` is TUI-only (`internal/tui/model.go`).
- How web `/new` and `/clear` reach the server, and whether they close the old session, was not determined in the review. The implementation plan decides it. Until then the idle timeout and the ttl cover those paths.
- **Idle timeout.** The timer counts only while the session has no active turn and no pending permission or question ask. It is paused during a turn. The hard cap applies to an ephemeral box only; a retained box has none.
- **Re-key.** The manager's session-keyed map is added to the `/reset-id` move list (`internal/server/handler_reset_id.go`).
- **Ownership label.** Every box carries a label that names ocode and this installation.
- **Journal and sweep.**
  - Each created box is journaled in the data directory with its kind (ephemeral or retained) and the owning process's lease. The lease holds the process id, the process start time and a heartbeat time.
  - Several ocode server processes can share one data directory. No session is live at server start, because agents are built lazily. So the sweep never uses "no live session" as its test.
  - The sweep (`Tick`) acts on two sets. Ephemeral boxes journaled by another process whose lease is dead are deleted (`Journal.Orphans`). Retained rows are read through `Journal.RetainedDue` and claimed through `Journal.ClaimDue`: a row past its delete-at is deleted (an unpaused row only when its owner is dead), and an unpaused row of a dead process inside its period is paused.
  - A lease is dead when its heartbeat is older than 90 s and its process is gone. A process created after the lease's last heartbeat is a reused pid, so that lease is dead too (`leaseAliveWith`). Each process heartbeats every 20 s.
  - Retained rows live in `upstash-boxes.json`. A retained row carries `LoginState`, `Paused`, `LastUsed` and `Deleting`, and its `ExpiresAt` is the delete-at. `Journal.Resume` is a compare-and-set. It returns `ErrRetainedBusy` for a row being deleted or run by a live peer. Inside one process, the `resuming` and `sweeping` reservations close the same races.
- **Hard cap mid-task (ephemeral).** The next call fails with a clear error that says the box expired. The model calls `start` again. The login state is restored. Page state is gone.
- **Teardown ordering.** Teardown takes the per-box lock, cancels the pending checkpoint, writes a final login-state save, then deletes the box (ephemeral) or pauses it (retained). No checkpoint runs after a delete or pause has started. The ordering is tested in phase 4. Login-state files are written atomically: a temporary file, then a rename (4.6).
- **Retained box (phase 5, built).**
  - It is created or resumed only by `start` with `retain`. Each such call is a permission ask (`browser.box_start`), and its text names the box as retained.
  - It needs a named login state. With no login state, or with `none`, the start is refused (`ErrRetainNeedsLoginState`). The rule is one retained box per (account, login state), named `ocode-ls-<login state>`.
  - It is created with no ttl and no keep-alive. Its journal row's `ExpiresAt` is its delete-at: last use plus the account's retained-box period.
  - A start resumes the journaled box through `Journal.Resume`, a compare-and-set. The browser connect call wakes a paused box, so no separate resume call is made (about 6.5 s live).
  - A running ephemeral box of the session is retired first, with its final save and delete. The retained start then creates or resumes its box.
  - On idle, `terminate`, session close and shutdown it is paused, not deleted. Each of these goes through `retireBox`: the final login-state save, the browser closed, then `parkRetained` pauses the box and journals it paused. A failed pause keeps the box running and queues it in the `unpaused` set, and `Tick` retries it.
  - A pause releases the login-state hold, so any session whose project uses that login state can resume the same box.
  - After an idle pause, browser calls return `ErrPaused` until the next start or terminate clears it. The 2 h hard cap does not apply.
  - `Tick` deletes it once its delete-at passes: a paused row at once, an unpaused row only when its owner is dead. Deletion needs a running ocode.
  - Ephemeral versus persistent is fixed at creation, because pausing an ephemeral box is refused by the API. So opting in while an ephemeral box runs terminates that box and starts a persistent one.

### 4.6 Login states

- **Naming.** The stored unit is called a "login state", not a "profile". "Profile" already means the credential profile (`internal/server/agent_session.go`) and the Chrome profile directory of `browse/chrome-profile` (`internal/server/server.go`).
- **Storage (as built).** Each login state is one file, `<GlobalDataDir>/browse/login-states/<name>.json`. The directory is 0700 and the files are 0600. A write goes to a temporary file, is fsynced, then renamed over the old file. A name must match `^[a-z0-9][a-z0-9_-]{0,39}$`, and `none` is reserved (`config.ValidLoginStateName`).
- **Contents.** A login state holds all cookies of the box's shared browser context, and the localStorage of each origin. It does not hold open tabs.
  - **Deviation:** localStorage is captured only for origins that have a tab open at capture time. The original text said "each visited origin". Section 8 lists this as a known limit.
- **Save.**
  - Every `Browser()` call arms a 10 s debounced checkpoint. It is forced after 60 s without one. One more checkpoint runs within 90 s of the box's expiry. A ttl delete or a crash gives no warning, so the checkpoint matters.
  - Teardown order: stop the checkpoint, final save (bounded at 10 s), close the attach Manager, delete the box (ephemeral) or pause it (retained), release the hold. No checkpoint runs after a delete has started.
  - `Shutdown` runs all final saves concurrently under one deadline, then tears each box down: an ephemeral box is deleted and a retained box is paused. When a retained box's save is pending, the saves get at least `shutdownSaveFloor` (2 s), detached from the caller's deadline.
  - Capture merge: cookies are replaced wholesale when the read succeeds, and a failed read keeps the file's cookies. An origin in the snapshot replaces the stored one, and an empty origin clears it. An origin that is absent, or over about 1 MiB, keeps its stored values.
- **Restore.**
  - Cookies are restored at `start`. Expired cookies are dropped. Rejected cookies are counted, and the count is one of the notes in the `start` result.
  - localStorage is seeded once per origin per box. Each origin gets one isolated-world new-document script. The script reports its origin back through an isolated-world binding (`Runtime.addBinding` with `executionContextName`), and the Manager then removes that origin's script from every tab. No marker key is written to page storage, so the page's own values are never overwritten. A legacy marker key is dropped on capture.
  - The restore runs before the box is published.
  - **Re-dial.** When the websocket drops, the attach Manager asks `Target.getBrowserContexts`. A surviving shared context is reused and never disposed. A missing one (the browser restarted) is replaced, and the restore hook runs before any tab opens. A failed restore fails the attach. This fixed a reviewed data-loss path, where an empty cookie jar was saved over a good state.
- **Hold and assignment.**
  - `start` takes the hold before it creates a box. A second session that asks for a state in use gets `ErrLoginStateInUse`, which names the holding session, and no box is created. It never gets a clean box silently.
  - The hold is a file, `<name>.lock.json`, that records the session id, the pid and the process start. A holder with our pid but another process start is stale.
  - A rekey (`Rekey`) moves the hold with `Transfer`.
  - **Per project, by the user only.** The assignment is `upstash_box.login_states` (project root to name) in ocodeconfig.json. It is set through `GET`/`PUT /api/upstash/login-state?project=` or the Upstash Box settings section. **Per-session assignment is not built**, and the model cannot name a login state. No tool argument names one. "none" gives a clean browser each time.
- **Protection.** Every tool whose path argument names the login-state directory — the file tools, `imagegen`/`ocr` (`image_path`) and the media tools (`output_path`) — is an Ask in normal and sandbox modes, as are bash lines that name it; locked mode denies it and yolo allows it. The Ask is human-only: no judge, no always-allow, nothing persisted. A path built from a shell variable, read inside an interpreter, or a recursive read of a parent directory (`grep -r` over the data dir) is not caught. The judge context withholds the files (`sensitiveContextFile` in `internal/agent/agent.go`). `redact.IsSensitiveFile` treats them like auth.json. In sandbox mode the directory is also a non-writable root (`AllowedRootsClassified` in `internal/agent/permissions.go`), so the OS blocks a sandboxed write into it. See [sandbox permission mode](../../concepts/sandbox-permission-mode.md).
- **Sync.** The `upstash_box` section (account names and ids, login-state assignments, no keys) syncs with ocodeconfig.json. Login-state files never sync.
- Login-state data never enters a provider payload or a Jev request.
- The local login state stays the source of truth for retained boxes too. It is saved before every pause and restored after every resume. Box keeps its Chromium profile under `/tmp` on the overlay disk. The profile survives a pause, but Chromium restarts on resume and every tab is gone (spike findings, phase 5).
- **Privacy.** While a box lives, the cookies are in the box, and all page traffic passes through Upstash.

### 4.7 Boxes panel

- Web only. A panel lists every box for each account. Columns: status, size, kind (ephemeral or retained), login state, owning session, last used, and expiry.
- The list is sorted and paginated, which is the project-wide rule for listings.
- Actions: pause, resume and delete. Delete asks for confirmation.
- For a retained box (phase 5): resume answers 409, because a retained start resumes the box. Pause goes through `Manager.PauseBox`. That detaches a box a live session holds (through `retireBox`) and pauses it. A box no session holds is paused through the API. It answers 409 when another live process runs the box. Delete runs the API delete and then `Manager.ForgetBox`. The list shows a "retained" badge and "Deleted if unused until", and no Resume button.
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
  - the sweep deletes only dead-lease ephemeral boxes. For retained rows it acts only on what `RetainedDue` lists (a row past its delete-at, or a running stray of a dead process), and never on a live process's box;
  - the `/reset-id` move;
  - teardown versus checkpoint ordering;
  - refusal of loopback and private URLs;
  - refusal of uploads;
  - the tools array unchanged when an account is added to a live session.
- Websocket transport tests against a local Chrome started with a debugging port.
- Login-state round trip: save, start a new browser, restore, then confirm that cookies are present in a tab, and that localStorage is restored once and not overwritten on the second load.
- Live tests, gated on the `UPSTASH_BOX_API_KEY` environment variable, follow the repo's gated-test pattern. The phase 3 live tests are `TestLiveBoxBrowser` and `TestLiveBoxNetworkPolicy` (`internal/upstashbox/live_test.go`) and `TestLiveBrowserToolOnBox` (`internal/server/browser_boxes_live_test.go`). The phase 4 login-state round trip is `TestLiveBoxLoginStateRoundTrip` (`internal/upstashbox`) and `TestLiveBrowserToolLoginState` (`internal/server`). The phase 5 retained-box test is `TestLiveBrowserToolRetainedBox` (`internal/server`).

### 4.9 Phases

1. Box API client, accounts and settings section, secret storage. Committed in f96e2da9.
2. Websocket transport, attach-mode `Manager` with one shared context, egress blocking. Committed in f96e2da9. Its blocking is shown by the gated live test (see section 8, live evidence). No tool exposure yet.
3. `backend`, `start` and `terminate` on the tools. Handler-level box manager. Permission category. Idle timeout, hard cap, journal and sweep. Boxes panel. Implemented, uncommitted.
4. Login states. Implemented, uncommitted (2026-11-22). See 4.6 and section 8.
5. `retain`: a persistent box per login state, paused when idle and deleted after the retained-box period. Implemented, uncommitted (2026-11-22). See 4.5 and section 8.
6. Later: htrcli attach mode; importing selected domains from the local browser into a login state; restoring open tabs; uploads; an in-box driver.

## 5. Open questions

- Whether ttl can be extended after creation. Not found in the MCP tool, the box CLI, or the docs.
- Whether 340 ms per command is acceptable for `browser_run`.
- Whether a Box network policy can deny private ranges while allowing everything else. The live probe found that a `custom` policy with only `DeniedCIDRs` loads no page; it works only with `AllowedDomains` too. Open decision: no network policy is set (4.4, part 3). Whether CDP request interception is enough for redirects is not settled by the live run.
- How web `/new` and `/clear` reach the server, and whether they close the old session.
- Sites that bind sessions to an IP address or a device may reject restored cookies. Rotating refresh tokens may log out another browser.
- Stability of the Box REST API, and of the external CDP URL on ephemeral boxes. The docs promise neither.
- A crash leaves a retained box running until an ocode start or a sweep pauses it as a stray. A persistent box has no ttl, so Upstash's idle auto-pause is the only other bound. A server-side idle cap would close this.
- A retained box past its delete-at is deleted only while an ocode process runs.
- Untested: `PauseBox` racing an in-flight `Start` of the same box. The `parkRetained` ordering fix has no deterministic test.
- Fixed 2026-11-22: the shared credential store (`internal/auth/store.go`) loaded once and wrote its whole cache, so a second ocode process with an older copy could erase a key saved by the first. Writes are now load-modify-write under a cross-process file lock. See [auth.json multi-process credential loss](../../gotchas/auth-json-multi-process-credential-loss.md).

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

A second, code-level review on 2026-11-21 found 8 major and 9 minor defects in the phase 1 and 2 code. All were fixed except the residual egress limits in 4.4. The credential-store issue in section 5 was fixed on 2026-11-22.

## 8. Status after phase 5 (2026-11-22)

- **Built in phases 3 to 5 (uncommitted).**
  - Phase 3: `backend` (`local` or `upstash`) on `browser` and `browser_run`, with the
    session flag `upstash_browser` fixed at creation. `start` and `terminate` on `browser`.
    The `browser.box_start` ask. The `internal/boxsession` Manager with
    its journal, lease, sweep, idle timer and hard cap. `GET /api/upstash/boxes`, with pause,
    resume and delete. The boxes list in the Upstash Box settings section, which polls every
    15 s while the page is visible.
  - Phase 4: the login-state store (`internal/boxsession/loginstate.go`) with the file layout
    in 4.6. The driver calls in `internal/browse/cdp/attach_storage.go`. The Manager's restore,
    checkpoint, final-save and hold logic (`internal/boxsession/manager_loginstate.go`). The
    re-dial context check. Per-project assignment, with `GET`/`PUT /api/upstash/login-state`,
    `GET /api/upstash/login-states` (counts only, never content) and
    `DELETE /api/upstash/login-states/{name}` (409 while held). The `UpstashLoginStates`
    settings component. A `login_state` column on the boxes list. The notes in the `start`
    result. The protection in `internal/agent/permissions.go` (`isLoginStatePath`,
    `loginStateBashHit`, `IsLoginStateRequest`) with the judge veto in `internal/agent/agent.go`.
    The refusal of a Pulse `permission_resolve` on a login-state ask. The `upstash_box` sync.
  - Phase 5: `retain` on `start` (`StartOptions.Retain`, `startRetained`, `resumeRetained`,
    `createRetained`), with the retained box as 4.5 describes it. Every teardown of a retained
    box pauses it through `retireBox` and `parkRetained`. The `unpaused` retry set and the `Tick`
    sweep over `RetainedDue` and `ClaimDue`. `Journal.Resume` is a compare-and-set. `leaseAliveWith`
    also covers a reused pid, for ephemeral orphans too. `shutdownSaveFloor` (2 s) guarantees a
    retained box's final save. The `browser.box_start` ask text for a retained start.
    `POST .../resume` answers 409 for a retained box. `POST .../pause` goes through
    `Manager.PauseBox`. `DELETE` deletes the box and then calls `Manager.ForgetBox`. The boxes list
    shows a "retained" badge and "Deleted if unused until". `handlerBoxes.Surfaces` now surfaces
    `ErrPaused` and `ErrExpired` instead of masking them.
- **Not built.**
  - No network policy is set on the box (4.4, part 3; decided in phase 3, and an open decision
    in section 5).
  - Per-session login-state assignment. The design says "session or project"; only project is built.
  - Assignment for remote (SSH/WSL) projects.
- **Known limits.**
  - localStorage is captured only for origins with a tab open at capture time. The design's
    "each visited origin" is a deviation.
  - Approved reads of a login-state file are redacted only when a tier-2 redaction scanner is
    configured. auth.json is handled the same way, and a probe showed it is returned raw by default.
  - Yolo does not guard the login-state directory.
  - Paths built from shell variables, or read inside an interpreter, are not caught by the
    static checks.
  - A corrupt holder file makes the login-state list fail.
  - A restored cookie that the browser silently rejects is only counted. It is not reported by name.
  - From phase 3, still open: a cancelled start is cleaned up by the next reaper pass, up to
    20 s later.    `boxSurfaces.Surface`, the navigate-and-load path, has only a fake-page test. The boxes
    list has not been checked in a browser. No full agent turn has been run against a live box.
    Name resolution still runs on the ocode machine (4.4, residual limits).
  - Phase 5: a crash leaves a retained box running until an ocode start or a sweep pauses it,
    and Upstash's idle auto-pause is the only backstop. Deleting an unused retained box needs a
    running ocode. Yolo starts a retained box without an ask. After an idle pause, browser calls
    return `ErrPaused` until the next start or terminate. `PauseBox` racing an in-flight `Start`
    of the same box is untested, and the `parkRetained` ordering fix has no deterministic test.
- **Live evidence.**
  - [Spike findings, phase 3 live findings](../../concepts/upstash-box-browser-spike-findings.md#phase-3-live-findings-2026-11-21).
  - [Spike findings, phase 4 live findings](../../concepts/upstash-box-browser-spike-findings.md#phase-4-live-findings-2026-11-22).
  - [Spike findings, phase 5 live findings](../../concepts/upstash-box-browser-spike-findings.md#phase-5-live-findings-2026-11-22).
