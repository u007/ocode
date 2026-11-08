---
type: Concept
title: Permissions allowed-hosts list (permissions.allowed_hosts)
description: The persisted permissions.allowed_hosts whitelist, covering the config key and its replace-not-merge load and save, the normalisation and wildcard matching rules, the GET and PUT permissions-hosts endpoints, the Settings UI, the Allow it grants for webfetch and browser navigate or open in Decide and its precedence against locked mode, YOLO, tool denies and session cache clicks, what it does not bypass (the outbound-network guard still judges every listed host), and the tests that pin each rule.
resource: internal/agent/permissions.go
tags:
  - permissions
  - allowed-hosts
  - webfetch
  - browser
  - config
  - network
  - architecture
timestamp: 2026-11-08T08:40:00Z
---
# Permissions allowed-hosts list (`permissions.allowed_hosts`)

## What it is

`permissions.allowed_hosts` is a user-maintained list of hosts the agent may fetch or open **without a permission prompt**. It is persisted in `ocodeconfig.json`. It is the standing version of the per-session "always allow this domain" click, which is not persisted.

It does **not** switch off the outbound-network guard. The guard still sees every request to a listed host and can escalate it. See [the outbound-network guardrail](webfetch-websearch-guardrails.md).

## Config and load

- Field: `PermissionConfig.AllowedHosts` (`internal/config/ocodeconfig.go`, the `AllowedHosts` field), JSON key `permissions.allowed_hosts`, an array of strings.
- Load: `applyPermissionConfig` (`ocodeconfig.go`) **replaces** the list rather than merging it, so a host left over from an earlier save cannot survive a shorter one. `PermissionManager.LoadFromOcode` (`internal/agent/permissions.go`) then calls `SetAllowedHosts`.
- Save: `SaveOcodeAllowedHosts` (`ocodeconfig.go`) is a load-modify-write under `withOcodeConfigLock`. It writes only this key and leaves the rest of the permissions block alone.
- `NormalizeAllowedHostList` (`ocodeconfig.go`) trims, lower-cases, de-duplicates and sorts the list, drops empty entries, and returns nil for an empty result. It does **not** strip URL parts. That is the endpoint's job, below.

## Normalisation and matching

`agent.NormalizeAllowedHost` (`internal/agent/permissions.go`) reduces a typed host, or a full URL, to the form matched against. It lower-cases the value, then strips the scheme, userinfo, port, path, query, fragment and any trailing dot. It keeps a leading `*.` so a wildcard stays a wildcard. It returns empty for anything unusable, including a bare `*`, and for a `*` anywhere else.

`PermissionManager.HostAllowed` matches the normalised host:

- Exact entries match case-insensitively, and the port is ignored.
- `*.example.com` matches any subdomain, such as `a.example.com` or `a.b.example.com`. It does **not** match `example.com` itself. List both if the apex is wanted.
- A nil manager allows nothing.

`SetAllowedHosts` normalises each entry and **replaces** the set, so the in-memory list always equals the last saved list. `AllowedHosts` returns it sorted, for a stable judge state. `Clone` copies it.

## Endpoints

Routes are registered in `internal/server/server.go` (`registerRoutes`, the `permissions-hosts` pair next to `permissions-auto`), behind `authMiddleware`:

- `GET /api/config/ocode/permissions-hosts` returns `{"hosts": [...]}`, read from the server's loaded config (`HandleGetAllowedHostsConfig`, `internal/server/handler_config.go`).
- `PUT /api/config/ocode/permissions-hosts` takes `{"hosts": [...]}` (`HandleSetAllowedHostsConfig`, `handler_config.go`). It:
  - runs every entry through `agent.NormalizeAllowedHost`, and **rejects with 400, naming the entry**, any that normalise to nothing. A typo cannot silently drop out of the list;
  - saves through `SaveOcodeAllowedHosts`, updates `h.cfg`, and calls `SetAllowedHosts` on every live agent (`h.allAgents()`, under `h.mu`). A turn's in-flight `Decide` sees either the old list or the new one, never a mix;
  - replies with the saved list, or `[]` when it is empty.

## Web UI

Settings, Permissions, "Allowed hosts" section (`web/src/components/Settings/PermissionsForm.tsx`). It calls `api.getAllowedHosts` and `api.setAllowedHosts` (`web/src/api/client.ts`). A plain Save writes the list only when it differs from the list the server last returned. Test: `web/src/components/Settings/PermissionsForm.hosts.test.tsx`.

## What it grants in `Decide`

`PermissionManager.Decide` (`internal/agent/permissions.go`) uses the list in two places.

**webfetch.** The check sits in the webfetch block, after the localhost allow:

- It returns `Allow` for a listed host, **before** the in-session `webfetchDomains` cache. A session deny click therefore cannot override the persisted list.
- Earlier gates still win. Locked mode denies every non-read tool, before this check. YOLO bare-allows before it,. Those are the existing YOLO and locked semantics.

**browser `navigate` and `open`.** The check sits in the browser branch:

- It runs after `browserURLRefusal`, which is called at the top of `Decide`. The metadata, link-local and non-http(s) hard refusal therefore still wins over a listed host.
- It returns `Allow` only when the tool's own rule is not `deny`. An explicit `permissions.tools.browser = deny` still wins. Test: `TestDecideBrowserExplicitToolDenyWinsOverAllowedHost`.
- The allow then passes through `browserDecision` (`internal/agent/agent.go`). That step only gates uploads of sensitive files and reads of user-owned tabs. `navigate` and `open` are not in its read set, so they stay allowed.

**Where the list does not apply.** `Decide` consults it only for webfetch and browser `navigate` and `open`. Nothing else reads it:

- `websearch` goes to a fixed endpoint with no per-host decision.
- `bash` `curl` and `wget` are decided by the prefix rules and the exfiltration detectors, not by the host list. The guard does see a bash target's `host_allowed` mark, but the permission decision does not use it.

## Precedence against explicit denies

Two cases are worth knowing, and they differ:

- **An explicit `permissions.tools.<tool> = deny` on webfetch.** `Decide` returns `Allow` for a listed host, before the tool-level rule at the foot of `Decide`. The call is still refused, though. `executeToolCallWithContext` (`internal/agent/agent.go`) calls `isToolAllowed` first, and `isToolAllowed` returns false for a tool whose `Check` is `deny`. The model then receives `denied: tool "webfetch" is not allowed`. So a listed host never runs a tool the user denied. The permission layer's own answer, though, is `Allow`, which is a precedence inconsistency. No test covers the webfetch case.
- **An explicit `permissions.tools.browser = deny`.** The browser branch checks the tool rule before the allow, so it is `Deny` and nothing runs. Pinned by `TestDecideBrowserExplicitToolDenyWinsOverAllowedHost`.

## What it does not bypass

- **The outbound-network guard.** The guard runs for every non-Deny decision, including the Allow from this list. A listed host is still judged. A credential in the request, an on-host data upload, a `destructive_action` such as a bulk delete or a payment, an internal address, or an unresolvable request still escalates. Pinned by `TestNetworkGuardAllowedHostIsStillJudged` and `TestNetworkGuardEscalatesDestructiveActionOnAllowedHost`.
- **Explicit denies and locked mode**, as described above.
- **The browser refusal** for non-http(s) schemes and link-local or cloud-metadata addresses, which runs first.
- **Other tools and bash network commands.** See "Where the list does not apply."

## What the judge is told

The guard's state carries `allowed_hosts`, the sorted union of this list and the session's webfetch "always allow" domains. A target whose host matches carries `host_allowed: true`. The rubric says the list is a convenience and not exhaustive, so a host that is not on it is judged as if the list were empty. See [the guardrail page](webfetch-websearch-guardrails.md#the-rubric-an-allow-or-escalate-policy).

## What stays session-only

The webfetch "always allow" click is **unchanged**. It is still held only in the in-session `webfetchDomains` cache and is not persisted. Only the Settings list is persisted.

## Tests

- `internal/agent/permissions_allowed_hosts_test.go`: `TestNormalizeAllowedHost`, `TestHostAllowedMatching`, `TestSetAllowedHostsReplacesNormalisesAndSorts`, `TestAllowedHostsNilSafe`, `TestAllowedHostsSurviveClone`, `TestLoadFromOcodeAppliesAllowedHosts`, `TestDecideWebfetchAllowedHostAllows`, `TestDecideWebfetchAllowedHostBeatsSessionDeny`, `TestDecideBrowserNavigateAndOpenAllowedHost`, `TestDecideBrowserExplicitToolDenyWinsOverAllowedHost`, `TestDecideBrowserMetadataStaysRefusedWhenListed`, `TestNewAgentFromConfigAllowsListedHost`.
- `internal/config/allowed_hosts_test.go`: `TestAllowedHostsRoundTrip`, `TestAllowedHostsSaveLeavesOtherPermissionsAlone`.
- `internal/server/handler_allowed_hosts_test.go`: `TestHandleAllowedHostsPutThenGetRoundTrips`, `TestHandleAllowedHostsRejectsInvalidEntry`, `TestHandleAllowedHostsRejectsMalformedBody`, `TestHandleAllowedHostsPushesToLiveAgents`.

## Related

- [Outbound-network guardrail](webfetch-websearch-guardrails.md): the judge that still sees every listed host.
- [Browser agent tool](browser-agent-tools.md): the `navigate` and `open` permission rows.
- [Auto-permission enforced categories](auto-permission-enforced-categories.md): the separate `permissions.auto` relaxation switches.
