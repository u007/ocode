package ghosttyconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resolveThemes turns the accumulated `theme` value plus explicit colour keys
// into the two palettes the client can pick from.
//
// Ghostty's own rule, which this mirrors: "Any additional colors specified via
// background, foreground, palette, etc. will override the colors specified in
// the theme." So a theme is the BASE, and explicit keys are applied on top.
func (p *parser) resolveThemes() (*ThemeColors, *ThemeColors) {
	lightName, darkName, _, ok := splitThemeSpec(p.themeSpec)
	if !ok {
		p.errf("theme %q must specify both light: and dark: names", p.themeSpec)
		lightName, darkName = "", ""
	}
	return p.themeFor(lightName), p.themeFor(darkName)
}

// themeFor resolves one theme name (empty means "no theme") and overlays the
// explicit colours from the main config.
func (p *parser) themeFor(name string) *ThemeColors {
	base := &ThemeColors{}
	if name != "" {
		resolved, err := p.loadThemeFile(name)
		if err != nil {
			p.errf("%v", err)
		} else if resolved != nil {
			base = resolved
		}
	}
	applyColorSet(base, p.colors, p.palette, p.paletteSet)
	deriveDefaults(base)
	if base.Foreground == "" && base.Background == "" && base.Palette == nil {
		return nil
	}
	return base
}

// loadThemeFile reads a theme file. A theme file is an ordinary Ghostty config
// file that may set any option, so it is parsed with the same code in
// themeOnly mode: `theme`/`config-file` are ignored (Ghostty does the same) and
// non-colour keys are skipped rather than reported, because a shipped theme's
// window options are not something the user wrote.
func (p *parser) loadThemeFile(name string) (*ThemeColors, error) {
	path, err := p.themePath(name)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("ghostty theme %q: %w", name, err)
	}
	sub := &parser{opts: p.opts, themeOnly: true}
	sub.parseContent(path, string(data))
	theme := &ThemeColors{
		Foreground:          sub.colors["foreground"],
		Background:          sub.colors["background"],
		Cursor:              sub.colors["cursor-color"],
		CursorText:          sub.colors["cursor-text"],
		SelectionBackground: sub.colors["selection-background"],
		SelectionForeground: sub.colors["selection-foreground"],
	}
	if palette := buildPalette(sub.palette, sub.paletteSet, nil, nil); palette != nil {
		theme.Palette = palette
	}
	// Only appearance-affecting keys are worth surfacing from a theme file.
	for _, item := range sub.cfg.Ignored {
		p.ignored(item.Key, item.Value, item.Reason)
	}
	return theme, nil
}

// themePath resolves a theme name (or absolute path) to a file, searching the
// user's themes directory and then Ghostty's shipped resources.
func (p *parser) themePath(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errNoThemePath
	}
	expanded := expandHome(name)
	if filepath.IsAbs(expanded) {
		if fileExists(expanded) {
			return expanded, nil
		}
		return "", fmt.Errorf("ghostty theme file %q does not exist", expanded)
	}
	// A name is a NAME: path separators would let a config reach arbitrary
	// filesystem paths, which a theme lookup has no reason to do.
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return "", fmt.Errorf("ghostty theme name %q must not contain path separators", name)
	}
	for _, dir := range themeDirs(p.opts) {
		candidate := filepath.Join(dir, name)
		if fileExists(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("ghostty theme %q not found (searched %s)", name, strings.Join(themeDirs(p.opts), ", "))
}

// splitThemeSpec understands Ghostty's two forms:
//
//	theme = Name
//	theme = light:Name,dark:Name
//
// The split form requires BOTH keys (Ghostty errors otherwise); ok is false for
// a half-specified split so the caller can report it.
func splitThemeSpec(spec string) (light, dark string, split bool, ok bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", "", false, true
	}
	if !strings.Contains(spec, "light:") && !strings.Contains(spec, "dark:") {
		return spec, spec, false, true
	}
	for _, part := range strings.Split(spec, ",") {
		key, value, found := strings.Cut(strings.TrimSpace(part), ":")
		if !found {
			continue
		}
		switch strings.TrimSpace(key) {
		case "light":
			light = strings.TrimSpace(value)
		case "dark":
			dark = strings.TrimSpace(value)
		}
	}
	if light == "" || dark == "" {
		return "", "", true, false
	}
	return light, dark, true, true
}

// applyColorSet overlays explicit colour keys onto a theme. Palette slots the
// config did not set keep the theme's colour.
func applyColorSet(dst *ThemeColors, colors map[string]string, palette [16]string, set [16]bool) {
	if dst == nil {
		return
	}
	if v, ok := colors["foreground"]; ok {
		dst.Foreground = v
	}
	if v, ok := colors["background"]; ok {
		dst.Background = v
	}
	if v, ok := colors["cursor-color"]; ok {
		dst.Cursor = v
	}
	if v, ok := colors["cursor-text"]; ok {
		dst.CursorText = v
	}
	if v, ok := colors["selection-background"]; ok {
		dst.SelectionBackground = v
	}
	if v, ok := colors["selection-foreground"]; ok {
		dst.SelectionForeground = v
	}
	if merged := buildPalette(palette, set, dst.Palette, nil); merged != nil {
		dst.Palette = merged
	}
}

// buildPalette merges two 16-slot palettes. A result of nil means "no palette
// specified" so the renderer keeps its own defaults rather than receiving a
// half-filled array.
func buildPalette(override [16]string, overrideSet [16]bool, base []string, baseSet []bool) []string {
	out := make([]string, 16)
	any := false
	for i := 0; i < 16; i++ {
		switch {
		case overrideSet[i]:
			out[i] = override[i]
			any = true
		case len(base) == 16 && base[i] != "":
			out[i] = base[i]
			any = true
		case len(baseSet) == 16 && baseSet[i]:
			out[i] = base[i]
			any = true
		}
	}
	if !any {
		return nil
	}
	return out
}

// deriveDefaults fills the colours a theme usually relies on Ghostty to derive.
// Doing it here (not in the client) keeps one source of truth: ghostty-web's
// own defaults are VS Code colours, so leaving selection/cursor unset would
// paint a bright grey selection over a dark Ghostty theme.
func deriveDefaults(t *ThemeColors) {
	if t.Foreground != "" && t.Cursor == "" {
		t.Cursor = t.Foreground
	}
	if t.Background != "" && t.CursorText == "" {
		t.CursorText = t.Background
	}
	if t.Foreground != "" && t.SelectionBackground == "" {
		t.SelectionBackground = t.Foreground
	}
	if t.Background != "" && t.SelectionForeground == "" {
		t.SelectionForeground = t.Background
	}
}

// ResolveTheme resolves one named theme (not a light:/dark: pair) using the
// default search paths. It lets the "ocode" appearance source borrow a Ghostty
// theme without reading the user's whole configuration.
func ResolveTheme(name string) (*ThemeColors, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	p := &parser{opts: DefaultOptions()}
	theme, err := p.loadThemeFile(name)
	if err != nil {
		return nil, err
	}
	deriveDefaults(theme)
	return theme, nil
}
