// Package ghosttyconfig reads the user's Ghostty configuration and reports the
// subset of it a browser terminal can honour.
//
// WHY this exists: ocode's interactive terminal is ghostty-web — libghostty-vt
// compiled to wasm and drawn on a 2D canvas. Ghostty's own config loader is not
// exposed by that build (ghostty_web's `Ghostty.load` takes a wasm path and
// nothing else), so nothing can "just use Ghostty's config". This package
// re-reads the same files, in Ghostty's own documented order, and maps the
// appearance keys onto the options ghostty-web does expose.
//
// It is deliberately NOT authoritative about the terminal: it produces a
// report (what was found, what maps, what cannot map) and the caller decides
// precedence. Everything here is read-only and never writes a Ghostty file.
package ghosttyconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// CellAdjust is a cell-size delta from the font's natural metrics, matching
// Ghostty's adjust-cell-width / adjust-cell-height semantics: the values are
// DELTAS (not absolute sizes), expressed either as a percentage of the measured
// cell or as a pixel count. A zero value means "no adjustment".
type CellAdjust struct {
	Percent float64 `json:"percent,omitempty"`
	Px      float64 `json:"px,omitempty"`
}

// IsZero reports whether the adjustment is "no change".
func (c CellAdjust) IsZero() bool { return c.Percent == 0 && c.Px == 0 }

// Keybind is one parsed `keybind = chord=action[:arg]` line.
type Keybind struct {
	Chord  string `json:"chord"`
	Action string `json:"action"`
	Arg    string `json:"arg,omitempty"`
	// Unbound is `keybind = chord=unbind`: Ghostty's way to remove a binding.
	Unbound bool `json:"unbound,omitempty"`
	// Global is a `global:` prefixed chord (system-wide hotkey in Ghostty). A
	// browser tab can never register one; the report keeps it visible.
	Global bool `json:"global,omitempty"`
}

// Ignored is one configuration key that was read but cannot be applied, with a
// human-readable reason. Surfacing these is the point: silently dropping a
// user's appearance settings is worse than saying why they do not transfer.
type Ignored struct {
	Key    string `json:"key"`
	Value  string `json:"value,omitempty"`
	Reason string `json:"reason"`
}

// ThemeColors is a resolved Ghostty theme. Empty fields are "not specified";
// the renderer's own defaults apply for those, so the caller must never fill
// them with a guessed palette.
type ThemeColors struct {
	Foreground          string `json:"foreground,omitempty"`
	Background          string `json:"background,omitempty"`
	Cursor              string `json:"cursor,omitempty"`
	CursorText          string `json:"cursor_text,omitempty"`
	SelectionBackground string `json:"selection_background,omitempty"`
	SelectionForeground string `json:"selection_foreground,omitempty"`
	// Palette is 16 "#rrggbb" entries (ANSI 0-15) when the config specified any;
	// nil means the theme did not set a palette.
	Palette []string `json:"palette,omitempty"`
}

// Config is the resolved result of reading Ghostty's configuration.
type Config struct {
	// Found is true when at least one Ghostty config file existed and was read.
	Found bool `json:"found"`
	// Paths are the config files actually read, in load order (later wins).
	Paths []string `json:"paths,omitempty"`
	// Errors are non-fatal problems (unreadable includes, bad theme, cycles).
	Errors []string `json:"errors,omitempty"`

	FontFamily string     `json:"font_family,omitempty"`
	FontSize   float64    `json:"font_size,omitempty"`
	CellWidth  CellAdjust `json:"cell_width"`
	CellHeight CellAdjust `json:"cell_height"`

	// ThemeName is the name Ghostty would show ("Bright Lights", or
	// "light:X,dark:Y" for a split theme).
	ThemeName string `json:"theme_name,omitempty"`
	// Light and Dark are the resolved palettes for each appearance. With a
	// single (non-split) theme both point at the same colours.
	Light *ThemeColors `json:"light,omitempty"`
	Dark  *ThemeColors `json:"dark,omitempty"`

	Keybinds []Keybind `json:"keybinds,omitempty"`
	Ignored  []Ignored `json:"ignored,omitempty"`
}

// Options lets tests pin the environment instead of touching the real one.
type Options struct {
	Home          string
	GOOS          string
	XDGConfigHome string
	// ResourcesDirs are candidate Ghostty resource roots, searched in order for
	// themes/. Empty means "use the platform defaults".
	ResourcesDirs []string
}

// DefaultOptions builds Options from the live process environment.
func DefaultOptions() Options {
	home, _ := os.UserHomeDir()
	return Options{
		Home:          home,
		GOOS:          runtime.GOOS,
		XDGConfigHome: os.Getenv("XDG_CONFIG_HOME"),
		ResourcesDirs: defaultResourcesDirs(os.Getenv("GHOSTTY_RESOURCES_DIR")),
	}
}

// Load reads Ghostty's configuration from the current user's standard
// locations. It never fails: a missing configuration is reported as
// Found=false, and parse problems land in Errors.
func Load() Config { return LoadWith(DefaultOptions()) }

// LoadWith is Load with an explicit environment.
func LoadWith(opts Options) Config {
	if opts.Home == "" {
		opts.Home, _ = os.UserHomeDir()
	}
	if opts.GOOS == "" {
		opts.GOOS = runtime.GOOS
	}
	if opts.ResourcesDirs == nil {
		opts.ResourcesDirs = defaultResourcesDirs("")
	}

	p := &parser{opts: opts}
	for _, path := range configPaths(opts) {
		if !fileExists(path) {
			continue
		}
		p.cfg.Found = true
		p.parseFile(path)
	}
	return p.finish()
}

// configPaths lists Ghostty's default config files in Ghostty's own load order
// (later files override earlier ones):
//
//	$XDG_CONFIG_HOME/ghostty/config.ghostty
//	$XDG_CONFIG_HOME/ghostty/config
//	~/Library/Application Support/com.mitchellh.ghostty/config.ghostty   (macOS)
//	~/Library/Application Support/com.mitchellh.ghostty/config           (macOS)
//
// All macOS-specific files load AFTER every XDG file, which is why `theme = …`
// in the app-support file wins over a font set in ~/.config/ghostty/config.
func configPaths(opts Options) []string {
	xdg := opts.XDGConfigHome
	if xdg == "" {
		xdg = filepath.Join(opts.Home, ".config")
	}
	dir := filepath.Join(xdg, "ghostty")
	paths := []string{
		filepath.Join(dir, "config.ghostty"),
		filepath.Join(dir, "config"),
	}
	if opts.GOOS == "darwin" {
		support := filepath.Join(opts.Home, "Library", "Application Support", "com.mitchellh.ghostty")
		paths = append(paths,
			filepath.Join(support, "config.ghostty"),
			filepath.Join(support, "config"),
		)
	}
	return paths
}

// themeDirs lists the directories Ghostty searches for a named theme, in its
// own order: the user's themes directory first, then the shipped resources.
func themeDirs(opts Options) []string {
	xdg := opts.XDGConfigHome
	if xdg == "" {
		xdg = filepath.Join(opts.Home, ".config")
	}
	dirs := []string{filepath.Join(xdg, "ghostty", "themes")}
	for _, root := range opts.ResourcesDirs {
		dirs = append(dirs, filepath.Join(root, "themes"))
	}
	return dirs
}

func defaultResourcesDirs(explicit string) []string {
	var dirs []string
	if explicit != "" {
		dirs = append(dirs, explicit)
	}
	if runtime.GOOS == "darwin" {
		dirs = append(dirs,
			"/Applications/Ghostty.app/Contents/Resources/ghostty",
			filepath.Join("/Applications", "Ghostty.app", "Contents", "Resources", "ghostty"),
		)
	}
	dirs = append(dirs,
		"/usr/share/ghostty",
		"/usr/local/share/ghostty",
		"/opt/homebrew/share/ghostty",
		"/snap/ghostty/current/share/ghostty",
	)
	return dirs
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// parser accumulates keys in load order so "later wins" and "empty value
// resets to default" work without keeping every intermediate state.
type parser struct {
	opts Options
	cfg  Config
	// themeOnly parses a theme file: `theme` is ignored (Ghostty does the same)
	// and unrelated keys are not reported, since the user did not write them.
	themeOnly bool

	// current file stack, for config-file cycle detection
	stack []string

	fontFamily string
	fontSize   float64
	cellWidth  CellAdjust
	cellHeight CellAdjust
	themeSpec  string
	colors     map[string]string
	palette    [16]string
	paletteSet [16]bool
	keybinds   []Keybind
}

func (p *parser) parseFile(path string) {
	if path == "" {
		// An optional `config-file = ?missing` include that does not exist.
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	for _, seen := range p.stack {
		if seen == abs {
			p.errf("config-file cycle at %s", path)
			return
		}
	}
	if len(p.stack) >= maxIncludeDepth {
		p.errf("config-file nesting deeper than %d levels at %s", maxIncludeDepth, path)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		p.errf("cannot read %s: %v", path, err)
		return
	}
	p.stack = append(p.stack, abs)
	defer func() { p.stack = p.stack[:len(p.stack)-1] }()

	if len(p.cfg.Paths) == 0 || p.cfg.Paths[len(p.cfg.Paths)-1] != path {
		p.cfg.Paths = append(p.cfg.Paths, path)
	}

	for _, inc := range p.parseContent(path, string(data)) {
		base := filepath.Dir(path)
		p.parseFile(resolveInclude(base, inc))
	}
}

// parseContent applies one file's lines and returns its config-file includes.
// Includes are NOT followed here: Ghostty processes them after the rest of the
// file, and the caller owns the recursion (so cycle detection stays in one
// place). Keeping this I/O-free is what makes the line grammar testable.
func (p *parser) parseContent(path, content string) []string {
	var includes []string
	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(rawLine, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = unquote(strings.TrimSpace(value))
		if key == "" {
			continue
		}
		if key == "config-file" {
			// Ghostty processes includes AFTER the rest of the file, which is
			// why a key written below a config-file line cannot override the
			// included file.
			if !p.themeOnly {
				includes = append(includes, value)
			}
			continue
		}
		if key == "theme" && p.themeOnly {
			continue
		}
		p.apply(key, value)
	}
	return includes
}

// resolveInclude expands a config-file value: "?/path" is optional (a missing
// file is not an error), and relative paths resolve against the including
// file's directory.
func resolveInclude(base, value string) string {
	optional := strings.HasPrefix(value, "?")
	if optional {
		value = strings.TrimPrefix(value, "?")
	}
	value = expandHome(value)
	if !filepath.IsAbs(value) {
		value = filepath.Join(base, value)
	}
	if optional && !fileExists(value) {
		return ""
	}
	return value
}

func expandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		return filepath.Join(home, path[2:])
	}
	return path
}

func (p *parser) apply(key, value string) {
	switch key {
	case "font-family":
		p.fontFamily = value
	case "font-size":
		if value == "" {
			p.fontSize = 0
			return
		}
		size, err := strconv.ParseFloat(value, 64)
		if err != nil {
			p.ignored(key, value, "not a number")
			return
		}
		p.fontSize = size
	case "adjust-cell-width":
		p.cellWidth = p.parseCellAdjust(key, value)
	case "adjust-cell-height":
		p.cellHeight = p.parseCellAdjust(key, value)
	case "theme":
		p.themeSpec = value
	case "background", "foreground", "cursor-color", "cursor-text",
		"selection-background", "selection-foreground":
		p.applyColor(key, value)
	case "palette":
		p.applyPalette(value)
	case "keybind":
		p.applyKeybind(value)
	default:
		reason := unsupportedReason(key)
		// A shipped theme may set window/session options the user never wrote;
		// only its appearance-affecting keys are worth reporting.
		if p.themeOnly && reason == genericReason {
			return
		}
		p.ignored(key, value, reason)
	}
}

// applyColor records an explicit colour (or clears it when the value is empty,
// which Ghostty defines as "reset to default").
func (p *parser) applyColor(key, value string) {
	if p.colors == nil {
		p.colors = map[string]string{}
	}
	if value == "" {
		delete(p.colors, key)
		return
	}
	hex, ok := normalizeHex(value)
	if !ok {
		p.ignored(key, value, "only #rrggbb colours are translated (named X11 colours are not)")
		return
	}
	p.colors[key] = hex
}

// applyPalette parses Ghostty's `palette = N=#rrggbb` form. Only ANSI 0-15 can
// be applied; ghostty-web's theme surface has exactly 16 entries.
func (p *parser) applyPalette(value string) {
	if value == "" {
		p.palette = [16]string{}
		p.paletteSet = [16]bool{}
		return
	}
	idxText, colorText, ok := strings.Cut(value, "=")
	if !ok {
		p.ignored("palette", value, "expected N=#rrggbb")
		return
	}
	idx, err := strconv.Atoi(strings.TrimSpace(idxText))
	if err != nil || idx < 0 || idx > 255 {
		p.ignored("palette", value, "index must be 0-255")
		return
	}
	hex, ok := normalizeHex(strings.TrimSpace(colorText))
	if !ok {
		p.ignored("palette", value, "only #rrggbb colours are translated")
		return
	}
	if idx > 15 {
		p.ignored("palette", value, "only ANSI 0-15 is configurable in the web renderer")
		return
	}
	p.palette[idx] = hex
	p.paletteSet[idx] = true
}

func (p *parser) applyKeybind(value string) {
	if value == "" {
		p.keybinds = nil
		return
	}
	chordText, actionText, ok := strings.Cut(value, "=")
	if !ok {
		p.ignored("keybind", value, "expected chord=action")
		return
	}
	chord := strings.TrimSpace(chordText)
	global := false
	if strings.HasPrefix(chord, "global:") {
		global = true
		chord = strings.TrimPrefix(chord, "global:")
	}
	actionSpec := strings.TrimSpace(actionText)
	action, arg, _ := strings.Cut(actionSpec, ":")
	action = strings.TrimSpace(action)
	if chord == "" || action == "" {
		p.ignored("keybind", value, "expected chord=action")
		return
	}
	kb := Keybind{Chord: chord, Action: action, Arg: strings.TrimSpace(arg), Global: global}
	if action == "unbind" {
		kb.Unbound = true
	}
	// Later bindings for the same chord replace earlier ones, matching Ghostty.
	for i := range p.keybinds {
		if p.keybinds[i].Chord == kb.Chord {
			p.keybinds[i] = kb
			return
		}
	}
	p.keybinds = append(p.keybinds, kb)
}

func (p *parser) parseCellAdjust(key, value string) CellAdjust {
	if value == "" {
		return CellAdjust{}
	}
	percent := strings.HasSuffix(value, "%")
	number := strings.TrimSuffix(value, "%")
	delta, err := strconv.ParseFloat(strings.TrimSpace(number), 64)
	if err != nil {
		p.ignored(key, value, "expected a pixel integer or a percentage")
		return CellAdjust{}
	}
	if percent {
		return CellAdjust{Percent: delta}
	}
	return CellAdjust{Px: delta}
}

func (p *parser) ignored(key, value, reason string) {
	for _, existing := range p.cfg.Ignored {
		if existing.Key == key {
			return
		}
	}
	p.cfg.Ignored = append(p.cfg.Ignored, Ignored{Key: key, Value: value, Reason: reason})
}

func (p *parser) errf(format string, args ...any) {
	p.cfg.Errors = append(p.cfg.Errors, fmt.Sprintf(format, args...))
}

// finish resolves the theme and flattens the accumulated state into Config.
func (p *parser) finish() Config {
	// Resolve BEFORE copying: theme resolution appends to p.cfg.Errors, and a
	// copy taken first would silently drop every theme problem.
	p.cfg.Light, p.cfg.Dark = p.resolveThemes()
	p.cfg.FontFamily = normalizeFontFamily(p.fontFamily)
	p.cfg.FontSize = clampFontSize(p.fontSize)
	p.cfg.CellWidth = p.cellWidth
	p.cfg.CellHeight = p.cellHeight
	p.cfg.ThemeName = p.themeSpec
	p.cfg.Keybinds = p.keybinds
	return p.cfg
}

func clampFontSize(size float64) float64 {
	if size <= 0 {
		return 0
	}
	if size < 6 {
		return 6
	}
	if size > 48 {
		return 48
	}
	return size
}

// normalizeFontFamily turns Ghostty's family list into a CSS font stack: each
// family is quoted when it contains spaces (an unquoted "Fira Code" makes the
// whole declaration invalid) and a generic monospace fallback is appended so a
// missing font still produces a usable terminal.
func normalizeFontFamily(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var families []string
	for _, part := range strings.Split(raw, ",") {
		fam := strings.TrimSpace(part)
		if fam == "" {
			continue
		}
		if !strings.HasPrefix(fam, `"`) && !strings.HasPrefix(fam, "'") {
			if strings.ContainsAny(fam, " \t") {
				fam = `"` + fam + `"`
			}
		}
		families = append(families, fam)
	}
	if len(families) == 0 {
		return ""
	}
	joined := strings.Join(families, ", ")
	if !strings.Contains(joined, "monospace") {
		joined += ", ui-monospace, SFMono-Regular, Menlo, monospace"
	}
	return joined
}

// normalizeHex accepts "#rrggbb" or "rrggbb" (either case) and returns the
// canonical "#rrggbb" form.
func normalizeHex(value string) (string, bool) {
	v := strings.TrimSpace(value)
	v = strings.TrimPrefix(v, "#")
	if len(v) != 6 {
		return "", false
	}
	for _, r := range v {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return "", false
		}
	}
	return "#" + strings.ToLower(v), true
}

func unquote(value string) string {
	if len(value) >= 2 && strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
		return value[1 : len(value)-1]
	}
	return value
}

// unsupportedReason explains, per key family, why a Ghostty option has no
// browser-terminal equivalent. A generic answer is better than silence.
func unsupportedReason(key string) string {
	switch {
	case strings.HasPrefix(key, "custom-shader"):
		return "custom GLSL shaders cannot run on the canvas renderer"
	case key == "background-opacity", strings.HasPrefix(key, "background-blur"):
		return "cells are drawn opaque on a 2D canvas"
	case strings.HasPrefix(key, "background-image"):
		return "background images are not rendered by the web terminal"
	case key == "window-padding-x", key == "window-padding-y",
		key == "window-padding-balance", key == "window-padding-color":
		return "padding is owned by the ocode panel layout"
	case strings.HasPrefix(key, "window-"):
		return "window chrome is owned by ocode, not the terminal emulator"
	case strings.HasPrefix(key, "font-feature"), strings.HasPrefix(key, "font-variation"):
		return "canvas 2D cannot select OpenType features or variations"
	case key == "font-family-bold", key == "font-family-italic", key == "font-family-bold-italic":
		return "the web renderer uses one family and synthesises bold/italic"
	case strings.HasPrefix(key, "shell-integration"):
		return "shell integration is server-side in ocode's pty"
	case key == "font-style", strings.HasPrefix(key, "font-thicken"):
		return "not exposed by the web renderer"
	case key == "quit-after-last-window-closed", key == "confirm-close-surface",
		key == "window-save-state", key == "restore-window-size":
		return "window lifecycle is owned by ocode"
	case key == "language", key == "macos-non-native-fullscreen",
		key == "macos-option-as-alt", key == "gtk-single-instance",
		key == "gtk-titlebar", key == "linux-cgroup", key == "class",
		key == "app-id", key == "title", key == "abnormal-command-exit-runtime":
		return "platform application setting with no browser equivalent"
	default:
		return genericReason
	}
}

// genericReason marks "read, but nothing about it applies here". Theme files use
// it to suppress noise about window/session options the user never wrote.
const genericReason = "not applicable in a browser terminal"

// maxIncludeDepth bounds config-file recursion. Cycles are detected separately;
// this catches a pathological chain that is not literally circular.
const maxIncludeDepth = 16

// errNoThemePath is returned by themePath when a theme name cannot be resolved.
var errNoThemePath = errors.New("theme not found")
