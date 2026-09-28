# 01 — Local cert + dual-protocol HTTP/2 listener

**Goal:** one TCP port serves both plain HTTP/1.1 and TLS with h2 negotiated.

- New package-level helper (internal/server, e.g. `localtls.go`): generate an
  in-memory ECDSA P-256 self-signed cert, SAN = IP 127.0.0.1 (+ ::1,
  `localhost`), short validity, per process launch. Expose DER bytes, SHA-256
  of DER (macOS/Linux pin) and base64 SHA-256 of SPKI (WebView2 flag).
- Sniffing listener wrapper: peek the first byte of each accepted conn; `0x16`
  (TLS handshake record) → wrap in `tls.Server`, otherwise pass through as
  plain. Peek must not block Accept for other conns (do it on the conn's own
  goroutine via a lazily-peeking conn, or a per-conn goroutine feeding an
  accept channel).
- `Server.Serve` gains a TLS-aware variant (or `Server` holds optional
  `*tls.Config`): `tls.Config.NextProtos` must list `h2` and `http/1.1`, and
  `http2.ConfigureServer`/`http.Server.Protocols` must enable HTTP/2 on the TLS
  side — Go's automatic h2 only wires into `srv.TLSConfig` when using
  `ServeTLS`, not a hand-built listener.
- Existing streaming (SSE flushers, gzip Flusher/Hijacker delegation) must work
  under h2. WebSockets (`Upgrade`/Hijack) need HTTP/1.1: do not advertise
  extended CONNECT, so browsers open WS on a separate HTTP/1.1 TLS connection.

**Verify:** Go test — same listener: plain `GET /api/health` over http works;
TLS client with the pinned cert negotiates `h2` (`resp.ProtoMajor == 2`); an
SSE route streams incrementally over h2; a WebSocket upgrade over TLS
(HTTP/1.1) works.
