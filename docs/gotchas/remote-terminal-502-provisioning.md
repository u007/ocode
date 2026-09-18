---
type: Gotcha
title: Remote SSH terminal 502s from provisioning and path bugs
description: 'Three reproduced root causes of remote SSH terminal 502s: binary not activated after upload, SSH launch command not detached, and tilde project paths not expanded on the host for terminal endpoints. Plus a bonus stale-registration fix.'
tags:
  - remote
  - ssh
  - "502"
  - provisioning
  - terminal
  - gotcha
timestamp: 2026-09-18T12:36:30Z
---
Three independent root causes that each produce a 502 on the remote SSH terminal flow, all fixed in the same session. All reproduced against `james@217.216.72.49`, project `~/www/aimsai2`.

## 1. `ensureBinary` uploaded but never activated

**Symptom:** 502 at stage `remote-connect`. Server log shows `exit 127: bash: line 1: /home/<user>/.ocode/bin/<ver>/ocode: No such file or directory`.

**Root cause:** `RemoteWorkspace.ensureBinary` (internal/remote/workspace.go) called `UploadBinary` only. The uploaded binary landed at `~/.ocode/bin/<ver>/.ocode.partial` — never chmod'd or moved to `~/.ocode/bin/<ver>/ocode`. The subsequent credential sync invoked `~/.ocode/bin/<ver>/ocode remote-receive-config`, hit exit 127 on the missing binary, and `Connect` returned the error → 502.

**Fix:** `ensureBinary` now delegates to `EnsureBinary` (provision.go), which does upload → `ActivateAndVerify` (chmod + mv + `--version` probe). A `prepareLocalBuildFn` seam was added for tests.

**Regression:** `TestEnsureBinaryActivatesUploadedBinary` in `internal/remote/workspace_test.go` — verified fails against the old body.

**Captured 502 body:**
```
{"error":"remote connect failed: ... exit 127: bash: line 1: /home/james/.ocode/bin/0.8.101/ocode: No such file or directory\n","stage":"remote-connect"}
```

---

## 2. Fresh-server launch hangs the SSH channel → 502 after connect backstop

**Symptom:** 502 at stage `remote-connect` after ~10 minutes (the `remoteConnectTimeout` backstop). The SSH exec call never returns.

**Root cause:** `launchServerCmd` (internal/remote/serve.go) built:
```bash
rm -f STATE; mkdir -p DIR && nohup BIN serve --remote ... & disown; echo launched
```
The `&` binds to the **entire** `mkdir && nohup` compound command. The forked subshell waits on the long-lived server process before exiting, keeping the SSH channel open. `StartFreshServer`'s `t.Exec(launchServerCmd(...))` blocks until the server dies → timeout → 502.

**Proof:** A trivial probe `ssh host 'nohup sleep 20 & disown; echo launched'` took 20.6 s wall time. The same command with `;` (detached subshell) returned in ~0.5 s.

**Fix:** Wrap the launch in a detached subshell:
```bash
rm -f STATE; mkdir -p DIR && (nohup BIN ... </dev/null >LOG 2>&1 &); echo launched
```
Returns immediately; the server survives. Validated live: fresh `EnsureRemoteServer` launch returns in 2–3.7 s.

**Regression:** `TestLaunchServerCmdDetachesServerInSubshell` in `internal/remote/serve_test.go`.

---

## 3. Host 403s for `~` project paths (terminal socket/history/list)

**Symptom:** Connect succeeds, but terminal HISTORY/LIST both return 403 `{"error":"project is not a project registered with this server"}`.

**Root cause:** Remote projects are saved verbatim as `~/www/app`. The SPA sends `project_path=~/www/app` through the reverse proxy. `projects.Add` on the host expands to `/home/james/www/app`, but the terminal handlers compared the raw `~` path against allowed roots → 403. The existing claim in `docs/concepts/remote-persistent-sessions-terminals.md` ("`~` is expanded by the host via `projects.ExpandHome`") was *not true* for terminal endpoints.

**Fix:** Expand a leading `~` with `projects.ExpandHome` on the **host only**, in:
- `Handler.resolveTerminalHistoryProject`
- `HandleTerminalWS`'s local (`host == ""`) branch

Both in `handler_terminal.go`. Never expand when `host != ""` — that path names another machine's home and is left verbatim.

**Regression:** `TestTerminalHistoryExpandsTildeProjectPath` in `internal/server/terminal_history_test.go` — verified fails with 403 on the old code.

**Live probe:** HISTORY/LIST both 403 before fix; 404/200 after.

---

## Bonus: Registration failure leaves stale `connected` entry → permanent 502

**Symptom:** After any failed `ensureRemoteProject` (dead workspace/tunnel), every later request for that host 502s at stage `remote-register` until the process restarts.

**Root cause:** `HandleRemoteProxy` (internal/server/handler_remote_proxy.go) only dropped the host entry on the reverse proxy's `ErrorHandler`. If `ensureRemoteProject` (which runs first) failed, the entry stayed `connected` and was never cleaned up.

**Fix:** Call `h.remoteHosts.drop(host)` on registration failure so the next request reconnects cleanly.

**Regression:** `TestHandleRemoteProxy_RegistrationFailureThenRetry` in `internal/server/handler_remote_proxy_test.go`.

---

## Cross-references

- `docs/concepts/remote-persistent-sessions-terminals.md` — line 23 claims `~` is expanded by the host; now true for terminal endpoints after root cause #3.
- Project memory entry "Remote SSH connect hangs whole app (2026-09-18, fixed)" — covers the separate `BatchMode`/connect-timeout work that co-existed with these fixes.
- `docs/gotchas/remote-project-path-trust-boundary.md` — related gotcha on remote path handling.
