---
type: Gotcha
title: 'MDX Preview: Rendered as Markdown, Never Evaluated'
description: 'Gotcha: MDX preview renders as Markdown (never evaluates JS for security); editor uses Monaco mdx grammar, preview uses markdown kind.'
timestamp: 2026-09-23T08:19:01Z
---
# MDX Preview: Rendered as Markdown, Never Evaluated

**Type:** Gotcha  
**Description:** Two rules for `.mdx` files: (a) preview renders MDX as Markdown via react-markdown + remark-gfm and NEVER evaluates it (security — would execute arbitrary JS); (b) the editor uses Monaco's `mdx` grammar while the preview uses the `markdown` kind.  
**Resource:** web/src/lib/previewKind.ts, web/src/lib/editorLanguage.ts, web/src/components/Preview/MarkdownViewer.tsx  
**Tags:** gotcha, web, mdx, preview, markdown, security, monaco, editor, language  

---

Since 2026-09-21, the ocode web/desktop file preview and editor support `.mdx` files. Two rules govern how they are handled, and both are load-bearing:

## 1. Preview renders MDX as Markdown — never evaluates it

`MarkdownViewer` (`web/src/components/Preview/MarkdownViewer.tsx`) renders `.mdx` files through `react-markdown` + `remark-gfm` only. This means:

- ESM `import`/`export` lines render as source-level text
- JSX components render as source-level text (or are dropped/escaped by react-markdown)
- No JavaScript is executed

**Reason:** Evaluating MDX would execute arbitrary JavaScript from a previewed file. Since previewing is a read-only operation (no user interaction with the code), there is no justification for running its code.

This applies to **all** surfaces that render MDX through `MarkdownViewer`:
- Sidebar `PreviewHost` (manual activation or `preview_open` tool)
- Files-tab Edit/Preview/Split mode switch (preview mode)
- Session-level Preview sub-tab

## 2. Editor uses `mdx` grammar; preview uses `markdown` kind

The two surfaces use different language identifiers for the same file extension:

| Surface | Language/Kind | Source |
|---------|--------------|--------|
| Editor (Monaco, FileEditor / FileTabContent edit mode) | `mdx` | `web/src/lib/editorLanguage.ts` `languageForFile` → Monaco 0.53 bundled `mdx` basic-language |
| Preview (`MarkdownViewer`, `previewKindForPath`) | `markdown` | `web/src/lib/previewKind.ts` specialized renderer set → `".mdx": "markdown"` |

Why different? The preview pipeline treats MDX as a markdown-family format (render with react-markdown + remark-gfm). The editor pipeline uses Monaco's grammar for syntax highlighting — Monaco 0.53 ships a bundled `mdx` basic-language, so that is what the editor uses. This is an intentional divergence, not a bug.

## Extension-to-kind mapping

`kindByExt` (`web/src/lib/previewKind.ts`) is now a **specialized renderer
map** (not a general allowlist):
- `.md`, `.markdown`, `.mdx` → `"markdown"` (previewable via `MarkdownViewer`)
- `.md`, `.markdown`, `.mdx` → **NOT** in `PREVIEW_ONLY_KINDS` (Files tab shows Edit/Preview/Split mode switch, default Edit)

`isMarkdownPath` (`web/src/lib/previewKind.ts`) returns `true` for `.md`, `.markdown`, and `.mdx`.

`languageForFile` (`web/src/lib/editorLanguage.ts`):
- `.md`, `.markdown` → `"markdown"`
- `.mdx` → `"mdx"`
- `.markdown` → `"markdown"` (previously fell through to plaintext)

## Server parity

- `internal/tool/preview.go` `previewOpenKinds`: specialized renderers +
  markdown family (`.mdx`/`.markdown` → `"text"`); `previewNonTextExts` holds
  the binary denylist (mirrors `web/src/lib/previewKind.ts` `NON_PREVIEWABLE_EXTS`)
- `internal/server/handler_files.go` `previewRawTypes`: `.markdown"` and ".mdx"`
  both `text/markdown; charset=utf-8`

**Sync contract:** specialized renderer extensions (here `.mdx`, `.markdown`)
must appear consistently in `kindByExt`, `previewOpenKinds`, and `previewRawTypes`.
The binary denylist (`NON_PREVIEWABLE_EXTS`/`previewNonTextExts`) is maintained
independently and need not appear in `previewRawTypes`.

## Tests

- `web/src/lib/editorLanguage.test.ts` (new) — `.mdx` → `mdx`, `.md`/`.markdown` → `markdown`
- `web/src/lib/previewKind.test.ts` — `.mdx` → markdown, new `isMarkdownPath` describe
- `web/src/components/Files/FileTabContent.test.tsx` — "treats .mdx as markdown too"
- `internal/tool/preview_test.go` — accepts `notes.markdown`/`docs/page.mdx`
- `internal/server/handler_preview_test.go` — markdown-family content types

Mutation-verified: removing the `kindByExt` `.mdx` entry causes 3 tests to fail.

## Related

- `docs/gotchas/files-tab-preview-only-routing.md` — Files-tab routing, markdown/MDX mode switch, open-classification model, UTF-16 BOM transcoding
- `docs/superpowers/specs/2026-09-10-preview-multipurpose-design.md` — design spec, updated for `.mdx`
