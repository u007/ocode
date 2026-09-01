# Part 07 — Real-Chrome integration test, docs, TODO

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Checkbox steps. High-level by policy (no code in plans). TDD per task; commit per task.

**Goal:** Prove the whole path against a real headless Chrome, and bring the docs in line with the shipped behaviour.

**Spec:** `docs/superpowers/specs/2026-08-31-browser-chrome-cdp-design.md` § Testing, § Rollout, § Non-goals.

**Global constraints:** integration test skips unless `OCODE_CHROME_PATH` is set (never fails CI without Chrome); `--no-sandbox` forbidden even in tests; docs are source of truth — every behaviour change from Parts 01–06 must be findable in `docs/`.

## Context an implementer needs

- `internal/browse/cdp` (Parts 02–04): `NewManager(ManagerOptions{ChromePath, IdleTimeout, Supervisor, Dialer, EmitNav, Log})`, `Attach(ctx, key, sink)`, `Target.Navigate`, `FrameSink`.
- `internal/browse` (Part 05): `New(apiToken, logger, Options{ChromePath, IdleTimeout, Supervisor})`, `MintGrant`, `/b/{key}/__cdp`, `NewSafeDialer`.
- `internal/tool.NewProcessSupervisor` (Part 01).
- Docs: `docs/superpowers/specs/2026-08-30-embedded-browser-panel-design.md` (External mode sections: `## Security model` bullets on cookies/UA, `### External mode`, `### SSRF guard (external mode)`, `### Injected capture script`), `docs/index.md` (doc index), `docs/architecture/` (if a browser-panel architecture note exists — grep `browse`), `docs/log.md` (changelog-style log — check its format and add an entry), `TODO.md` (project root; find the existing embedded-browser entries and add beside them).

---

### Task 1: Real-Chrome integration test

**Files:**
- Create: `internal/browse/cdp/integration_test.go`

- [ ] Step 1: Write the test (skips when `OCODE_CHROME_PATH` is empty): start an `httptest` server that serves an HTML page which (i) `console.log("hello-ocode")`, (ii) `fetch("/sub.json")` (served 200), (iii) `fetch("http://10.0.0.1/")` and `new WebSocket("ws://127.0.0.1:1/")` in try/catch. Dialer: the httptest upstream is loopback and must be reachable while `10.0.0.1` and `127.0.0.1:1` must be blocked, so wrap `browse.NewSafeDialer(false)` in a test dialer that allow-lists exactly the upstream's `host:port` and delegates everything else to the safe dialer. Attach a recording sink for key `tab:it`; `Navigate` to the upstream URL. Assert within 15 s: ≥1 `Frame` with `w,h > 0` and a JPEG SOI marker (`0xFF 0xD8`); a `Console{Level:"log"}` containing `hello-ocode`; a `Network` row for `/sub.json` with status 200; a `Network` row for `http://10.0.0.1/` with `Blocked != ""`; a `Console` error or `Network` blocked row for the WebSocket; the nav emitter saw `{Status:200, URL:<upstream>}`. Then `Revoke` + `Close` and assert the temp profile dir is gone and the supervisor snapshot shows the process exited.
- [ ] Step 2: Run `OCODE_CHROME_PATH="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" go test ./internal/browse/cdp/ -run Integration -v` → pass. Run without the env var → `SKIP`.
- [ ] Step 3: Commit `test(browse/cdp): real-chrome integration test`.

---

### Task 2: End-to-end WS smoke through the browse server (gated)

**Files:**
- Modify: `internal/browse/server_test.go` (or new `cdp_e2e_test.go` in package `browse`)

- [ ] Step 1: Write a gated test: `browse.New` with real `Options{ChromePath: env, Supervisor: tool.NewProcessSupervisor(...)}`; mint a grant with SPA origin `http://spa.test`; dial `/b/tab:e2e/__cdp?__grant=…` with `gorilla/websocket` and `Origin: http://spa.test`; send `{"t":"nav","url":"<httptest page>"}`; assert one binary frame with a valid header and one `console` JSON message arrive; close; `Revoke`; server `Close`.
- [ ] Step 2: Run gated → pass; ungated → skip.
- [ ] Step 3: Commit `test(browse): gated end-to-end CDP socket smoke`.

---

### Task 3: Docs

**Files:**
- Modify: `docs/superpowers/specs/2026-08-30-embedded-browser-panel-design.md` — add a banner under the title: *"External mode sections superseded by `2026-08-31-browser-chrome-cdp-design.md` (rev 2)"*, and a one-line note at the top of `### External mode`, `### SSRF guard (external mode)` pointing there. Do not rewrite the old sections.
- Modify: `docs/index.md` — link the new spec and this plan directory.
- Modify: `docs/architecture/` browser note if one exists (else create `docs/architecture/embedded-browser.md` with: mode routing table, process/lifecycle summary, egress proxy diagram in text, WS wire format, config keys, limitations — all copied from the spec, kept ≤ 120 lines).
- Modify: `docs/log.md` — entry dated 2026-08-31: "Embedded browser: external mode now headless Chrome via CDP; proxy removed; loopback blocked from Chrome contexts; Windows unsupported for Chrome mode."
- Modify: the config reference page from Part 01 — confirm `browser.chrome_path`, `browser.idle_timeout_minutes` are documented and note that `browser.extensions` is reserved and rejected.
- Modify: `web/src/components/Browser/` README or the AGENTS.md section on the browser panel if one exists (grep `AGENTS.md` for "browser") — update the mode names.

- [ ] Step 1: Make the edits.
- [ ] Step 2: `grep -rn "proxied" docs/ AGENTS.md README.md` → only inside the superseded sections of the 08-30 spec.
- [ ] Step 3: Commit `docs: chrome-mode browser panel`.

---

### Task 4: TODO.md entries

**Files:** `TODO.md`

Add, next to the existing embedded-browser entries, one bullet each:

- Extensions: config schema `browser.extensions: [{path, enabled}]` → `--load-extension`; note branded Google Chrome dropped `--load-extension` in 2025 (Chromium/Canary/Edge still honour it); no popup/toolbar UI in headless.
- Loopback opt-in from Chrome contexts (OAuth callbacks to `localhost:PORT`): per-stateKey "allow once" prompt or `browser.allow_loopback_ports`; must never be automatic (SSRF, see spec rev 2 review decisions).
- Windows Chrome mode: `--remote-debugging-port=0` + `DevToolsActivePort` file, Origin/Host-gated.
- Canvas text selection / clipboard, file-upload dialogs, downloads, IME composition.
- Headed "attach to my Chrome" mode.

- [ ] Step 1: Add the entries.
- [ ] Step 2: Commit `docs(todo): chrome-mode follow-ups`.

## Verification for the part

- `go test ./...` (minus the documented pre-existing server failure) and `cd web && bunx vitest run && bunx tsc --noEmit -p .` green.
- Gated tests pass locally with `OCODE_CHROME_PATH` set on macOS.
- Manual checklist (record results in the final PR description): `example.com` renders and is interactive; console/network rows appear; `localhost:<vite>` still iframe with HMR; `example.com` → link to a site redirecting to `localhost` shows the "not reachable from Chrome mode — open externally" error; killing the Chrome process (`kill <pid>` from `ps`) shows "chrome exited" and the next navigation relaunches; closing every browser surface and waiting `idle_timeout_minutes` reaps the process.
