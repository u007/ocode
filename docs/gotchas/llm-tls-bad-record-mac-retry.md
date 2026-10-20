---
type: Gotcha
title: 'A received TLS alert is not a net.Error: bad record MAC hard-failed an LLM turn'
description: 'Why a corrupted TLS record ("remote error: tls: bad record MAC") ended an LLM turn with no retry, and the narrow text-match classifier that makes it retryable without widening to TLS config failures.'
tags:
  - llm
  - retry
  - tls
  - net-error
  - client
  - tui
timestamp: 2026-10-20T05:00:03Z
---
# A received TLS alert is not a net.Error: bad record MAC hard-failed an LLM turn

## Symptom

```
llm request failed after 1 attempt(s): Post "https://opencode.ai/zen/go/v1/messages": remote error: tls: bad record MAC
```

One corrupted TLS record ended the whole turn with no retry.

## Root cause

Go surfaces a **received** TLS alert (`crypto/tls/conn.go`, the `readRecord` path) as
`&net.OpError{Op: "remote error", Err: alert(desc)}`, where `alert` is crypto/tls's
**unexported** type. It does implement `net.Error`, but `Timeout()` and `Temporary()` are
both `false`, and the error text matched none of `isRetryableLLMClientError`'s arms
(timeout / connection reset / refused / eof / goaway / DNS / HTTP/2 stream error).

Note that `crypto/tls.AlertError` **is** exported (Go 1.26+) but is used for **locally
generated** alerts and QUIC handshake errors — it does not catch a received TCP alert, so
type-matching it alone is insufficient; the classification has to match the error text.

## Why retrying is safe

`bad record MAC` is an AEAD tag mismatch — bytes corrupted in flight (a gateway or
middlebox glitch), not a malformed request. The response is delivered whole or not at
all, and the retry loop's `deltaEmitted` gate still refuses to re-send once partial
content has streamed, so a retry cannot duplicate output.

## Fix

`isTransientTLSRecordError` in `internal/agent/client.go`, called from
`isRetryableLLMClientError` — so both retry loops get it: the main `ChatWithContext` loop
and the auto-permission judge loop via the shared `isRetryableLLMError`. Budget and delay
are unchanged: non-429 path, `llmMaxRetries` = 3 retries → 4 attempts, `llmRetryBaseDelay`
(500 ms) linear backoff.

## Narrowness (load-bearing)

Only the `tls: bad record MAC` text is matched. Permanent TLS configuration failures stay
non-retryable:

- `x509: certificate signed by unknown authority`
- expired certificates
- `tls: first record does not look like a TLS handshake`
- `remote error: tls: handshake failure`
- `protocol version not supported`
- `unknown certificate authority`
- `no application protocol`

Never widen this to a bare `tls:` substring.

## TUI duplicate

`internal/tui/model.go` has a **duplicate** `isRetryableLLMError` used to decide whether
to offer a manual retry after the turn fails; it gained the same narrow arm so the retry
affordance still appears once the client's own attempts are exhausted. The two must be
kept in sync — there is no shared exported predicate.

## Tests

All mutation-verified (deleting the classifier call reproduces `failed after 1 attempt(s)`;
broadening the match to `tls:` fails the negative guards):

- `TestIsRetryableLLMErrorRetriesTLSBadRecordMAC`
- `TestIsRetryableLLMErrorKeepsTLSConfigurationFailuresPermanent`
- `TestChatRetriesTLSBadRecordMAC`
- `TestChatTLSBadRecordMACUsesUsualMaxRetries`
- `TestChatTLSConfigurationFailureDoesNotRetry` — in `internal/agent/client_test.go`
- `TestRetryableLLMErrorDetection` — in `internal/tui/model_test.go`

## How to recognise this class

Any transport error that is **not** `Timeout()`/`Temporary()` and whose text misses the
classifier's arms needs an explicit classifier arm. Verify the exact text shape with a
fixture assertion — the tests assert the fixture reproduces
`remote error: tls: bad record MAC` verbatim.
