---
type: Concept
title: Streamable HTTP MCP (type "http") and the ElevenLabs preset
description: 'MCP config type http (Streamable HTTP): the one-time initialize/initialized handshake, Mcp-Session-Id and MCP-Protocol-Version headers, JSON-or-SSE decoding, session reset after a token refresh, and the ElevenLabs preset whose OAuth client_id is a jsDelivr-hosted client-metadata document because ElevenLabs has no dynamic client registration. Also the loopback-only connect endpoint, the canonical-preset merge that keeps only user-owned fields, and what is not live-verified.'
resource: internal/mcp/streamable.go
tags:
  - mcp
  - http
  - oauth
  - elevenlabs
timestamp: 2026-11-15T15:09:56Z
---
# Streamable HTTP MCP and the ElevenLabs preset

MCP config `type: "http"` selects the Streamable HTTP transport: every message
is a POST to the server URL and the reply is JSON or one SSE frame. It is a
remote client (`NewRemoteClient`) with an initialize handshake; the ElevenLabs
hosted server is the first preset shipped on it.

## Transport (`internal/mcp/streamable.go`)

- `ensureStreamable` runs the ONE-TIME handshake: `initialize` with
  `protocolVersion`/`capabilities`/`clientInfo`, capture `Mcp-Session-Id` and the
  negotiated protocol version, then send `notifications/initialized` (202, no body).
- Every request carries `Accept: application/json, text/event-stream`,
  `Mcp-Session-Id` and `MCP-Protocol-Version`; after a token refresh the session
  is dropped (`resetStreamable`) and the handshake re-runs
  (`internal/mcp/client.go:530`, `:531`).
- `decodeRemoteResponse` routes a `text/event-stream` body to
  `decodeSSEJSONRPC`, which matches the request id and skips notifications and
  non-JSON-RPC frames; anything else is plain JSON-RPC.
- Both agent switches accept `"http"`: `internal/agent/agent.go:7053` and
  `internal/agent/mcp_clients.go:210`.

## ElevenLabs CIMD

`https://api.elevenlabs.io/v1/mcp`, verified 2026-11-15: POST-only, `xi-api-key`
rejected (401 "OAuth bearer token required"), and the authorization server
advertises `client_id_metadata_document_supported` with NO dynamic client
registration. The OAuth `client_id` is the URL of `client-metadata.json` at the
repo root, served as `application/json` by
`https://cdn.jsdelivr.net/gh/u007/ocode@main/client-metadata.json` (GitHub's raw
host serves `text/plain`). It must be committed and pushed to `main` before a
connect works; jsDelivr can lag a few minutes after the push.

`redirect_uris` is `http://localhost:8085/mcp-callback`, the fixed port in
`auth.MCPAuthFlow` (`internal/auth/mcp_auth.go:450`, listener `:468`); changing
it means editing both the JSON and the flow.

## Connect flow

`POST /api/mcp/elevenlabs/connect` (`internal/server/handler_mcp_elevenlabs.go`):

1. Refuses a non-loopback caller with 403 (`isLoopbackRequest`,
   `TestConnectElevenLabsRefusesRemoteCaller`): the callback only reaches the
   machine running ocode. The remote proxy refuses it too: `isMCPAuthRestPath`
   (`internal/server/handler_remote_proxy.go`) also matches
   `mcp/elevenlabs/connect`, so a remote SSH host is rejected before connecting,
   pinned by `TestMCPAuthRestPathMatching` and
   `TestConnectElevenLabsRefusesRemoteCaller`.
   - `POST /api/mcp/elevenlabs/connect` does not consult a session permission
     lock. It has no session id and mirrors the sibling MCP routes
     (`POST /api/mcp/{name}/auth`, enable/disable), which also write MCP config
     without a lock; the `locked` mode governs tool execution (MCP Apps views
     refuse it), not MCP configuration writes.
2. Refuses a server provided by a plugin (400).
3. Always writes the canonical preset, merged with the user-owned fields:
   `mergeElevenLabsPreset` keeps an existing `timeout`, `headers` and
   `environment`, and takes the URL, `type: "http"` and the OAuth block from the
   preset. A placeholder or SSE-era entry is therefore replaced on connect
   (`SaveMCPServer`), not preserved.
4. Starts the normal OAuth job and replies `202` with a job id; `GET
   /api/mcp/auth/{id}` carries the outcome.

Settings → MCP Servers has an ElevenLabs card with a Connect/Reconnect button.
It polls the job status (130 s cap), then reloads MCP so the tools appear.

## Invariants

- Connect is the canonical installer for the entry, so the preset's URL,
  transport and OAuth identity always win; only `timeout`/`headers`/
  `environment` survive a merge. It persists with `SaveMCPServer` and is
  mirrored into the live config, which can hold the merged entry.
- Plugin-provided servers are refused. The generic `/api/mcp/{name}/auth`
  accepts only `""`/`remote` (`handler_mcp_auth.go:198`), so type `http` uses
  this route.

## Open gaps

No real ElevenLabs sign-in has been completed: the transport, handshake and
preset were verified against published metadata, unit tests and the Settings
test only. The scope list and `tools/list` output stay unconfirmed until the
first live connect (`TODO.md`).
