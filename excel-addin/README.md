# ocode for Excel

A task-pane add-in that puts an ocode chat beside the open workbook. Every conversation is a real
`ocode serve` session (history, recap, compact, stop, permissions all work), and the agent gets Excel tools that
act on the **live** workbook through Office.js, with every write checkpointed and undoable.

Status: **built and tested without Excel; never run inside real Excel.** Start with [RESUME.md](RESUME.md).
Design background and alternatives: [docs/plans/2026-10-30-excel-addin-sidepane.md](../docs/plans/2026-10-30-excel-addin-sidepane.md).

```
 Excel task pane (React + Office.js) --HTTPS, same origin--> bridge (Go) --Bearer--> ocode serve
        ^   runs excel_* tools                                  |  serves the pane, proxies /api,
        |   (ExcelPort -> Office.js)                            |  relays tool calls, keeps the journal
        +---- SSE "call" / POST "result" ------------------------+<--- MCP tools/call (POST /mcp) --- ocode agent
```

| folder | what |
|---|---|
| `bridge/` | Go, stdlib only, own `go.mod`. Static hosting, `/api` proxy (adds the ocode token), tool relay, MCP endpoint, small state store, workspace creation. `tools.json` is the single tool contract. |
| `taskpane/` | TypeScript. `src/tools/` the 13 tools (pure logic), `port.ts` the only Excel surface, `officePort.ts` the Office.js implementation, `fakePort.ts` the in-memory one used by tests, `ui/` the chat pane. |
| `manifest/` | Sideload manifest template; `scripts/gen-manifest.mjs` fills in the URL and key. |

## Install in Excel
Quick path (macOS or Windows): `cd excel-addin && make install-excel`. It installs the trusted dev certificate if missing,
creates a stable pane key and GUID in `~/.config/ocode-excel/`, builds the pane and bridge, generates the manifest and
sideloads it (macOS: copies to Excel's `wef` folder, needs Full Disk Access for the terminal or a Finder copy; Windows:
`office-addin-dev-settings sideload`, untested). Then start the bridge with the same key, add the `excel` MCP entry, quit
and reopen Excel, open Home > Add-ins > Ask ocode. **Re-run it after every `make test`**: `manifest.test.ts` overwrites
`manifest/manifest.xml` with a test key, which makes the pane fail with "The string did not match the expected pattern".

The full ordered setup (certificate, pane key, MCP entry, manifest, sideload, macOS pitfalls) is the `ocode-excel` skill:
`skills/ocode-excel/SKILL.md`. Check it with `scripts/preflight.sh`.

## Try it without Excel
```
make test            # Go (race, 31 tests) + TypeScript (153 tests)
make smoke           # real Chromium against the real bridge and a fake ocode; writes a screenshot
cd taskpane && pnpm dev   # ?fake=1 runs the pane on an in-memory workbook in a normal browser
```

## Tools
`describe_workbook get_selection read_range profile_range check_errors write_range fill_formula format_range
create_table create_chart create_pivot checkpoint undo_step`. ocode prefixes MCP tools with the server name, so
with the server named `excel` the agent sees `excel_read_range` etc. Rules baked into the tools (each has a test):
refuse to overwrite occupied cells or turn a formula into a constant unless told to; read back what Excel
evaluated and report new error cells; reject a chart range that includes a Total row, a header, text, or covers
data; undo refuses (changing nothing) if the user edited those cells since.
