---
type: Concept
title: 'Excel preview: read-only Univer sheet over ExcelJS and SheetJS'
description: 'How the Excel viewer works after the Univer rewrite: .xlsx converted through ExcelJS, csv/tsv/.xls through SheetJS, both turned into Univer workbook data and shown in a read-only Univer sheet. Covers the file and sheet limits, loud failures, the ExcelJS internals it reads, what was removed, and the .xlsm routing gap.'
resource: web/src/components/Preview/ExcelViewer.tsx; web/src/components/Preview/univerData.ts
tags:
  - web
  - preview
  - excel
  - xlsx
  - univer
  - exceljs
  - sheetjs
  - spreadsheet
timestamp: 2026-11-03T16:00:23Z
status: active
---
# Excel preview: read-only Univer sheet over ExcelJS and SheetJS

## Summary

`ExcelViewer` (`web/src/components/Preview/ExcelViewer.tsx`) shows a spreadsheet read-only. The grid is an open-source Univer sheet (`@univerjs/presets`, `@univerjs/preset-sheets-core` and `@univerjs/core`, all `^1.1.0`, see `web/package.json`). Univer's xlsx importer is a Pro feature, so ocode converts the workbook itself into Univer's `IWorkbookData` and never gives Univer a file.

## Pipeline by input

| Input | Parser | Converter |
| --- | --- | --- |
| `.xlsx` (and `.xlsm`, see the gap below) | ExcelJS `wb.xlsx.load`, dynamically imported so it stays out of the main bundle (`univerData.ts:421`) | `workbookFromExcelJs` (`univerData.ts:300`) |
| `.csv`, `.tsv` | SheetJS on the UTF-8 decoded text (`univerData.ts:416-418`) | `workbookFromSheetJs(wb, true)` (`univerData.ts:329`), first row styled as a header |
| `.xls` | SheetJS, `type: "array"` (`univerData.ts:426`) | `workbookFromSheetJs(wb, false)` |
| split-view `content` (controlled mode) | SheetJS on the live string, via `workbookFromDelimitedText` (`univerData.ts:398`, called from `ExcelViewer.tsx:77`) | `workbookFromSheetJs(wb, true)` |

The file rows are parsed by `workbookFromFileBytes` (`univerData.ts:410`). `ExcelViewer` calls it (`ExcelViewer.tsx:90`), and so does the print document (`preview-print-save-as-pdf.md`), so both show the same cells.

Delimited text is decoded as UTF-8 before SheetJS reads it. Raw bytes would be read as latin-1 and garble every non-ASCII cell. The bytes come from `api.fetchFileRaw` (`ExcelViewer.tsx:88`). The server allowlists `.xlsx`, `.xls` and `.csv` in `previewRawTypes` (`internal/server/handler_files.go:1202`).

## Runtime (`univerMount.ts`)

- `mountUniverSheet` (`univerMount.ts:22`) is loaded with a dynamic import (`ExcelViewer.tsx:105`), so Univer is a lazy chunk. `PreviewSurface` lazy-loads `ExcelViewer` too (`PreviewSurface.tsx:20`).
- Chrome is off: header, toolbar, formula bar, footer and context menu (`univerMount.ts:41-45`). The workbook is locked with `setEditable(false)` (`univerMount.ts:52`).
- Dark mode follows the editor appearance (`ExcelViewer.tsx:44-50`, `:127-129`).
- One Univer instance per converted workbook. A late mount that resolves after cancellation is disposed (`ExcelViewer.tsx:100-124`).
- The sheet tab bar is ocode's own `role="tablist"` (`ExcelViewer.tsx:154-171`). `activateSheet` throws when Univer has no such sheet (`univerMount.ts:55-58`).

## Limits

- File size: refused before any parser runs at 15 MiB (`MAX_FILE_BYTES`, `univerData.ts:40`; `assertWithinFileLimit`, `univerData.ts:80-85`). ExcelJS and SheetJS both hold the whole file, which is why the check comes first (`univerData.ts:411-412`).
- Per sheet: 50000 rows (`MAX_ROWS`, `univerData.ts:42`) and 200 columns (`MAX_COLS`, `univerData.ts:44`). SheetJS is asked for one row past the cap (`SHEETJS_READ_ROWS`, `univerData.ts:49`) so a longer sheet is detected.
- Truncation is visible. The tab bar says `Showing first 50000 rows … large sheet truncated` (`ExcelViewer.tsx:147-150`, `:168-170`). Nothing is cut silently.

## Failure behaviour

- Loud by design. A cell conversion error is rethrown with the sheet name and address (`atCell`, `univerData.ts:125-131`). The viewer shows `Spreadsheet failed: …` (`ExcelViewer.tsx:143`) and logs through `console.error` with an `ExcelViewer:` prefix.
- Theme: a workbook with no theme part uses the Office 2013+ default palette (`parseThemePalette` and `DEFAULT_THEME_PALETTE`, `xlsxStyle.ts:81-82` and `:40`). This is the approved fallback: the user approved it on 2026-11-03 because it matches Excel (see the comment at `xlsxStyle.ts:79`). A theme part that is present but malformed throws.
- Colours: an unresolvable colour throws (`resolveColor`, `xlsxStyle.ts:118`), and the error names the cell. Automatic colour is black for text on a filled cell and for borders with no colour (`AUTOMATIC_BLACK`, `xlsxStyle.ts:71`, used at `:222` and `:254`). This is Excel's own rule, not a fallback.
- Dates: `excelSerial` (`xlsxStyle.ts:297`) is the exact inverse of ExcelJS's read conversion, in the workbook's own date system (1900 or 1904; `DATE_1904_OFFSET_DAYS` at `xlsxStyle.ts:284`, flag read at `univerData.ts:303`). The Lotus 1900 leap-day bug is deliberately not corrected. ExcelJS does not correct it on read, so correcting it here would move early-1900 dates one serial away from the file.

## ExcelJS internals this viewer reads

Two private properties are read with `Reflect.get`, validated, and throw on a shape change. Neither falls back silently.

- `ws._merges` (`mergeBoundsOf`, `univerData.ts:198-207`): ExcelJS has no public list of merged ranges.
- `wb._themes.theme1` (`themeXmlOf`, `univerData.ts:210-217`): the theme XML that resolves theme colours.

Check both when bumping `exceljs` (`^4.4.0` in `web/package.json`).

## Removed

The old DOM grid's selection toolbar (Copy, Ask LLM) and its column-resize drag are gone. Univer's canvas grid replaces them, so selecting cells does not offer Copy or Ask LLM.

## Known gaps

- **`.xlsm` never reaches this viewer.** `OOXML_WORKBOOK` (`univerData.ts:403`) accepts `.xlsm`, but three routing gates reject it first: `NON_PREVIEWABLE_EXTS` in `web/src/lib/previewKind.ts:127` (so `previewKindForPath` returns `null`), `previewRawTypes` in `internal/server/handler_files.go:1202` (no `.xlsm` entry, so the raw fetch is refused), and the opaque-binary list in `internal/tool/preview.go:108`. Fix all three together. Until then `.xlsm` files take the OS-open fallback.
- Read-only is a view lock (`setEditable(false)`). Nothing is written back from the converted copy.
- A sheet over 50000 rows or 200 columns shows only its first rows and columns.
- **Horizontal `fill` alignment renders left (approved known gap).** Excel's `fill` repeats the cell's content across the column width. Univer's `HorizontalAlign` enum has no fill member, so `HORIZONTAL_ALIGN` maps `fill` to `LEFT` (`xlsxStyle.ts:165-166`). The user approved this substitution on 2026-11-03; the comment at `xlsxStyle.ts:165` records the approval.

## Tests

- `web/src/components/Preview/univerData.test.ts`, `xlsxStyle.test.ts` and `ExcelViewer.test.tsx`. The styled fixture is `web/src/components/Preview/__fixtures__/styled.xlsx`.

## Related

- [Multi-Use Preview design spec](../superpowers/specs/2026-09-10-preview-multipurpose-design.md) (historical draft; its `PreviewSurface` line now names Univer).
- [Files Tab Auto-Previews Binary/Office/Media Formats](../gotchas/files-tab-preview-only-routing.md) (routing and the OS-open fallback).
