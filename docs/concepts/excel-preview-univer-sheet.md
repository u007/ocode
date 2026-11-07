---
type: Concept
title: 'Excel preview and Save: Univer sheet over ExcelJS and SheetJS'
description: 'How the Excel viewer works: .xlsx converted through ExcelJS, csv/tsv/.xls through SheetJS, all shown in a Univer sheet. Uncontrolled .xlsx is editable and Save writes it back through PUT /api/files/raw (values only, one-time .bak); closing a dirty tab asks Save / Discard / Cancel, and dirty tabs arm the desktop quit guard. Covers limits, loud failures, the ExcelJS internals it reads, and the raw save contract (client and server SHA-256 expected_hash). A remote save uses the local ?host= route, never the /api/remote/{host} proxy prefix.'
resource: web/src/components/Preview/ExcelViewer.tsx; web/src/components/Preview/univerMount.ts; web/src/components/Preview/univerSerialize.ts; web/src/components/Preview/univerData.ts; web/src/hooks/useEditorTabs.ts; web/src/components/Files/FileTabContent.tsx; web/src/api/client.ts; internal/server/handler_files.go; internal/server/handler_remote_files.go; internal/server/handler_remote_work.go; internal/server/handler_remote_proxy.go
tags:
  - web
  - preview
  - excel
  - xlsx
  - univer
  - exceljs
  - sheetjs
  - spreadsheet
  - save
  - quit-guard
timestamp: 2026-11-07T02:07:18Z
status: active
---
# Excel preview and Save: Univer sheet over ExcelJS and SheetJS

## Summary

`ExcelViewer` (`web/src/components/Preview/ExcelViewer.tsx`) shows a spreadsheet in an open-source Univer sheet (`@univerjs/presets`, `@univerjs/preset-sheets-core` and `@univerjs/core`, all `^1.1.0`, see `web/package.json`). Univer's xlsx importer is a Pro feature, so ocode converts the workbook itself into Univer's `IWorkbookData` and never gives Univer a file. An uncontrolled `.xlsx` with no capped sheet is editable and can be saved back (see Editing and Save). Every other kind is read-only.

## Pipeline by input

| Input | Parser | Converter |
| --- | --- | --- |
| `.xlsx` (and `.xlsm`, see Known gaps) | ExcelJS `wb.xlsx.load`, dynamically imported so it stays out of the main bundle (`univerData.ts:421`) | `workbookFromExcelJs` (`univerData.ts:300`) |
| `.csv`, `.tsv` | SheetJS on the UTF-8 decoded text (`univerData.ts:416-417`) | `workbookFromSheetJs(wb, true)` (`univerData.ts:329`), first row styled as a header |
| `.xls` | SheetJS, `type: "array"` (`univerData.ts:426`) | `workbookFromSheetJs(wb, false)` |
| split-view `content` (controlled mode) | SheetJS on the live string, via `workbookFromDelimitedText` (`univerData.ts:398`, called from `ExcelViewer.tsx:272`) | `workbookFromSheetJs(wb, true)` |

The file rows are parsed by `workbookFromFileBytes` (`univerData.ts:410`). `ExcelViewer` calls it (`ExcelViewer.tsx:287`), and so does the print document (`preview-print-save-as-pdf.md`), so both show the same cells on disk. Print reads the saved file, not the live grid, so unsaved edits do not appear in print.

Delimited text is decoded as UTF-8 before SheetJS reads it. Raw bytes would be read as latin-1 and garble every non-ASCII cell. The bytes come from `apiClient.fetchFileRaw` (`ExcelViewer.tsx:283`). The server allowlists `.xlsx`, `.xlsm`, `.xls` and `.csv` in `previewRawTypes` (`internal/server/handler_files.go:1224`, entries at `:1228-1231`).

## Runtime (`univerMount.ts`)

- `mountUniverSheet` (`univerMount.ts:61`) is loaded with a dynamic import (`ExcelViewer.tsx:307`), so Univer is a lazy chunk. `PreviewSurface` lazy-loads `ExcelViewer` too (`PreviewSurface.tsx:22`).
- Chrome is off: header, toolbar, formula bar, footer and context menu (`univerMount.ts:81-85`). The workbook is editable only when the viewer asks for it: `workbook.setEditable(editable)` (`univerMount.ts:95`), where `editable` comes from `ExcelViewer` (`ExcelViewer.tsx:305-306`, passed at `:310`).
- Dark mode follows the editor appearance (`ExcelViewer.tsx:100-101`, `:351-354`).
- One Univer instance per converted workbook. A late mount that resolves after cancellation is disposed (`ExcelViewer.tsx:297-349`).
- The sheet tab bar is ocode's own `role="tablist"` (`ExcelViewer.tsx:379`). `activateSheet` throws when Univer has no such sheet (`univerMount.ts:100-103`, the throw at `:102`).
- `activateRange` (`univerMount.ts:106-114`) resolves the A1 address and checks it before any sheet switch, so a bad address leaves the view alone. Univer does not throw on malformed A1; it returns NaN coordinates. `assertRangeInSheet` (`univerMount.ts:169-188`) therefore requires integer row, column, height and width, a height and width of at least 1, and a rectangle inside `getMaxRows()` and `getMaxColumns()`. Anything else throws `invalid range: <a1>`. Whole-column addresses such as `A:A` are not verified in a real browser.
- The handle's interface (`univerMount.ts:30-53`) is `activateSheet`, `activateRange`, `sheetNames`, `activeSheetName`, `selection`, `onSelectionChange`, `setDarkMode`, `snapshot` and `dispose`. The old `rebase` member is gone.

## Limits

- File size: refused before any parser runs at 15 MiB (`MAX_FILE_BYTES`, `univerData.ts:40`; `assertWithinFileLimit`, `univerData.ts:80-85`). ExcelJS and SheetJS both hold the whole file, which is why the check comes first (`univerData.ts:411-412`).
- Per sheet: 50000 rows (`MAX_ROWS`, `univerData.ts:42`) and 200 columns (`MAX_COLS`, `univerData.ts:44`). SheetJS is asked for one row past the cap (`SHEETJS_READ_ROWS`, `univerData.ts:49`) so a longer sheet is detected.
- Truncation is visible. The tab bar says `Showing first 50000 rows … large sheet truncated` (`ExcelViewer.tsx:409-411`). Nothing is cut silently. A capped sheet also makes the workbook read-only (see Editing and Save).

## Failure behaviour

- Loud by design. A cell conversion error is rethrown with the sheet name and address (`atCell`, `univerData.ts:125-131`). The viewer shows `Spreadsheet failed: …` (`ExcelViewer.tsx:368`) and logs through `console.error` with an `ExcelViewer:` prefix.
- Theme: a workbook with no theme part uses the Office 2013+ default palette (`parseThemePalette` and `DEFAULT_THEME_PALETTE`, `xlsxStyle.ts:81-82` and `:40`). This is the approved fallback: the user approved it on 2026-11-03 because it matches Excel (see the comment at `xlsxStyle.ts:79`). A theme part that is present but malformed throws.
- Colours: an unresolvable colour throws (`resolveColor`, `xlsxStyle.ts:118`), and the error names the cell. Automatic colour is black for text on a filled cell and for borders with no colour (`AUTOMATIC_BLACK`, `xlsxStyle.ts:71`, used at `:222` and `:254`). This is Excel's own rule, not a fallback.
- Dates: `excelSerial` (`xlsxStyle.ts:297`) is the exact inverse of ExcelJS's read conversion, in the workbook's own date system (1900 or 1904; `DATE_1904_OFFSET_DAYS` at `xlsxStyle.ts:284`, flag read at `univerData.ts:303`). The Lotus 1900 leap-day bug is deliberately not corrected. ExcelJS does not correct it on read, so correcting it here would move early-1900 dates one serial away from the file.

## ExcelJS internals this viewer reads

Two private properties are read with `Reflect.get`, validated, and throw on a shape change. Neither falls back silently.

- `ws._merges` (`mergeBoundsOf`, `univerData.ts:198-207`): ExcelJS has no public list of merged ranges.
- `wb._themes.theme1` (`themeXmlOf`, `univerData.ts:210-217`): the theme XML that resolves theme colours.

Check both when bumping `exceljs` (`^4.4.0` in `web/package.json`).

## Editing and Save

- **Editable gate.** Editable only when all three hold: uncontrolled (`content` undefined), the path ends in `.xlsx` (case-insensitive), and no sheet is capped (`ExcelViewer.tsx:133-134`). `.xlsm`, CSV/TSV, legacy `.xls` and capped sheets stay read-only. While clean and editable, the bar shows `Editable — edits are saved when you click Save` (`ExcelViewer.tsx:412-414`).
- **What counts as an edit.** Univer runs mutations of its own while mounting, and a dirty flag set by those would show Save on an untouched file. `watchUserEdits` (`univerMount.ts:207-231`) counts a mutation only when the grid has reached `LifecycleStages.Rendered` (`:212`), the mutation runs inside a COMMAND (`commandDepth`, incremented at `:219`, decremented at `:223`), and it is not flagged onlyLocal, fromCollab, fromChangeset or syncOnly (`:227`). Each counted edit calls `onDirty`, which bumps `revision` (`ExcelViewer.tsx:311`). `dirty` is `revision > savedRevision` (`:136`). The Save button shows only when editable and dirty (`ExcelViewer.tsx:397-407`).
- **Save path.** `save` (`ExcelViewer.tsx:144-188`) takes `host.snapshot()`, serializes it with `serializeWorkbookBytes` (`univerSerialize.ts:41`) and PUTs the bytes with `apiClient.saveFileRaw` (`ExcelViewer.tsx:159-163`). `expected_hash` is `baseHashRef.current`, the `rawContentHash` of the bytes the viewer loaded (`:158`; set at `:286`). On success the base hash moves to the `rawContentHash` of the bytes just written (`:164`) and `savedRevision` is set to the revision captured at save start (`:168`). An edit made during the save therefore stays dirty.
- **Hash contract.** `expected_hash` and the server's check compare the same value: lowercase hex SHA-256 of the exact bytes. The client side is `rawContentHash` in `univerSerialize.ts` (`univerSerialize.ts:25`, `@noble/hashes` `^1`, pure JS, so it works on plain-http origins). The server side is `rawContentHash` in `handler_files.go` (`handler_files.go:1709-1711`, `crypto/sha256`). A 409 therefore means the bytes on disk differ from the bytes the viewer last loaded or saved.
- **Failure.** The error shows in a `role="alert"` span (`ExcelViewer.tsx:408`) and is logged with `console.error` (`:178`). `savedRevision` is not advanced, so the workbook stays dirty. A 409 appends `Reload the file and re-apply your edits.` (`:174`).
- **`onSave` override.** A caller can pass `onSave` to replace the built-in request (`ExcelViewer.tsx:152-153`). It goes through the same `savedRevision` and error handling.
- **First-save confirmation.** `requestSave` (`ExcelViewer.tsx:198-209`) saves at once for a file already confirmed this session (`confirmedSaveKeys`, `:20`, keyed by host, root and path at `:142`), so a remount does not ask again. Otherwise it opens a ConfirmDialog, `Save spreadsheet?` (`ExcelViewer.tsx:420-436`), and returns a promise that settles with the user's answer: the save's result after Save, `false` after Cancel. A second request while the dialog is open gets the same promise (`confirmRef`), so every caller sees the real outcome. Unmounting the viewer settles a waiting request with `false` (`:213-219`). The dialog warns that formulas, charts, images and pivot tables may not be preserved and names the `.bak`.
- **Files-tab entry points.** The Files-tab toolbar has its own Save button (`web/src/components/Files/FileTabContent.tsx:293-303`, disabled unless dirty at `:297`). Cmd/Ctrl+S is a window keydown listener (`:127-135`) that acts only while the tab is `props.active` and dirty. Both call the handler the viewer hands up through `onSaveRef` (`ExcelViewer.tsx:232-240`), which is `requestSave`, so the first-save confirmation gates every entry point. `FileTabContent.tsx:326` passes the ref down. An in-flight save is joined, not repeated (`ExcelViewer.tsx:145-146`), so the App's Cmd/Ctrl+S (which also saves the active tab) and the tab's own listener can both reach the viewer on one keypress and send one PUT.
- **What gets written** (`univerSerialize.ts`, values only). Cell values, cell formulas that exist in the Univer snapshot, column widths and merges are written. Styles are not written back. Charts, images, pivots and anything the loader discarded are lost, because the loader kept only display text (see the header comment in `univerSerialize.ts`).

### Close confirm (Save / Discard / Cancel)

- **Dirty check.** `requestCloseTab` (`useEditorTabs.ts:536-547`) looks up the tab and, when `tab.isDirty` is set, opens `ConfirmCloseDialog` through `pendingClose` (`:541`) instead of closing. Excel preview tabs reuse the same dialog and the same `pendingClose` state as Monaco editor tabs; a clean tab closes immediately.
- **Save delegates to the registered handler.** `saveEditorTab` (`useEditorTabs.ts:549-599`) routes every preview-only tab (Excel, PDF, DOCX and the other viewer-only kinds) to the handler registered for its tab id (`:559-566`), not `api.saveFileContent`, because `tab.content` is `""` for a preview-only tab. A dirty tab with no registered handler throws; a clean tab with no handler is a no-op. For Excel the handler is `invokeExcelSave` from `FileTabContent` (`web/src/components/Files/FileTabContent.tsx:105-109`), which calls the viewer's current save through `excelSaveRef` at call time and throws when the viewer is not mounted. The viewer's `requestSave` returns `Promise<boolean>`: `true` when the bytes were written, `false` when the save failed or the first-save confirmation was cancelled. Every entry point shares it, so the close dialog's Save, the toolbar Save button and Cmd/Ctrl+S behave identically. On an unconfirmed file the close dialog's Save waits for the first-save answer; on Save it writes and closes the tab, and on Cancel `saveEditorTab` throws `Save failed or was cancelled`, which the close dialog shows while the tab stays open.
- **Success.** The tab closes. `isDirty` is cleared, `diffVersion` is bumped (the diff and print surfaces re-read), the draft is cleared and the quit guard is disarmed (`useEditorTabs.ts:572-573`).
- **`false` or a thrown error.** `saveEditorTab` throws `Save failed or was cancelled` (`useEditorTabs.ts:566`) and sets `saveError`; `confirmSaveAndClose` catches it and leaves both the tab and the dialog in place (`:608-618`). The guard stays armed, so a later quit still prompts, and the user can retry, Discard or Cancel.
- **Discard** closes without saving (`confirmDiscardAndClose`, `:620-625`) and clears the guard. **Cancel** only clears `pendingClose` and `saveError` (`cancelClose`, `:627-630`); the tab and its grid edits stay.
- **Handler lifecycle.** `FileTabContent` registers `invokeExcelSave` in an effect keyed by the tab id (`FileTabContent.tsx:114-120`), so it registers once per tab, and unregisters it on unmount (`:117-119`), so a stale handler never outlives its tab. `invokeExcelSave` reads `excelSaveRef.current` each time it runs (`:105-108`), so the viewer can replace its save closure on every edit without re-registering.

### Quit guard

- Dirty Excel tabs arm the desktop unsaved-draft quit guard through the existing tab-dirty path. `setTabDirty` (`useEditorTabs.ts:509-526`) calls `noteDraftPersistPending(id, path)` when dirty (`:524`) and `noteDraftPersistSuccess(id)` when clean (`:525`). The state updater returns the previous list when the flag is unchanged; the guard call sits outside it, because React may run an updater twice and the guard is module-level state.
- The guard reconciles from tab dirtiness (`reconcileDraftGuard(editorTabs.filter((t) => t.isDirty))`, `useEditorTabs.ts:367`), so Excel tabs are included without a separate registry: it arms while grid edits live only in memory and clears on save, discard or close.
- `FileTabContent` reports the grid's dirtiness up with `onDirtyChange` (`FileTabContent.tsx:97-100`), so the tab's `isDirty` is the single value both the close confirm and the quit guard read.
- The desktop-shell side (blocking quit and the "Keep editing / Quit anyway" prompt) is unchanged; see [Desktop quit guard for unsaved drafts](../gotchas/desktop-quit-guard-and-sticky-port-fallback.md).

## Raw save endpoint (`PUT /api/files/raw`)

The route is `PUT /api/files/raw` behind `authMiddleware` (`internal/server/server.go:417`). A local project uses `HandleSaveFileRaw` (`internal/server/handler_files.go:1737`). A remote project (`?host=`) uses `remoteSaveFileRaw` (`internal/server/handler_remote_files.go:590`). Both apply the same checks in the same order.

**The client always calls this local route, never the proxy prefix.** `apiClient.saveFileRaw` (`web/src/api/client.ts:3726`) builds `PUT /api/files/raw?path=…&project_root=…&expected_hash=…[&force=1][&host=…]` on its own origin; a remote project's `host` rides in the query (`:3735`), so `HandleSaveFileRaw` dispatches to `remoteSaveFileRaw` on `hostParam(r)` (`handler_files.go:1739-1741`). It must not be routed through `/api/remote/{host}/api/files/raw`. That proxy's admission resolves the project identity from `?project=` or `?path=`, then the `X-Ocode-Project` header, then `?project_path=`, then a POST body `project_path` (`resolveProjectPath`, `internal/server/handler_remote_proxy.go:272`), and it never reads `?project_root=`. For a raw save `?path=` is the FILE and `?project_root=` is the project, so the proxied form resolved the file path as the project and always answered 403 `unknown host or unregistered project path` (`handler_remote_proxy.go:79`). **Rule:** a call routed through `/api/remote/{host}` must carry the project identity in `?project=`, `X-Ocode-Project` or `?project_path=`; an endpoint that already has a local `?host=` remote branch (file read, save and access checks) uses that branch instead of the proxy. `?path=` is ambiguous — it is the project path only on `/api/projects/sessions`.

- **Body.** `application/octet-stream`, or no Content-Type. Any other type is a 400 (`handler_files.go:1751-1758`). The body is read by `readRawSaveBody` (`handler_files.go:1718`) under a 32 MiB cap (`previewRawMaxBytes`, `handler_files.go:1186`). Only an over-cap body is a 413. Any other read failure is a logged 400.
- **Query.** `path`, `expected_hash`, `force` (`1` or `true`, `handler_files.go:1759-1760`). `project_root` is required for remote saves (`handler_remote_files.go:610`).
- **Extension.** Only `.xlsx`, `.docx` and `.pdf` (case-insensitive, `rawSaveAllowedExt` in `internal/server/raw_save_ext.go`), checked on the resolved target path. `.docx` and `.pdf` are there for the in-app Word and PDF editors, per the comment at `handler_files.go:1798-1800`. Anything else is a 400 (`handler_files.go:1801-1804`; remote `handler_remote_files.go:626-628`).
- **Containment.** Local: `writeScopeRoot` picks the root (`handler_files.go:1762`), then the target's parent is resolved through symlinks and must sit inside the real root (`:1787-1797`). Remote: lexical containment by `remoteRelCheck`, then `remoteDirInside` (`handler_remote_work.go:204`, called at `handler_remote_files.go:633`) checks the parent's realpath.
- **Symlink refusal.** A symlinked final component is refused before anything is read or written. Local: `handler_files.go:1817-1829`. Remote: `remoteLstatKind` (`handler_remote_files.go:749`), called at `:659`, refuses at `:666`.
- **Hash.** `expected_hash` is the lowercase hex SHA-256 of the exact stored bytes (`rawContentHash`, `handler_files.go:1709-1711`). A missing target hashes as `__missing__` (`rawMissingHash`, `handler_files.go:1699`). A mismatch is a 409 (`handler_files.go:1847-1856`). The text save's `hashContent` is a different function and is never used for raw saves.
- **Requirement.** When the target exists and `force` is not set, an empty `expected_hash` is a 400 (`handler_files.go:1843-1846`; remote `handler_remote_files.go:682`). A missing target can be created without a hash.
- **Existing bytes are always read.** The `.bak` needs them, so they are read on every save, even with `force=1`. Local: `handler_files.go:1834-1840`. Remote: `handler_remote_files.go:673-676`, where a read failure (including a transport error) is a 502, never treated as a missing file.
- **One-time `.bak`.** Before the rename, the pre-existing bytes are written to `<path>.bak`, but only after every containment and symlink check has passed (local `handler_files.go:1915-1937`). The `.bak` path is Lstat'ed first (`:1920`). A symlink there is a 400 (`:1926-1929`). A stat failure is a logged 500 locally (`:1921-1925`) and a 502 remotely (`handler_remote_files.go:704`). The backup is written only when no `.bak` exists (`:1930`), so an existing `.bak` is never overwritten and holds the bytes from before the first raw save. A backup write failure aborts the save with a logged 500 (`handler_files.go:1931-1935`; remote `handler_remote_files.go:711-718`).
- **Permissions.** Local: the temp file takes the existing file's permission bits, and a new file gets `0600` (`handler_files.go:1897-1909`). Remote: `remoteWriteFileAtomic` copies an existing file's mode onto the temp file with `cp -p`, and a new file keeps `mktemp`'s `0600` (`handler_remote_files.go:735-743`).
- **Write.** Local writes to a temp file in the same directory and renames it over the target (temp at `handler_files.go:1860`, rename at `:1939`), so the write is atomic. The local temp file is fsynced before the rename (`handler_files.go:1874-1877`); the remote helper does not fsync (see TODO.md, review follow-ups round 2). Remote does the same in one remote command (`remoteWriteFileAtomic`, `handler_remote_files.go:735-743`, called at `:720`): `mktemp` makes the temp file in the target's directory, `cp -p` copies an existing target's mode onto it, the bytes stream in through `remoteWriteStdin` (`handler_remote_work.go:907`), a `wc -c` check must equal the byte count, and `mv -f` renames it over the target. Every failure path removes the temp file and leaves the target as it was, so a stdin stream cut short cannot install a truncated file. The one-time `.bak` uses the same helper. `remoteWriteFile` (`handler_remote_work.go:900-902`) still uses `cat >` for the gitignore and text saves; raw saves do not use it.
- **Response.** `{"path": …, "saved": true}`.

## Removed

The old DOM grid's selection toolbar (Copy, Ask LLM) and its column-resize drag are gone. Univer's canvas grid replaces them, so selecting cells does not offer Copy or Ask LLM.

## Known gaps

- **Known data loss: the loader keeps display text only.** `excelJsCell` (`web/src/components/Preview/univerData.ts:280`) stores each cell's formatted string as Univer text and nothing else. On save, numbers, dates and booleans are written back as text (`1234.5` with `#,##0.00` becomes `"1,234.50"`, a date becomes `"2024-01-15"`, `TRUE` becomes `"TRUE"`), and a formula is replaced by its cached result as text. The `KNOWN LOSS` assertions in `web/src/components/Preview/univerSerialize.test.ts` (`:123-129`) pin this. The open work, including the decision on blocking or warning on save for workbooks with formulas, is in [TODO.md](../../TODO.md) under "review follow-ups round 2".
- **Save is values only.** Formulas that the loader discarded, number formats, styles, charts, images and pivot tables are not written back. The first-save dialog is the only warning.
- **Capped sheets are read-only.** A sheet over 50000 rows or 200 columns shows only its first rows and columns, and the workbook cannot be saved while any sheet is capped.
- **Force-save is not offered for preview-only tabs.** `saveEditorTab` ignores `force` on that branch (`useEditorTabs.ts:559-576`), and the viewer always sends its own base hash, so there is no forced-overwrite path. No UI reaches it today; the gap is tracked in `TODO.md`.
- **`.xlsm` reaches the viewer, read-only.** The routing gates that once excluded it are now permissive: `kindByExt` maps `.xlsm` to `excel` (`web/src/lib/previewKind.ts:59`), `previewRawTypes` lists it (`handler_files.go:1229`), and `OOXML_WORKBOOK` accepts it (`univerData.ts:403`). The viewer edits only `.xlsx` (`ExcelViewer.tsx:133-134`), and the raw save refuses it. Whether ExcelJS loads a macro-enabled package was not checked for this page.
- **Horizontal `fill` alignment renders left (approved known gap).** Excel's `fill` repeats the cell's content across the column width. Univer's `HorizontalAlign` enum has no fill member, so `HORIZONTAL_ALIGN` maps `fill` to `LEFT` (`xlsxStyle.ts:165-166`). The user approved this substitution on 2026-11-03; the comment at `xlsxStyle.ts:165` records the approval.

## Tests

- `web/src/components/Preview/univerData.test.ts`, `xlsxStyle.test.ts`, `univerMount.test.ts`, `univerSerialize.test.ts` and `ExcelViewer.test.tsx`. The styled fixture is `web/src/components/Preview/__fixtures__/styled.xlsx`.
- `web/src/api/client.remote.test.ts` (`saves a remote .xlsx through the local ?host= route, never the proxy prefix`) pins the remote save route: the request goes to `/api/files/raw?…&host=…` on this origin and never to `/api/remote/{host}/…`.
- `internal/server/handler_files_raw_test.go` covers the raw save contract: the `.bak` rules, the SHA-256 hash (`TestRawContentHashIsSHA256Hex`), the 409 and force paths, the symlink and traversal refusals, and the remote twin. `TestSaveFileRawRemoteBacksUpOnceAndSaves` pins the remote one-time `.bak`; that the backup is one-time is a product decision recorded in `TODO.md`.
- The `rawContentHash` vectors are mirrored by convention: `univerSerialize.test.ts` (`describe("rawContentHash")`) and `TestRawContentHashIsSHA256Hex` change together, never one alone. `ExcelViewer.test.tsx` asserts that the viewer sends `rawContentHash` of the loaded bytes, then of the saved bytes, as `expectedHash`.
- `web/src/hooks/useEditorTabs.test.ts`, `describe("Excel preview tab close-confirm (t5)")`: `setTabDirty` marks an Excel tab dirty and arms the quit guard, save goes through the registered handler, a `false` handler keeps the tab and dialog open with the guard armed, success closes and disarms, Discard closes without saving, and `reconcileDraftGuard` drops entries for closed Excel tabs.
- `web/src/components/Files/FileTabContent.test.tsx`, `describe("FileTabContent preview-only toolbar (t4)")`: Print plus a disabled-until-dirty Save for `.xlsx`, Save enabling once dirty through the registered handler, Cmd/Ctrl+S while dirty, and the lifecycle case that unregisters the handler on unmount.

## Related

- [Multi-Use Preview design spec](../superpowers/specs/2026-09-10-preview-multipurpose-design.md) (historical draft; its `PreviewSurface` line now names Univer).
- [Files Tab Auto-Previews Binary/Office/Media Formats](../gotchas/files-tab-preview-only-routing.md) (routing and the OS-open fallback).
- [Preview Print / Save as PDF](preview-print-save-as-pdf.md) (the print document reads the same converter).
- [Desktop quit guard for unsaved drafts](../gotchas/desktop-quit-guard-and-sticky-port-fallback.md) (the shell side of the guard).
