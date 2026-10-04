---
type: Concept
title: Settings → Connectors (web/desktop)
description: 'The web/desktop Settings → Connectors surface: base auth.json credentials, masked secrets, host threading, OAuth auto/manual completion modes, flow-state invariants, and known gaps.'
tags:
  - connectors
  - settings
  - auth
  - oauth
  - credentials
  - web
  - desktop
  - security
timestamp: 2026-10-02T08:37:10Z
---
# Settings → Connectors (web/desktop)

**Type:** Concept  
**Description:** The web/desktop Settings → Connectors surface: base `auth.json` credential management, masked secrets, host threading, OAuth completion modes (auto/manual), flow-state invariants, and known gaps.  
**Tags:** connectors, settings, auth, oauth, credentials, web, desktop, security  

---

## Scope

`web/src/components/Settings/ConnectorsForm.tsx` manages the **base** credential store (`auth.json`) — the TUI `/connect` parity surface. Per-profile key overrides are a different layer and live in `ProfilesManager.tsx` (`auth.profiles.json`); do not conflate the two.

`SettingsPanel.tsx` renders `<ConnectorsForm />` with **no `host` prop, deliberately**. Settings are a global surface by convention, so that instance manages the credentials of the machine serving the request. This is pinned by a test — do not "fix" it by threading a host.

## Masked credentials

`maskConnectCredential` (`internal/server/handler_connect.go`):

- Key shorter than 16 characters → masked **whole**.
- Otherwise → **only the trailing 4 characters** are shown; the head is never revealed.

The previous implementation showed a 4+4 window, which handed over 8 characters of a 9-character key. No endpoint ever returns a full secret.

`ConnectFlow.snapshot()` in the same file is an **explicit allowlist**, not a struct marshal. That is structurally why the PKCE verifier and the OAuth `state` can never reach the poll response, even though the in-memory flow struct holds them.

## Host threading

All eight connect client methods take a trailing `host`: `listConnectProviders`, `setConnectCredential`, `removeConnectCredential`, `testConnectCredential`, `startConnectFlow`, `getConnectFlow`, `submitConnectFlowInput`, `cancelConnectFlow`.

Credentials are per-machine, so a call that drops `host` writes to the wrong `auth.json`.

These routes are **machine-global by design**:

- No `?project=` parameter, no `allowedProjectRoots()` validation, never `h.workDir`, never `os.Getwd()`.
- All eight sit behind `s.authMiddleware`, deliberately never `healthMiddleware`.
- `handler_connect.go` contains zero `h.mu` references — never hold `h.mu` across a flow probe or a flow.

## Completion modes

A loopback OAuth flow can complete two ways:

- **`auto`** binds a loopback port on the server and finishes when the provider redirects the browser back to it — which requires the browser to be on the server's machine.
- **`manual`** binds nothing and takes the redirect as paste-back input, so it works from a `serve --remote` host or a second device.

Each connect method may carry a `modes` array. `oauthFlowTakesMode` (`internal/server/handler_connect.go`) is the **single predicate** feeding both the advertised `modes` and the start handler's dispatch, so the two cannot drift. It is true only for method id `oauth` on a provider whose catalog entry declares `OAuthFlow == "openai"`.

### Traps

- It is keyed on **`OAuthFlow`, not the provider id**. `codex` also declares `OAuthFlow: "openai"`, and `google` uses the same method id `oauth` while having no manual mode yet.
- A plugin **replaces** a provider's built-in OAuth flow (`MethodsFor` in `internal/auth/methods.go`). The built-in `codex` plugin (`internal/plugin/codex/codex.go`) registers for provider id `openai` and is linked by `main.go`, so in every shipped binary `openai` offers only `apikey` plus two `plugin_*` methods and **no `oauth` method at all**. Manual mode is therefore reachable via `codex`, not `openai`. Corollary for testing: `internal/server` tests see a catalog no shipped binary has, because that package does not link the plugin.

### The client picks the mode

A client must never render a mode chooser the server did not advertise. An Anthropic paste-code flow always waits for a paste, so offering a choice there is a control the handler ignores — the user picks a mode, gets the same flow, and concludes sign-in is broken.

**The client picks the mode, not the server.** `remoteMode` lives on `*Server` while these routes are registered as `s.handler.*`, so the handler never sees it; only the client knows whether the *browser* shares the server's host.

- `ConnectFlowPanel` defaults to `host ? "manual" : "auto"`.
- It sends **no `mode` field at all** when the method advertises none.
- An absent or unrecognised `mode` is treated as `auto`, preserving historical behaviour for every pre-existing client.

## Flow states and invariants

- **`waiting_input` is load-bearing** — it is the only thing that renders the paste box, and the panel never renders a field the server did not send.
- **`beginInput`** is a `waiting_input` → `running` compare-and-set that also installs the cancel func under one lock. It is what makes a pasted code **single-use**; a replayed paste gets **409**.
- **`beginCommit`** claims the exclusive right to persist a credential, moving the flow to `committing`. A **cancelled** flow's credential is discarded rather than saved behind the user's back, and cancel is refused with **409** once a flow is `committing`.
- A pasted value carrying no `state`, or a `state` that does not match this flow, is rejected. A bare code is deliberately refused: it carries no state, so accepting it would let an attacker-supplied code be redeemed on the user's machine.
- **Polling is the only completion signal** (`GET /flows/{id}`); the server holds the loopback listener, so nothing is pushed to the tab.

## Known gaps

State these as gaps, not as behaviour:

- **Google has no manual mode.** Its flow binds a loopback port, so it cannot complete from a remote host, and it would additionally need user-supplied client credentials.
- **OpenAI's own remote sign-in is unreachable.** The codex plugin's browser method calls `auth.OpenAILogin`, which binds a localhost callback on the server's machine and calls `openBrowser`; its device-code method is host-agnostic but `startPluginConnectFlow` never surfaces `userCode` / `verificationUri`, so the user is never shown their code. Reviewed 2026-10-02 and deliberately left unchanged — a remote OpenAI user uses an API key or switches to `codex`.

## References

- Component: `web/src/components/Settings/ConnectorsForm.tsx`
- Server handler: `internal/server/handler_connect.go` (`maskConnectCredential`, `ConnectFlow.snapshot`, `oauthFlowTakesMode`, `beginInput`, `beginCommit`)
- Method catalog: `internal/auth/methods.go` (`MethodsFor`)
- Codex plugin: `internal/plugin/codex/codex.go`
- Related: `superpowers/specs/2026-10-01-web-connector-settings-design.md`, `superpowers/plans/2026-10-01-web-connector-settings.md`