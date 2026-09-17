---
type: Gotcha
title: PDF preview fails on WebKit with `undefined is not a function (near '...e of t...')`
description: WebKit ReadableStream async-iterator gap kills pdf.js text layer — feature-detected shim fix
resource: web/src/lib/readableStreamAsyncIterator.ts; web/src/components/Preview/PdfViewer.tsx
tags:
  - gotcha
  - web
  - pdf
  - webkit
  - safari
  - wkwebview
  - desktop
  - pdfjs
  - readablestream
  - asynciterator
  - async-iteration
  - feature-detection
  - typescript
  - tsgo
timestamp: 2026-09-16T13:16:00Z
---
# PDF preview fails on WebKit with `undefined is not a function (near '...e of t...')`

**Type:** Gotcha  
**Description:** pdf.js v6 `PDFPageProxy.getTextContent()` uses `for await … of` on a `ReadableStream` to build the text layer. WebKit (Safari, WKWebView) has never implemented `ReadableStream.prototype[Symbol.asyncIterator]`, so the call throws a `TypeError` with WebKit's opaque wording. The rasterised canvas paints first, then the panel flips to `PDF failed: undefined is not a function (near '...e of t...')`. Chrome/Firefox are unaffected. Fixed via a feature-detected async-iterator shim installed at module scope in `PdfViewer.tsx`.  
**Resource:** web/src/lib/readableStreamAsyncIterator.ts; web/src/components/Preview/PdfViewer.tsx  
**Tags:** gotcha, web, pdf, webkit, safari, wkwebview, desktop, pdfjs, readablestream, asynciterator, async-iteration, feature-detection, typescript, tsgo  

---

# PDF preview fails on WebKit with `undefined is not a function (near '...e of t...')`

Fixed **2026-09-16**. See `CHANGES.md` ("PDF preview no longer dies with …") for the changelog entry.

## KEY LESSON / GENERAL RULE

When a **platform capability gap** exists in the engine the desktop shell ships
(WKWebView), the failure surfaces as an **opaque WebKit error string** from
inside a third-party dependency — not from your own code. Two corollaries:

1. **Verify capability assumptions in WKWebView, not just Chrome, before
   shipping.** A feature that works in Chrome during development can fail
   silently in the desktop shell. The WebKit implementation gap is invisible at
   authoring time.
2. **TypeScript's DOM libs (`lib.dom.asynciterable`) type
   `ReadableStream[Symbol.asyncIterator]` as always present.** `tsgo --noEmit`
   (or `tsc`) **cannot catch** this class of bug — the type says the method
   exists even though the engine does not implement it. **Runtime
   feature-detection is required.**

## Diagnostic trick: drive a real WKWebView from Swift on macOS

You can reproduce WebKit-only web bugs without Safari by loading the URL in a
small Swift script:

```swift
import WebKit

let url = URL(string: "http://localhost:5173")!  // or the built bundle server
let config = WKWebViewConfiguration()
let webView = WKWebView(frame: .zero, configuration: config)
webView.load(URLRequest(url: url))

// Wait for the page to settle, then probe:
DispatchQueue.main.asyncAfter(deadline: .now() + 5) {
    webView.evaluateJavaScript("""
        typeof ReadableStream.prototype[Symbol.asyncIterator]
    """) { result, error in
        print("asyncIterator:", result ?? error ?? "nil")
        exit(0)
    }
}

RunLoop.main.run()
```

This runs the real WebKit engine (not Safari) and is useful for diagnosing
any WebKit-only web bug — capability gaps, CSS quirks, API differences.

## What happened

The sidebar / Files-tab PDF preview in the web UI paints the PDF page to the
canvas, then the panel immediately flips to:

```
PDF failed: undefined is not a function (near '...e of t...')
```

The canvas paints because rasterisation (`render()`) completes before the text
layer is built. The error comes from `PDFPageProxy.getTextContent()` in
pdf.js v6 (`pdfjs-dist` 6.3.289), which assembles the text layer with:

```js
for await (const chunk of this.streamTextContent()) { … }
```

This is async iteration over a `ReadableStream`. Chrome and Firefox implement
`ReadableStream.prototype[Symbol.asyncIterator]`; WebKit does not — it never
has. The `for await` construct calls `[Symbol.asyncIterator]()`, gets
`undefined`, and throws `TypeError: undefined is not a function`. WebKit's
error message truncates to `…near '…e of t…'` (the `of this` part of the
source), which is why the user sees that specific wording.

## The fix

### 1. Feature-detected shim (`web/src/lib/readableStreamAsyncIterator.ts`)

`ensureReadableStreamAsyncIterator()` checks whether the engine already has a
native implementation. If not, it installs a spec-shaped
`ReadableStream.prototype[Symbol.asyncIterator]` backed by `getReader()`, with
`next`/`return`/`throw` and proper reader cancellation on early `break`. When
the native method exists, the shim is a no-op (`replacedNative=false`).

### 2. Module-scope call in `PdfViewer.tsx`

```ts
// web/src/components/Preview/PdfViewer.tsx — module scope, before any PDF loads
ensureReadableStreamAsyncIterator();
```

This guarantees the shim is installed before pdf.js can call `getTextContent()`.
Chrome/Firefox keep their native implementation; the test suite verifies
`replacedNative=false` on un-shimmed engines.

## Tests

- **`web/src/lib/readableStreamAsyncIterator.test.ts`** — verifies the bug
  (un-shimmed `for await` rejects with `TypeError`), then the shim yields all
  chunks, is a no-op when present, cancels the reader on early `break`, and
  propagates stream errors.
- **`web/src/components/Preview/PdfViewer.webkitStream.test.tsx`** — deletes
  the method to simulate WebKit, then importing the viewer module must install
  it. Fails if the call is removed from `PdfViewer.tsx`.
- **End-to-end in WKWebView** — `asyncIterator` is `undefined` before the
  module loads, `function` after; `render` / `getTextContent` / text-layer
  spans all resolve (canvas 918×1188); no `PDF failed`.

## Maintenance note

If pdf.js is upgraded, re-check whether `streamTextContent()` still uses
`for await … of` on a `ReadableStream`. If pdf.js switches to a different
iteration mechanism, the shim may become unnecessary — but the feature-detect
is harmless to keep as a belt-and-suspenders safeguard for any future dependency
that makes the same assumption.
