# Part 10: `/computer` command (TUI + web) and server endpoints

**Files:**
- Modify: `internal/tui/commands.go` (command table around line 175 next to `/ocr`; add `runComputerCmd` near `runOcrCmd` around line 1800)
- Modify: `internal/tui/model.go` (add `handleComputerCmd(args []string) tea.Cmd` next to `handleOcrCmd` around line 9606; add `"/computer"` to the command list around line 8634 where `"/ocr"` appears)
- Create: `internal/tui/computer_cmd_test.go`
- Modify: `internal/server/handler_config.go` (add `HandleGetComputerUseConfig`, `HandleSetComputerUseConfig` next to the OCR handlers around line 645)
- Modify: `internal/server/server.go` (routes next to `/api/config/ocr` around line 382: `GET /api/config/computer-use`, `PUT /api/config/computer-use`; thin `handleGet/SetComputerUseConfig` wrappers near line 2037)
- Create: `internal/server/handler_computeruse_test.go`
- Modify: `web/src/api/types.ts` (`ComputerUseConfig { enabled: boolean }`), `web/src/api/client.ts` (`getComputerUseConfig`, `setComputerUseConfig` next to the OCR client methods around line 1022)
- Modify: `web/src/components/Chat/commands.ts` (register `/computer` in the command list near line 71; dispatch near line 362; `handleComputer` next to `handleOcr` around line 609)
- Create/Modify: the web command test file that covers `/ocr` (grep `handleOcr\|"/ocr"` in `web/src/components/Chat/*.test.ts*`) — add `/computer` cases in the same file.

**Interfaces:**
- Consumes: `config.SaveComputerUseConfig`, `config.DefaultComputerUseConfig` (Part 02).
- Produces: HTTP `GET/PUT /api/config/computer-use` with body `{"enabled": bool}`; TUI and web `/computer [status|enable|disable]`.

## Behaviour

- `enable`/`disable` write `m.config.Ocode.ComputerUse.Enabled`, call `SaveComputerUseConfig`, then `broadcastTUIStatus()`, and reply `Computer use: enabled. Takes effect in new sessions.` (or disabled). Web does the same through the PUT endpoint and replies with the same text.
- `status` prints enabled/disabled, the platform backend name (`macOS: screencapture + CGEvent`, `Windows: PowerShell SendInput`, `Linux: xdotool/scrot` or `ydotool/grim`), and on darwin a reminder line: `Grant Screen Recording and Accessibility to your terminal or ocode-desktop under System Settings → Privacy & Security.` Status text is built by one shared Go function `computer.StatusLines(cfg config.ComputerUseConfig) []string` in `internal/computer/status.go` so TUI and server share it; the web command fetches `GET /api/config/computer-use` which also returns `"status_lines": []string`.
- Verify how the TUI builds tools for a new session (`internal/tui/model.go` around line 2251 calls `tool.InitBuiltinTools(m.lspMgr, m.config, nil)`); confirm a new session picks up the new enabled flag. If it does not, the reply text must say "restart ocode" instead of "new sessions".

## Steps

- [ ] **Step 1: Write failing tests.** TUI: `TestHandleComputerCmd_EnableDisableStatus` following the pattern of the existing `/ocr` TUI test (grep `handleOcrCmd` in `internal/tui/*_test.go`); assert config saved and message text. Server: `TestComputerUseConfigEndpoints` GET default false, PUT true, GET true, disk file contains `computer_use`. Web: `/computer status`, `/computer enable` produce the expected assistant messages with a mocked api.
- [ ] **Step 2: Run** `go test ./internal/tui -run ComputerCmd -v`, `go test ./internal/server -run ComputerUse -v`, and `cd web && pnpm vitest run commands`. Expected: FAIL.
- [ ] **Step 3: Implement** all sites listed above plus `internal/computer/status.go`.
- [ ] **Step 4: Run** the tests, then `go vet ./internal/tui ./internal/server ./internal/computer` and `cd web && pnpm tsc --noEmit && pnpm vitest run commands`. Expected: PASS.
- [ ] **Step 5: Commit** `git add internal/tui/commands.go internal/tui/model.go internal/tui/computer_cmd_test.go internal/server/handler_config.go internal/server/server.go internal/server/handler_computeruse_test.go internal/computer/status.go web/src/api/types.ts web/src/api/client.ts web/src/components/Chat/commands.ts <web test file> && git commit -m "feat: /computer command in TUI and web, computer-use config endpoints"`.
