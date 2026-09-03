# Sidebar PreviewHost Design (2026-09-03)

Goal: the sidebar browser becomes a Cowork-style artifact panel — web
browsing (as before) plus file preview/editor, Word/PowerPoint/PDF
viewers with multi-page navigation, clickable/zoomable mermaid diagrams,
highlight → LLM context, and AI tools that drive the panel.

## Approaches considered

1. **Native embed (OnlyOffice/Collabora iframe).** Pixel-perfect docx/pptx
   fidelity, real animations, collaborative editing. Rejected for v1:
   heavy server dependency, licensing/ops cost, breaks offline dev.
2. **All-client lightweight (mammoth + pptxjs + raw PDF embed).** No
   server work, but pptxjs is unmaintained and mammoth loses fidelity.
   Rejected: violates the stable-maintained-libs requirement.
3. **Hybrid (chosen).** BrowserPanel untouched under a Browser tab; new
   Preview tab with per-kind renderers on stable libs (pdfjs-dist,
   docx-preview, mermaid, jszip), a Monaco editor for text/code, server
   raw-bytes + OS-open endpoints, and a sentinel-based AI activation path
   over the existing chat stream (no new SSE channel). Native playback is
   covered by "Open in app" (OS default: PowerPoint/Keynote/Word).

## Components

- `PreviewHost` — Browser | Preview tab shell in the side pane.
- `PdfViewer` (pdf.js, canvas + selectable text layer, pager, ←/→).
- `DocxViewer` (docx-preview paginated sections, selectable).
- `PptxViewer` (jszip slide-XML parse: filmstrip + slide + Present
  fullscreen click-through; per-click stepped fade/slide reveals
  approximate entrance animations).
- `MermaidViewer`/`MmdViewer`/`MarkdownViewer` (mermaid SVG, zoom
  buttons + ctrl-wheel, g.node click → branch toolbar with Copy / Ask AI
  about this branch; `click <href>` opens the linked file in PreviewHost).
- `TextViewer` (Monaco FileEditor + save bar), `ImageViewer` (blob URL).
- `SelectionToolbar` + `usePreviewSelection` — mouseup highlight inside
  the renderer → floating Copy / Ask LLM → `ocode:preview-context`.
- `usePreviewActivation` — merges `ocode:open-preview` events and
  `PREVIEW_OPEN:` sentinels scanned from the active session transcript.
- Chat: `ChatInput` preview chip (`@path (label): "excerpt"`), App opens
  the side pane on fresh activations and clears the chip on tab switch.
- FileTree: "Preview in sidebar" menu item (previewable kinds only).

## Backend

- `GET /api/files/raw` — allowlisted binary preview bytes (32 MiB cap,
  project-root anchoring, `Cache-Control: no-store`).
- `POST /api/files/open` gains `mode: editor|os` (os forces
  `systemOpener`) and `project_root` anchoring via `resolveOpenPath`.
- `preview_open` agent tool — validates workspace-relative path +
  allowlist, returns `PREVIEW_OPEN:<path>|kind=<k>|page=<n>`.

## Error handling

Viewers show inline errors (fetch/parse failures) and never break the
Browser tab; oversized/unpreviewable files are rejected server-side with
400s; OS-open failures surface in the PreviewHost toolbar transiently.

## Testing

- Go: `preview_test.go` (tool validation matrix), `handler_preview_test.go`
  (raw serve/reject, open mode validation, project-root anchoring).
- Web: `previewKind.test.ts` (kind map, sentinel parse, event contracts);
  full suite green (80 files / 692 tests), `tsc --noEmit` clean.

## Follow-ups (out of scope)

OnlyOffice/Collabora native embed, docx tracked-change editing,
pptx text editing, slide thumbnail rastering, per-branch diagram
deep-links (`file.mmd#node-id`).
