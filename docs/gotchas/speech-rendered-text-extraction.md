---
type: Gotcha
title: Speech Rendered Text Extraction — DOM, Not Markdown Source
description: 'New gotcha: speech text must be extracted from rendered DOM, not markdown source, to avoid audible formatting markers and lost block breaks.'
tags:
  - gotcha
  - TTS
  - speech
  - DOM-extraction
  - web-frontend
  - markdown
timestamp: 2026-09-16T12:34:11Z
---
# Speech Rendered Text Extraction — DOM, Not Markdown Source

**Status:** Active  
**Last Updated:** 2026-09-16

## The Gotcha

When implementing speech/TTS text extraction in the ocode web frontend, always extract spoken text from the **rendered DOM**, never from the raw markdown source string. This is a common pitfall because the raw markdown is easier to access and looks "close enough" to spoken text — but it produces two classes of audible artefacts that degrade the user experience.

## Why Markdown Source Fails

### 1. Formatting markers are read aloud

Raw markdown contains syntax characters that the browser renderer strips before paint. If the extractor reads source text instead of rendered `textContent`, the TTS engine speaks these characters literally:

| Markdown source | What gets spoken (wrong) | What should be spoken |
|---|---|---|
| `# Heading` | "hash Heading" | "Heading" |
| `**bold text**` | "asterisk asterisk bold text" | "bold text" |
| `` `code` `` | "backtick code backtick" | "code" |
| `[link](url)` | "open bracket link close bracket open paren url close paren" | "link" |
| `- item` | "hyphen item" | "item" |

### 2. Block-level structure is lost

Markdown source separates paragraphs and code blocks with blank lines and fence markers, but these don't map cleanly to spoken pauses. The rendered DOM uses distinct block-level elements (`<p>`, `<pre>`, `<li>`) whose boundaries can be detected and converted into natural pauses or line breaks. A source-level stripper has no reliable way to know where one paragraph ends and the next begins, so adjacent paragraphs merge into one run-on sentence.

## The Correct Approach

Use the DOM helpers in `web/src/components/Speech/speechUtils.ts`:

| Helper | Purpose |
|---|---|
| `renderedSpeechText(root)` | Walk a rendered DOM subtree; collapse whitespace; skip `[data-speech-exclude]`, `aria-hidden`, script/style/svg; insert line breaks at block-level tags. Used by per-message Speak button. |
| `renderedSpeechTexts(root)` | Collect all `[data-speech-content]` blocks in DOM order into a single string. Used by "Speak visible" viewport button. |
| `lastRenderedSpeechText(root)` | Return the last `[data-speech-content]` block. Used by auto-speak at-bottom. |

## Component Contract

Every markdown-rendered message block must carry these data attributes:

- **`data-speech-content`** — marks the element whose text content should be spoken. Set on the markdown subtree (e.g. `AssistantText`'s rendered block).
- **`data-speech-exclude`** — marks child elements inside a speech-content subtree that must NOT be spoken (e.g. the Speak button itself, which would have its own label read aloud without this attribute).

## Scope Note

`ThinkingBlock` reasoning and terminal selections are **plain text**, not markdown-rendered content. They are passed through unchanged — no DOM extraction needed for those surfaces.

## Cross-Reference

The full design rationale and implementation details live in the TTS specification:
`superpowers/specs/2026-09-09-tts-speech-playback-design.md` → §10.1 "Rendered-Text Extraction (DOM)"