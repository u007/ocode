---
type: Concept
title: In-app Word (.docx) and PDF editors
description: 'Editable .docx and .pdf preview tabs: PDF page operations (rotate/delete/move/merge) through pdf-lib, Word through a JSZip/DOMParser importer that preserves paragraphs, paragraph alignment (left/center/right/justified) plus bold/italic/underline inline runs, a Univer Docs mount and an exporter, both saving through PUT /api/files/raw with a SHA-256 expected_hash and a one-time .bak. Covers the server allowlist (rawSaveAllowedExt), the PreviewSurface/FileTabContent wiring (editable flag, Save button, Cmd/Ctrl+S, dirty tracking, close-confirm handler registration), the Word first-save warning, and the fidelity limits that remain open.'
resource: internal/server/raw_save_ext.go; internal/server/handler_files.go; internal/server/handler_remote_files.go; web/src/components/Preview/PdfEditor.tsx; web/src/components/Preview/DocxEditor.tsx; web/src/components/Preview/docxData.ts; web/src/components/Preview/univerDocMount.ts; web/src/components/Preview/PreviewSurface.tsx; web/src/components/Files/FileTabContent.tsx; web/src/api/client.ts
tags:
  - web
  - preview
  - docx
  - word
  - pdf
  - editor
  - univer
  - pdf-lib
  - jszip
  - save
  - raw-save
  - backup
  - fidelity
  - alignment
  - close-confirm
timestamp: 2026-11-07T04:46:31Z
---
# In-app Word (.docx) and PDF editors

## Summary

Opening a `.docx` or `.pdf` in a Files tab used to be preview-only. Both kinds now render an **editable** viewer when the host asks for it:

- **PDF** — `PdfEditor` (`web/src/components/Preview/PdfEditor.tsx`) does page operations (rotate, delete, move, merge) with `pdf-lib`, then re-serialises the whole document on save.
- **Word** — `DocxEditor` (`web/src/components/Preview/DocxEditor.tsx`) reads paragraphs, paragraph alignment and inline bold/italic/underline runs out of `word/document.xml`, shows them in a Univer Docs mount, and writes a **new** minimal `.docx` on save.

Neither editor is a fidelity-preserving Office editor. PDF has no annotation or text-edit layer; Word keeps paragraphs, paragraph alignment and bold/italic/underline run formatting only. The one-time `.bak` written by the raw save is the recovery path for both.

The raw-save contract itself (hash semantics, `force`, 413 cap, atomic write, remote transport) is shared with editable Excel and is documented in `concepts/excel-preview-univer-sheet.md`; this page covers the parts specific to these two editors plus the two new writable extensions.

## Backend: the raw-save allowlist

`rawSaveAllowedExt` (`internal/server/raw_save_ext.go:8-14`) is the single allowlist for `PUT /api/files/raw`:

| Extension | Writable | Editor |
| --- | --- | --- |
| `.xlsx` | yes | `ExcelViewer` (see the Excel concept page) |
| `.docx` | yes | `DocxEditor` |
| `.pdf` | yes | `PdfEditor` |
| `.xls`, `.xlsm`, `.csv`, `.doc`, `.ppt`, `.pptx` | **no** | preview / legacy OS-open only |

The check runs on the **resolved** target path, after symlink and containment checks (`internal/server/handler_files.go:1801`; remote twin at `internal/server/handler_remote_files.go:626`). Both the local handler and the remote branch call the same helper, so the two gates cannot drift.

Save-time guarantees that the editors depend on (local anchors, remote mirrors them):

- **`expected_hash` is mandatory when overwriting** (`handler_files.go:1841-1844`, `handler_remote_files.go:679-682`) unless `force=1`. The hash is lowercase hex SHA-256 of the exact stored bytes, produced by `rawContentHash` (`handler_files.go:1709-1712`) — deliberately **not** the text save's rune-based `hashContent`, because invalid UTF-8 collisions there would let a stale save pass.
- **A missing target uses the sentinel `__missing__`** (`rawMissingHash`, `handler_files.go:1697-1699`). The editors only open existing files, so this is the create-from-scratch path, not an editor path.
- **A one-time `.bak` is written before the overwrite** (`handler_files.go:1915-1935`, remote `handler_remote_files.go:696-700`). It is written *last*, after every containment/symlink check passes, and only when no `.bak` already exists. A backup that cannot be made **aborts the save** — the target is never replaced unbacked. Consequence: after several saves the `.bak` still holds the file from before the *first* save, not the last good version.
- Local writes are temp file + chmod + fsync + rename (mode preserved, new files `0600`); the remote branch uses `remoteWriteFileAtomic`. Symlinked targets and symlinked backups are refused.

## PDF editor (`PdfEditor.tsx`)

### Load

Both editors read through the same binary endpoint, with `?host=` for a remote project (`PdfEditor.tsx:48-52`):

```ts
fetch(`/api/files/raw?path=…&project_root=…&host=…`)
```

`PdfEditor` loads the bytes into `PDFDocument.load` (`PdfEditor.tsx:57`), records `sha256Hex(bytes)` in `originalHashRef` (`:61`), and separately renders thumbnails with **pdf.js** (`:74-87`) — pdf-lib is the editor, pdf.js is only the renderer. The editor is therefore one document object plus canvases, and every operation mutates the pdf-lib document.

### Page operations

All operations call `markDirty()` (`PdfEditor.tsx:92-95`), which sets local `dirty` **and** notifies the host through `onDirtyChange`:

| Operation | Implementation | Anchor |
| --- | --- | --- |
| Rotate 90° | `page.setRotation(degrees((angle + deg) % 360))` | `PdfEditor.tsx:97-104` |
| Delete page | `doc.removePage(idx)`; refused when only one page remains | `PdfEditor.tsx:106-113` |
| Move up / down | remove at `idx`, `insertPage(target, page)` | `PdfEditor.tsx:115-126` |
| Merge PDF… | file input → `PDFDocument.load` → `doc.copyPages(src, …)` → `addPage` | `PdfEditor.tsx:128-149` |

There is no annotation layer and no text editing; the toolbar is exactly those page operations plus Save.

### Save

`save` (`PdfEditor.tsx:151-168`) serialises the whole document (`doc.save()`), hashes the **new** bytes, and PUTs them with the hash of the **bytes that were loaded**:

```ts
apiClient.saveFileRaw(path, u8.buffer, {
  projectRoot, host: projectHost, expectedHash: originalHashRef.current,
})
```

On success it replaces `originalHashRef` with the new hash, clears `dirty`, and reports clean. That reassignment is what makes a second save in the same tab work: without it every subsequent save would 409 as "changed on disk". A mismatch therefore means a third party changed the file, not that this tab saved earlier.

Because page operations require a full rewrite, `doc.save()` re-serialises every object rather than appending an incremental update.

## Word editor (`DocxEditor.tsx`)

### Import (paragraphs, alignment and inline runs)

`docxToUniver` (`web/src/components/Preview/docxData.ts:24-87`) unzips the package with JSZip, parses `word/document.xml` with `DOMParser`, and walks every `w:p` under the WordprocessingML namespace. For each paragraph it walks the `w:r` runs, concatenates each run's `w:t` text, appends `\r\n`, and builds a flat Univer `IDocumentData` (`dataStream` + one paragraph entry per `w:p`).

Paragraph alignment is read from each `w:p`'s `w:pPr` (`docxData.ts:61-77`): the importer takes `w:jc/@w:val` and maps `center` → 2, `right` → 3, `both`/`justify` → 4 and `left` → 1 onto `paragraph.paragraphStyle.horizontalAlign` (Univer's `HorizontalAlign`: LEFT 1, CENTER 2, RIGHT 3, JUSTIFIED 4). Word's `both` therefore lands on `JUSTIFIED` (4), not Univer's separate `BOTH` (5) value. A paragraph with no `w:jc` gets no `paragraphStyle` at all, so its alignment stays unspecified.

Inline formatting is read from each run's `w:rPr` child (`docxData.ts:43-49`): `<w:b/>` sets `bl`, `<w:i/>` sets `it`, and any `<w:u/>` (regardless of `w:val`) sets `ul`. When a run carries at least one flag, the importer records a `textRuns` entry `{ st, ed, ts }` (`:34`, `:53`) — `st`/`ed` are the inclusive offsets of that run's text in `dataStream`, and `ts` is the `RunStyle` (`{ bl?, it?, ul? }`, interface at `:8-12`). Unstyled and empty runs are omitted, so the model carries exactly the styled spans.

`rPrFromTs` (`docxData.ts:14-22`) is the inverse mapping the exporter uses: it renders a `RunStyle` back to `<w:rPr><w:b/><w:i/><w:u w:val="single"/></w:rPr>`. `escapeXml` (`:4-6`) escapes text for both styled and unstyled runs.

This is still a partial fidelity surface. **Bold, italic, underline and paragraph alignment survive the round-trip**; other paragraph properties (style, indents, spacing), tables, images, lists, headers/footers, hyperlinks and section properties are **not read** — they are not represented in the Univer model, so editing cannot preserve them.

A document with no `w:p` gets a single empty paragraph so the model still has a body. A package without `word/document.xml` throws (`docxData.ts:26-27`).

### Runtime (`univerDocMount.ts`)

`mountUniverDoc` (`univerDocMount.ts:24-82`) is dynamically imported from `DocxEditor` (`DocxEditor.tsx:59`) so the open-source `@univerjs/*` packages stay out of the entry chunk. It creates a Univer instance with the sheets-core preset but every chrome surface off (`header`, `toolbar`, `formulaBar`, `footer`, `contextMenu`, `univerDocMount.ts:41-56`), creates the document from the converted `IDocumentData`, and applies editability through `doc.setEditable?.(editable)` (`:59-61`, guarded because the method is missing in some `@univerjs/core` versions).

The handle exposes `snapshot()`, `setDarkMode()` and `dispose()` (`univerDocMount.ts:16-22`). `snapshot()` returns the live `IDocumentData` and is what save serialises.

### Export

`univerToDocx` (`docxData.ts:104-178`) is a **new package builder**, not an in-place edit:

- derive each paragraph's `[start, end]` text range in `dataStream` from `paragraphs[].startIndex` (`:109-126`); when no paragraph list exists, split on `\r\n` instead;
- emit `<w:pPr><w:jc w:val="…"/></w:pPr>` from `paragraphs[].paragraphStyle.horizontalAlign` (`:132-139`): 2 → `center`, 3 → `right`, 4 → `both`, 1 → `left`; a paragraph with no alignment emits no `w:pPr`. Univer `JUSTIFIED` (4) exports as Word `both`, so a Word `both` → 4 → `both` round-trip is stable;
- for each paragraph, take the `textRuns` overlapping the range, sort them by `st`, and slice the paragraph into styled and unstyled segments (`:141-154`); unstyled gaps become plain runs;
- emit one `<w:r>` per non-empty segment, with `rPrFromTs` producing `<w:rPr>` for the segment's style and `<w:t xml:space="preserve">` around the XML-escaped text (`:155-159`); a paragraph with no text at all becomes `<w:p/>`;
- write a fixed `w:sectPr` (US Letter `12240×15840` twips, 1-inch margins);
- write `[Content_Types].xml`, `_rels/.rels`, `word/_rels/document.xml.rels` and `word/document.xml`.

Every part that carried styling, numbering, media or relationships in the original package is gone, and only run-level `bl`/`it`/`ul` and paragraph-level alignment survive. Paragraph styles other than alignment, tables, images, numbering and relationships are still dropped. That is why the save stays destructive and why the frontend asks first.

### Tests

`docxData.test.ts` (7 `it` blocks; `preserves paragraph alignment` appears twice verbatim) pins the round-trip behaviour:

- imports paragraphs from a minimal docx, and throws on a package with no `word/document.xml` (`docxData.test.ts:6-29`);
- reads bold/italic/underline `textRuns` with exact `st`/`ed` offsets, including a combined bold+italic run and adjacent unstyled runs (`:31-74`);
- verifies the export emits `<w:b/>`, `<w:i/>`, `<w:u w:val="single"/>` and `xml:space="preserve"`;
- XML-escapes `&` and `<` inside a styled run (`:76-96`);
- preserves paragraph alignment: `w:jc` `center` → 2 and `right` → 3, an unaligned paragraph leaves `paragraphStyle` undefined, and the re-export contains `<w:jc w:val="center"/>` / `<w:jc w:val="right"/>` (`:98-122`, duplicated at `:124-148`);
- exports and re-imports plain paragraphs (`:150-164`).

### First-save confirmation

`DocxEditor` gates saving behind a confirmation (`DocxEditor.tsx:104-113`, dialog at `:133-150`):

> "Saving rewrites this .docx from the edited text. Formatting, tables, images and styles are not preserved — only paragraphs are kept. The original is backed up as .bak on first save."

Details worth knowing:

- **The dialog copy predates inline-run and alignment support.** It still says "only paragraphs are kept", but the importer/exporter now also preserve bold/italic/underline and paragraph alignment; the fidelity section above is authoritative.
- **It is once per file per page load, not once per document.** `confirmedSaveKeys` is a module-level `Set` (`DocxEditor.tsx:13`) keyed `` `${projectHost}::${projectRoot}::${path}` `` (`:33`). After a browser reload the warning appears again; it is not persisted.
- **A concurrent save waits on the open dialog** instead of stacking dialogs: `requestSave` returns the in-flight promise when `confirmRef.current` is non-null (`:106-107`). Cancel resolves `false`.
- **The Save button and the registered save handler both go through `requestSave`** (`:104`, `:116`), so Cmd/Ctrl+S and close-confirm honour the warning too.
- `TODO.md:7064` now records "paragraphs + basic inline formatting" and "First-save confirmation shipped", so the earlier "pending" note is resolved.

The PDF editor has no such confirmation — page operations are structural, not lossy of text.

## Wiring

### `PreviewSurface` (lazy, `editable` switch)

`PdfEditor` and `DocxEditor` are `lazy()` imports (`web/src/components/Preview/PreviewSurface.tsx:18-20`) so pdf-lib, pdf.js, JSZip and Univer stay in hashed chunks. Dispatch is a pair per kind on the `editable` prop (`PreviewSurface.tsx:120-123`):

```tsx
{kind === "pdf"  &&  editable && <PdfEditor …/>}
{kind === "pdf"  && !editable && <PdfViewer …/>}
{kind === "docx" &&  editable && <DocxEditor …/>}
{kind === "docx" && !editable && <DocxViewer …/>}
```

`editable` defaults to undefined, so the sidebar `PreviewHost` and the session Preview sub-tab keep the read-only viewers. The positive value only comes from `FileTabContent`.

### `FileTabContent` (the editable host)

`FileTabContent` marks only PDF and DOCX preview tabs editable (`web/src/components/Files/FileTabContent.tsx:324`, `editable={isPdfPreview || isDocxPreview}`).

Four behaviours are wired around that flag, and all of them use the same `excelSaveRef` / `excelDirty` state that the Excel viewer introduced — the names are historical, the plumbing is shared across Excel, PDF and DOCX:

1. **Dirty tracking up** — `onDirtyChange` from the viewer is lifted into `excelDirty` and reported to the parent (`FileTabContent.tsx:96-100`) so close-confirm and the desktop quit guard see the tab as dirty. For preview-only tabs this state, not `props.dirty` (Monaco's), is the source of truth.
2. **Save-handler registration on mount** — registered under `tabId ?? path` before the first edit (`:105-120`) and resolved lazily at call time, so the handler never captures a stale save closure. `saveEditorTab` refuses a preview-only tab with no registered handler, which is why registration cannot wait for a dirty change.
3. **Toolbar Save button** — rendered for Excel/PDF/DOCX (`:293-305`), disabled while clean, calling the viewer's save.
4. **Cmd/Ctrl+S** — a window `keydown` listener gated on `props.active` and a dirty buffer (`:124-135`). The active-tab gate matters because every editor tab stays mounted: without it a background dirty document would save on someone else's keystroke. App-level Cmd/Ctrl+S also reaches the same registered handler.

Both editors also register their save function through `onSaveRef` and fire `onSaveHandlerRegistered` (`PdfEditor.tsx:177-180`, `DocxEditor.tsx:115-119`), so the host can pick up the ref value without waiting for a re-render.

### Save path (shared)

Both editors call `apiClient.saveFileRaw` (`web/src/api/client.ts:3726-3755`), which:

- sends the bytes as `application/octet-stream`;
- passes `project_root`, `expected_hash`, `force` and — for a remote project — **`host` as a query parameter**;
- must **not** use the `/api/remote/{host}` proxy prefix. That prefix reads `?path=` as the project path, so the proxied form 403s on every save (same trap documented for Excel).

`HandleSaveFileRaw` dispatches on `host` to the remote transport and the remote server writes on its own filesystem. The remote `.bak` and hash checks are the same contract.

## Limits and known gaps

Tracked in `TODO.md` under "Open: in-app Word/PDF editing" (`TODO.md:7061-7066`):

- **PDF: no annotations, no text editing.** Page ops only; PDF is treated as final-form. Annotations are explicitly TBD.
- **PDF print reads the saved file**, not the live in-memory document, so unsaved page operations do not appear in print.
- **Word: paragraphs, paragraph alignment plus bold/italic/underline only.** Inline bold, italic and underline are preserved as run-level `textRuns`; `w:jc` alignment is preserved as `paragraphStyle.horizontalAlign`. Paragraph styles other than alignment, lists, numbering, tables, images, headers/footers, hyperlinks and section properties are still dropped on the first save and cannot be recovered from the new package; only the `.bak` holds the original. (`TODO.md:7064`'s gap list still names paragraph alignment as dropped; the importer/exporter now map `w:jc`, so this page is authoritative.)
- **Close-confirm / quit-guard is only partially integrated.** Dirty state and the save handler are wired (above), but the TODO still lists PDF/DOCX close-confirm and the desktop quit guard as not fully verified end-to-end.
- **The `.bak` never refreshes** — a product decision shared with Excel, not an editor bug.
- **Unverified live**: neither editor's save has a documented manual end-to-end run in the TODO; the contract is pinned by unit tests on the server side and by the shared raw-save tests.

## Related

- `concepts/excel-preview-univer-sheet.md` — the raw-save contract in full (hash, `force`, 413 cap, atomic write, remote `?host=` rule) and the Excel viewer that established `excelSaveRef` / `excelDirty`.
- `gotchas/files-tab-preview-only-routing.md` — why `.pdf`/`.docx` never open in Monaco, and the fetch-skip / watcher guards a preview-only tab depends on.
- `concepts/preview-print-save-as-pdf.md` — print for these kinds, which reads the file on disk rather than the live editor state.