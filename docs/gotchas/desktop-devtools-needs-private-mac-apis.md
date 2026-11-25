---
type: Gotcha
title: macOS desktop DevTools silently does nothing without -tags private_mac_apis
description: View → Open Developer Tools (⌥⌘I) and the tray's "Open DevTools" are silent no-ops in a default macOS desktop build; Wails binds them to a WKWebView inspector that only exists under -tags private_mac_apis, and -tags devtools only keeps the menu item, not a working opener.
tags:
  - gotcha
  - desktop
  - wails
  - macos
  - devtools
  - build-tags
  - private-apis
  - webinspector
timestamp: 2026-11-24T08:35:14Z
resource: "cmd/ocode-desktop/main.go; Makefile; skills/ocode-desktop/SKILL.md; github.com/wailsapp/wails/v3@v3.1.0 pkg/application/{roles_dev.go,webview_window_darwin_dev.go,webview_window_darwin_production.go,mac_public_api_darwin.go,mac_private_api_darwin.go}"
---
# macOS desktop DevTools needs `-tags private_mac_apis`

## Context

The macOS desktop shell is Wails v3.1.0 (`cmd/ocode-desktop`, shipped as
`bin/ocode.app`). The user reported that **View → Open Developer Tools** (⌥⌘I)
and the tray's **Open DevTools** did nothing: no panel, no error, no log line.

## Cause

- `menu.AddRole(application.ViewMenu)` in `cmd/ocode-desktop/main.go` adds
  Wails' "Open Developer Tools" item. Wails gates the item on
  `//go:build !production || devtools` (`pkg/application/roles_dev.go`, which
  calls `viewMenu.AddRole(OpenDevTools)`) and binds it to
  `window.OpenDevTools()`. ocode never builds with the `production` tag, so the
  item ships in **every** build.
- On macOS, `OpenDevTools()` reaches `wailsPrivateOpenWebInspector`, declared
  by `pkg/application/webview_window_darwin_dev.go` (`openDevTools()`). In a
  default build that C function comes from
  `pkg/application/mac_public_api_darwin.go`, whose body is a documented
  no-op — `(void)window;` under `#ifdef WAILS_MAC_DEVTOOLS`.
- The real implementation is `pkg/application/mac_private_api_darwin.go`,
  compiled only under `-tags private_mac_apis`. It declares `_WKInspector`
  and `WKWebView._inspector` and calls `[nsWindow.webView._inspector show]`,
  also under `#ifdef WAILS_MAC_DEVTOOLS`.
- `-tags devtools` is **not** the fix. It only controls whether the menu *item*
  exists when `production` is also set (`roles_dev.go`). Without
  `private_mac_apis`, `openDevTools()` still resolves to the public no-op; a
  plain `production` build without `devtools` goes further and stubs
  `openDevTools()` to nothing (`webview_window_darwin_production.go`).

## Fix

- `Makefile` defines `RELEASE_TAG_NAMES := htr models`,
  `RELEASE_TAGS := -tags "$(RELEASE_TAG_NAMES)"`, and
  `DESKTOP_TAGS := -tags "$(RELEASE_TAG_NAMES) private_mac_apis"`.
  `DESKTOP_BUILD` and `DESKTOP_INSTALL` use `DESKTOP_TAGS`; the two
  `docker-desktop-darwin` `go build` lines pass `-tags private_mac_apis`
  directly. Remote CLI binaries and `make install` still use `RELEASE_TAGS`
  only.
- The tag selects no files off macOS (verified with
  `GOOS=linux go list -tags private_mac_apis` against the Wails package: zero
  `mac_private*` files), so it is inert on Linux/Windows. Those platforms'
  devtools already work through Wails' public paths.
- `private_mac_apis` also enables Wails' private webview transparency /
  background-colour / glass calls (`wailsPrivateSetWebviewTransparent`,
  `wailsPrivateSetWebviewBackgroundColour`, `wailsPrivateSetGlassStyle`,
  `wailsPrivateSetGlassGrouping`). This shell sets no `BackgroundColour` and
  uses the normal backdrop, so the only behavioural difference is the working
  inspector.
- The documented fast-iteration path — a bare
  `go build ./cmd/ocode-desktop` — must pass `-tags private_mac_apis` too, or
  devtools silently regresses. `skills/ocode-desktop/SKILL.md` (build table plus
  gotcha 19) records this.

## Verification

Verified 2026-11-24 in this repo:

- `go build -tags "htr models private_mac_apis" ./cmd/ocode-desktop` succeeds;
  `go vet` and `go test -tags "htr models private_mac_apis" ./cmd/ocode-desktop`
  pass.
- `otool -ov <binary> | grep -c _inspector` is `1` for the tagged binary and
  `0` for the untagged one.
- A launched tagged binary's Wails **Build Info** log line reports
  `-tags=htr,models,private_mac_apis`.
- **Not verified:** the literal GUI click — the automation session lacked
  Screen Recording / Accessibility permission — and notarization (the repo has
  no `notarytool`/`stapler` step; the bundle script only codesigns).

## Gotcha / Limits

- **The menu item is not the signal.** It is present in every default build and
  does nothing, so "the item is there" never means devtools work. Check the
  build tags or the `_inspector` string in the binary.
- **Do not reach for `-tags devtools`.** It keeps the menu item under
  `production`; it does not compile the inspector bridge.
- **`private_mac_apis` calls undocumented Apple APIs.** That is fine for
  direct-download distribution (codesign only, no App Store review). An App
  Store build would need the DevTools item removed, or a separate build target
  that omits the tag.
- Exposure is unchanged by the fix: the menu item already shipped in every
  build; it was simply broken.
