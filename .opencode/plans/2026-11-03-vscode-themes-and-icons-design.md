# VS Code themes and file icon themes — design spec

Date: 2026-11-03
Status: approved in chat (2026-11-03); awaiting written-spec review
Owner decisions: server-side conversion; reach = chrome + Monaco + terminal + TUI;
full SVG `iconPath` icon themes; one global setting; marketplace = Open VSX;
a third terminal appearance source `vscode`.

**Why this file is not in `docs/superpowers/specs/`:** `docs/` is an active OKF
bundle (`docs/index.md` carries `okf_version: 0.1`) and `docs/.okfignore` only
excludes `okf/`. `CLAUDE.md` makes the `context` sub-agent the sole automated
writer of the bundle, so the main agent must not create pages there. This spec
lives in `.opencode/plans/` (tracked, outside the bundle, and already used for
implementation plans). After shipping, a `context` sub-agent adds one short
concept page plus its index entry.

---

## 1. Problem

ocode ships a fixed theme registry (`internal/theme`) and a hardcoded file-icon
map. Users who already invested in a VS Code setup cannot reuse it:

- Colour themes are limited to builtin palettes plus the opencode desktop-theme
  format. A VS Code `.json` theme (hundreds of `colors` keys plus
  `tokenColors`) is ignored.
- The file tree renders 13 emoji mappers (`web/src/components/Files/FileTree.tsx:202`)
  and lucide fallbacks; the ⌘P file picker renders no icon at all.
- Monaco defines two hardcoded themes (`FileEditor.tsx:484`, `:523`), so syntax
  colours never follow the app theme.

## 2. Goals

1. Discover VS Code-family theme contributions already installed on the machine
   (VS Code, VS Code Insiders, Cursor, Windsurf, VSCodium, plus user-added dirs).
2. Install/uninstall theme and icon-theme extensions from **Open VSX**.
3. Convert one VS Code colour theme into four consistent outputs: web chrome,
   Monaco, terminal palette, TUI palette.
4. Render a VS Code file icon theme (SVG `iconPath`) in the file tree, the ⌘P
   file picker and editor tabs.
5. One global setting pair (`appearance.color_theme`, `appearance.icon_theme`).
6. Never regress existing themes: builtin and opencode themes keep working
   exactly as today when no VS Code theme is selected.

## 3. Non-goals (explicitly not building)

- Extension auto-update, version pinning UI beyond a version selector, or a
  dependency/extension host runtime.
- Workspace (per-project) themes. One global choice only.
- `semanticTokenColors` fidelity. Monaco has no equivalent in
  `defineTheme`; the mapping is `tokenColors` only.
- Product icon themes (`contributes.productIconThemes`).
- Fetching from `marketplace.visualstudio.com` (undocumented API, bot-hostile).
  Open VSX only.
- Letting VS Code theme JSON drive layout, fonts, or status colours. Status
  colours stay intentionally fixed (`web/src/index.css` two-tier rule).

## 4. Architecture

```
Go: internal/appearance
  discover.go     scan extension dirs -> Manifest{dir, publisher, name, version,
                  themes[], iconThemes[]}
  marketplace.go  Open VSX search + VSIX fetch/extract
  vsix.go         hardened zip extraction (allowlist + caps)
  color.go        VS Code JSON -> ConvertedTheme{Colors(16), UI(map), Monaco, Terminal}
  jsonc.go        comment/trailing-comma tolerant decode + `include` resolution
  icons.go        icon theme JSON -> IconThemeModel
  assets.go       contained asset resolution for the asset endpoint
  index.go        <GlobalDataDir>/appearance/index.json cache
  service.go      facade: Themes(), Resolve(id), IconThemes(), ResolveIconTheme(id),
                  Install(), Remove(), Settings()

Go: internal/server
  handler_appearance.go   HTTP surface (local only)
  terminal_appearance.go  + "vscode" source branch

Go: internal/theme
  external.go     copy-on-write registry overlay + RegisterExternal/Reload

Web: web/src
  lib/appearanceTheme.ts  fetch/cache rich payload, apply CSS vars
  lib/monacoTheme.ts      defineTheme + setTheme from payload
  lib/iconTheme.ts        resolve(name,isDir,expanded) -> asset URL
  components/Files/FileIcon.tsx  shared icon component
  components/Settings/AppearanceForm.tsx (replaces/extends ThemeForm)
```

Flow for a colour theme:

```
extension package.json -> contributions.themes[].path
  -> jsonc decode (+ include chain, contained)
  -> ConvertedTheme (cached by (abs path, mtime, size))
  -> GET /api/appearance/theme/{id}
  -> useTheme()          -> --background/--card/... HSL triplets
  -> monacoTheme.ts      -> monaco.editor.defineTheme + setTheme
  -> terminal appearance -> ghostty-web ITheme (source = "vscode")
  -> theme registry      -> Bubble Tea TUI ThemeColors
```

## 5. Identity and storage

```text
<GlobalDataDir>/appearance/
  index.json                       discovery cache + install records
  extensions/<namespace>.<name>-<version>/   extracted VSIX (allowlisted files only)
```

Theme id: `vscode:<namespace>.<extension>/<variant-slug>`.

- `name` is the theme's `name` field (fallback: `label`, then the file stem),
  slugified to `[a-z0-9-]`.
- `variant-slug` deduplicates themes with the same name in one extension by
  appending `-2`, `-3`, ….
- Light and dark variants are **separate entries**. ocode has one active palette
  and no polarity toggle; VS Code already names variants distinctly
  (`Catppuccin Mocha` / `Catppuccin Latte`).
- Builtin and opencode themes keep their existing names, which are not
  namespaced. A collision is impossible because no builtin name contains
  `vscode:`.

## 6. Discovery

Scanned roots, in order:

| Root | Platform |
| --- | --- |
| `appearance.extra_extension_dirs[]` | any (user config) |
| `~/.vscode/extensions` | all |
| `~/.vscode-insiders/extensions` | all |
| `~/.vscode-oss/extensions` | all |
| `~/.cursor/extensions` | all |
| `~/.windsurf/extensions` | all |

Rules:

- An extension is a directory holding a readable `package.json` with `name`,
  `publisher` and `contributes`.
- Collect `contributes.themes[]` (`{label, uiTheme, path}`) and
  `contributes.iconThemes[]` (`{id, label, path}`).
- Missing or unparsable contribution files mark the entry `unavailable` with a
  reason; they never abort the scan.
- Discovery is on demand, memoised for 5 minutes, and forced fresh after any
  install/remove or settings change. It runs outside `h.mu`.

## 7. Marketplace (Open VSX)

Verified live during design:

```
GET https://open-vsx.org/api/-/search?query=<q>&size=<n>
GET https://open-vsx.org/api/<namespace>/<name>[?version=<v>]
  -> files.download = https://open-vsx.org/api/<ns>/<name>/<ver>/file/<file>.vsix
```

Measured during design (2026-11-03), and these facts change the implementation:

- The download URL answers **302 to a different host**:
  `https://open-vsx.org/api/PKief/material-icon-theme/5.40.0/file/PKief.material-icon-theme-5.40.0.vsix`
  → `https://openvsx.eclipsecontent.org/PKief/material-icon-theme/5.40.0/PKief.material-icon-theme-5.40.0.vsix`
  (200, `application/octet-stream`, 893,199 bytes, a valid zip).
- Archive contents are rooted at **`extension/`**, not the zip root:
  `extension/package.json`, `extension/dist/material-icons.json`,
  `extension/icons/git.svg`, `extension/LICENSE.txt`. 1,283 entries total.

Hard requirements:

- HTTPS only. Host **exact-match** allowlist for every hop:
  `open-vsx.org` (API) and `openvsx.eclipsecontent.org` (download CDN),
  re-checked in `CheckRedirect`. At most 3 redirects. Suffix matching is
  forbidden (`open-vsx.org.example.com` must not pass).
- 20 s timeout per request; 50 MB download cap; `http.Client` with a fixed
  `User-Agent` and a cookie-free jar.
- Extraction runs in `internal/appearance/vsix.go`, never in the installer's
  `sourcebundle` (different module, different guarantees).
- Archive root is detected, not assumed: use `extension/` when present, else the
  zip root. Every path in the manifest is resolved **relative to that root**.
- Two-pass allowlist extraction:
  1. read `<root>/package.json` from the archive;
  2. extract only `<root>/package.json`, each `contributes.themes[].path`, each
     `contributes.iconThemes[].path`, each resolved `iconDefinitions[].iconPath`,
     and the first present of `<root>/LICENSE`, `LICENSE.txt`, `LICENSE.md`.
- Caps: ≤ 20 000 entries, ≤ 25 MB per entry, ≤ 100 MB total, compression ratio
  ≤ 100:1. Any violation aborts and deletes the temp extraction dir.
- Entry names rejected on: absolute path, `..` component, backslash-normalised
  traversal, symlink/dir entry whose resolved path leaves the destination,
  duplicate names.
- The install record is written to `index.json` only after extraction succeeds;
  a failed install leaves no directory behind.

## 8. Colour conversion

### 8.1 Decoding

- VS Code theme files are JSONC: strip comments and trailing commas before
  `json.Unmarshal`. Reuse the shape of `internal/config.stripJSONCComments`
  (regex over `jsoncComments`) — it already skips quoted strings — but move a
  copy into `internal/appearance` rather than exporting from `config`.
- `"include": "./base.json"` chains are resolved depth-first, max depth 5,
  each path contained in the extension root after `EvalSymlinks`. The child's
  `colors` and `tokenColors` are merged first, the including file overrides.
- `tokenColors` may be a string path (a `.tmTheme` plist). Only a JSON file with
  the `[{scope,settings}]` shape is accepted; anything else yields "no syntax
  rules" plus a non-fatal warning. Documented limitation.
- `type` (`"dark"`/`"light"`) determines polarity; missing → infer from
  `editor.background` luminance.

### 8.2 Web chrome (`ui`)

Mapped keys (first present wins; the theme's value is used verbatim):

| CSS var | VS Code key |
| --- | --- |
| `--background` | `editor.background`, `sideBar.background` |
| `--foreground` | `editor.foreground`, `sideBar.foreground` |
| `--card` | `sideBar.background`, `editor.background` |
| `--card-foreground` | `sideBar.foreground`, `editor.foreground` |
| `--popover` | `menu.background`, `editorWidget.background`, `editor.background` |
| `--popover-foreground` | `menu.foreground`, `editorWidget.foreground`, `editor.foreground` |
| `--primary` | `button.background`, `focusBorder`, `textLink.foreground` |
| `--secondary` | `input.background`, `editorWidget.background` |
| `--secondary-foreground` | `input.foreground`, `editor.foreground` |
| `--muted` | `list.hoverBackground`, `editorWidget.background`, `sideBar.background` |
| `--muted-foreground` | `descriptionForeground`, `editorLineNumber.foreground` |
| `--accent` | `list.activeSelectionBackground`, `list.hoverBackground` |
| `--accent-foreground` | `list.activeSelectionForeground` |
| `--destructive` | `errorForeground`, `terminal.ansiRed` |
| `--border` | `panel.border`, `sideBar.border`, `editorGroup.border`, `contrastBorder` |
| `--input` | `input.border`, `panel.border` |
| `--ring` | `focusBorder`, `textLink.foreground` |
| `--link` | `textLink.foreground` |

**Contrast policy (advisor-corrected).** The theme author's colours are used
verbatim. `pickReadableFg` is applied **only** to foreground vars the theme does
not supply (`--primary-foreground`, `--destructive-foreground`,
`--secondary-foreground`, `--accent-foreground`), and `--link` is repaired only
when it fails AA against both `--background` and `--muted` (the existing
`ensureContrast` rule). No global palette repair: a user who picked a low-contrast
theme gets that theme, minus illegible synthesized pairs.

### 8.3 Monaco (`monaco`)

`colors` is a passthrough of the keys Monaco understands, copied verbatim when
present:

```
editor.background, editor.foreground, editorCursor.foreground,
editor.lineHighlightBackground, editor.lineHighlightBorder,
editorLineNumber.foreground, editorLineNumber.activeForeground,
editor.selectionBackground, editor.inactiveSelectionBackground,
editor.selectionHighlightBackground, editorBracketMatch.background,
editorBracketMatch.border, editorGutter.background, editorWidget.background,
editorWidget.border, editorHoverWidget.background, editorHoverWidget.border,
editorSuggestWidget.background, editorSuggestWidget.border,
editorSuggestWidget.selectedBackground, input.background, input.border,
input.foreground, list.activeSelectionBackground, list.hoverBackground,
scrollbar.shadow, scrollbarSlider.background, scrollbarSlider.hoverBackground,
scrollbarSlider.activeBackground, minimap.background,
editorGroupHeader.tabsBackground, tab.activeBackground, tab.activeForeground,
tab.inactiveBackground, tab.inactiveForeground
```

`base` = `"vs-dark"` for a dark theme, `"vs"` for a light theme, `inherit: false`
(so ocode's hardcoded zinc palette cannot leak in).

`rules` come from `tokenColors`, built by an ordered scope-prefix table. Later
entries overwrite earlier ones for the same Monaco token (TextMate's
later-wins), and `fontStyle` is accumulated per token:

| TextMate scope prefix | Monaco token |
| --- | --- |
| `comment` | `comment` |
| `string` | `string` |
| `constant.numeric` | `number` |
| `regexp` | `regexp` |
| `keyword.operator` | `operator` |
| `keyword`, `storage` | `keyword` |
| `entity.name.function`, `support.function` | `function` |
| `entity.name.type`, `entity.name.class`, `support.class`, `support.type` | `type` |
| `variable.parameter` | `parameter` |
| `variable`, `support.variable` | `variable` |
| `meta.object-literal.key`, `entity.other.attribute-name` | `property` / `attribute` |
| `entity.name.tag` | `tag` |
| `constant` | `constant` |
| `punctuation` | `delimiter` |
| `invalid` | `invalid` |

A scope that matches no prefix is skipped. This is deliberately approximate;
§3 excludes semantic tokens.

### 8.4 Terminal (`terminal`)

Returned as `{dark?, light?}` using the existing `TerminalThemeColors` shape so
`web/src/components/Terminal/terminalAppearance.ts` needs no new type:

| Field | VS Code key |
| --- | --- |
| `background` | `terminal.background`, `editor.background` |
| `foreground` | `terminal.foreground`, `editor.foreground` |
| `cursor` | `terminalCursor.foreground`, `editorCursor.foreground` |
| `cursor_text` | `terminalCursor.background`, computed from `background` |
| `selection_background` | `terminal.selectionBackground` |
| `selection_foreground` | `terminal.selectionForeground` |
| `palette[0..15]` | `terminal.ansiBlack` … `terminal.ansiBrightWhite` |

Only the block matching the theme's polarity is emitted. Any missing ANSI slot
falls back to the same index of ocode's default terminal palette, so the
renderer always receives 16 colours.

### 8.5 TUI (`colors`, the 16 existing fields)

| Field | Source |
| --- | --- |
| `User` | `terminal.ansiBlue`, `textLink.foreground` |
| `Assistant` | `terminal.ansiMagenta`, `editor.foreground` |
| `Header` | `terminal.ansiCyan`, `textLink.foreground` |
| `Border` | `panel.border`, `sideBar.border` |
| `Hint` | `descriptionForeground`, `editorLineNumber.foreground` |
| `Text` | `editor.foreground` |
| `Background` | `editor.background` |
| `StatusBg` | `statusBar.background`, `sideBar.background` |
| `StatusFg` | `statusBar.foreground`, `descriptionForeground` |
| `SelectedFg` | `list.activeSelectionForeground`, readable fg for `SelectedBg` |
| `SelectedBg` | `list.activeSelectionBackground` |
| `Success` | `terminal.ansiGreen`, `gitDecoration.addedResourceForeground` |
| `Error` | `errorForeground`, `terminal.ansiRed` |
| `Accent` | `focusBorder`, `textLink.foreground` |
| `Dim` | `editorLineNumber.foreground`, `descriptionForeground` |
| `Thinking` | first `tokenColors` rule whose scope contains `comment`, else `Dim` |

The TUI uses the mapped values verbatim (no repair); ANSI-based fields are
chosen because TUI messages are rendered in those slots by convention, matching
how the builtin `tokyonight` palette is authored.

### 8.6 Editor theme precedence (three controls — resolved here)

Three controls already decide the editor palette, and adding `appearance.color_theme`
would make four behaviours unless the precedence is written down:

| Control | Where | Current effect |
| --- | --- | --- |
| `monaco.theme` | `internal/monaco/monaco.go:25` (`Theme`), `GET\|PUT /api/monaco/settings`, read at `FileEditor.tsx:340` | persisted Monaco theme id; default `ocode-dark` |
| `editorAppearance` | `web/src/lib/editorAppearance.ts` | explicit light/dark quick toggle; `FileEditor.tsx:190` coerces a light saved theme back to `ocode-dark` when the toggle says dark |
| `appearance.color_theme` | new | proposed single source for chrome/terminal/TUI |

That coercion is the defect this design must remove: pick Catppuccin Latte while the
toggle says dark and the editor silently shows `ocode-dark`, with nothing in the UI
saying why.

**Editor palette resolution order (first match wins):**

1. `editorAppearance` is **explicitly** `light` or `dark` → `ocode-light` / `ocode-dark`.
   The Settings page then states *"Editor override is on — the theme's editor colours
   are not used"* and offers **Use theme colours** to clear the override.
2. Active `appearance.color_theme` is a VS Code theme → its converted Monaco theme.
3. Active theme is builtin/opencode → `monaco.theme` when it names a defined Monaco
   theme, else the polarity default (`ocode-dark` / `ocode-light`) — today's behaviour.
4. Fallback → `ocode-dark`.

`editorAppearance` gains a third stored value `"auto"`. An absent or unparsable stored
value reads as `"auto"`, so existing users stop being coerced and instead get the active
theme's own editor palette. An explicitly stored `light`/`dark` keeps today's meaning.

Chrome, terminal and TUI have **no override**: they always follow
`appearance.color_theme`.

`monaco.theme` is not removed and not migrated. It is simply outranked while a VS Code
theme is active, so an install that never selects one sees no change at all. Retiring
the control is deferred (§19).

## 9. Terminal appearance source `vscode`

- `internal/config`: `TerminalAppearanceSourceVSCode = "vscode"`;
  `NormalizeTerminalAppearanceSource` accepts it.
- `resolveTerminalAppearance`: when source is `vscode`, resolve the configured
  `appearance.color_theme`; use its `terminal` block. Any failure (theme
  missing, not a VS Code theme, unreadable file) appends a non-fatal message to
  `errors[]` and falls back to the `ocode` source. Never a hard error.
- Selecting a Ghostty theme keeps today's behaviour; this source is opt-in.
- `TerminalForm` gains the third option with a hint naming the active colour
  theme.

## 10. TUI registry integration

The registry in `internal/theme` is a plain map populated in `init()` and read
without synchronisation on hot paths.

- Replace the map read path with a copy-on-write snapshot:
  `var registry atomic.Pointer[map[string]ThemeDefinition]`. `Get`,
  `GetTheme`, `AvailableThemes` and `DisplayName` read the snapshot (lock-free).
- Add `RegisterExternalThemes(map[string]ThemeDefinition)` and
  `ReloadThemes()` which rebuild builtin + generated + disk + external into a new
  map and swap it.
- The appearance service owns the external set: converted VS Code themes
  (`vscode:…` keys) plus icon-theme extensions' colour themes.
- `appearance.color_theme` is authoritative when non-empty; otherwise the
  existing `tui.theme` is used. Both are read from `ocodeconfig.json`, so a
  separate CLI/TUI process resolves the same theme without an extra setting.
- VS Code theme JSON is **never** written into `~/.config/opencode/themes`;
  registration is in-memory and re-derived on start.

## 11. File icon themes

### 11.1 Model

```json
{
  "id": "vscode:PKief.material-icon-theme/material-icon-theme",
  "label": "Material Icon Theme",
  "polarity": "dark",
  "definitions": { "typescript": "/api/appearance/icon-theme/<id>/asset?p=icons%2Ftypescript.svg" },
  "file_names": { "package.json": "npm" },
  "file_extensions": { "ts": "typescript" },
  "folder_names": { "src": "folder-src" },
  "folder_names_expanded": { "src": "folder-src-open" },
  "file": "file",
  "folder": "folder",
  "folder_expanded": "folder-open",
  "root_folder": "folder-root",
  "root_folder_expanded": "folder-root-open",
  "hides_explorer_arrows": false
}
```

- `definitions` contains only ids actually referenced by one of the maps
  (Material Icon Theme: 1,245 definitions, a few hundred referenced).
- The `light` (and `highContrast`, ignored) blocks override `fileExtensions` and
  `fileNames` maps when the app polarity is light. Polarity comes from the
  active colour theme (`type`), defaulting to dark.
- `rootFolder*` applies only to the tree root.

### 11.2 Resolution (client)

Order: exact file name (lowercased) → longest dotted suffix match
(`foo.test.ts` → `test.ts` → `ts`) → `file`. Folders: the matching key wins,
using `folderNamesExpanded` when expanded and `folderNames` otherwise, then
`folderExpanded`/`folder`, and `rootFolder*` at depth 0.

### 11.3 Asset endpoint

`GET /api/appearance/icon-theme/{id}/asset?p=<relative-path>`

- `id` must match `^[A-Za-z0-9._-]+$` and exist in the index. The relative path
  is never trusted as a filesystem path on its own.
- Resolve `EvalSymlinks(root)` and `EvalSymlinks(join(root, clean(p)))`; the
  target must be `root + separator + …`.
- Extension allowlist: `.svg`, `.png`, `.gif`, `.jpg`, `.jpeg`, `.webp`, `.woff`,
  `.woff2`, `.ttf`. Max 2 MB.
- Response headers: correct `Content-Type`, `X-Content-Type-Options: nosniff`,
  `Content-Security-Policy: sandbox; default-src 'none'`,
  `Cache-Control: public, max-age=31536000, immutable`, plus an `ETag` from
  size+mtime.
- SVGs are scanned for `<script`, `<foreignObject`, `on<attr>=`,
  `href="http`, `xlink:href="http`; a hit rejects the asset (fail closed). SVGs
  are only ever used as an `<img src>`, never inlined into the DOM.

### 11.4 Rendering surfaces

- `components/Files/FileIcon.tsx` — single resolver used by the tree (three call
  sites), the ⌘P picker (new), and editor tabs. Fixed 16×16 box,
  `loading="eager"`, `decoding="async"`, `alt=""`.
- No icon theme selected → today's emoji/lucide map, unchanged.
- Icon theme selected but the theme lacks a definition for a name → the generic
  `file`/`folder` definition → the emoji/lucide fallback.

## 12. HTTP surface

All routes are **local-only**. They must not be reachable through
`/api/remote/{host}/api/...` proxying: appearance is a property of the local
SPA, and a remote host's themes/assets are irrelevant. A test asserts no proxy
route and no `host` parameter is accepted.

Wrapper choice, made deliberately as the repo rules require: every route is
registered in `Server.registerRoutes()` as `s.authMiddleware(s.<handler>)`.
Nothing here is wrapped for remote proxying, project scoping, or event-bus
streaming.

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/appearance/themes` | unified list `{id,label,source,kind,extension,has_icon_theme}` |
| GET | `/api/appearance/theme/{id}` | `{id,label,kind,colors,ui,monaco,terminal,warnings[]}` |
| GET | `/api/appearance/icon-themes` | icon theme list |
| GET | `/api/appearance/icon-theme/{id}` | resolved icon model |
| GET | `/api/appearance/icon-theme/{id}/asset?p=` | asset bytes |
| GET | `/api/appearance/extensions` | discovered + installed, with `themes`/`icon_themes` counts |
| POST | `/api/appearance/extensions/search` | body `{query,limit}` → Open VSX results |
| PUT | `/api/appearance/extensions/{namespace}/{name}` | body `{version?}` → install |
| DELETE | `/api/appearance/extensions/{namespace}/{name}` | remove |
| GET/PUT | `/api/appearance/settings` | `{color_theme, icon_theme, extra_extension_dirs}` |

Existing `/api/themes` and `/api/theme?name=` keep their exact response shapes and
both must resolve `vscode:` ids:

- `/api/themes` appends the converted VS Code themes (sorted, after the existing
  entries) so the current Theme form keeps working during the transition.
- `/api/theme?name=vscode:…` returns the same 16-field `colors` used by
  `/api/appearance/theme/{id}`, so the two endpoints cannot disagree.

`GET /api/appearance/themes` is the richer list; the SPA moves to it, and
`/api/themes` remains for older clients.

## 13. Configuration

```json
"appearance": {
  "color_theme": "vscode:catppuccin.catppuccin-vsc/mocha",
  "icon_theme": "vscode:PKief.material-icon-theme/material-icon-theme",
  "extra_extension_dirs": []
}
```

- Unknown keys are preserved (existing `ocodeconfig.go` raw-map pattern).
- An unresolvable `color_theme` falls back to `tui.theme`, then `tokyonight`,
  and records a warning surfaced in Settings; it never blocks startup.
- Settings writes are validated server-side (id shape, existence) before persist.

## 14. Web wiring

- `lib/appearanceTheme.ts` — one module store (same pattern as
  `chatVerbosity.ts`): fetch `GET /api/appearance/theme/{id}` once, cache by id,
  expose `getAppearanceTheme()`, `setColorTheme(id)`, and a subscribe hook.
- `useTheme()` calls it first; when the payload has `ui`, apply those hex values
  through `hexToHslTriplet`, then apply the contrasts of §8.2. When it does not,
  the current `computeThemeVars(colors)` path runs unchanged.
- `lib/monacoTheme.ts` — `applyMonacoTheme(payload, appearance)`, called by
  `FileEditor` before `setTheme`. Builtin themes keep `ocode-dark`/`ocode-light`;
  VS Code themes define `ocode-vscode` once per id and select it. The four-case
  precedence in §8.6 lives in one exported resolver
  (`resolveEditorTheme({appearance, colorTheme, monacoTheme})`) so `FileEditor`
  stops coercing on its own (`FileEditor.tsx:190`).
- `useIconTheme()` — loads the icon model for the configured id, memoises
  lookup maps, and returns `resolveIcon(name, isDir, expanded, depth)`.
- Settings: the existing group id `theme` (internal navigation key, not a URL)
  keeps its id and gains the label **"Theme & icons"**. `ThemeForm.tsx` is
  superseded in place by `AppearanceForm.tsx` — colour list (source + kind
  badge), icon theme select, extension browser (search/install/remove), and a
  preview panel that applies the palette to itself. Keeping the group id avoids
  breaking the Settings nav tests and any persisted active-group state.
- All appearance settings are written through the authenticated
  `GET|PUT /api/appearance/settings`; no localStorage for the theme itself.

## 15. Testing

Go (all mutation-verified):

- `color_test.go` — golden conversions for Catppuccin Mocha (dark), a light
  theme (Gruvbox Light / Catppuccin Latte), a JSONC theme with comments and
  trailing commas, and a theme with `include`. Assert exact `ui`/`monaco`/`terminal`/`colors`
  output.
- `jsonc_test.go` — comment stripping does not corrupt quoted `//`, `/* */`
  inside strings; trailing commas accepted; malformed input errors.
- `vsix_test.go` — zip-slip, absolute path, symlink entry, oversized entry,
  ratio bomb, entry-count cap, allowlist (non-listed file not extracted),
  `extension/`-rooted archive, rootless archive, archive whose manifest points
  outside the root.
- `marketplace_test.go` — httptest server: search decode, the two allowed hosts,
  an off-host redirect rejected, a fourth redirect rejected, timeout, download
  size cap, and a `.vsix`-content-type mismatch.
- `icons_test.go` — mapping precedence, `light` overrides, referenced-definition
  pruning, missing file → `file` fallback.
- `assets_test.go` — traversal (`..`, absolute, encoded), symlink escape,
  disallowed extension, oversized file, SVG script rejection, cache headers.
- `server` — endpoint shapes, local-only routing (no host), terminal `vscode`
  source success + fallback.
- `theme` — registry swap visibility, no data race under `-race`.

Web:

- `appearanceTheme.test.ts` — payload→CSS var mapping, fallback path unchanged,
  contrast repair limited to synthesized foregrounds.
- `iconTheme.test.ts` — resolution order, dotted suffixes, folder expansion,
  root folders, fallbacks.
- `FileIcon.test.tsx` / `FileTree` / `FilePicker` render tests.
- `monacoTheme.test.ts` — rules built from scopes, later-wins, base polarity, and
  the full §8.6 precedence table (explicit override, VS Code theme, builtin +
  `monaco.theme`, fallback), including the `editorAppearance` `"auto"` migration
  (absent/unparsable stored value reads as auto, explicit `light`/`dark` unchanged).
- `AppearanceForm.test.tsx` — list, install/remove via mocked API, error states.
- `useTheme.test.ts` — extended for the rich payload, existing cases kept.

Live validation: `go build -o /tmp/ocode-test .`, serve on a scratch port, drive
Playwright to select Catppuccin Mocha and Material Icon Theme, then assert the
computed `--background`, the Monaco token colour, and a rendered tree icon.

### 15.1 Repo-rule compliance

- Every goroutine (discovery scan, marketplace fetch, cache warming) goes
  through `crashguard.Go`.
- Handlers use the existing `s.authMiddleware(...)` wrapper and are registered in
  `Server.registerRoutes()`. No `?host=` handling and no remote proxy entry.
- No `os.Getwd()` in handlers; resolve everything from `paths.GlobalDataDir()`.
- Theme listings are sorted by label and paginated in the response
  (`limit`/`offset`, default 200) so a large extension set cannot produce an
  unbounded payload.
- **No silent colour fallbacks.** A key absent from the mapping table yields an
  absent CSS var (the stylesheet default applies) or an explicitly computed
  readable foreground — never `??`-style substitution of an unrelated key.
  Every mapped key is listed in §8; unmapped keys are ignored by design.
- Tests that override `HOME` use the package's `setHomeTree(t, home)` helper.
- Web typecheck is `cd web && bun check .`, not `tsc --noEmit`.
- Deferred items are recorded in `TODO.md` (§19).
- Commits for this feature are path-limited: the working tree currently holds
  unrelated edits to `internal/paths/paths.go`, `internal/server/server.go`,
  `internal/remote/*` and several `docs/` pages that must not be swept in.

## 16. Delivery slices

Ordered so the highest-security work (network + zip extraction) lands last and
each slice is independently shippable.

1. **Converter + discovery + web chrome + Monaco.** Pure-Go converter with
   golden tests, extension scan, `GET /api/appearance/theme/{id}`, `useTheme` and
   Monaco consumption. No terminal, TUI, icons or marketplace.
2. **Icon themes** — parse/serve/render (tree, ⌘P picker, editor tabs).
3. **Terminal source `vscode` + TUI integration** — config normalization,
   resolver branch, copy-on-write registry, `appearance.color_theme` precedence.
4. **Marketplace + full Settings UI + preview** — Open VSX search/install/remove,
   unified theme list, extension browser.
5. **Docs and validation** — `docs/concepts/vscode-themes-and-icons.md` via the
   `context` sub-agent, `skills/ocode-web` touch-ups where applicable,
   CHANGES entry, live Playwright pass.

Each slice lands green on its own: `go build ./...`, `go vet`, the touched Go
packages, `cd web && bun check .`, `vite build`, and the touched web suites.

## 17. Risks and mitigations

| Risk | Mitigation |
| --- | --- |
| Open VSX unavailable / rate-limited | Local discovery is independent; search and install surface the error; results are not cached as failures |
| Untrusted VSIX content | Allowlist extraction + caps + host allowlist + no execution of extension code |
| Download redirects to a CDN host | Exact-match allowlist of `open-vsx.org` and `openvsx.eclipsecontent.org`; any other hop fails the install loudly |
| Archive layout changes (`extension/` root) | Root is detected per archive; an unknown layout fails the install instead of half-extracting |
| SVG/XSS | `<img src>` only, CSP `sandbox`, nosniff, script-scan rejection |
| Monaco token mapping is approximate | Documented limitation; `tokenColors` only; colours come from the theme |
| Large icon JSON (450 KB) + many assets | Referenced-definition pruning, on-demand immutable asset caching |
| Registry race / hot path cost | `atomic.Pointer` copy-on-write snapshot |
| Theme looks wrong after repair | Repair limited to synthesized foregrounds; author colours pass through |
| Three editor theme knobs silently disagree | §8.6 fixes one precedence order, adds `editorAppearance: "auto"`, and shows an explicit "override is on" notice with a reset action |
| Confusing UX with 3 terminal sources | Explicit option + hint naming the active colour theme |
| `include` escaping the extension dir | Depth cap + containment after `EvalSymlinks` |

## 18. Decisions log

- Server-side conversion, not client-side (TUI needs the same result).
- Open VSX, not the VS Code Marketplace.
- Separate entry per light/dark variant, no polarity toggle.
- Author palette verbatim; repair only synthesized foreground pairs.
- Editor palette precedence: explicit `editorAppearance` override > active VS Code
  theme > `monaco.theme` > polarity default (§8.6). No silent coercion.
- Local-only endpoints; never proxied to remote hosts.
- Spec stored outside the OKF bundle; concept page added by the context agent
  after shipping.

## 19. Deferred work (record in `TODO.md`)

Each item is a deliberate limitation of this design, not an oversight:

1. `semanticTokenColors` → Monaco. Monaco's `defineTheme` has no semantic-token
   rules; would need `semanticHighlighting` + language-service work.
2. Per-project (workspace) theme overrides.
3. Product icon themes (`contributes.productIconThemes`) for the activity bar.
4. Extension auto-update and an update indicator.
5. VS Code Marketplace as a second source (currently Open VSX only).
6. `.tmTheme` (plist) `tokenColors` support; today only JSON is accepted.
7. A theme-editing UI for adjusting a converted theme in place.
8. Retiring the `monaco.theme` control (`PUT /api/monaco/settings`). It stays,
   outranked by a VS Code theme, so existing installs are unaffected.
