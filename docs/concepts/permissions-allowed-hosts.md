---
type: Concept
title: Permissions allowed-hosts list (permissions.allowed_hosts)
description: The persisted permissions.allowed_hosts whitelist of hosts and URL prefixes, covering the config key and its replace-not-merge load and save, the normalisation and wildcard matching rules, the GET and PUT permissions-hosts endpoints, the Settings UI, the Allow it grants for webfetch and browser navigate or open in Decide and its precedence against locked mode, YOLO, tool denies and session cache clicks, what it does not bypass (a listed bash or websearch request is still judged by the outbound-network guard, while a webfetch or browser URL the list covers skips it), the URL-prefix matching rules for path entries, and the tests that pin each rule.
resource: internal/agent/permissions.go
tags:
  - permissions
  - allowed-hosts
  - webfetch
  - browser
  - config
  - network
  - architecture
timestamp: 2026-11-15T10:45:00Z
---
# Permissions allowed-hosts list (`permissions.allowed_hosts`)

## What it is

`permissions.allowed_hosts` is a user-maintained list of hosts or URL prefixes the agent may fetch or open **without a permission prompt**. It is persisted in `ocodeconfig.json`. It is the standing version of the per-session "always allow this domain" click, which is not persisted.

The list switches off the outbound-network guard for one case only: a webfetch or browser URL it covers. A listed bash or websearch request is still judged and can still escalate. See [the outbound-network guardrail](webfetch-websearch-guardrails.md).

## Config and load

- Field: `PermissionConfig.AllowedHosts` (`internal/config/ocodeconfig.go`, the `AllowedHosts` field), JSON key `permissions.allowed_hosts`, an array of strings.
- Load: `applyPermissionConfig` (`ocodeconfig.go`) **replaces** the list rather than merging it, so a host left over from an earlier save cannot survive a shorter one. `PermissionManager.LoadFromOcode` (`internal/agent/permissions.go`) then calls `SetAllowedHosts`.
- Save: `SaveOcodeAllowedHosts` (`ocodeconfig.go`) is a load-modify-write under `withOcodeConfigLock`. It writes only this key and leaves the rest of the permissions block alone.
- `NormalizeAllowedHostList` (`ocodeconfig.go`) trims, de-duplicates and sorts the list, drops empty entries, and returns nil for an empty result. It lower-cases only the host part (before the first `/`) and keeps a path's case, because URL paths are case-sensitive. It does **not** strip URL parts. That is the endpoint's job, below.

## Normalisation and matching

`agent.NormalizeAllowedHost` (`internal/agent/permissions.go`) reduces a typed host, or a full URL, to the form matched against. It lower-cases the host, then strips the scheme, userinfo, port, query, fragment and any trailing dot. The scheme is stripped only when nothing before `://` is `/`, `?` or `#`, so a URL inside an entry's query cannot replace the host. It keeps a leading `*.` so a wildcard stays a wildcard. A path is kept with its case and cleaned with `path.Clean`: `/docs/` and `/docs` are one entry, and a `/` or empty path gives a host-only entry. A pasted `https://docs.site/guide` is stored as `docs.site/guide`. It returns empty for anything unusable: a bare `*` or `*.`, a `*` anywhere in the host or the path, or no host at all.

`PermissionManager.HostAllowed` matches a host against host-only entries. A path entry never allows a bare host:

- Exact entries match case-insensitively, and the port is ignored.
- `*.example.com` matches any subdomain, such as `a.example.com` or `a.b.example.com`. It does **not** match `example.com` itself. List both if the apex is wanted.
- A nil manager allows nothing.

### URL prefixes (`PermissionManager.URLAllowed`)

`PermissionManager.URLAllowed` decides whether a webfetch or browser URL is covered. `Decide` and both outbound and inbound guards use it.

- Only `http` and `https` URLs are covered. A URL that does not parse, has no host, or contains a backslash is not. A browser reads `\` as `/`, so one URL could name one path to this check and another to the server.
- The host matches as `HostAllowed` matches it: exactly, or by a `*.` wildcard.
- A host-only entry covers every path on that host.
- A path entry such as `example.com/docs` covers that path and anything below it, on a segment boundary. `/docs`, `/docs/` and `/docs/a/b` match. `/docsx`, `/doc`, `/Docs` and `/docs/../admin` do not. The request path is percent-decoded and cleaned with `path.Clean` before the comparison, so `/docs/%2e%2e/admin` is `/admin` and is not covered.
- An entry that contains percent-encoding is not decoded, so it never matches. It fails closed.

`SetAllowedHosts` normalises each entry and **replaces** the set, so the in-memory list always equals the last saved list. `AllowedHosts` returns it sorted, for a stable judge state. `Clone` copies it.

## Endpoints

Routes are registered in `internal/server/server.go` (`registerRoutes`, the `permissions-hosts` pair next to `permissions-auto`), behind `authMiddleware`:

- `GET /api/config/ocode/permissions-hosts` returns `{"hosts": [...]}`, read from the server's loaded config (`HandleGetAllowedHostsConfig`, `internal/server/handler_config.go`).
- `PUT /api/config/ocode/permissions-hosts` takes `{"hosts": [...]}` (`HandleSetAllowedHostsConfig`, `handler_config.go`). It:
  - runs every entry through `agent.NormalizeAllowedHost`, and **rejects with 400, naming the entry**, any that normalise to nothing. A typo cannot silently drop out of the list;
  - saves through `SaveOcodeAllowedHosts`, updates `h.cfg`, and calls `SetAllowedHosts` on every live agent (`h.allAgents()`, under `h.mu`). A turn's in-flight `Decide` sees either the old list or the new one, never a mix;
  - replies with the saved list, or `[]` when it is empty.

## Web UI

Settings, Permissions, "Allowed hosts" section (`web/src/components/Settings/PermissionsForm.tsx`). It calls `api.getAllowedHosts` and `api.setAllowedHosts` (`web/src/api/client.ts`). A plain Save writes the list only when it differs from the list the server last returned. Test: `web/src/components/Settings/PermissionsForm.hosts.test.tsx`. The help text and placeholder name the three forms: a host, a `*.` wildcard, or a host with a path prefix such as `example.com/docs`.

## What it grants in `Decide`

`PermissionManager.Decide` (`internal/agent/permissions.go`) uses the list in two places.

**webfetch.** The check sits in the webfetch block, after the localhost allow:

- It returns `Allow` for a URL the list covers (a host, or a path prefix as described above), **before** the in-session `webfetchDomains` cache. A session deny click therefore cannot override the persisted list. An explicit `permissions.tools.webfetch = deny` still wins, because the branch checks the tool rule first.
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

- **An explicit `permissions.tools.webfetch = deny`.** The webfetch branch checks the tool rule before the allow, so `Decide` does not return `Allow` for a listed URL. Pinned by `TestDecideWebfetchExplicitDenyBeatsAllowedHost`. This closes the inconsistency an earlier version of this page described, where the list allowed first and only the executor refused the call.
- **An explicit `permissions.tools.browser = deny`.** The browser branch checks the tool rule before the allow, so it is `Deny` and nothing runs. Pinned by `TestDecideBrowserExplicitToolDenyWinsOverAllowedHost`.

## What it does not bypass

- **The outbound-network guard, for bash and websearch.** The guard runs for every non-Deny decision except a webfetch or browser URL this list covers. For a listed bash or websearch request it still judges the request: a credential in the request, an on-host data upload, a `destructive_action` such as a bulk delete or a payment, an internal address, or an unresolvable request still escalates. Pinned by `TestNetworkGuardAllowedHostIsStillJudged` and `TestNetworkGuardEscalatesDestructiveActionOnAllowedHost` (a bash `curl -X DELETE`). A covered webfetch or browser URL is not judged at all. See [the allowed-URL skip](webfetch-websearch-guardrails.md#the-allowed-url-skip-2026-11-15), which also lists its residuals.
- **Explicit denies and locked mode**, as described above.
- **The browser refusal** for non-http(s) schemes and link-local or cloud-metadata addresses, which runs first.
- **Other tools and bash network commands.** See "Where the list does not apply."

## What the judge is told

The guard's state carries `allowed_hosts`, the sorted union of this list's host entries and the session's webfetch "always allow" domains. Path entries are left out, because the judge reads hosts only. A target whose host matches a host-only entry carries `host_allowed: true`. A covered webfetch or browser URL never reaches the judge, so these reach it only on bash and websearch requests. The rubric says the list is a convenience and not exhaustive, so a host that is not on it is judged as if the list were empty. See [the guardrail page](webfetch-websearch-guardrails.md#the-rubric-an-allow-or-escalate-policy).

## What stays session-only

The webfetch "always allow" click is **unchanged**. It is still held only in the in-session `webfetchDomains` cache and is not persisted. Only the Settings list is persisted.

## Tests

- `internal/agent/permissions_allowed_hosts_test.go`: `TestNormalizeAllowedHost`, `TestHostAllowedMatching`, `TestURLAllowed`, `TestDecideAllowedURLPrefixCoversOnlyThatPath`, `TestDecideWebfetchExplicitDenyBeatsAllowedHost`, `TestSetAllowedHostsReplacesNormalisesAndSorts`, `TestAllowedHostsNilSafe`, `TestAllowedHostsSurviveClone`, `TestLoadFromOcodeAppliesAllowedHosts`, `TestDecideWebfetchAllowedHostAllows`, `TestDecideWebfetchAllowedHostBeatsSessionDeny`, `TestDecideBrowserNavigateAndOpenAllowedHost`, `TestDecideBrowserExplicitToolDenyWinsOverAllowedHost`, `TestDecideBrowserMetadataStaysRefusedWhenListed`, `TestNewAgentFromConfigAllowsListedHost`.
- `internal/config/allowed_hosts_test.go`: `TestAllowedHostsRoundTrip`, `TestAllowedHostsSaveLeavesOtherPermissionsAlone`, `TestAllowedHostsRoundTripKeepsPathCase`.
- `internal/server/handler_allowed_hosts_test.go`: `TestHandleAllowedHostsPutThenGetRoundTrips`, `TestHandleAllowedHostsRejectsInvalidEntry`, `TestHandleAllowedHostsRejectsMalformedBody`, `TestHandleAllowedHostsPushesToLiveAgents`, `TestHandleAllowedHostsKeepsURLPathCase`.

## Related

- [Outbound-network guardrail](webfetch-websearch-guardrails.md): the judge, which still sees bash and websearch requests to a listed host and skips a covered webfetch or browser URL.
- [Browser agent tool](browser-agent-tools.md): the `navigate` and `open` permission rows.
- [Auto-permission enforced categories](auto-permission-enforced-categories.md): the separate `permissions.auto` relaxation switches.
