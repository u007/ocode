---
type: Design
title: Web Chat Message Copy Menu — Design
description: 'Design spec for the web chat per-block copy menu: split Copy control, rendered-vs-raw clipboard paths, shared clipboard helper.'
tags:
  - web
  - chat
  - clipboard
  - design-spec
timestamp: 2026-09-25T04:49:17Z
---
# Web Chat Message Copy Menu — Design

## Problem
Web chat messages are not reliably copyable. There is no per-block copy
affordance, and copying a rendered assistant message by hand picks up
renderer-visible text rather than the original Markdown. Users want:
- every visible chat block to offer Copy;
- the default Copy to put the rendered ("as it is") text on the clipboard;
- a dropdown option to copy the raw Markdown/source instead.

## Goals
- Add a split Copy control to every visible chat block: user messages,
  assistant Markdown, thinking blocks, tool calls, tool results, status
  blocks, and notice blocks (including collapsed notice groups).
- Default click copies rendered text; the chevron opens a menu with a
  raw-source copy item.
- The control works identically for committed transcript rows and the live
  streaming tail.
- Make the clipboard write reliable in the desktop WKWebView and insecure
  LAN origins, matching the proven ShareDialog fallback.

## Non-goals
- No server/API change.
- No change to how messages are stored or streamed.
- No global toast system; feedback stays inline on the control.
- No new markdown renderer or changed Markdown behaviour.

## Decisions
- **Approach: one shared block-level control.** A single
  `BlockCopyControl` component is embedded in the reusable chat blocks, so
  both committed and live render paths get it without per-call-site
  duplication.
- **Default = rendered text.** "Copy as it is" means the text the browser
  paints, extracted from the rendered DOM with the existing
  `renderedSpeechText()` helper (block boundaries preserved, markdown
  syntax removed, control affordances excluded).
- **Raw item is labeled per block.** Assistant and user text say
  "Copy as raw Markdown"; thinking/tool/status/notice blocks say
  "Copy as raw source" so the label never claims Markdown where there is
  none.
- **Clipboard helper is extracted**, not duplicated. `ShareDialog`'s
  private `copyTextToClipboard` moves to `web/src/lib/clipboard.ts` and
  `ShareDialog` imports it.

## Architecture
- `web/src/components/Chat/BlockCopyControl.tsx` (new)
  - Split button: main Copy button plus a chevron trigger.
  - Main button calls `copyTextToClipboard(resolveRendered())`, where
    `resolveRendered()` is the caller's `getRenderedText()` when it returns
    non-empty, otherwise `rawText`.
  - Chevron opens a Radix `Popover` (the only menu primitive in this repo)
    containing one item that calls `copyTextToClipboard(rawText)`.
  - Props: `rawText: string`, `getRenderedText?: () => string`,
    `rawLabel?: string` (default "Copy as raw Markdown"),
    `ariaLabel?: string`.
  - The whole control carries `data-speech-exclude` so it is never included
    in rendered-text extraction or spoken output.
- `web/src/lib/clipboard.ts` (new)
  - `copyTextToClipboard(text: string): Promise<boolean>` using
    `navigator.clipboard.writeText` when available, then the focus-safe
    scratch-textarea `document.execCommand("copy")` fallback. Trusts the
    fallback only when the scratch field actually holds focus; always
    restores the previous focus and removes the scratch node; returns false
    for empty text and for unrecoverable failures.
- `web/src/components/Layout/ShareDialog.tsx`
  - Delete the private helper and import from `lib/clipboard.ts`; keep
    existing copy/error behaviour and tests passing.

## Raw-source mapping
| Block | Rendered (default) | Raw |
|---|---|---|
| Assistant Markdown | `renderedSpeechText` over the rendered Markdown subtree | original `content` |
| User message | verbatim rendered text | original `content` |
| Thinking | reasoning text | reasoning text |
| Tool call / tool result | visible header, args and output text | args followed by output, as source text |
| Tool result message (role `tool`) | same as tool result | output source |
| Status / Notice | notice text | same text |
| Notice group | all underlying notices joined, ignoring collapsed state | same |
| Live text/tool parts | current snapshot | `part.text` / `part.command` / `part.output` |

## Placement
- `AssistantText`: add the control to the existing footer row beside Speak.
- `UserBubble`: add it beneath the bubble, aligned right.
- `ThinkingBlock`: add it to the header row beside Speak.
- `ToolBlock`: add it to the header row.
- `StatusBlock`, `NoticeBlock`: add it inline at the right edge.
- `NoticeGroupBlock`: add it to the collapsed header row.
- Each block only renders the control when it has non-empty copyable
  content.

## Accessibility and UX
- Main button: `aria-label` defaulting to "Copy message"; visible
  `Copy` label/icon. Chevron: `aria-label="More copy options"`,
  `aria-haspopup="menu"`, `aria-expanded`.
- Popover content uses `role="menu"` and the item `role="menuitem"`.
- Radix handles Escape and outside-click dismissal; selection closes the
  menu and returns focus to the trigger.
- Success shows an inline "Copied" state for about 1.5 s, then resets.
- Failure shows an inline "Copy blocked" state with a title explaining the
  user can select the text and press Ctrl/Cmd+C; it resets after a few
  seconds. There is never a false success state.

## Error handling
- Clipboard rejection falls through to the execCommand fallback.
- If both paths fail, no text is reported as copied; the control shows the
  blocked state.
- Empty raw/rendered values return false and keep the idle state.

## Testing
- `web/src/lib/clipboard.test.ts`: navigator path, navigator rejection then
  fallback, fallback focus requirement, empty string false, focus restore.
- `web/src/components/Chat/BlockCopyControl.test.tsx`: main button copies
  rendered text and not raw; menu item copies raw text; copied state shows
  then clears; failure state shows; menu semantics.
- `web/src/components/Chat/TurnParts.test.tsx`: Thai/thinking/tool/status/
  notice blocks expose the control; tool raw text contains args and output.
- `web/src/components/Chat/MessageBubble.copy.test.tsx`: assistant Markdown
  default copy strips Markdown (for example `# Title` becomes `Title`),
  raw copy returns the original Markdown, and user messages are covered.
- `web/src/components/Chat/ChatPanel.test.tsx`: a live streamed text block
  exposes the control.
- Mutation-verify the core assertions (rendered-vs-raw, fallback path) by
  temporarily reverting each fix and confirming the test fails.

## Validation
- `cd web && npm run typecheck` and `npm run build` clean.
- Focused Vitest suites green.
- Real-browser check with Playwright against a freshly built binary:
  default Copy puts rendered text on the clipboard, the menu item puts raw
  Markdown, and the control works on a live streaming block.
- Documentation: `skills/ocode-web/SKILL.md` file map/regression item and a
  `CHANGES.md` entry.

## Risks and open questions
- Rendered-text extraction is DOM-based; virtualized rows must be mounted
  for their control to be clickable, which is already true by definition
  because the user can see them.
- Copying very large tool outputs may be slow but is bounded by the
  existing rendered output.