# Part 2: Tier-2 Local-Model Scanner

Spec §2 tier-2. Sends **tier-1-masked** text to a verified-local model (LM Studio/Ollama/localhost only) to find contextual secrets. Returns verbatim spans; hallucinated spans dropped. Never falls back to cloud.

Prereq: Part 1 (`Scanner` interface on `Redactor`).

### Task 2.1: Local-endpoint guard

**Files:**
- Create: `internal/redact/scanner.go`
- Test: `internal/redact/scanner_test.go`

- [ ] Write failing tests: `IsLocalEndpoint(baseURL)` true for `http://localhost:1234/v1`, `http://127.0.0.1:11434`, `[::1]`; false for any non-loopback host (including LAN IPs and cloud URLs).
- [ ] Implement via `url.Parse` + `net.ParseIP`/hostname check (`localhost`, loopback IPs only).
- [ ] Test PASS. Commit `feat(redact): loopback-only endpoint guard for security model`.

### Task 2.2: LLM scanner

**Files:**
- Modify: `internal/redact/scanner.go`
- Test: `internal/redact/scanner_test.go` (httptest server faking an OpenAI-compatible `/chat/completions`)

- [ ] Write failing tests: `LLMScanner{BaseURL, Model}.Scan(maskedText)` sends one chat request with a fixed system prompt instructing "return JSON array of exact secret substrings"; constructor (or `Scan`) returns error for non-local BaseURL; spans returned only for substrings present **verbatim** in input (fake model returning a hallucinated string → span dropped); dropped spans logged via `log.Printf` with offsets/kind only — assert the raw secret text never appears in log output (capture `log.SetOutput`); network error returns wrapped error (callers decide fail-mode).
- [ ] Implement using a plain `net/http` client (do NOT reuse `agent.GenericClient` — avoids import cycle and guarantees no cloud routing). Parse model JSON output defensively; on unparseable output return error.
- [ ] Test PASS. Commit `feat(redact): tier-2 local LLM scanner with verbatim-span verification`.

### Task 2.3: Wire into Redactor + fail-mode result

**Files:**
- Modify: `internal/redact/redactor.go`
- Test: `internal/redact/redactor_test.go`

- [ ] Write failing tests: with Scanner set, `RedactChat` runs tier-1 first, passes the **already-masked** text to `Scan`, registers returned spans as `source: "model"`; with Scanner returning error, `RedactChat` returns `(maskedText, ErrScannerUnavailable)` — text is still tier-1-masked, error signals callers to raise the block/prompt modal (TUI handles in Part 4); `RedactFile` never calls Scanner (file-content rule).
- [ ] Implement. Define exported `ErrScannerUnavailable` sentinel (wrapped).
- [ ] `go test -race ./internal/redact/` → PASS. Commit `feat(redact): tier-2 wired, fail-mode surfaced as sentinel error`.
