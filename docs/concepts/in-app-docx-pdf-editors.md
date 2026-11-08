---
type: Concept
title: In-app Word (.docx) and PDF editors
description: 'Editable .docx and .pdf preview tabs: PDF page operations (rotate/delete/move/merge) and rotation-aware drag-to-select flattened annotations (highlight, text note) through pdf-lib, guarded by a render-finished check so an annotation cannot map through a stale viewport; Word through a JSZip/DOMParser importer that keeps paragraphs, paragraph alignment (left/center/right/justified) and inline run formatting (bold/italic/underline/strike, colour, font family and size, highlight/shading, sub/superscript) editable, imports simple Word tables as native editable Univer tables (TABLE_* stream sentinels plus body.tables/tableSource, exported by rebuilding w:tbl) and re-emits the original table/row/cell property XML (borders, shading, table style), imports inline image paragraphs as native Univer drawings (base64 source resolved from the package media; a resize is written back to `wp:extent`; native tables render in the live mount, but inline images are not painted by the OSS Univer 1.1.0 stack), and preserves merged/complex tables, block-level content controls and anchored/VML/mixed image paragraphs and paragraph-level section breaks verbatim as read-only raw blocks, each shown in the editor as a placeholder marker paragraph and restored on export by matching that marker (not a stream anchor), so text inserted before a preserved block cannot move it; exported by copying the original package and replacing only word/document.xml (so media, relationships, styles, headers/footers and sectPr survive), both saving through PUT /api/files/raw with a SHA-256 expected_hash and a one-time .bak. Covers the server allowlist (rawSaveAllowedExt), the PreviewSurface/FileTabContent wiring (editable flag, Save button, Cmd/Ctrl+S, dirty tracking, close-confirm handler registration), the Word first-save warning, and the fidelity limits that remain open, including run formatting inside an edited native-table cell being simplified, that inserted/pasted images are embedded from base64, http(s)/blob URLs or a resolvable UUID (SVG rejected) with per-image failures reported, that inline images do not render in the editor, and that no external Word client has validated the saved output.'
resource: internal/server/raw_save_ext.go; internal/server/handler_files.go; internal/server/handler_remote_files.go; web/src/components/Preview/PdfEditor.tsx; web/src/components/Preview/pdfAnnotate.ts; web/src/components/Preview/DocxEditor.tsx; web/src/components/Preview/docxData.ts; web/src/components/Preview/docxData.test.ts; web/src/components/Preview/univerDocMount.ts; web/src/components/Preview/PreviewSurface.tsx; web/src/components/Files/FileTabContent.tsx; web/src/api/client.ts
tags:
  - web
  - preview
  - docx
  - word
  - pdf
  - editor
  - univer
  - univer-table
  - native-table
  - pdf-lib
  - jszip
  - save
  - raw-save
  - backup
  - fidelity
  - alignment
  - close-confirm
  - annotation
  - drag-select
  - race-guard
  - raw-blocks
  - preservation
  - markers
  - placeholder
  - verification-gap
timestamp: 2026-11-08T13:20:00Z
---
# In-app Word (.docx) and PDF editors

## Summary

Opening a `.docx` or `.pdf` in a Files tab used to be preview-only. Both kinds now render an **editable** viewer when the host asks for it:

- **PDF** — `PdfEditor` (`web/src/components/Preview/PdfEditor.tsx`) does page operations (rotate, delete, move, merge) and basic annotations (highlight, text note) with `pdf-lib`, then re-serialises the whole document on save. Both annotation tools accept a **drag-selected region** on the page thumbnail and place it correctly on rotated pages.
- **Word** — `DocxEditor` (`web/src/components/Preview/DocxEditor.tsx`) reads paragraphs, paragraph alignment and inline run formatting (bold, italic, underline, strike, colour, font family/size, highlight/shading, sub/superscript) out of `word/document.xml`, shows them editable in a Univer Docs mount, imports **simple Word tables as native editable Univer tables** (re-emitting the original table/row/cell property XML so borders, shading and the table style survive), imports **inline image paragraphs as native Univer drawings**, and **preserves every block it cannot model** — merged/complex tables, block-level content controls, and anchored/VML/mixed image paragraphs — verbatim as read-only raw blocks, each standing in as a visible placeholder paragraph (`[Table preserved]`, `[Image preserved]`, `[Content preserved]`), and paragraph-level section breaks (`[Section break preserved]`, with the `w:sectPr` kept in a raw block). On save it copies the original package and replaces only `word/document.xml`, so media, relationships, styles, headers/footers and the page-setup `sectPr` survive.

Neither editor is a full fidelity-preserving Office editor. PDF has no text-edit layer, and its annotations are flattened page content rather than PDF annotation objects, so a reader cannot remove them. Word keeps the editable paragraph/run surface, imports simple tables as native editable tables (their table/cell borders and shading are carried through; run formatting inside an edited cell is simplified), imports inline images as native drawings (move/resize, with the resize written back to `wp:extent`) and carries merged/complex tables, content controls and anchored/VML/mixed images through unchanged, but those preserved blocks are **read-only** — they are outside the editor's text model, so they cannot be edited in place. A preserved block is re-attached at export by **matching the placeholder marker paragraph**, not a stream offset, so text inserted around one cannot move it. Anything that is neither modelled nor copied as a package part is still lost. The one-time `.bak` written by the raw save is the recovery path for both.

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

Both editors read through the same binary endpoint, with `?host=` for a remote project (`PdfEditor.tsx:54-58`):

```ts
fetch(`/api/files/raw?path=…&project_root=…&host=…`)
```

`PdfEditor` loads the bytes into `PDFDocument.load` (`PdfEditor.tsx:63`), records `sha256Hex(bytes)` in `originalHashRef` (`:67`), and separately renders thumbnails with **pdf.js** (`:76-103`) — pdf-lib is the editor, pdf.js is only the renderer. The editor is therefore one document object plus canvases, and every operation mutates the pdf-lib document.

### Page operations

All operations call `markDirty()` (`PdfEditor.tsx:105-108`), which sets local `dirty` **and** notifies the host through `onDirtyChange`:

| Operation | Implementation | Anchor |
| --- | --- | --- |
| Rotate 90° | `page.setRotation(degrees((angle + deg) % 360))` | `PdfEditor.tsx:110-117` |
| Delete page | `doc.removePage(idx)`; refused when only one page remains | `PdfEditor.tsx:119-126` |
| Move up / down | remove at `idx`, `insertPage(target, page)` | `PdfEditor.tsx:128-139` |
| Merge PDF… | file input → `PDFDocument.load` → `doc.copyPages(src, …)` → `addPage` | `PdfEditor.tsx:141-162` |

There is no text editing, and the annotation tools in the next section are flattened page content rather than a removable annotation layer. Rotation is a page property (`Rotate 90°` above), and annotation placement is rotation-aware: the per-page pdf.js viewport transform is inverted when mapping a selection, so a mark lands where it was dragged on a rotated page too.

### Annotations (`pdfAnnotate.ts`)

`web/src/components/Preview/pdfAnnotate.ts` holds the annotation helpers:

| Function | Draws / maps | Anchor |
| --- | --- | --- |
| `PdfRect` | a rectangle in PDF points, origin bottom-left | `pdfAnnotate.ts:4-9` |
| `canvasToPdfRect(sel, { scale, pageHeight, transform? })` | maps a canvas-pixel selection (top-left origin) to PDF points (bottom-left origin). With the optional 6-value pdf.js viewport `transform` it inverts the matrix (`det = a*d - b*c`) and returns the axis-aligned bounding box of the two mapped corners, which is exact for any page rotation; without it the legacy unrotated formula runs: `x / scale`, `pageHeight - (y + height) / scale`, `width / scale`, `height / scale` | `pdfAnnotate.ts:25-55` |
| `addHighlight(pdfBytes, pageIndex, rect)` | `page.drawRectangle` with `rgb(1, 0.92, 0.2)`, `opacity: 0.35`, `borderWidth: 0` | `pdfAnnotate.ts:57-70` |
| `addTextNote(pdfBytes, pageIndex, x, y, text)` | `doc.embedFont(StandardFonts.Helvetica)` then `page.drawText(text, { x, y, size: 11, … })` | `pdfAnnotate.ts:76-88` |

Both draw helpers load the input with `PDFDocument.load`, draw onto the requested page, and return `doc.save()` (new bytes). They are **page-content operations, not PDF annotation objects**: pdf-lib has no annotation-object API, so each mark is flattened into the page's content stream and a PDF reader cannot un-draw it.

#### Drag-to-select

`PdfEditor` tracks a selection rectangle per page thumbnail:

- `sel` state (`PdfEditor.tsx:42`) holds `{ page, x, y, width, height }` in **canvas pixels**; `dragRef` (`:43`) holds the drag origin (`{ page, startX, startY }`).
- `viewportRefs` (`PdfEditor.tsx:44`) holds each page's pdf.js viewport, assigned inside the render effect immediately after `page.getViewport({ scale: 0.3 })` (`:87-88`), so its `transform` is available to the mapping even when the canvas is not.
- `relPoint` (`:164-169`) converts a mouse event into canvas pixels using the bounding rect and the backing-store/CSS scale ratio, so a resized thumbnail still yields correct pixels.
- `onPageMouseDown` (`:170-177`) selects the page, records the origin and seeds a zero-size `sel`; `onPageMouseMove` (`:178-191`) normalises the rect with `Math.min`/`Math.abs` so a drag in any direction produces a positive-size rectangle; `onPageMouseUp` (`:192-194`) clears `dragRef` (it does not clear `sel`, so the last selection stays usable). These handlers are wired on each page wrapper at `:299-309` (`onMouseDown`/`onMouseMove`/`onMouseUp`/`onMouseLeave`, with a `cursor-crosshair` at `:305`).

The two toolbar buttons consume that selection:

- **Highlight** (`applyHighlight`, `PdfEditor.tsx:196-217`; button at `:289`) maps the selection with `canvasToPdfRect(sel, { scale: 0.3, pageHeight: doc.getPage(selected).getSize().height, transform: viewportRefs.current[selected]?.transform })` when `sel.page === selected && sel.width > 1 && sel.height > 1`; otherwise it falls back to the fixed rectangle `{ x: 40, y: 40, width: 300, height: 24 }`.
- **Add note…** (`applyNote`, `PdfEditor.tsx:219-245`; button at `:290`) prompts with `window.prompt("Note text")` and anchors the note at the selection's top-left corner via `canvasToPdfRect({ ...sel, height: 0 }, { …, transform: viewportRefs.current[selected]?.transform })` when `sel.page === selected`; otherwise it falls back to `{ x: 40, y: 80 }`.

The `0.3` scale is not arbitrary: thumbnails are rendered at `page.getViewport({ scale: 0.3 })` (`PdfEditor.tsx:87`), and the mapping is called with the same constant, so the selected region lands exactly where the user dragged. The viewport's `transform` already composes the page's `/Rotate`, that scale and the y-flip, which is why supplying it makes the mapping exact at any rotation.

**Render-finished guard (no stale-viewport annotations).** Both annotation actions refuse to run until the thumbnail render pass has completed for the bytes currently in state. `renderedBytesRef` (`PdfEditor.tsx:45`) is assigned the current `pdfBytes` only after the render loop finishes (`:97`), and the `rendering` state (`:46`) is set `true` when a pass starts (`:79`) and cleared in the `finally` block (`:99`). `applyHighlight` and `applyNote` both early-return while `renderedBytesRef.current !== pdfBytes` (`:198`, `:221`), and both toolbar buttons are `disabled={rendering}` (`:289`, `:290`). The point is that a new render pass — the effect depends on `pdfBytes`, `pageCount` and `doc` — can be in flight while `viewportRefs` still holds the previous pass's transforms; clicking during that window would map the selection through a stale viewport (for example a transform captured before a rotation) and save the mark in the wrong place. The identity check is deliberately stricter than the boolean: `renderedBytesRef` is written only on a completed loop, so a failed or cancelled render leaves annotations blocked rather than silently falling back to an old viewport. Only the annotation buttons are gated this way — the page-operation buttons stay live.

**Rotation.** `canvasToPdfRect` is rotation-aware whenever the viewport transform is supplied: it inverts the 6-value matrix (`det = a*d - b*c`) and returns the axis-aligned bounding box of the two mapped corners. A 90° rotation therefore swaps the selection's width and height, and a 180° rotation flips both axes, while the mark still lands on the dragged region. There is no rotation refusal — `applyHighlight` and `applyNote` annotate rotated pages exactly like unrotated ones. Only when the transform is unavailable (for example a page whose thumbnail has not rendered yet) does the legacy unrotated formula apply.

Both paths reload the returned bytes with `PDFDocument.load`, swap them into state (`pdfBytes` + `doc`), and call `markDirty()`.

`pdfAnnotate.test.ts` pins eight behaviours (8 `it` blocks, `pdfAnnotate.test.ts:11-90`): the unrotated canvas→PDF mapping — `{ x: 30, y: 30, width: 60, height: 30 }` at `scale: 0.3`, `pageHeight: 400` becomes `x 100, y 200, width 200, height 100` (`:12-18`); a 90° page through transform `[0, 0.3, 0.3, 0, 0, 0]` maps to `x 100, y 100, width 100, height 200` (`:21-31`); a 180° page through transform `[-0.3, 0, 0, 0.3, 120, 0]` maps to `x 100, y 100, width 200, height 100` (`:33-43`); a 270° page through transform `[0, -0.3, -0.3, 0, 120, 120]` maps to `x 200, y 100, width 100, height 200` (`:45-55`), covering the fourth quarter-turn; a non-zero MediaBox origin — transform `[0.3, 0, 0, -0.3, -3, 126]` produced from `MediaBox [10, 20, 410, 420]` — maps the same selection to `x 110, y 220, width 200, height 100` (`:57-67`), proving the translation terms `e`/`f` carry the MediaBox origin instead of assuming `0,0`; a highlight keeps the page count and grows the byte length (`:69-76`); a text note on page 1 of 2 keeps both pages and grows the bytes (`:78-84`); and an out-of-range page rejects (`:86-89`).

### Save

`save` (`PdfEditor.tsx:247-270`) serialises the whole document (`doc.save()`), hashes the **new** bytes, and PUTs them with the hash of the **bytes that were loaded**:

```ts
apiClient.saveFileRaw(path, u8.buffer, {
  projectRoot, host: projectHost, expectedHash: originalHashRef.current,
})
```

On success it replaces `originalHashRef` with the new hash, clears `dirty`, and reports clean. That reassignment is what makes a second save in the same tab work: without it every subsequent save would 409 as "changed on disk". A mismatch therefore means a third party changed the file, not that this tab saved earlier.

Because page operations and annotations both require a full rewrite, `doc.save()` re-serialises every object rather than appending an incremental update.

## Word editor (`DocxEditor.tsx`)

### Import (paragraphs, alignment, inline runs and preserved blocks)

`docxToUniver` (`web/src/components/Preview/docxData.ts`) unzips the package with JSZip, parses `word/document.xml` with `DOMParser`, and walks **only the top-level children of `w:body`** (`top-level body walk`). Each child is classified once:

- a `w:p` with no unsupported object is imported as editable text (paragraphs, alignment and inline runs below). A mid-document section break on such a paragraph (`w:pPr/w:sectPr`) is kept as well: its text is imported, a `[Section break preserved]` marker follows it, and a raw block of kind `section` holds the `w:sectPr`;
- a **simple** `w:tbl` that `parseSimpleTable` accepts is imported as a **native Univer table** (next section);
- a `w:p` whose only content is one **inline** `w:drawing` is imported as a **native Univer drawing** (next section);
- every other block — a merged/nested/multi-paragraph `w:tbl`, a block-level `w:sdt` content control, an **anchored** (`wp:anchor`) / VML / mixed text+image paragraph, or any other top-level element — is serialized and stored in `body.docxRawBlocks` as `{ startIndex, xml }`, and a **placeholder marker paragraph** is appended to the `dataStream` at that same position (`marker paragraph`; the field is `body.docxRawBlocks`). For a preserved block the block's own text is **not** appended to the `dataStream`, so its text cannot duplicate into the editable text.

A paragraph is "unsupported" when `hasUnsupportedObject` (`:117-124`) finds a `drawing`, `pict`, `object` or `txbxContent` descendant; an image-only paragraph is tried for native conversion first, and only anchored/VML/mixed cases remain preserved. An image-only paragraph that carries a section break is kept verbatim instead, so its `w:sectPr` survives. The body-level `sectPr` (the last direct `w:sectPr` child of `w:body`, `bodySectPrXml`, `docxData.ts:162-169`) is skipped here and reused at export. If no paragraph was imported at all (a document that is only a table, say), a single empty paragraph keeps the Univer model valid (`empty-paragraph fallback`). A package without `word/document.xml` throws (`missing document.xml` throw).

For each imported paragraph the importer walks its `w:r` descendants, concatenates each run's `w:t` text, appends `\r\n`, and builds a flat Univer `IDocumentData` (`dataStream` + one paragraph entry per `w:p`). A paragraph entry's `startIndex` is the index of its terminating `\r`, which is Univer's convention. The exporter reads a paragraph's text from just after the previous terminator up to that `\r`, so hand-built snapshots must use the same convention.

#### Inline images

`importImageParagraph` converts a paragraph whose only content is one `wp:inline` drawing into a native Univer image: it appends the `CUSTOM_BLOCK` (`\b`) sentinel to the `dataStream`, records `body.customBlocks` `{ startIndex, blockId }`, and registers `drawings[drawingId]` with `imageSourceType: "BASE64"`, a `source` data URL read from the referenced `word/media` part, `drawingType: 0` (image), `layoutType: 0` (inline), and a size derived from `wp:extent` EMU / 9525 (px). `drawingsOrder` is kept in document order and `__imageMeta[drawingId] = { xml, width, height }` stores the original `<w:drawing>` for export. Anchored (`wp:anchor`), VML, linked and mixed text+image paragraphs return false and fall through to the preserved-block path. The conversion mirrors the shape Univer's own paste path creates, so the mount can render the image.

Paragraph alignment is read from each `w:p`'s `w:pPr` (`docxData.ts`, alignment): the importer takes `w:jc/@w:val` and maps `center` → 2, `right`/`end` → 3, `both`/`justify` → 4, `left`/`start` → 1 and `distribute` → 6 onto `paragraph.paragraphStyle.horizontalAlign` (Univer's `HorizontalAlign`: LEFT 1, CENTER 2, RIGHT 3, JUSTIFIED 4). Word's `both` therefore lands on `JUSTIFIED` (4), not Univer's separate `BOTH` (5) value. `start`/`end` collapse to left/right (1/3) because Univer has no locale-aware start/end. A paragraph with no `w:jc` gets no `paragraphStyle` at all, so its alignment stays unspecified.

Inline formatting is read from each run's `w:rPr` child (`docxData.ts`, inline runs). The `RunStyle` interface (`:8-18`) now carries:

| OOXML source | `RunStyle` field | Notes |
| --- | --- | --- |
| `<w:b/>` | `bl` | `ooxmlOn` |
| `<w:i/>` | `it` | `ooxmlOn` |
| `<w:u w:val="…"/>` | `ul: { s: 1 }` | absent `w:val` means on; `none`/`false`/`0`/`off`/`no` means off (`:491-492`) |
| `<w:strike/>` | `st: { s: 1 }` | `ooxmlOn` |
| `<w:color w:val="…"/>` | `cl: { rgb: "#RRGGBB" }` | `auto` is ignored (`:494-495`) |
| `<w:sz w:val="…"/>` | `fs` | half-points ÷ 2, e.g. `21` → `10.5` pt (`:496-500`) |
| `<w:rFonts w:ascii|w:hAnsi>` | `ff` | `ascii` wins, then `hAnsi` (`:501-507`) |
| `<w:highlight w:val="…"/>` or `<w:shd w:fill="…"/>` | `bg: { rgb: "#RRGGBB" }` | named highlight mapped through `OOXML_HIGHLIGHT_HEX`; `w:shd/@w:fill` used directly; `fill="auto"` means no background (`:508-518`) |
| `<w:vertAlign w:val="…"/>` | `va` | `subscript` → 2, `superscript` → 3 (Univer `BaselineOffset`) (`:519-521`) |

`ooxmlOn` (`:61-66`) implements the OOXML on/off rule: an absent `w:val` means on, while an explicit `false`/`0`/`off`/`no` means off. `decorationOn` (`:55-58`) accepts either a bare number or `{ s }` and treats `{ s: 0 }`/`0` as off. `toOoxmlHex` (`:42-52`) normalises `#RGB`, `#RRGGBB` and `rgb(r,g,b)`; `OOXML_HIGHLIGHT_HEX` (`:22-39`) is the named-colour table (black, blue, cyan, green, magenta, red, yellow, white plus the eight dark/light variants).

When a run carries at least one of those properties, the importer records a `textRuns` entry `{ st, ed, ts }` (`:523-527`) — `st`/`ed` are the inclusive offsets of that run's text in `dataStream`. Unstyled and empty runs are omitted, so the model carries exactly the styled spans.

`rPrFromTs` (`docxData.ts:68-86`) is the inverse mapping the exporter uses. It emits the members in `EG_RPrBase` sequence so Word accepts the element order: `<w:rFonts w:ascii w:hAnsi>` (`:72`), `<w:b/>` (`:73`), `<w:i/>` (`:74`), `<w:strike/>` (`:75`), `<w:color w:val>` (`:76-77`), `<w:sz w:val>` as `Math.round(pt * 2)` (`:78`), `<w:u w:val="single"/>` (`:79`), `<w:shd w:val="clear" w:color="auto" w:fill>` (`:80-81`), and `<w:vertAlign w:val="subscript|superscript"/>` (`:82-83`). Only `w:shd` is emitted for a background, never `w:highlight`, so the two cannot conflict on re-import. `escapeXml` (`:4-6`) escapes text for both styled and unstyled runs.

#### Native tables

Simple `w:tbl` elements are **native Univer tables**: their cell text lives in the document stream, so a cell can be edited in place and survives an export. `appendTable` (`docxData.ts`) encodes a table with Univer's `DataStreamTreeTokenType` sentinels (`DS`, `:186-195`):

| Token | Character |
| --- | --- |
| `PARAGRAPH` | `\r` |
| `SECTION_BREAK` | `\n` |
| `TABLE_START` | `\x1a` |
| `TABLE_ROW_START` | `\x1b` |
| `TABLE_CELL_START` | `\x1c` |
| `TABLE_CELL_END` | `\x1d` |
| `TABLE_ROW_END` | `\x0e` |
| `TABLE_END` | `\x0f` |

The emitted shape is `TABLE_START (ROW_START (CELL_START text PARAGRAPH SECTION_BREAK CELL_END)* ROW_END)* TABLE_END PARAGRAPH`. The trailing `PARAGRAPH` is the paragraph that encloses the whole table. `appendTable` appends `body.tables` entries `{ startIndex, endIndex, tableId }`, where `endIndex` is the index **after** `TABLE_END` (the enclosing `PARAGRAPH` position); it also pushes a `body.paragraphs` entry for each cell's `PARAGRAPH` and for the enclosing paragraph (one per cell and one for the enclosing paragraph), a `body.sectionBreaks` entry per cell, `document.tableSource[tableId]` from `makeTableSource`, and the parsed `tblPrXml`/`rowPrXml`/`tcPrXml` into a document-level `__tableStyles[tableId]` map.

`parseSimpleTable` decides whether a `w:tbl` is representable. It returns `null` — so the caller falls back to verbatim preservation — when any cell has `w:gridSpan`, `w:vMerge` or `w:hMerge`, when a cell nests another `w:tbl`, when a cell holds more than one `w:p`, when a cell holds a drawing, `pict`, object, text box, hyperlink, content control, simple field or complex field (`docxData.ts:320-323`, because export rebuilds cell text as one plain run), or when the rows or the grid disagree on column count. Otherwise it returns `{ gridTwips, tblPrXml, rowPrXml, rows }`, where each row is an array of `ParsedCell { text, tcPrXml }` and the table/row/cell property XML is captured with `serializeRawBlock`.

`makeTableSource` mirrors Univer's own `genEmptyTable`/`genTableSource` defaults: `trHeight` AUTO 30, cell margins 10/10/5/5, `align` START 0, `textWrap` NONE 0, `position` PAGE/PAGE 0. Grid column widths are scaled from `w:tblGrid` twips to `TABLE_CONTENT_WIDTH` — 624 layout px.

On export, `renderTableXml` (`renderTableXml`) rebuilds a `w:tbl` from a stream range: it slices the `TABLE_*` sentinels back into rows, converts each column's layout width to twips (`Math.round(px * 15)`), and emits `<w:tbl>` with the original `w:tblPr` — falling back to `<w:tblPr/>` only when import captured none (`tblPr` fallback) — a `<w:tblGrid>`, then `<w:tr>`/`<w:tc>` cells that re-emit each row's `w:trPr` and each cell's `w:tcPr` before `<w:p><w:r><w:t>` (`cells`). Table/cell borders, shading and the table style therefore survive a native-table round-trip; only the run formatting *inside* an edited cell is flattened to a single unstyled run (see Limits).

#### Preserved blocks (merged/complex tables, content controls, anchored/VML images)

`serializeRawBlock` (`docxData.ts:172-178`) serialises the element with `XMLSerializer` and then runs `canonicalizePrefixes` (`:140-154`). The serializer invents prefixes (`ns1:`) for namespaces declared on the document root rather than on the serialized subtree; `canonicalizePrefixes` reads the fragment's `xmlns:*` declarations, maps each URI through `CANONICAL_PREFIX` (`:127-133`) and rewrites both the declarations and every use back to the conventional Word prefixes (`w`, `r`, `wp`, `a`, `pic`). If the fragment still lacks `xmlns:w`, `serializeRawBlock` declares it on the root element, so the emitted XML is self-contained and re-emittable as-is.

The markers are declared in one place: `PRESERVED_MARKERS` (`docxData.ts:88-93`) maps each preserved kind to its text (`table: "[Table preserved]"`, `image: "[Image preserved]"`, `content: "[Content preserved]"`, `section: "[Section break preserved]"`) and `MARKER_TEXTS` (`:95`) is the membership set the exporter tests against. On import, a preserved block's marker is written as an ordinary paragraph at the block's stream position (`:674`) and its raw XML is pushed to `rawBlocks` with that offset as `startIndex` (`:675`).

The stored block is **opaque to the editor**, but it no longer renders as an invisible gap: Univer renders the `dataStream`, so an unsupported table, content control or anchored/VML/mixed image paragraph shows up as its marker line exactly where the block sits. The marker is ordinary text, so typing before or after it is safe; only editing the marker line itself detaches the block from that position (see Export). The block's bytes are carried through the save unchanged but cannot be edited here — you cannot type inside a merged table's cell, change a control's value or move an image, and the first-save dialog states this explicitly.

### Runtime (`univerDocMount.ts`)

`mountUniverDoc` (`univerDocMount.ts:31-104`) is imported statically by `DocxEditor` (`DocxEditor.tsx:4`) and called at `DocxEditor.tsx:70`. The open-source `@univerjs/*` packages stay out of the entry chunk because `DocxEditor` is itself a lazy import (see Wiring), not because of a dynamic import inside it. It creates a Univer instance with the sheets-core preset but every chrome surface off (`header`, `toolbar`, `formulaBar`, `footer`, `contextMenu`, `univerDocMount.ts:49-64`), creates the document from the converted `IDocumentData`, and applies editability through `doc.setEditable?.(editable)` (`:68-69`, guarded because the method is missing in some `@univerjs/core` versions).

The handle exposes `snapshot()`, `resolveImage()`, `setDarkMode()` and `dispose()` (`univerDocMount.ts:16-29`). `snapshot()` returns the live `IDocumentData` and is what save serialises; `resolveImage()` asks the mounted document's image host for a UUID source (see the save path).

### Export

`univerToDocx` (`docxData.ts`) takes an optional `DocxExportContext { originalBytes?, rawBlocks?, tableStyles?, imageMeta?, resolveImage?, imageFetch?, onImageError? }`. `DocxEditor`'s save supplies all eight (`imageMetaOut` is the eighth, `DocxEditor.tsx:112`), so in practice the save **copies the original package and replaces only `word/document.xml`**. Steps:

- derive each paragraph's text range in `dataStream` from `paragraphs[].startIndex`, which is the index of the paragraph's terminating `\r`. The text starts just after the previous terminator, skipping gap tokens (the `\n` section break and the `TABLE_*` sentinels, `PARAGRAPH_GAP_TOKENS`, `docxData.ts:204`), and ends before that `\r`. A table or raw block between two paragraphs therefore never falls inside a paragraph's range. When no paragraph list exists, split on `\r\n` instead (`docxData.ts:1038-1045`);
- choose the raw blocks: the snapshot's `body.docxRawBlocks` when it is non-empty, else `ctx.rawBlocks`. Either way those `startIndex` values are used only to **order** the blocks; where a preserved block lands is decided by its marker paragraph, not by an offset;
- collect `body.tables` ranges, `document.tableSource` and the table-style map (`ctx?.tableStyles ?? document.__tableStyles`). Before rendering each paragraph, flush every table whose `startIndex <= paragraph.start` with `renderTableXml`, and **skip** paragraphs inside a table range — cell paragraphs and the table's enclosing paragraph are rendered by the table itself. Any remaining table ranges are flushed after the loop;
- a paragraph whose text is **exactly** one of `MARKER_TEXTS` emits the next raw block's XML instead of that paragraph (a `section` block is emitted as a section break, see below). The marker is a paragraph like any other, so it moves with the surrounding text and inserting/deleting text before it cannot shift the block;
- an image paragraph (`CUSTOM_BLOCK`, `\b`) is matched to `customBlocks` by its start index (falling back to `drawingsOrder`). The original `<w:drawing>` is re-emitted with `patchDrawingExtent` rewriting `wp:extent`/`a:ext` cx/cy from the drawing's current px size, so a resize saved in the editor is written back. A `CUSTOM_BLOCK` paragraph whose drawing has no `__imageMeta` entry (an image inserted or pasted in the editor) is resolved by `resolveImageBytes` and written as a new part: `parseImageDataUrl` decodes a `data:` URL; an `http(s)://`, `blob:` or relative URL is fetched with `credentials: "omit"` under a 15 s abort and a 20 MB streamed cap, while `imageRequestHeaders` adds the bearer token only when the URL is same-origin and never to a cross-origin host, and the body must sniff as PNG/JPEG/GIF/WEBP/BMP by magic bytes (SVG is rejected); a UUID is delegated to `ctx.resolveImage`, which the editor wires to `IImageIoService.getImage` — the default host is an identity function, so an unresolvable UUID is reported rather than written. `buildInlineDrawingXml` emits the `<w:drawing>` with a fresh `rId`, the bytes go to `word/media/opencode-image-N.<ext>` with a relationship and a `[Content_Types].xml` default, and `nextMediaPath` skips names already present in the package. Identical sources share one media part and `rId`. A per-image failure goes through `ctx.onImageError` (the save continues and `DocxEditor` shows a banner); without a handler the export throws;
- a block whose marker was edited away is **appended** before the `sectPr` rather than silently dropped; duplicate markers re-associate blocks in document order;
- a `section` block is re-emitted as a section break. `pushSectionBreak` (`docxData.ts:1105-1110`) merges its `w:sectPr` into the `w:pPr` of the preceding plain paragraph when that paragraph is still the last part written (`attachSectPr`, `docxData.ts:817-821`), and otherwise emits it as a standalone empty paragraph. Leftover blocks are never dropped;
- each image embedded from a new source reports its `<w:drawing>` XML, size and Univer drawing id through `ctx.imageMetaOut` (`docxData.ts:1167`). `DocxEditor` merges those entries into `imageMetaRef` only after the save succeeds (`DocxEditor.tsx:125`), so the next save reuses the media part and relationship instead of embedding the image again. New drawing ids start above the largest `docPr` id already in the package (`docxData.ts:1090-1095`), so they cannot collide;
- `renderParagraphXml` emits the `w:jc` for `paragraphStyle.horizontalAlign` (2 → `center`, 3 → `right`, 4 → `both`, 1 → `left`, 6 → `distribute`), slices overlapping `textRuns` into styled and unstyled segments, and emits one `<w:r>` per non-empty segment with `rPrFromTs` and `<w:t xml:space="preserve">` around the XML-escaped text;
- reuse the original body `w:sectPr` when `originalBytes` is present (`bodySectPrXml` on the original `word/document.xml`, `docxData.ts:1204-1205`), so page setup and header/footer references survive; otherwise write a fixed US Letter `w:sectPr` (12240×15840 twips, 1-inch margins);
- with `originalBytes`, load the original zip, replace `word/document.xml` and regenerate it: media, relationships, styles, headers/footers and every other part are copied verbatim. Without it (tests and non-editor callers), build a minimal package from `[Content_Types].xml`, `_rels/.rels`, `word/_rels/document.xml.rels` and `word/document.xml`.

Two consequences follow. First, saving is no longer destructive for anything the raw-block path covers: a merged/complex table keeps its cell text, merges and styling; an anchored image keeps its media part and its `r:embed` relationship; a header/footer reference survives because the `sectPr` that points at it is reused and the referenced part is copied. A **native** table is rebuilt from its stream cells, so edited cell text, column widths and the original table/row/cell property XML (borders, shading, table style) round-trip, while run formatting inside an edited cell is simplified. A **native inline image** is re-emitted from its original drawing XML, with only the extent updated when it was resized. Second, preserved blocks are **not editable**, and their position is marker-driven rather than offset-driven: an edit before a preserved block no longer changes where it is re-emitted, because the marker moves with its text; editing the marker line itself appends the block at the end of the body instead. Only the body `w:sectPr` is reused as the page setup. Paragraph-level section breaks survive as described above, but a section break inside a table cell does not, because cell text is rebuilt from plain runs.

### Tests

`docxData.test.ts` pins the round-trip behaviour:

- imports paragraphs from a minimal docx (`docxData.test.ts:6-23`), and throws on a package with no `word/document.xml` (`:25-29`);
- reads bold/italic/underline `textRuns` with exact `st`/`ed` offsets, including a combined bold+italic run and adjacent unstyled runs (`:31-74`);
- verifies the export emits `<w:b/>`, `<w:i/>`, `<w:u w:val="single"/>` and `xml:space="preserve"`;
- XML-escapes `&` and `<` inside a styled run (`:76-96`);
- preserves paragraph alignment: `w:jc` `center` → 2 and `right` → 3, an unaligned paragraph leaves `paragraphStyle` undefined, and the re-export contains `<w:jc w:val="center"/>` / `<w:jc w:val="right"/>` (`:98-122`);
- maps `start`/`end`/`distribute`: `start` → 1, `end` → 3, `distribute` → 6, re-exported as `left`/`right`/`distribute` (`:124-149`);
- exports and re-imports plain paragraphs (`:151-165`);
- imports colour/size/font/strike/highlight/vertAlign (`:167-202`): `w:color` → `#FF0000`, `w:sz 21` → 10.5, `w:rFonts ascii` → `Arial`, `w:strike` → `st.s = 1`, `w:highlight="yellow"` → `#FFFF00`, `w:vertAlign="superscript"` → `va = 3`, and a run with `w:b w:val="0"`, `w:i w:val="false"`, `w:u w:val="none"` yields **no** `textRuns` entry, while `w:shd w:fill="00FF00"` → `#00FF00`;
- exports rich run properties and re-imports them (`:240-289`): the emitted XML contains the exact `rFonts`/`b`/`i`/`strike`/`color`/`sz`/`shd`/`vertAlign` tags, and the re-imported run carries the same values back;
- does not export disabled decorations (`:291-306`): `ul: { s: 0 }` and `st: { s: 0 }` produce neither `<w:u ` nor `<w:strike/>`;
- **imports a simple table as a native Univer table and round-trips it** (`:330-372`): the fixture table carries `<w:tblPr><w:tblStyle w:val="TableGrid"/>…` and a `<w:tcPr><w:shd … w:fill="EEEEEE"/>` (`:332-334`); the cell text **is** in `dataStream` alongside the `\x1a` sentinel (`:346-347`), `body.tables` has one entry and `body.docxRawBlocks` is empty (`:348-349`), `tableSource[tableId]` has 2 rows × 2 columns (`:350-352`), and `paragraphs.length` is 2 body paragraphs + 4 cell paragraphs + 1 enclosing paragraph (`:354`). The re-exported `document.xml` contains `<w:tbl`/`<w:gridCol`, still carries the `TableGrid` style and the `EEEEEE` fill, and does **not** emit an empty `<w:tblPr/>` (`:359-364`); it has exactly one `Cell A`, two `<w:tr>` and four `<w:tc>`, ordered `Before` < `<w:tbl` < `After` (`:365-369`), is well-formed (`assertWellFormed` at `:325-328`, called at `:370`), and still carries an unrelated `word/styles.xml` part (`:371`);
- **falls back to verbatim preservation for a merged-cell table** (`:374-395`): `body.tables` is empty, the cell text `Merged` is **not** in `dataStream`, the stream contains `"[Table preserved]"`, exactly one `docxRawBlocks` entry is kept and it carries `w:gridSpan` (`:383-387`); the re-export keeps `w:gridSpan`, contains no marker text, and is well-formed (`:392-394`);
- **falls back to verbatim preservation when a table cell holds a drawing** (`:397-422`), the same route as the merged-cell case, because the cell text would otherwise be rebuilt without the picture;
- imports an inline image as a native drawing and round-trips it (`:424-464`): `body.customBlocks` and `drawingsOrder` are populated, `source` is a `data:image/png;base64,` URL, the size comes from `wp:extent`, and after save the original `r:embed="rId5"` and the same `word/media/image1.png` bytes are still present; the exported XML is well-formed;
- writes a resized native image extent back (`:466-480`): changing `drawings[id].transform` to 200×100 makes the export emit `cx="1905000" cy="952500"`;
- writes an image pasted in the editor into the package (`:482-521`): the base64 data URL is decoded into one `word/media/opencode-image-N.png` part, the emitted `<w:drawing>` carries an `r:embed` whose id appears in `word/_rels/document.xml.rels`, and `[Content_Types].xml` gains an `Extension="png"` default;
- adds pasted-image media to an existing package (`:523-560`): exporting over an original `.docx` keeps its `word/styles.xml` part while adding the new media part and relationship;
- keeps an anchored image as a preserved block (`:562-579`): `wp:anchor` produces no drawing, stays in `docxRawBlocks`, and round-trips;
- orders text, table and image blocks in a mixed document (`:581-605`): both the table and the image are native (`body.tables` one entry, `drawingsOrder` one entry, no raw blocks), and the exported XML's `One` / `<w:tbl` / `Two` / `<w:drawing>` / `Three` positions are strictly increasing;
- **keeps a native table in place after text is inserted before it** (`:607-642`): the test prepends a `"New paragraph\r\n"` paragraph to the snapshot's `dataStream`, shifts every existing paragraph's `startIndex` by that length, and — as the editor does — shifts every `body.tables` `startIndex`/`endIndex` by the same amount. It then asserts `New paragraph` < `Before` < `<w:tbl` < `After`, exactly one `Cell A`, and no `[Table preserved]` in the output, proving the table follows the text, not the stale offsets;
- reuses the original `sectPr` and keeps header references (`:644-664`): the saved `document.xml` carries `r:id="rId7"` and the original `w:top="720"` margins, and `word/header1.xml` is still in the zip;
- emits a plain document with no raw blocks (`:847-856`): `docxRawBlocks` is empty and the output contains no `<w:tbl`;
- pins the paragraph terminator convention (`:204-238`): each exported paragraph's `startIndex` sits on its `\r`, and the imported body paragraphs are anchored the same way;
- preserves mid-document section breaks (`:729-845`): the break stays on its paragraph and the body keeps its final `sectPr`, a break merges into an empty paragraph's `w:pPr` (`:755-775`) or next to an existing `w:jc` (`:777-794`), it imports as a marker paragraph right after its paragraph (`:796-806`), survives an edited-away marker (`:808-828`), and an image-only paragraph keeps its break verbatim (`:830-845`). The last of several body-level `sectPr` elements is reused (`:666-696`).

The image-source suite (`:859-1030`) covers the URL/UUID path:

- fetches an http(s) image source and embeds it without credentials (`:899-915`): a stubbed `fetch` returns PNG bytes, one media part is written, and the request carried `credentials: "omit"`;
- resolves a UUID image through the supplied resolver (`:917-923`): `resolveImage` is called with the id and `"UUID"`, and the returned data URL becomes the media part;
- reuses one media part for repeated identical sources (`:925-934`): two drawings share one `rId` and one `word/media` part;
- reports embedded images so the next save reuses their media and relationship (`:936-958`); the bearer token goes only to same-origin fetches (`:995-1002`), and an SVG response is rejected (`:1004-1019`);
- reports a failed fetch and still saves the rest of the document (`:960-975`): `onImageError` receives a message naming the URL, no media is written, and `word/document.xml` is still produced;
- rejects a response that is not an image and reports it (`:977-989`): an HTML body fails the magic-byte sniff and no part is written;
- throws for an unsupported source when no error handler is supplied (`:991-993`): `javascript:` rejects with `Cannot save image`;
- lists external hosts the save will download, skipping packaged images (`:1021-1030`): only http(s) drawings without an `__imageMeta` entry contribute a host.

### First-save confirmation

`DocxEditor` gates saving behind a confirmation (`DocxEditor.tsx`, `requestSave`; the dialog's `description` prop):

> "Saving rewrites the document text. Paragraphs, alignment, inline formatting and simple tables are editable; table/cell borders and shading are kept, but formatting inside edited table cells is simplified. Inline images are editable (move/resize); anchored or complex images are preserved unchanged. Images pasted or inserted in the editor are embedded in the file from base64, http(s)/blob URLs, or a resolvable UUID; SVG is not embedded. Saving will download an image from <host>. Other parts (styles, headers, media) are copied verbatim. The original is backed up as .bak on first save."

Details worth knowing:

- **The copy matches the editor's actual surface.** Simple tables are native and editable, and their table/cell borders and shading are kept; formatting *inside* an edited cell is simplified; inline images are editable (move/resize); anchored/complex images are preserved unchanged but read-only; pasted images are written into the package (base64 only); the package copy (styles, headers, media) and the one-time `.bak` are all stated.
- **It is once per file per page load, not once per document.** `confirmedSaveKeys` is a module-level `Set` (`DocxEditor.tsx:13`) keyed `` `${projectHost}::${projectRoot}::${path}` `` (`:37`). After a browser reload the warning appears again; it is not persisted.
- **A concurrent save waits on the open dialog** instead of stacking dialogs: `requestSave` returns the in-flight promise when `confirmRef.current` is non-null (`:141-142`). Cancel resolves `false`.
- **The Save button (`:162`) and the registered save handler (`:152-156`) both go through `requestSave`** (`:139`), so Cmd/Ctrl+S and close-confirm honour the warning too.
- `TODO.md:7061-7068` records the state: simple tables are native and editable, merged/complex tables and images are preserved, and the first-save confirmation has shipped.

The PDF editor has no such confirmation — page operations are structural, not lossy of text, and annotations are additive page-content marks.

## Wiring

### `PreviewSurface` (lazy, `editable` switch)

`PdfEditor` and `DocxEditor` are `lazy()` imports (`web/src/components/Preview/PreviewSurface.tsx:18-20`) so pdf-lib, pdf.js, JSZip and Univer stay in hashed chunks. Dispatch is a pair per kind on the `editable` prop (`PreviewSurface.tsx:119-122`):

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

1. **Dirty tracking up** — `onDirtyChange` from the viewer is lifted into `excelDirty` and reported to the parent (`FileTabContent.tsx:97-100`) so close-confirm and the desktop quit guard see the tab as dirty. For preview-only tabs this state, not `props.dirty` (Monaco's), is the source of truth.
2. **Save-handler registration on mount** — registered under `tabId ?? path` before the first edit (`:113-120`) and resolved lazily at call time, so the handler never captures a stale save closure. `saveEditorTab` refuses a preview-only tab with no registered handler, which is why registration cannot wait for a dirty change.
3. **Toolbar Save button** — rendered for Excel/PDF/DOCX (`:293-304`), disabled while clean, calling the viewer's save.
4. **Cmd/Ctrl+S** — a window `keydown` listener gated on `props.active` and a dirty buffer (`:122-136`). The active-tab gate matters because every editor tab stays mounted: without it a background dirty document would save on someone else's keystroke. App-level Cmd/Ctrl+S also reaches the same registered handler.

Both editors also register their save function through `onSaveRef` and fire `onSaveHandlerRegistered` (`PdfEditor.tsx:272-276`, `DocxEditor.tsx:152-156`), so the host can pick up the ref value without waiting for a re-render.

### Save path (shared)

Both editors call `apiClient.saveFileRaw` (`web/src/api/client.ts`), which:

- sends the bytes as `application/octet-stream`;
- passes `project_root`, `expected_hash`, `force` and — for a remote project — **`host` as a query parameter**;
- must **not** use the `/api/remote/{host}` proxy prefix. That prefix reads `?path=` as the project path, so the proxied form 403s on every save (same trap documented for Excel).

`HandleSaveFileRaw` dispatches on `host` to the remote transport and the remote server writes on its own filesystem. The remote `.bak` and hash checks are the same contract.

### Live rendering check (2026-11-08)

A temporary probe page on a private Vite port (driven through htrcli/Chrome CDP, page captured with `Page.captureScreenshot`) ran a real `.docx` through `docxToUniver` and `mountUniverDoc`, then exported it again. Results:

- **Native tables render.** The imported 2×2 table drew `Cell A` / `Cell B` / `Cell C` with borders, paragraph text above it, and the status bar counted 8 words.
- **Inline images do not render** with the installed OSS stack (`@univerjs/*` 1.1.0). A packaged `word/media/probe.png` (base64 drawing) and an editor-added blob-URL drawing both round-trip through the model and the exporter, but the screenshot contained zero red pixels where both should have painted. Registering the official `@univerjs/docs-drawing` + `@univerjs/docs-drawing-ui` 1.1.0 packages and calling `DocDrawingController.loadDrawingDataForUnit` changed nothing, and a search of the installed packages found no docs image painter (the only `drawImage` paths are sheet-cell and canvas-cache code). Image **display** is therefore a gap; save and round-trip work. `DocxEditor` surfaces it in the UI: when the document contains images it shows a banner — "This document contains N image(s). They are preserved and saved, but the editor cannot display them yet." The official `InsertDocDrawingCommand` was also exercised with `@univerjs/docs-drawing`, `@univerjs/docs-drawing-ui` and `@univerjs/drawing-ui` 1.1.0 registered: it returned `true` and changed the model, but painted no pixels and created no `<img>` (inline and floating layout types both tried). Inspecting the published 1.4.0 tarballs (`@univerjs/docs-ui`, `@univerjs/engine-render`) shows the same absence of a docs image painter, so a newer OSS version does not close the gap; it needs a renderer patch/fork or a package that ships one.
- **A drawing without `docTransform.positionH`/`positionV` crashes Univer's line layout** (`_positionLineDrawings`), which is why the importer always writes the full `docTransform`.
- **URL embedding works live.** A blob-URL drawing with no package part was fetched during export and written as `word/media/opencode-image-1.png` (75 bytes), the existing `word/media/probe.png` part was reused, and the saved package re-imported with its text intact.
- **Save round trip verified through the real handler and client helpers.** A scratch `ocode serve` (no auth, `127.0.0.1:4280`, cwd `/tmp/docx-rt`) accepted saves from the page. The first run replicated the PUT (5,178 bytes, table + 2 media parts). The second loaded `api/client.ts` with an empty `BASE` (document path rewritten to `/`), set the backend to the scratch server, and called the real `api.saveFileRaw` / `api.fetchFileRaw`: `saved: true`, 4,816 bytes fetched back, `word/media/probe.png` + `word/media/opencode-image-1.png`, both relationships, and a re-import with 2 drawings and the body text. `unzip -l` on disk confirmed the parts.

## Limits and known gaps

Tracked in `TODO.md` under "Open: in-app Word/PDF editing" (`TODO.md:7061-7068`):

- **PDF: no text editing (treated as final-form), and annotations are flattened, not removable.** Page ops plus highlight/text-note tools; Highlight and Add-note use the drag-selected region when the selection belongs to the selected page (a zero/small selection falls back to a default rect). Because the marks are drawn into the page content stream, a reader cannot un-draw them. Annotation placement is **rotation-aware**: the pdf.js viewport transform captured per page (`viewportRefs`) is inverted in `canvasToPdfRect`, so a highlight or note lands on the dragged region even when the page is rotated. Both annotation actions are additionally held while a thumbnail render pass is in flight (see the render-finished guard above), so a mark can never be mapped through a previous pass's viewport.
- **PDF print reads the saved file**, not the live in-memory document, so unsaved page operations and annotations do not appear in print.
- **Word: paragraphs, alignment, a bounded set of inline run properties, simple tables and inline images are editable; merged/complex tables, content controls and anchored/VML images are preserved but read-only.** Editable: run bold, italic, underline, strike, colour, font family/size, highlight/shading, sub/superscript, paragraph alignment, and the text inside a **simple** table cell (via native Univer tables). Preserved verbatim but **not editable**: merged/nested/multi-paragraph tables, block-level content controls and anchored/VML/mixed image paragraphs, each represented in the editor by a **placeholder marker paragraph** (`[Table preserved]` / `[Image preserved]` / `[Content preserved]`, `docxData.ts:88-95`) and restored on export by matching that marker back to `body.docxRawBlocks` — plus, through the copied package, media, relationships, styles, headers/footers and the `w:sectPr` page setup that references them. **Table/cell borders and shading are preserved; run formatting inside an edited cell is simplified:** a native table round-trips cell text, column widths and the original `w:tblPr` / per-row `w:trPr` / per-cell `w:tcPr` (borders, shading, `w:tblStyle`), captured at import into the document-level `__tableStyles` map and re-emitted by `renderTableXml`, which falls back to `<w:tblPr/>` only when the source had none. Bold/italic/colour/font/size inside a `w:tc` are not read, so edited cell text is written as a single unstyled run. Inserting/deleting text before a preserved block is safe (the marker moves with the text and the block follows it), but editing a marker line itself detaches that block, which is then appended at the end of the body rather than dropped; duplicate markers consume the block list in document order, so they can re-associate blocks. Inline images are native and editable (move/resize; the resize is written back to `wp:extent`); a pasted or inserted image is written into `word/media/` with a new relationship on save (base64, http(s)/blob URLs, or a resolvable UUID; SVG is not embedded). Still dropped: character caps/smallCaps, character spacing/kerning, language, paragraph/run borders, theme colours, paragraph spacing/indentation/named styles, list/numbering formatting (flattened to text), hyperlink targets, and section breaks inside table cells (paragraph-level section breaks are preserved, see Import). (`TODO.md:7068(d)` previously listed paragraph alignment as dropped and tables/images among the losses; both are stale — the importer/exporter map `w:jc`, round-trip the run properties above, import simple tables natively and preserve merged/complex raw blocks, so this page is authoritative.)
- **No external Word client has validated the output, and native inline images do not paint.** LibreOffice/`soffice` and python-docx are not installed, so no saved `.docx` has been opened in Word/Pages/LibreOffice. `docxData.test.ts` plus the live check above are the evidence: a native table renders and edits in the Univer mount, but an inline image is imported, laid out (it reserves space), saved and re-imported without ever being painted. That catches malformed XML and lost parts, but not a Word repair prompt, layout differences, or schema-order problems a real client rejects. Treat a first real-client open as unverified.
- **Close-confirm / quit-guard is only partially integrated.** Dirty state and the save handler are wired (above), but the TODO still lists PDF/DOCX close-confirm and the desktop quit guard as not fully verified end-to-end.
- **The `.bak` never refreshes** — a product decision shared with Excel, not an editor bug.
- **Unverified live**: the Word save **was** exercised end-to-end against a no-auth scratch server (above), and table rendering was checked live; inline images still do not paint. The PDF drag-select path is covered by mapper unit tests only, and no browser-level drag or PDF save round trip was exercised (`TODO.md:7064`).

## Related

- `concepts/excel-preview-univer-sheet.md` — the raw-save contract in full (hash, `force`, 413 cap, atomic write, remote `?host=` rule) and the Excel viewer that established `excelSaveRef` / `excelDirty`.
- `gotchas/files-tab-preview-only-routing.md` — why `.pdf`/`.docx` never open in Monaco, and the fetch-skip / watcher guards a preview-only tab depends on.
