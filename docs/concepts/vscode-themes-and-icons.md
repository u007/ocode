---
type: Concept
title: VS Code themes and file icon themes
description: 'Reusing an installed VS Code colour theme across web chrome, Monaco, terminal and TUI: theme identity, discovery roots and the five-minute cache, the four conversion outputs and alpha compositing, editor-theme precedence with editorAppearance auto, the local-only API guarantee, the unresolvable-theme fallback chain, and the deliberate limits still unimplemented (icons, marketplace, terminal source vscode, TUI registration).'
tags:
  - appearance
  - themes
  - vscode
  - monaco
  - terminal
  - tui
  - web
timestamp: 2026-11-03T14:51:26Z
---
# VS Code themes and file icon themes

## Status

Delivery is sliced (design spec `.opencode/plans/2026-11-03-vscode-themes-and-icons-design.md` §16).

- **Shipped — colour themes end-to-end (slice 1):** extension discovery, JSONC/`include`
  decoding, server-side conversion into four palettes, the appearance API, web chrome
  (CSS variables) and Monaco consumption, and the Settings theme picker.
- **Not shipped:** file icon themes (slice 2), terminal appearance source `vscode` and
  TUI theme registration (slice 3), Open VSX marketplace search/install/remove (slice 4).
  The remaining work is recorded in `TODO.md` under
  "Open: VS Code themes and icons — deferred items (2026-11-03)".

So today a VS Code theme recolours the **web/desktop chrome, Monaco and the converted
terminal/ TUI palettes in the API payload**; it does not yet recolour the TUI, populate the
terminal `vscode` source, or change file icons.

## Why

ocode ships a fixed theme registry (`internal/theme`) and a hardcoded file-icon map. Users
who already invested in a VS Code setup could not reuse it: colour themes were limited to
builtin/open­code palettes, Monaco had two hardcoded themes, and file icons were an emoji
map. This feature discovers the VS Code-family theme extensions already installed on the
machine and converts one colour theme into every palette ocode renders with.

Conversion happens **server-side**, not in the browser, because the TUI needs the same
result and the theme file lives on this machine.

## Identity

A converted theme id is:

```text
vscode:<namespace>.<extension>/<variant-slug>
```

- The namespace half is the extension's `<publisher>.<name>` (its package.json `name` +
  `publisher`), so it is unique per installed extension.
- The variant slug is the theme's `name` (fallback `label`, then the file stem), slugified
  to `[a-z0-9-]`. Two themes with the same name in one extension get `-2`, `-3`, ….
- **Light and dark variants are separate entries.** ocode has one active palette and no
  polarity toggle; VS Code themes already name their variants distinctly
  (`Catppuccin Mocha` / `Catppuccin Latte`).
- Builtin and opencode themes keep their un-namespaced names, so a collision with
  `vscode:` is impossible.

No VSIX is installed yet, so nothing is written under `<GlobalDataDir>/appearance/` today;
the extraction/index path belongs to the marketplace slice, not this one.

## Discovery

`internal/appearance/discover.go` scans, in order, the configured extra roots then the
well-known editor directories under the user's home:

| Root | Notes |
| --- | --- |
| `appearance.extra_extension_dirs[]` | user config, scanned first |
| `~/.vscode/extensions` | VS Code |
| `~/.vscode-insiders/extensions` | VS Code Insiders |
| `~/.vscode-oss/extensions` | VSCodium |
| `~/.cursor/extensions` | Cursor |
| `~/.windsurf/extensions` | Windsurf |

An extension is a directory holding a readable `package.json` with `name`, `publisher`
and `contributes`. `contributes.themes[]` and `contributes.iconThemes[]` are collected;
a broken manifest or contribution is marked unavailable with a reason and **never aborts
the scan**. Manifest reads are bounded (1 MiB).

`extra_extension_dirs` entries must be absolute or `~/…`-prefixed. A relative path would
resolve against the process working directory, so it is dropped from discovery with a
warning and rejected with HTTP 400 by the settings write. A bare `~` is also refused (it
would make the whole home an extension root); a non-existent configured root is simply
skipped.

Discovery is on demand but **memoised for five minutes** (`discoveryTTL` in
`internal/appearance/service.go`). A settings write that actually changes the extra roots
refreshes the snapshot immediately rather than waiting for the memo to expire; changing
only the colour theme does not, so a trivial save never costs a full filesystem walk.
Scans are serialised so concurrent first requests share one walk, and a generation guard
discards a walk that began before a refresh. Each conversion is cached against the theme
file's `(mtime, size)`, so editing a theme on disk invalidates it.

## Conversion outputs

`internal/appearance/color.go` turns one `RawTheme` into a `ConvertedTheme` with four
outputs, each with a different consumer. The mapping tables in the design spec (§8.2–§8.5)
are the authority; the short version:

| Output | Consumer | Shape |
| --- | --- | --- |
| `ui` | web chrome | CSS variables (`--background`, `--card`, `--primary`, …) |
| `monaco` | editor syntax + editor colours | `base` (`vs`/`vs-dark`, `inherit: false`) + passthrough `colors` + `rules` |
| `terminal` | ghostty-web terminal palette | `{dark?, light?}` with 16 ANSI slots, only the matching polarity |
| `colors` | TUI | the existing 16-field `theme.ThemeColors` |

Rules that matter:

- **The author's colours are used verbatim.** The converter does not "fix" a theme the
  user chose. It synthesizes only foregrounds the theme does not author
  (`--primary-foreground`, `--destructive-foreground`, `--secondary-foreground`,
  `--accent-foreground`) for readability, and repairs `--link` only when it fails AA
  against both the page background and the muted surface. When the theme names its
  partner key (`button.foreground`, `input.foreground`,
  `list.activeSelectionForeground`) that value wins; only an absent one is synthesized.
- **Alpha is composited, never emitted — every value is `#RRGGBB`.** VS Code themes
  routinely carry 8-digit `#RRGGBBAA` values (`input.border: "#00000000"`,
  `list.hoverBackground: "#31324480"`). None of the four consumers can carry alpha: the
  CSS variables are HSL triplets consumed as `hsl(var(--x) / <alpha-value>)` and the web
  converter accepts six digits only (an 8-digit value is silently skipped), lipgloss and
  ghostty-web also render `#RRGGBB` only. Truncating the alpha channel would turn
  Catppuccin's deliberately invisible input border into a black one, so each value is
  **composited over its background** (the theme's `editor.background` for `ui`/`colors`,
  each terminal block's own background for `terminal`) and flattened once. The same
  choke point handles synthesized foregrounds and the colours-less derivation. An
  unparseable value is cleared (or replaced by the xterm default palette slot for
  terminal ANSI), never passed through.
- **Monaco is `tokenColors` only.** `base` is `vs-dark` for a dark theme and `vs` for a
  light one, with `inherit: false` so ocode's hardcoded palette cannot leak in. `rules`
  come from an ordered TextMate-scope-prefix table (`comment`, `keyword`, `string`,
  `entity.name.function`, …). It is deliberately approximate.
- **Terminal slots fall back per index** to ocode's default terminal palette, so the
  renderer always receives 16 colours.

On the web side `web/src/lib/appearanceTheme.ts` applies `ui` hex values through
`hexToHslTriplet` (and re-checks the four synthesized foreground pairs); when a payload
has no `ui` the previous `computeThemeVars(colors)` path runs unchanged.
`web/src/lib/monacoTheme.ts` defines the converted Monaco palette as
`ocode-vscode:<id>` and selects it.

## Editor theme precedence (`editorAppearance` is now `auto`)

Three controls could disagree about the editor palette, so one resolver owns the order.
`resolveEditorTheme` in `web/src/lib/monacoTheme.ts` is `FileEditor`/`FileTabContent`'s
single source (via `web/src/hooks/useResolvedEditorTheme.ts`), and `useResolvedEditorTheme`
also derives the polarity the toggle and the light-surface seam use:

1. `editorAppearance` is explicitly `light`/`dark` → `ocode-light`/`ocode-dark`.
   Settings shows "Editor override is on — the theme's editor colours are not used" and a
   **Use theme colours** action that writes `auto` back.
2. The active colour theme is a VS Code theme → its converted Monaco palette.
3. `monaco.theme` names a defined Monaco theme → that theme. This deliberately also works
   with no loaded colour theme, so a slow/failed appearance fetch cannot silently drop the
   user's editor palette.
4. A loaded builtin/opencode theme without a usable `monaco.theme` → the theme's polarity
   default (`ocode-dark` / `ocode-light`).
5. Nothing loaded → `ocode-dark`.

`editorAppearance` gained a third stored value `"auto"`, and an **absent or unparsable
stored value reads as `auto`**. Existing users therefore stop being coerced to `ocode-dark`
and instead get the active theme's own editor colours; an explicitly stored `light`/`dark`
keeps its old meaning. `monaco.theme` is outranked, not removed or migrated, so an install
that never selects a VS Code theme sees no change.

Chrome, terminal and TUI have **no override**: they always follow `appearance.color_theme`.

## API surface, and why it is local-only

Routes in `internal/server/server.go`, handlers in
`internal/server/handler_appearance.go`:

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/appearance/themes` | unified list (`{id,label,source,kind,extension,has_icon_theme}`), paginated |
| GET | `/api/appearance/theme/{id...}` | converted theme (`colors`, `ui`, `monaco`, `terminal`, `warnings`) |
| GET | `/api/appearance/settings` | stored selection + derived `effective_color_theme` + warnings |
| PUT | `/api/appearance/settings` | validate and persist `{color_theme, icon_theme, extra_extension_dirs}` |

The `{id...}` wildcard is required because a theme id contains `/`. The list puts builtin
and opencode entries first, then VS Code themes sorted by label, and is capped
(default 200, max 1000; the SPA asks for 1000). The legacy `/api/themes` and
`/api/theme?name=` endpoints keep their shapes and also resolve `vscode:` ids, returning
the same 16-field palette as `/api/appearance/theme/{id}`, so the two cannot disagree.

These routes are **local-only**. They are registered as `s.authMiddleware(handler)` and
nothing else: no `?host=`, no project scoping, no event stream. The generic remote proxy
has a catch-all for `/api/remote/{host}/api/...`, so "no route" is not sufficient — the
appearance prefix is **denied inside the proxy itself, before any connect**, for saved and
unsaved hosts alike (403), including encoded traversal attempts that would normalise to
`/api/appearance`. The reason: appearance is a property of the machine rendering the SPA.
The theme list is derived from the **local** filesystem, so proxying it to a remote host
would either show the wrong machine's themes or require shipping its extension files, and
neither is meaningful.

## Fallback for an unresolvable stored theme

A stored `color_theme` can stop resolving (the extension was removed). The settings
response then reports the stored value plus a warning and a derived
`effective_color_theme`:

1. the stored theme, when it resolves;
2. otherwise `tui.theme`, if it is a registered builtin;
3. otherwise `tokyonight`.

Startup is never blocked. The warning is surfaced in Settings (`ThemeForm` renders the
list's and the settings response's warnings, and echoes `effective_color_theme` as
"Current"). The stored value is not overwritten by the fallback, and a `PUT` validates
that a `vscode:` id resolves before persisting, so a typo cannot be stored in the first
place. `/api/theme?name=vscode:…` answers 404 rather than substituting an unrelated
palette.

## Deliberate limits

The spec's non-goals, still unimplemented and tracked in `TODO.md`:

- **No `semanticTokenColors` → Monaco fidelity.** Monaco's `defineTheme` has no
  semantic-token rules; only `tokenColors` is mapped, so themes relying on semantic
  highlighting look approximate.
- **No `.tmTheme` (plist) `tokenColors`.** Only a JSON file with the `[{scope,settings}]`
  shape is accepted; a string path to a plist yields "no syntax rules" plus a non-fatal
  warning.
- **No Open VSX marketplace yet** (search/install/remove, and the `<GlobalDataDir>/appearance/`
  extracted-VSIX index). Discovery covers only extensions already installed by an editor.
- **No file icon themes yet.** The theme list reports `has_icon_theme`, but there is no
  icon-theme model, asset endpoint, or `FileIcon` renderer; the file tree, ⌘P picker and
  editor tabs keep the emoji/lucide map.
- **No per-project (workspace) themes.** One global `appearance.color_theme` /
  `appearance.icon_theme`; the `appearance` block lives in `ocodeconfig.json`
  (`internal/config/ocodeconfig.go`) with `color_theme`, `icon_theme`,
  `extra_extension_dirs`, and unknown keys preserved.
- **No theme-editing UI** and no extension auto-update.
- **TUI registration is not built.** The server emits the 16-field palette and
  `/api/theme` returns it, but `internal/theme` is still a plain map with no copy-on-write
  overlay, so the TUI does not yet render a `vscode:` theme. Likewise the terminal
  appearance source is still `ocode`|`ghostty`; `vscode` is not accepted by
  `NormalizeTerminalAppearanceSource` yet.

## Key files

- `internal/appearance/jsonc.go` — JSONC decode, `include` chains, `tokenColors` shapes,
  containment, 8 MiB bounded reads.
- `internal/appearance/color.go` — one conversion into `ui`, `monaco`, `terminal` and
  `theme.ThemeColors`.
- `internal/appearance/discover.go` — extension roots and manifest parsing.
- `internal/appearance/service.go` — memoised discovery, conversion cache, refresh.
- `internal/appearance/types.go` — the wire shapes and the conversion contract.
- `internal/server/handler_appearance.go`, `internal/server/handler_theme.go`,
  `internal/server/server.go` — endpoints and route registration; the remote-proxy denial
  is in `internal/server/handler_remote_proxy.go`.
- `internal/config/ocodeconfig.go` — the `appearance` block and normalization.
- `web/src/lib/appearanceTheme.ts`, `web/src/lib/monacoTheme.ts`,
  `web/src/hooks/useResolvedEditorTheme.ts`, `web/src/components/Settings/ThemeForm.tsx` —
  apply/persist, Monaco, precedence, and the Settings surface.
