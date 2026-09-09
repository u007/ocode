---
type: Guide
title: Terminal History Persistence and Restore
description: Accurate guide to ocode's terminal history persistence and restore mechanism, aligning with source implementation
tags: [terminal, history, persistence, restore]
timestamp: 2026-09-08T05:43:02Z
resource: internal/server/terminal_history.go; internal/server/server.go; web/src/components/Terminal/terminalHistory.ts; TerminalPanel.tsx
---
# Terminal History Persistence and Restore

This guide describes the terminal history persistence and restore behavior implemented in ocode.

## Disk history

Named terminals write raw PTY bytes to append-only project-scoped logs:

```
<OcodeGlobalDataDir>/project/<ProjectSlug(project)>/terminal/<sha256-terminal-id>.log
```

The terminal ID is represented by its SHA-256 hexadecimal digest, not used directly as a path component. Anonymous terminals have no persistent history. PTY chunks are written before the in-memory 256 KiB WebSocket replay buffer, so the replay cap does not truncate the durable record.

Writes are not synchronized to disk after every chunk. `terminalHistory.close()` calls `file.Sync()` and closes the file when the shell exits. The closed log remains readable after session exit and server restart. Explicit terminal deletion removes the log.

## History API

The only history endpoint is:

```
GET /api/terminal/{id}/history
```

The route is protected by the server authentication middleware and terminal access policy. Local requests provide `project`; remote requests provide `host` and `project_path`. The project must be a registered local root or a registered remote host/path pair. An active terminal ID must belong to the requested project.

Query parameters:

- `offset` — starting byte offset; defaults to `0`.
- `limit` — requested byte-page size; defaults to 64 KiB and must not exceed 256 KiB (larger values return 400).
- `snapshot_end` — optional pinned upper bound from the first page. Later requests send it to keep the same append-only snapshot. It does **not** mean “start after the previous cursor”; `offset` is the page start.

Successful responses contain:

```json
{
  "id": "terminal-id",
  "offset": 0,
  "next_offset": 65536,
  "snapshot_end": 182734,
  "eof": false,
  "data": "base64-encoded raw PTY bytes",
  "state": "active"
}
```

`data` is base64 because PTY output can contain arbitrary control bytes. Missing history returns `404`; a requested offset past the snapshot returns `416`; active project ownership conflicts and snapshot changes return `409`; invalid or unauthorized project access returns `403`. There is no `/api/terminal-history` endpoint, no HMAC cursor format, no entry array response, and no terminal-history clear endpoint.

## REST-to-WebSocket handoff

The frontend fetches pages from oldest to newest before opening the terminal WebSocket. After the final page, it connects with:

```
history_offset=<snapshot_end>
```

For an active session, the server validates the offset, acquires the terminal session mutex, and replays only bytes appended after that offset before allowing live delivery to continue. This prevents a gap or duplicate between the REST snapshot and live output. A WebSocket attach without `history_offset` retains the existing capped in-memory replay behavior.

The REST and WebSocket paths share `TextDecoder` state so a multi-byte UTF-8 sequence split at the snapshot boundary is decoded correctly.

## Frontend restore behavior

`web/src/components/Terminal/terminalHistory.ts` validates response identity, offsets, snapshot stability, EOF metadata, base64, and byte progression. `TerminalPanel.tsx` aborts restore on unmount and guards writes after cancellation.

The first-page history `404` is the only LocalStorage fallback. The bounded serialized xterm buffer is implemented in `web/src/components/Terminal/terminalPersistence.ts` with the `ocode.term.buf.` key prefix plus terminal ID; it has no expiration-timestamp protocol. The fallback is visibly marked. Other restore failures are shown and do not silently substitute truncated LocalStorage content.

History pages are loaded sequentially during restore, then the complete restored data is available through normal xterm scrolling. This is bounded network paging during initialization, not lazy fetch-on-user-scroll.

Xterm scrollback capacity grows per restored page using a conservative row estimate and is trimmed after the asynchronous xterm write callback, preserving rendered rows without allocating based on the total history byte count.

## Validation

Focused validation covers:

- Reopen-after-close and range boundaries.
- Snapshot pinning while the log grows.
- Project ownership, authentication, access policy, and path safety.
- REST page ordering, empty history, split UTF-8, malformed metadata/base64, 404 fallback, and abort cancellation.
- WebSocket history cursor construction and session handoff behavior.

The focused Go history/session race tests, `go vet ./internal/server`, terminal Vitest tests, and Vite production build pass. The full server suite still contains unrelated provider/bootstrap/session and environment-dependent PTY failures. Repository TypeScript checking remains blocked by the unrelated unused `editorMountVersion` state in `web/src/components/Files/FileEditor.tsx:143`.
