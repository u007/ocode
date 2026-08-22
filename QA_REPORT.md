# Desktop App Web QA — Chat Session

**Date:** 2026-08-22  
**Target:** ocode desktop (Wails wrapper) + embedded web SPA + Go server  
**Server under test:** `./bin/ocode serve -port 34568 -host 127.0.0.1` (same `internal/server` + `web.FS()` as desktop `internal/desktop.StartServer`; SPA `web/dist` built 2026-08-21)  
**Model:** `opencode-go/muse-spark-1.2-contributor`  
**Session ID:** `ses_2026-08-22-181125-25ec571a` (`Bash Echo Command Test`)

## Checklist

| # | Requirement | Status | Evidence |
|---|-------------|--------|----------|
| 1 | Chat session works | PASS | Session created via UI New, 12 messages, title auto-generated |
| 2 | Triggers bash tool | PASS | UI: bash {"command":"echo hello-bash-qa-12345"} + output hello-bash-qa-12345; API: tool_calls [bash] |
| 3 | Triggers global tool (glob) | PASS | UI: glob {"pattern":"**/*.md"} -> 102 results, 5 shown; API: tool_calls [glob] |
| 4 | Still works with additional msg | PASS | POST /api/sessions/{id}/message -> 200 {"content":"additional-msg-works-123"}; transcript retains history; input enabled |

## API Verification
```
GET /api/sessions/ses_2026-08-22-181125-25ec571a -> 12 messages: user, assistant[bash], tool, assistant, user, assistant[glob], tool, assistant, user, assistant "additional-msg-works-123", user, assistant "additional-msg-works-123"
```

## Findings
- CRITICAL/HIGH: None
- MEDIUM: YOLO checkbox not toggling via agent-browser click (use eval/API instead)
- LOW: tiny final screenshot due to headless window

## Verdict
PASS — all requirements satisfied.

## Addendum — Build/Test Verification (2026-08-22 21:18, advisor checkpoint)

**Compiler checks:** `go vet ./internal/server` → exit 0, `go vet ./internal/agent` → exit 0, `go build ./...` → exit 0 (benign ld warnings: `-lobjc` duplicate, macOS 26.0 vs 11.0 object files, expected for `cmd/ocode-desktop` CGO), `go test ./internal/server -run TestChatSessionsRunConcurrently` → exit 0 (`ok 1.125s`).

**Scratch file:** `/tmp/qa_bash_glob_test.go` (11 KB `seqFakeClient` harness, `//go:build ignore`) was temporary for fake-LLM chat plumbing and has been removed; not part of repo, no test output needed beyond manual browser/API QA above. No `/tmp` file is tracked.

**Servers:** QA servers on 34567/34568 (PIDs 41787, 33980) killed via `pkill -f "ocode serve"`; verified no `ocode serve` processes remain.

**Known limitations (separate from PASS verdict):** YOLO checkbox not toggleable via `agent-browser click` (use `eval`/API), final screenshot 3.7 KB due to backgrounded window (use `qa-02-after-bash.png` 145 KB for visual), desktop Wails chrome (dock/tray/menu) not exercised — server/chat path identical to desktop.

