# VS Code Themes — Slice 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (default) or superpowers:subagent-driven-development (long plans of independent tasks) to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Convert locally installed VS Code colour themes into ocode's four palettes (chrome, Monaco, terminal, TUI) and make the web UI apply chrome + Monaco from that conversion.

**Architecture:** A new `internal/appearance` package discovers VS Code-family extensions, reads a theme's JSONC (resolving `include`), and converts it once into a single `ConvertedTheme` bundle that is cached by file identity. Two new read endpoints plus one settings endpoint expose it; the SPA applies the bundle through a module store, while existing `/api/themes` and `/api/theme` keep working by also resolving `vscode:` ids.

**Tech Stack:** Go 1.26 (`encoding/json`, `archive/zip` not needed in this slice), React 19 + Vite + Monaco, Vitest.

**Spec:** `.opencode/plans/2026-11-03-vscode-themes-and-icons-design.md` (§5–§8, §12–§14, §15)

**Detail:** decisions, not code — the implementer writes the implementation.

**Knowledge bundle:** lookup performed before planning (`knowledge_lookup`, question: theming/theme registry/`useTheme`/Monaco/terminal sources/gotchas). Result: the bundle has **no** page on the UI theme registry, `useTheme` CSS variables, Monaco themes, or theme precedence — so nothing contradicts this plan, and the gap is exactly what "Post-slice documentation" fills. Two existing facts it did confirm:

- `docs/concepts/terminal-appearance-ghostty.md`: a `?host=` answer deliberately carries **no** `appearance` block, because appearance applies on the viewer's machine. That is an independent confirmation of the local-only endpoint decision (§12).
- `docs/concepts/web-terminal-ghostty-web.md`: changing the terminal theme **rebuilds** the wasm terminal and loses modes, alt-screen, selection and the link cache. Slice 3 must expect that rebuild when the `vscode` source changes; it is not introduced by this plan.

## Working-tree safety

The checkout is shared and dirty. `internal/server/server.go` is **already modified** by unrelated work
(`serve_registry`, remote, paths). Task 3 must therefore:

- make one small anchored edit inside `registerRoutes` and stage only that hunk
  (`git diff > /tmp/appearance.patch`, keep the appearance hunks, `git apply --cached`);
- never run `git checkout --`, `git restore`, `git stash`, or a bare `git reset`;
- re-read `server.go` immediately before editing, because another session can change it between turns.

Files this plan touches that are currently clean: `internal/config/ocodeconfig.go`,
`internal/server/handler.go`, `internal/theme/theme.go`, `web/src/hooks/useTheme.ts`,
`web/src/components/Files/FileEditor.tsx`. All other appearance files are new.

## Global Constraints

- No new Go or npm dependencies. `encoding/json` only for this slice.
- Go 1.26; `gofmt` clean; `go vet ./...` clean.
- Web typecheck is `cd web && bun check .`; build is `cd web && npm run build`.
- Every goroutine goes through `crashguard.Go`.
- New routes use `s.authMiddleware(...)`, are local-only, take no `?host=`, and no handler calls `os.Getwd()`.
- Tests that override `HOME` use the package's `setHomeTree(t, home)` helper.
- Theme ids are `vscode:<namespace>.<extension>/<variant-slug>`; slugs match `^[a-z0-9-]+$`.
- The theme author's colours are used verbatim. Only synthesized foregrounds and `--link` may be repaired (spec §8.2).
- Mapping tables in spec §8.2–§8.5 are the complete key lists. No `??`-style substitution of an unrelated key.
- Commits are path-limited; the working tree holds unrelated edits in `internal/paths/paths.go`, `internal/server/server.go`, `internal/remote/*`, `docs/*`.

## Review Focus

1. **A theme with no `colors` block** (tokenColors-only) must still convert, deriving `ui`/`colors` from token colours, and must never panic or emit empty CSS vars.
2. **An `include` path that escapes the extension directory** or exceeds depth 5 is rejected with a warning; the theme still loads without the included layer.
3. **JSONC containing `//` inside a quoted string** (a URL in a colour value) is preserved; comment stripping must skip strings.
4. **A VS Code theme whose polarity contradicts an explicit editor override** must show the theme's colours only when the override is off; the override must win and the UI must say so.
5. **A configured `color_theme` id that no longer exists** (uninstalled extension) must fall back to `tui.theme` then `tokyonight`, emit a warning, and never break page load.

---

### Task 1: JSONC decoding, `tokenColors` shape, and `include` chains

**Risk:** high — untrusted file parsing with two shape variants and a containment rule.

**Files:**
- Create: `internal/appearance/jsonc.go`
- Test: `internal/appearance/jsonc_test.go`
- Create fixtures: `internal/appearance/testdata/jsonc/comments.json`, `internal/appearance/testdata/include/base.json`, `internal/appearance/testdata/include/child.json`, `internal/appearance/testdata/include/escape.json`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type RawTheme struct { Name string; Type string; Include string; Colors map[string]string; TokenColors tokenColors }`
  - `type tokenColors struct { Rules []TokenRule; File string }` with `UnmarshalJSON` accepting an array **or** a string path.
  - `type TokenRule struct { Scope scopeList; Settings TokenSettings }`, `type TokenSettings struct { Foreground string; Background string; FontStyle string }`, `type scopeList []string` accepting a string or `[]string`.
  - `func DecodeJSONC(data []byte, v any) error`
  - `func LoadThemeTree(path, root string, depth int) (*RawTheme, []string, error)` — returns the merged theme and non-fatal warnings.

- [ ] **Step 1: Write the failing tests**

```go
func TestDecodeJSONCPreservesCommentMarkersInsideStrings(t *testing.T) {
	data := []byte(`{"colors":{"textLink.foreground":"https://x.example/a//b"}, /* c */ "type":"dark",}`)
	var out map[string]any
	if err := DecodeJSONC(data, &out); err != nil { t.Fatal(err) }
	got := out["colors"].(map[string]any)["textLink.foreground"]
	if got != "https://x.example/a//b" { t.Fatalf("got %v", got) }
}

func TestLoadThemeTreeMergesIncludeAndChildWins(t *testing.T) {
	theme, warn, err := LoadThemeTree("testdata/include/child.json", "testdata/include", 0)
	// child sets editor.background #111111 and inherits editor.foreground #eeeeee
	// assert: no warnings, both keys present, child's background wins.
}

func TestLoadThemeTreeRejectsEscapingInclude(t *testing.T) {
	_, warn, err := LoadThemeTree("testdata/include/escape.json", "testdata/include", 0)
	// assert: err == nil, exactly one warning mentioning containment,
	// and the returned theme has no inherited colours.
}

func TestTokenColorsAcceptsPathString(t *testing.T) {
	// RawTheme JSON with "tokenColors":"./theme.tmTheme" decodes with Rules == nil
	// and File == "./theme.tmTheme"; a loader warning is produced by LoadThemeTree.
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/appearance -run 'JSONC|Include|TokenColors' -v`
Expected: FAIL — package/function not defined.

- [ ] **Step 3: Implement `DecodeJSONC`, the raw-theme types, and `LoadThemeTree`**

Copy the comment-matching shape from `internal/config/config.go:624` (`jsoncComments`) rather than importing `config`. Strip trailing commas after comment removal. `LoadThemeTree` resolves `include` depth-first (cap 5), rejoins paths against `root`, and applies `EvalSymlinks` containment before reading. Merge order: included layer first, child overrides per key; `TokenColors.Rules` from the child replaces the parent's when non-empty. Any containment/depth failure appends a warning and continues with what was read.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/appearance -run 'JSONC|Include|TokenColors' -v`
Expected: PASS.

---

### Task 2: Conversion into the four palettes

**Risk:** high — branching key lookups plus a scope-mapping table that must match the spec exactly.

**Files:**
- Create: `internal/appearance/types.go`, `internal/appearance/color.go`
- Test: `internal/appearance/color_test.go`
- Create fixtures: `internal/appearance/testdata/catppuccin-mocha.json` (trimmed real theme: full `colors` block, ~10 representative `tokenColors` entries), `internal/appearance/testdata/gruvbox-light.json` (hand-authored light theme), `internal/appearance/testdata/no-colors.json` (tokenColors only)

**Interfaces:**
- Consumes: `RawTheme`, `TokenRule` from Task 1.
- Produces:
  - `type Polarity string` (`"dark"`, `"light"`)
  - `type ConvertedTheme struct { ID string; Label string; Kind Polarity; Colors theme.ThemeColors; UI map[string]string; Monaco MonacoTheme; Terminal TerminalPalette; Warnings []string }`
  - `type MonacoTheme struct { Base string; Colors map[string]string; Rules []MonacoRule }`, `type MonacoRule struct { Token, Foreground, FontStyle string }`
  - `type TerminalPalette struct { Dark *TerminalColors; Light *TerminalColors }` and `type TerminalColors struct { Foreground, Background, Cursor, CursorText, SelectionBackground, SelectionForeground string; Palette []string }`
  - `func ConvertTheme(id, label string, raw *RawTheme) (ConvertedTheme, error)`
  - `func ResolveTokenScopes(rules []TokenRule) []MonacoRule`
  - `func PolarityFromType(kind, background string) Polarity`

- [ ] **Step 1: Write the failing tests**

```go
func TestConvertCatppuccinMocha(t *testing.T) {
	got, err := ConvertTheme("vscode:test/mocha", "Catppuccin Mocha", loadFixture(t, "catppuccin-mocha.json"))
	if err != nil { t.Fatal(err) }
	assertEq(t, got.Kind, Polarity("dark"))
	assertEq(t, got.UI["--background"], "#1e1e2e")
	assertEq(t, got.UI["--card"], "#181825")
	assertEq(t, got.UI["--ring"], "#cba6f7")
	assertEq(t, got.Monaco.Base, "vs-dark")
	assertEq(t, got.Monaco.Colors["editor.background"], "#1e1e2e")
	assertEq(t, got.Terminal.Dark.Palette[1], "#f38ba8") // ansiRed
	assertEq(t, got.Terminal.Light, (*TerminalColors)(nil))
	assertEq(t, got.Colors.Accent, "#cba6f7")
	assertEq(t, got.Colors.Text, "#cdd6f4")
	assertRule(t, got.Monaco.Rules, "comment", "#9399b2", "italic")
	assertRule(t, got.Monaco.Rules, "string", "#a6e3a1", "")
}

func TestConvertLightThemePutsPaletteInLight(t *testing.T) {
	// gruvbox-light: Kind == "light", Monaco.Base == "vs",
	// Terminal.Light != nil, Terminal.Dark == nil.
}

func TestConvertThemeWithoutColorsDerivesFromTokenColors(t *testing.T) {
	// no-colors.json: no error, UI["--background"] non-empty,
	// Colors.Text non-empty, at least one warning.
}

func TestUIHasNoRepairedSourceColours(t *testing.T) {
	// For catppuccin: UI["--foreground"] == "#cdd6f4" (author value, untouched).
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/appearance -run 'Convert|ResolveToken|Polarity' -v`
Expected: FAIL — not defined.

- [ ] **Step 3: Implement the conversion**

Implement exactly the four tables in spec §8.2 (web), §8.3 (Monaco colours + scope→token prefixes), §8.4 (terminal), §8.5 (TUI). Lookups use a first-present-wins helper over an ordered key list per variable; an unmapped variable is omitted, not invented. Synthesized foregrounds (`--primary-foreground`, `--destructive-foreground`, `--secondary-foreground`) use a WCAG AA pick between the palette's text colour and pure black/white; `--link` is repaired only when it fails AA against both `--background` and `--muted`. Missing ANSI slots fall back to ocode's default palette at the same index (reuse the builtin `tokyonight` values as the fallback source). `TerminalPalette` carries only the block matching `Kind`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/appearance -run 'Convert|ResolveToken|Polarity' -v`
Expected: PASS.

---

### Task 3: Discovery, service cache, config block, and HTTP surface

**Risk:** high — filesystem scanning, id validation, a persisted setting, and public endpoints.

**Files:**
- Create: `internal/appearance/discover.go`, `internal/appearance/service.go`
- Test: `internal/appearance/discover_test.go`, `internal/appearance/service_test.go`
- Modify: `internal/config/ocodeconfig.go` (new `Appearance` block in the `Ocode` struct + save/load + payload handling), `internal/config/ocodeconfig_test.go`
- Create: `internal/server/handler_appearance.go`, `internal/server/handler_appearance_test.go`
- Modify: `internal/server/handler.go` (construct the service), `internal/server/server.go` (routes), `internal/server/handler_theme.go` (resolve `vscode:` ids)

**Interfaces:**
- Consumes: `ConvertTheme`, `LoadThemeTree` from Tasks 1–2.
- Produces:
  - `type ThemeRef struct { ID, Label string; Source string; Kind Polarity; Extension string }`
  - `func ExtensionRoots(home string, extra []string) []string` — ordered: extra, `.vscode`, `.vscode-insiders`, `.vscode-oss`, `.cursor`, `.windsurf` under `home`, each `…/extensions`.
  - `type Manifest struct { ID, Namespace, Name, Version, Dir string; Themes, IconThemes []Contribution }`, `type Contribution struct { ID, Label, Path string }`
  - `func Discover(ctx context.Context, roots []string) ([]Manifest, []string, error)`
  - `type Service struct{…}`; `func NewService(home string, extra []string) *Service`; `func (s *Service) Themes(ctx context.Context) ([]ThemeRef, []string, error)`; `func (s *Service) Resolve(ctx context.Context, id string) (ConvertedTheme, error)`; `func (s *Service) Refresh()`
  - Config: `config.AppearanceConfig{ ColorTheme string; IconTheme string; ExtraExtensionDirs []string }` on `Ocode`, JSON key `appearance`.
  - Handlers: `HandleAppearanceThemes`, `HandleAppearanceTheme`, `HandleGetAppearanceSettings`, `HandleSetAppearanceSettings`.

- [ ] **Step 1: Write the failing tests**

```go
func TestDiscoverReadsThemeContributions(t *testing.T) {
	// temp dir with fake-extension/package.json declaring one theme;
	// Discover returns Manifest{ID:"acme.themes", Themes:[{Label:"Acme Dark", Path:…}]}.
}

func TestDiscoverSkipsDirectoriesWithoutManifest(t *testing.T) {
	// a dir with no package.json produces no manifest and no error.
}

func TestServiceResolveRejectsMalformedID(t *testing.T) {
	// "vscode:../../etc/passwd" -> error; "vscode:acme.themes/ok" -> found.
}

func TestServiceCachesByModTime(t *testing.T) {
	// Resolve twice; converter runs once (count via a package-level hook var).
	// Touch the file; third Resolve converts again.
}

func TestAppearanceSettingsRoundTrip(t *testing.T) {
	// Set ColorTheme + ExtraExtensionDirs, reload config, values persist,
	// unknown keys in ocodeconfig.json survive.
}

func TestSetAppearanceSettingsRejectsUnknownTheme(t *testing.T) {
	// PUT with "vscode:nope/none" -> 400 and nothing persisted.
}

func TestGetThemeResolvesVSCodeID(t *testing.T) {
	// GET /api/theme?name=vscode:acme.themes/dark returns 16-color ThemeColors
	// identical to /api/appearance/theme/{id}.colors.
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/appearance ./internal/config ./internal/server -run 'Discover|Service|Appearance|Theme' -v`
Expected: FAIL — not defined.

- [ ] **Step 3: Implement discovery, service, config, and endpoints**

Discovery scans in `ExtensionRoots` order, memoises for 5 minutes (spec §6), and returns warnings for unreadable contributions. The service validates ids with `^vscode:[A-Za-z0-9._-]+/[a-z0-9-]+$` before touching the filesystem, resolves the manifest, then caches `ConvertedTheme` keyed by absolute path + mtime + size. Routes (spec §12): `GET /api/appearance/themes`, `GET /api/appearance/theme/{id}`, `GET /api/appearance/settings`, `PUT /api/appearance/settings`. `PUT` validates the id resolves before persisting and returns the stored block. `/api/themes` appends converted refs; `/api/theme?name=` resolves `vscode:` ids through the service so the two endpoints cannot disagree. Unknown configured theme → warning + fallback to `tui.theme` then `tokyonight`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/appearance ./internal/config ./internal/server -run 'Discover|Service|Appearance|Theme' -v`
Expected: PASS. Then `go build ./... && go vet ./...`.

---

### Task 4: Web appearance store, rich `useTheme`, and persisted selection

**Risk:** standard — new module store plus one hook change.

**Files:**
- Create: `web/src/lib/appearanceTheme.ts`, `web/src/lib/appearanceTheme.test.ts`
- Modify: `web/src/api/client.ts` (4 methods), `web/src/api/types.ts` (`ConvertedTheme`, `ThemeRef`, `AppearanceSettings`)
- Modify: `web/src/hooks/useTheme.ts`, `web/src/hooks/useTheme.test.ts`
- Modify: `web/src/components/Settings/ThemeForm.tsx`, `web/src/components/Settings/ThemeForm.test.tsx` (new)

**Interfaces:**
- Consumes: the endpoints from Task 3.
- Produces:
  - `api.getAppearanceThemes()`, `api.getAppearanceTheme(id)`, `api.getAppearanceSettings()`, `api.setAppearanceSettings(patch)`
  - `getColorThemeId(): string`, `setColorThemeId(id: string): Promise<void>`
  - `loadAppearanceTheme(id?: string): Promise<ConvertedTheme>`
  - `useAppearanceTheme(): { theme: ConvertedTheme | null; colorThemeId: string; setColorThemeId(id: string): Promise<void> }`
  - `applyAppearanceUI(theme: ConvertedTheme): void` — writes `theme.ui` as HSL triplets, then repairs only the synthesized foregrounds.
  - `useTheme()` unchanged in shape (`{ serverColors }`), now routing through the store.

- [ ] **Step 1: Write the failing tests**

```ts
it("applies the theme's ui map verbatim", async () => {
  await apply(); // mocked fetch returns ui {"--background":"#1e1e2e"}
  expect(document.documentElement.style.getPropertyValue("--background")).toBe("240 21.1% 14.9%");
});
it("leaves a missing ui key at the stylesheet default", async () => { … });
it("repairs only a synthesized foreground that fails AA", async () => { … });
it("persists the picked theme through setAppearanceSettings", async () => { … });
it("falls back to the legacy 16-colour path when ui is absent", async () => { … });
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && npx vitest run src/lib/appearanceTheme.test.ts src/hooks/useTheme.test.ts src/components/Settings/ThemeForm.test.tsx`
Expected: FAIL — modules not found.

- [ ] **Step 3: Implement the store, hook change, and form persistence**

The store mirrors `chatVerbosity.ts`: fetch once per id, keep last-good on error, never apply a partial payload. `useTheme()` calls the store on mount; when the payload has `ui`, call `applyAppearanceUI`, else keep the existing `computeThemeVars` path untouched. `ThemeForm` click handler awaits `setColorThemeId` (PUT) before applying, surfaces failures inline, and shows the current id.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd web && npx vitest run src/lib/appearanceTheme.test.ts src/hooks/useTheme.test.ts src/components/Settings/ThemeForm.test.tsx`
Expected: PASS. Then `cd web && bun check .`.

---

### Task 5: Editor precedence — `auto`, one resolver, Monaco wiring

**Risk:** high — three existing controls with a documented, visible precedence.

**Files:**
- Modify: `web/src/lib/editorAppearance.ts`, `web/src/lib/editorAppearance.test.ts`
- Create: `web/src/lib/monacoTheme.ts`, `web/src/lib/monacoTheme.test.ts`
- Modify: `web/src/components/Files/FileEditor.tsx:179-205,480-560,1079`, `web/src/components/Files/FileTabContent.tsx:98`, `web/src/components/Preview/*` call sites of `editorAppearanceClass`

**Interfaces:**
- Consumes: `ConvertedTheme` from Task 4.
- Produces:
  - `type EditorAppearanceValue = "auto" | "light" | "dark"`
  - `getEditorAppearance(): EditorAppearanceValue` (unset/unparsable → `"auto"`)
  - `resolveEffectiveEditorAppearance(value: EditorAppearanceValue, themePolarity: "dark" | "light"): "dark" | "light"`
  - `resolveEditorTheme(input: { appearance: EditorAppearanceValue; theme: ConvertedTheme | null; monacoTheme: string }): { id: string; define: MonacoTheme | null }`
  - `applyMonacoTheme(resolved)` — `monaco.editor.defineTheme(id, …)` once per id, then `setTheme(id)`.

- [ ] **Step 1: Write the failing tests**

```ts
it.each([
  ["light",  theme,  "ocode-light"],
  ["dark",   theme,  "ocode-dark"],
  ["auto",   theme,  "ocode-vscode:acme/dark"],
  ["auto",   null,   "ocode-dark"],
  ["auto",   builtin,"vs-dark-from-monaco.theme"],
])("resolves the editor theme for %s", (appearance, theme, expected) => { … });

it("reads an absent stored value as auto", () => {
  localStorage.clear();
  expect(getEditorAppearance()).toBe("auto");
});
it("keeps an explicitly stored preference", () => { … });
it("defines the monaco theme once per id", () => { … });
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && npx vitest run src/lib/monacoTheme.test.ts src/lib/editorAppearance.test.ts`
Expected: FAIL.

- [ ] **Step 3: Implement the resolver and rewire `FileEditor`**

`FileEditor` stops computing its own theme and stops coercing (`FileEditor.tsx:190`); it calls `resolveEditorTheme`, defines on change, and `setTheme`. `editorAppearanceClass` receives the **effective** value, so callers pass `resolveEffectiveEditorAppearance(...)`. The Settings page (still `ThemeForm` in this slice) shows the "editor override is on" notice with a **Use theme colours** action that writes `"auto"` when an explicit override is set.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd web && npx vitest run src/lib/monacoTheme.test.ts src/lib/editorAppearance.test.ts src/components/Files` and `cd web && bun check .`
Expected: PASS, typecheck clean.

---

## Slice Acceptance

- `go build ./...`, `go vet ./...`, `go test ./internal/appearance ./internal/config ./internal/server/` pass.
- `cd web && bun check .`, `cd web && npx vitest run src/lib src/hooks src/components/Settings src/components/Files` pass.
- Manual: with `appearance.color_theme` set to a discovered theme, the chrome, Monaco and (once slice 3 lands) terminal/TUI reflect it; `/api/themes` lists it; selecting another theme persists across a reload.

## Post-slice documentation

All `docs/` writes go through the `context` sub-agent (`doc_write`), never the main agent:

1. **Concept page** `concepts/vscode-themes-and-icons.md` — source discovery paths, the four conversion
   outputs, the `/api/appearance/*` surface, the §8.6 editor-theme precedence, and the deliberate
   limitations (no semantic tokens, `.tmTheme` unsupported, Open VSX only).
2. **Index and log** — `docs/index.md` and `docs/log.md` are updated by `doc_write` itself; verify the new
   entry landed in the right section afterwards (it has mis-filed entries before).
3. **`CHANGES.md`** — one entry naming the feature and the settings key.
4. **`skills/ocode-web/SKILL.md`** — extend the theming section with the appearance store and the
   editor-precedence resolver. **Read the file first** (`CLAUDE.md` "read the target first"); it is
   concurrently edited, so use an anchored `edit`, never a whole-file rewrite.
5. **`TODO.md`** — the deferred items from spec §19 already exist; add nothing new unless the slice
   discovers one.

Code anchors: every `file.go:NNN` referenced by the design spec (`FileTree.tsx:202`, `FileEditor.tsx:190`,
`internal/monaco/monaco.go:25`, `internal/config/config.go:624`) must be re-derived after this slice, because
added lines shift them. Do it mechanically (print each cited line) rather than trusting a list.
