---
type: Concept
title: 'Terminal appearance: following Ghostty config in a browser terminal'
description: How ocode follows Ghostty's config file for terminal appearance inside ghostty-web (canvas 2D), mapping the keys it can honour and reporting the rest.
tags:
  - terminal
  - ghostty
  - appearance
  - config
  - web
timestamp: 2026-10-28T16:16:41Z
---
# Terminal appearance: following Ghostty config in a browser terminal

## Why

`ocode`'s interactive terminal is **ghostty-web** — libghostty-vt compiled to WebAssembly, rendered to a canvas 2D surface (`web/src/components/Terminal/`). Ghostty's own config loader is **not** exposed by that build: `Ghostty.load()` takes only a wasm path and `createTerminal(cols, rows, config)`. There is no plugin mechanism in Ghostty that transfers configuration. So ocode re-reads Ghostty's files itself and maps the appearance keys it can honour, and exposes them through the same Settings surface used for native ocode appearance.

## Architecture

Three layers: a Go parser package on the server, a resolved-appearance block in the terminal config API, and a client module that applies the values to ghostty-web's (limited) option surface.

### Server: `internal/ghosttyconfig`

Discovery follows Ghostty's documented order; **later files win**:

1. `$XDG_CONFIG_HOME/ghostty/config.ghostty`
2. `$XDG_CONFIG_HOME/ghostty/config` (default `~/.config`)
3. on macOS, `~/Library/Application Support/com.mitchellh.ghostty/config.ghostty`
4. on macOS, `~/Library/Application Support/com.mitchellh.ghostty/config` (all macOS files load **after** all XDG files)

Syntax: `key = value`, `#` comments only on their own line, quoted values, empty value = reset to default. `config-file` includes are processed at the **end** of the containing file (so a key below them cannot override the include), relative paths resolve to the containing file, `?` prefix = optional, cycles + depth >16 are reported as errors.

Themes: `theme = Name`, `theme = light:X,dark:Y` (both required), or an absolute path. Names are searched in `$XDG_CONFIG_HOME/ghostty/themes` then Ghostty's resource themes dirs (`/Applications/Ghostty.app/Contents/Resources/ghostty/themes`, `/usr/share/ghostty/themes`, `/usr/local/share/ghostty/themes`, `/opt/homebrew/share/ghostty/themes`, `$GHOSTTY_RESOURCES_DIR`). Names containing `/` or `..` are rejected. Explicit `background`/`foreground`/`palette` override theme values (Ghostty's own rule). Derived defaults: cursor = foreground, cursor-text = background, selection bg/fg = foreground/background, so ghostty-web's VS Code defaults never leak through.

Mapped keys: `font-family` (quoted into a CSS stack + monospace fallback), `font-size` (clamped 6..48), `adjust-cell-width`, `adjust-cell-height` (**deltas**: px or %), `theme`, `background`, `foreground`, `cursor-color`, `cursor-text`, `selection-background`, `selection-foreground`, `palette = N=#rrggbb` (ANSI 0-15 only), `keybind`.

Not portable — reported with a reason in the API/Settings: `custom-shader*` (GLSL cannot run on canvas 2D), `background-opacity`/`background-blur*` (opaque cells), `background-image*`, `window-padding*` (panel layout owns padding), `window-*` chrome, `font-feature*`/`font-variation*` (canvas 2D), `font-family-{bold,italic,bold-italic}` (one family, synthesised weight), `shell-integration*` (server-side pty), GTK/macOS app settings. Everything else: "not applicable in a browser terminal". `keybind` chord/action/arg are parsed; `global:` and `unbind` are kept.

### API: `GET/PUT /api/config/terminal`

New `appearance` block: `source` (effective: `ghostty`|`ocode`), `requested` (what the setting says — they differ when Ghostty was requested but no config exists), `font_family`, `font_size`, `cell_width`/`cell_height` as **deltas**, `theme_name`, `light`/`dark` colour sets, and `settings` (the persisted ocode-source values, so the UI can keep editing them while Ghostty is active).

New `ghostty` report: `found`, `paths`, `errors`, parsed font/cell/theme, `keybinds`, `ignored[] {key,value,reason}`.

`appearance_scope`: `"local"` on a local answer, `"remote"` on a `?host=` answer. A remote answer deliberately carries **no** `appearance`/`ghostty`: the handler would be reading the remote machine's Ghostty config (usually none), not the viewer's.

New config keys (targeted load-modify-write savers — never a whole-file overwrite): `terminal_appearance_source` (`"ghostty"` default | `"ocode"`), `terminal_cell_width_percent`, `terminal_cell_height_percent`, `terminal_theme`.

New defaults: `terminal_font_size` default 13 → 12, and cell height 110% (extra row spacing) — both only when the value is unset; a persisted size is untouched. The 110% applies only to the `"ocode"` source; following Ghostty uses Ghostty's own spacing.

### Precedence (whole block, user's choice)

`ocode` settings, OR the resolved Ghostty config when it exists, OR ocode's defaults when `"ghostty"` is requested but no config exists (source reports `"ocode"`, requested still `"ghostty"` so the UI explains the fallback). The two sources are **never** mixed per field: a Ghostty config that sets only a theme still gets the default font.

## Client

### `web/src/components/Terminal/terminalAppearance.ts`

Wire types, app-polarity pick for `light:`/`dark:` themes, `ITheme` mapping, and `scaleCellMetrics` (integer results so canvas CSS size and backing store cannot drift a fraction per cell; baseline centred in the taller cell).

### Cell adjustments

ghostty-web has **no** line-height/cell option (`ITerminalOptions` = cols, rows, cursorBlink, cursorStyle, theme, scrollback, fontSize, fontFamily, allowTransparency, convertEol, disableStdin, linkHandler, smoothScrollDuration, ghostty). The adjustment is applied by scaling `CanvasRenderer.metrics` via the private-access helpers in `ghosttyInternals.ts`: `remeasureFont()` first (so repeated application cannot compound), then `resize` + a full redraw. FitAddon, ocode's mouse reporting and ghostty-web's selection all read `renderer.getMetrics()` / the canvas rect, so every coordinate system stays consistent. Regression evidence: a live Chromium check at DPR 2 (12px cell → 13px, canvas 640×312 / backing 1280, 3-cell drag selected exactly "AAA").

### Theme

The wasm colour palette is fixed at **construction** (no runtime setter). A new terminal gets the theme in the constructor; a live change (Settings save) uses the same serialize → `reset()` → replay path the scrollback setting already uses, accepting the same documented losses (terminal modes, alternate screen, selection, link cache). The buffer serializer's "default colour" pair follows the **active** theme.

### Keybinds (`ghosttyKeybinds.ts`)

Applied subset: `copy_to_clipboard`, `paste_from_clipboard`, `increase_font_size[:n]`, `decrease_font_size[:n]`, `reset_font_size`, `scroll_page_up/down`, `scroll_to_top/bottom`, `search`. Tab/window/split/quick-terminal actions are reported as **not applied** (ocode's own shortcuts own them: ⌘T new terminal, ⌘W close). Bindings run first in the panel's custom key handler (user config beats ocode defaults), only while the terminal is focused. Paste returns `false` and never `preventDefault`s, so the native `paste` event stays the single owner of clipboard text. Font zoom is per-surface and ephemeral, like Ghostty; changing the configured size resets it.

### Settings (`TerminalForm.tsx`)

Whole-block source radio, resolved-read-only summary with "Copy to ocode settings", persisted ocode fields seeded from `appearance.settings` (never from the resolved values, or switching sources would save Ghostty's numbers), cell percentages stated absolutely (100% = natural), and the applied/not-applied keybind lists plus the "not portable" key list.

## Limits

- A browser tab can only use fonts installed on the machine running the UI. For a remote project the appearance still comes from the **local** server (that is where the viewer is) — exactly why the remote response omits it.
- Theme changes rebuild the wasm terminal (documented losses above).
- `background-opacity`, shaders, keybind actions owned by the app, and background images do not transfer.

## Tests

- `internal/ghosttyconfig/config_test.go`
- `internal/server/terminal_appearance_test.go`
- `web/src/components/Terminal/terminalAppearance.test.ts`
- `web/src/components/Terminal/ghosttyKeybinds.test.ts`
- `web/src/components/Terminal/TerminalPanel.keybinds.test.tsx`
- `web/src/components/Settings/TerminalForm.appearance.test.tsx`