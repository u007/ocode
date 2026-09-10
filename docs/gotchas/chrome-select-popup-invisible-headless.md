---
type: Gotcha
title: Headless Chrome <select> Dropdown Invisible and Unreachable via CDP
description: 'Gotcha: in the embedded Chrome tab, clicking a <select> or pressing ArrowDown appeared to do nothing. Headless Chrome draws the native select popup as a separate OS widget that the screencast never captures and page-session Input.dispatchKeyEvent never reaches.'
tags:
  - gotcha
  - browse
  - cdp
  - chrome
  - select
  - headless
timestamp: 2026-09-10T00:00:00Z
---
## Problem

In the embedded browser tab (headless Chrome over CDP), clicking a `<select>`
focused it but no dropdown appeared, and ArrowDown / Enter never changed its
value.

## Root cause

`--headless=new` renders the native `<select>` popup as a separate popup
widget, not part of the page frame. `Page.startScreencast` only captures the
page, so the open popup is invisible. Keys sent with `Input.dispatchKeyEvent`
on the page session go to the page, not to the popup, so arrow/Enter are
swallowed. The popup even stalls later `Runtime.evaluate` calls while open.
Escape via CDP does not close it either.

Verified 2026-09-10 against Chrome 152 on macOS: click → `activeElement` is
`SELECT`, value unchanged after ArrowDown + Enter, frame shows no popup.

## Fix

`Manager.injectInPageSelectPicker` registers a
`Page.addScriptToEvaluateOnNewDocument` (with `runImmediately`) on every
attached page session that adopts a stylesheet:

```css
select, ::picker(select) { appearance: base-select; }
```

This switches every `<select>` to Chrome's customizable select, whose picker
is rendered in the page's top layer. The screencast shows it and mouse /
ArrowDown / Enter work as ordinary in-page DOM events. Gated integration test:
`TestIntegrationSelectPickerInPage` (needs `OCODE_CHROME_PATH`).

## Trade-off

The picker uses base-select's default look instead of the OS-native menu, and
pages that style `select { appearance: none }` for a custom chevron get the
base-select rendering instead. Accepted: a visible, keyboard-reachable picker
beats an invisible native one in a remote-viewed browser.

## Other native popups (probed 2026-09-10, Chrome 152 macOS)

- `<input type=date|time>`: the calendar/clock popup (icon click) is the same
  invisible off-frame widget, and keys sent while it is open land in it
  (ArrowDown in the invisible calendar jumps a week). The injected
  stylesheet therefore also hides `::-webkit-calendar-picker-indicator`, so
  the popup cannot be opened by mouse; the value stays editable per segment
  (click a segment, ArrowUp/Down or type digits). Verified.
- `<input list=…>` (datalist): suggestion popup invisible; typing works,
  keyboard pick does not (see
  `chrome-headless-mac-key-redispatch-freeze.md`).
- `<input type=color>`: click focuses the input, no picker opens in headless,
  value cannot be changed by mouse or keyboard. Only a page-side custom
  picker would fix this. Not done.
- `<select multiple>` / `size>1`, `<dialog>`, `popover`, `<input type=file>`
  (Page.fileChooserOpened intercept): all in-page already, unaffected.
- The ~11 s "stall after Escape" seen while probing these was the
  key-redispatch freeze above, not the pickers.
