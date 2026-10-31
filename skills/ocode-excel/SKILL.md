---
name: ocode-excel
description: Set up and verify the ocode Excel add-in (task pane + local bridge + excel_* tools) so an agent can work in a live workbook, including the macOS permission and sideload pitfalls. Use whenever excel_* tools are missing, fail with "no Excel task pane is connected", or the user asks to install, run or test the Excel plugin.
when_to_use: When an excel_* tool errors or is not visible; when asked to install, start, sideload, debug or evaluate the Excel add-in or to run an Excel task "through the plugin"; before any claim that the add-in is working. Do NOT fall back to openpyxl or editing the .xlsx on disk while the workbook is open.
---

# ocode Excel add-in: setup checklist

Code and rationale: `excel-addin/` (`RESUME.md`, `README.md`). Verify at any time with
`excel-addin/scripts/preflight.sh`: it prints PASS/FAIL per step with the fix. Run it first, fix the first FAIL, run it
again. Never say the add-in works until it ends with "a task pane is connected".

**Rules.** The `excel_*` tools only work through a connected pane, so do not substitute python/openpyxl or edit the
file on disk. Some steps need the user (marked USER: admin password, macOS privacy grants, restarting Excel); say so
plainly and give the exact command, with the `! ` prefix so it runs in the session. Pin tool majors (`@3`), never
`latest`.

## Quick path
`cd excel-addin && make install-excel` (macOS and Windows) does steps 3, 4 (stored in `~/.config/ocode-excel/`), 2 (build),
6 and 7 in one go; you still do 5 (bridge + MCP entry) and 8 (restart Excel). Re-run it after every `make test`, which
overwrites `manifest/manifest.xml` with a test key. The Windows branch is untested.

## Steps (in order)

1. **Prerequisites.** Microsoft 365 Excel (Windows or Mac), Go >= 1.26, Node >= 22, pnpm, **Bun** (the taskpane `build` runs `bun check .`), the `ocode` binary.
2. **Build and test.** `cd excel-addin && make test build` (Go race tests + TypeScript tests; builds `bin/excel-bridge`
   and `taskpane/dist`). Must be green.
3. **HTTPS certificate (USER).** Office refuses an http task pane. `! npx office-addin-dev-certs@3 install` installs a
   trusted root certificate (asks for the admin password) and writes `~/.office-addin-dev-certs/localhost.{crt,key}`.
4. **Pane key.** One value shared by the bridge, the manifest and the ocode MCP entry (16+ chars of `[A-Za-z0-9_-]`):
   `export EXCEL_BRIDGE_PANE_KEY=$(node -e "console.log(require('crypto').randomBytes(24).toString('hex'))")`.
   `make install-excel` instead generates and stores a stable key plus GUID under `~/.config/ocode-excel/` (0600,
   directory 0700). That location is deliberate: the add-in is a separate tool, so its state is NOT under ocode's
   `GlobalDataDir()`.
5. **Start ocode and the bridge.**
   - `ocode serve -host 127.0.0.1 -port 4096` (add `-port 4097` plus a separate `HOME` to test without touching the
     user's config).
   - Register the tools in the ocode config used by that server (`opencode.json`, `mcp` map), server name MUST be
     `excel`: `{"type":"remote","url":"https://localhost:3443/mcp","headers":{"X-Bridge-Key":"<key>"},"timeout":120000,"enabled":true}`.
     The default 20 s timeout is too short for big reads.
   - `cd excel-addin && ./bin/excel-bridge -static taskpane/dist -ocode http://127.0.0.1:<ocode port> -cert ~/.office-addin-dev-certs/localhost.crt -key ~/.office-addin-dev-certs/localhost.key`
6. **Manifest.** `node scripts/gen-manifest.mjs --base https://localhost:3443` (uses the pane key), then validate with
   Microsoft's tool: `npx office-addin-manifest@3 validate manifest/manifest.xml` must say "The manifest is valid".
   `<Version>` must be >= 1.0.0.0 (0.x is rejected).
7. **Sideload (USER).**
   - **Grant ocode permission first (Settings → Paths & Uploads).** The "Microsoft Office add-in folders" list has a
     checkbox per app (Excel/Word/PowerPoint/Outlook/OneNote on macOS; classic Add-ins/Word startup on Windows). Tick
     Excel and Save; that appends the folder to `extra_allowed_paths`, which is ocode's own write permission. It does
     NOT bypass macOS TCC: the process still needs Full Disk Access (below). The list applies to the machine running
     ocode only — for a remote project, open Settings on that host.
   - **macOS:** copy `manifest/manifest.xml` to `~/Library/Containers/com.microsoft.Excel/Data/Documents/wef/ocode-excel.xml`.
     macOS blocks terminals (and Finder scripts) from writing into another app's container with
     "Operation not permitted". The user must grant the terminal app **Full Disk Access** (System Settings > Privacy &
     Security), quit and reopen it, then run the copy; or copy the file in Finder by hand. Alternative with no file
     access: Excel on the web, Insert > Add-ins > Upload My Add-in.
     Finder: COPY the file (hold Option while dragging, or Edit > Copy then Paste Item), never drag it: a plain drag
     MOVES `manifest/` out of the repo, and cleaning up `wef` then deletes the tracked template. `wef` must hold only
     `ocode-excel.xml` (Excel reads every `.xml` there, so a second manifest shows the add-in twice, and the template
     has unfilled placeholders). If the repo copy is
     lost, restore with `git show HEAD:excel-addin/manifest/<file> > ...`, reapply `<Version>1.0.0.0`, regenerate.
   - **Windows:** shared-folder catalog (Microsoft's guide: "create a network shared folder catalog").
8. **Restart Excel (USER).** Quit and reopen, open a workbook, Home > Add-ins > **Ask ocode**. Both status dots green.
9. **Verify.** `excel-addin/scripts/preflight.sh` (export `EXCEL_BRIDGE_PANE_KEY` for the `/mcp` check).

## Known failure modes

| symptom | cause | fix |
|---|---|---|
| tool error "no Excel task pane is connected" | Excel closed, pane not opened, or step 7/8 not done | run preflight, follow its FAIL line |
| `curl` / ocode rejects the bridge certificate | dev root cert not trusted | step 3 again |
| add-in missing from Home > Add-ins | manifest not sideloaded or Excel not restarted | steps 7 and 8 |
| manifest "version too low" | `<Version>` below 1.0 | step 6 |
| `excel_*` tools not visible on a cold turn | ocode discovery gates MCP tools (open item U7 in `RESUME.md`) | call `discover_more` |
| pane says "ocode could not start: history.replaceState is not a function" | Excel's webview nulls `history.replaceState`; `boot.ts` called it | fixed (guarded); rebuild `make build`, reopen the pane |
| pane says "ocode could not start: The string did not match the expected pattern" (or "the bridge rejected this pane (HTTP 401)") | the sideloaded `manifest.xml` carries the wrong pane key: `make test` (`manifest.test.ts`) overwrites `manifest/manifest.xml` with the test key `abcdef0123456789abcdef` | regenerate with the real key (step 6) AFTER the last `make test`, check `grep -o 'key=[A-Za-z0-9_-]*' manifest/manifest.xml` equals the bridge key, COPY it to `wef` again (step 7), restart Excel |
| recalculation hangs for the xlsx benchmark checker | separate: sandboxed Excel file-access prompt | see `docs/gotchas/xlsx-probe-excel-recalc-backend.md` |

Record what you verified in a new dated section at the end of `excel-addin/RESUME.md`.
