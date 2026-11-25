---
type: Concept
title: Clickable HTTP(S) URL chooser (web/desktop)
description: All ordinary clickable HTTP(S) links in the web/desktop SPA enter one cancelable app-level chooser that opens either in the originating project's embedded Browser-panel tab or in the external OS browser; covers first-wins refusal, owner metadata, remote routing, deliberate direct-launch exceptions and verification status.
resource: web/src/lib/externalLinks.ts; web/src/lib/urlOpenContext.tsx; web/src/components/common/URLOpenChooserHost.tsx; web/src/components/Layout/AppBridge.tsx; web/src/components/Chat/OwnerChatPanel.tsx; web/src/components/Preview/OwnerMarkdownViewer.tsx; web/src/components/common/URLOpenDialog.tsx
tags: [web, desktop, links, browser, mcp, security, ux]
timestamp: 2026-11-25T03:09:40Z
status: active
---
# Clickable HTTP(S) URL chooser (web/desktop)

## Policy

Except for the deliberate direct controls listed below, every clickable `http:`
or `https:` URL in the ocode web/desktop SPA asks where to open it:

- **Open in browser tab** — an embedded Browser-panel tab owned by the project
  that owns the clicked surface. A global link with no owner falls back to the
  active project; with no active project, only the external choice remains.
- **Open in external browser** — the OS browser.

The chooser is the confirmation boundary for links originating in React,
Monaco, ghostty-web and sandboxed MCP Apps widgets. An ordinary link
provider never opens an HTTP(S) URL directly.

## Flow

1. **Request.** `requestURLOpen` (`web/src/lib/externalLinks.ts:46`) validates
   the value with `isHTTPURL` (`web/src/lib/externalLinks.ts:30`) and accepts only
   `http:`/`https:`. It dispatches a cancelable `ocode:open-url` `CustomEvent`
   (`OPEN_URL_EVENT`, `web/src/lib/externalLinks.ts:14`) whose detail is
   `{ url, projectPath?, host?, refused? }` (`OpenURLDetail`,
   `web/src/lib/externalLinks.ts:16`). A non-HTTP(S) value is ignored.
2. **Listener contract.** `dispatchEvent` returns `false` only when a listener
   called `preventDefault`. If no listener handled the request,
   `requestURLOpen` throws rather than silently dropping the click or bypassing
   the chooser. If the listener marks `detail.refused`, it throws
   `requestURLOpen: a URL chooser is already open`.
3. **First request wins.** `URLOpenChooserHost` owns the sole listener
   (`web/src/components/common/URLOpenChooserHost.tsx:15`, handler at
   `web/src/components/common/URLOpenChooserHost.tsx:31`). `AppBridge` mounts it
   once alongside `SessionTabSync`
   (`web/src/components/Layout/AppBridge.tsx:11`,
   `web/src/components/Layout/AppBridge.tsx:12`), and `HomeApp` uses that bridge
   (`web/src/App.tsx:1758`). The host validates again
   (`web/src/components/common/URLOpenChooserHost.tsx:33`), prevents the event
   (`web/src/components/common/URLOpenChooserHost.tsx:35`) and, when a chooser is
   already pending, sets `detail.refused = true` and returns without changing
   `pendingURL` (`web/src/components/common/URLOpenChooserHost.tsx:37`). Otherwise
   it stores the request
   (`web/src/components/common/URLOpenChooserHost.tsx:41`). A newer request cannot
   replace the URL already under the cursor, so a hostile widget cannot swap the
   destination after the user opens the chooser.
4. **Choose.** `URLOpenDialog` (`web/src/components/common/URLOpenDialog.tsx:12`)
   is a Radix dialog showing the full URL and the two destinations. Cancel,
   Escape and outside-overlay dismissal clear the pending URL and open nothing.
5. **Embedded destination.** `openURLInBrowserTab`
   (`web/src/components/common/URLOpenChooserHost.tsx:47`) resolves an owner by
   exact path **and** host
   (`web/src/components/common/URLOpenChooserHost.tsx:52`); a request without an
   owner uses the active project
   (`web/src/components/common/URLOpenChooserHost.tsx:56`). `useBrowserTabs` is
   bound to that owner path
   (`web/src/components/common/URLOpenChooserHost.tsx:21`), then
   `openOwnerBrowserTab()` creates the strip tab and `browserActions.open` loads
   the URL (`web/src/components/common/URLOpenChooserHost.tsx:58`,
   `web/src/components/common/URLOpenChooserHost.tsx:59`). The host clears the
   pending request, selects the owner project if it was not active, and queues
   `{ kind: "browser", projectPath, host }` through `tabFocusActions`
   (`web/src/components/common/URLOpenChooserHost.tsx:67`). `TabFocusKind`
   includes `browser` (`web/src/lib/tabFocus.ts:4`), and the request shape is
   `TabFocusRequest` (`web/src/lib/tabFocus.ts:12`). Opening the strip tab already
   makes it active, so the focus request does not identify the tab. An unknown
   owner disables the embedded button
   (`web/src/components/common/URLOpenChooserHost.tsx:91`) but leaves the external
   choice available. Because the tab is filed under the owner and rendered with
   that project's browse host, a remote project's `localhost` URL stays on the
   remote browse host.
6. **External destination.** `openPendingURLExternal`
   (`web/src/components/common/URLOpenChooserHost.tsx:80`) calls
   `openExternalURL` synchronously inside the button's click handler, then clears
   the pending URL. Keeping that call in the gesture preserves popup behavior.
   `openExternalURL` (`web/src/lib/externalLinks.ts:79`) uses the desktop Wails
   `ocode:open-external:<url>` bridge for the native system browser; on the web it
   uses `window.open(..., '_blank', 'noopener,noreferrer')`, with the existing
   same-tab navigation fallback when a popup blocker rejects it.

## Owner routing

`URLOpenOwnerProvider` and `useURLOpenOwner` live in
`web/src/lib/urlOpenContext.tsx:6` and `web/src/lib/urlOpenContext.tsx:22`. The
provider yields no owner when `projectPath` is empty, which is how global
surfaces retain the active-project fallback.

- **Chat and its MCP Apps widgets.** `OwnerChatPanel` wraps `ChatPanel` in
  `URLOpenOwnerProvider`, deriving `projectPath` via
  `findProjectPathForTab(projectStateRef.current, sessionId)` and taking `host`
  from props (`web/src/components/Chat/OwnerChatPanel.tsx:13`,
  `web/src/components/Chat/OwnerChatPanel.tsx:14`,
  `web/src/components/Chat/OwnerChatPanel.tsx:16`).
  `MarkdownLink` reads that context (`web/src/components/common/MarkdownLink.tsx:63`),
  and `McpAppView` reads the same owner before calling `requestURLOpen`
  (`web/src/components/Chat/mcpApp/McpAppView.tsx:140`,
  `web/src/components/Chat/mcpApp/McpAppView.tsx:239`).
- **Markdown preview.** `OwnerMarkdownViewer` wraps `MarkdownViewer` with
  `projectRoot` and `projectHost`
  (`web/src/components/Preview/OwnerMarkdownViewer.tsx:10`,
  `web/src/components/Preview/OwnerMarkdownViewer.tsx:11`). `PreviewSurface`
  lazy-loads this wrapper
  (`web/src/components/Preview/PreviewSurface.tsx:24`); `MarkdownViewer` itself
  is unchanged.
- **Terminal.** `TerminalPanel` passes `{ projectPath, host }` directly to
  `requestURLOpen` (`web/src/components/Terminal/TerminalPanel.tsx:1260`).
- **Monaco.** `FileEditor` marks its root with `data-url-project-path` and
  `data-url-project-host` (`web/src/components/Files/FileEditor.tsx:1026`). The
  link opener reads the focused editor's closest owner element and forwards that
  pair (`web/src/lib/editorLinks.ts:527`,
  `web/src/lib/editorLinks.ts:533`).
- **Global TTS, connector and sync links.** These call `requestURLOpen` without
  an owner, so the chooser uses the active project.

The exact path+host pair disambiguates a local project from a remote project
that happens to have the same absolute path. Owner metadata is routing data,
not authorization: an unknown owner cannot create an embedded tab, and the
external choice never depends on project resolution.

## Routed surfaces

| Surface | Route into the chooser |
| --- | --- |
| Markdown links | `MarkdownLink` calls `requestURLOpen` for HTTP(S) hrefs; this covers every react-markdown surface, including chat messages. Ctrl/Cmd-click uses `onClick`; desktop middle-click (`auxclick`, button 1) is intercepted separately so it cannot bypass the chooser (`web/src/components/common/MarkdownLink.tsx:70`, `web/src/components/common/MarkdownLink.tsx:86`). |
| Terminal links | `TerminalPanel`'s custom OSC 8 URL handler calls `requestURLOpen` with the terminal's owner; ghostty-web's built-in `window.open` providers are removed. |
| Monaco links | `editorLinks` calls `requestURLOpen` for HTTP(S) resources with the focused editor's owner. |
| TTS licenses | Both engine-license anchors in `TTSForm` use `MarkdownLink`, inheriting owner context when present; these settings surfaces normally have none, so the chooser falls back to the active project (`web/src/components/Settings/TTSForm.tsx:188`, `web/src/components/Settings/TTSForm.tsx:227`). |
| Connector sign-in/device code | `ConnectFlowPanel`'s sign-in and verification-address anchors use `MarkdownLink`, inheriting owner context when present; these settings surfaces normally have none, so the chooser falls back to the active project (`web/src/components/Settings/ConnectFlowPanel.tsx:271`, `web/src/components/Settings/ConnectFlowPanel.tsx:285`). |
| Sync verification | The `SyncStatusWidget` verification button and its `MarkdownLink` address call `requestURLOpen` without an owner. |
| MCP Apps | `ui/open-link` validates HTTP(S), calls the host's `openLink` option (`requestURLOpen`) and answers the widget after dispatch. A chooser refusal is logged with server/tool/error (not the URL), rethrown and returned as JSON-RPC `-32000`, so the widget does not wait forever. There is no inline Open/Cancel row and therefore no second confirmation. |

MCP details are in [MCP Apps widgets and plugin MCP servers](mcp-apps-widgets.md).
Chat file links keep their separate Files-tab routing; see
[Chat file links with relative or ~ hrefs silently did nothing](../gotchas/chat-file-links-silent-noop.md).
The embedded tab's origin and host rules are unchanged; see
[Per-tab browse origins](browse-per-tab-origin.md) and
[Browser agent tool](browser-agent-tools.md).

## Deliberate direct exceptions

These paths do not represent an ordinary clickable HTTP(S) link, or must keep a
synchronous launch gesture:

- **Embedded browser's explicit Open External controls.** `BrowserPanel` and
  `ChromeViewport` call `openExternalURL` directly; the user is already acting
  on a chosen browser tab.
- **Programmatic OAuth launches.** `CloudConnectorsForm` and
  `SyncStatusWidget.startLogin` launch the provider URL directly. These are
  programmatic OAuth launches rather than ordinary link clicks;
  `SyncStatusWidget.startLogin` preserves its synchronous popup gesture, and
  desktop uses the native Wails bridge. A later verification-link click still
  uses the chooser.
- **PDF blob print/download fallback.** `PdfViewer` opens or downloads its
  in-memory `blob:` URL when the iframe cannot host the print dialog; this is
  not an HTTP(S) link.
- **Default handlers.** `mailto:`, `tel:`, `sms:` and in-page `#hash` links keep
  their native/default behavior in `MarkdownLink`.

## Persistence race

The first browser-tab GET can resolve after the chooser creates a local tab.
`applyIncoming` keeps unsynced local tabs, but it previously restored
`incoming.activeId` unconditionally. An empty first read therefore erased the
new tab's active pointer, and `HomeApp` reset browser focus before the pane could
mount. The merge now tracks the current local active id
(`web/src/components/Browser/browserPersistence.ts:455`) and preserves it only
when the server supplied no active id and that id belongs to a kept unsynced tab
(`web/src/components/Browser/browserPersistence.ts:500`). A server-supplied
active id still wins; a synced tab closed by another client still clears the
pointer. The deferred-restore regression and its negative case are pinned in
`web/src/components/Browser/browserPersistenceSync.test.tsx:162` and
`web/src/components/Browser/browserPersistenceSync.test.tsx:196`.

## Verification

Verified for this change:

- 186 focused Vitest tests across 14 files pass: `externalLinks`, `URLOpenDialog`,
  `App.browser`, `MarkdownLink`, `MessageBubble.links`, `TerminalPanel.links`,
  `editorLinks`, `TTSForm`, `TTSForm.speechSummary`, `ConnectFlowPanel`,
  `SyncStatusWidget`, `McpAppView`, `mcpAppHost` and `browserPersistenceSync`.
- `cd web && bun check .` passes across 1,049 files.
- `cd web && bun run test -- --silent` passes all 596 files and 6,122 tests.
- `cd web && bun run build` passes.
- `go test ./cmd/ocode-desktop ./internal/desktop` passes.

- `go build -o /tmp/ocode-desktop-url-chooser ./cmd/ocode-desktop` passes; only
  pre-existing macOS deployment-version and duplicate-library linker warnings
  remain.
- Isolated Playwright QA against the rebuilt SPA uses `domcontentloaded` plus
  explicit locators because `/api/events` stays open. It passes Cancel (no
  embedded tab), Open in browser tab (pane mounts and fixture renders), Open in
  external browser (`popup.url() === target`, and the popup renders), and reports
  no page errors.
- The desktop WKWebView click-through was not live-run: the isolated Wails
  process exited before creating its debug handle, and desktop GUI automation
  lacked Accessibility permission. The native bridge remains covered by focused
  tests, and the desktop binary build passes.

## Known limitation

`browserTabsStore` remains keyed by project path, not host. A local project and
a remote project with the same absolute path therefore share browser-strip
state. The chooser resolves and selects the exact owner, but this change does
not rework that pre-existing identity model.
