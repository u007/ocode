// Package appearance parses and converts VS Code colour themes and file icon
// themes into the palettes ocode renders with.
//
// Parsing is deliberately tolerant: theme files are JSONC (comments and
// trailing commas), "include" chains merge a base theme underneath the
// including one, and "tokenColors" is either an inline array or a path to a
// JSON file. Everything read is confined to a containment root so a theme
// shipped inside an extension cannot pull in arbitrary files.
package appearance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// maxIncludeDepth caps how many "include" hops are followed. A deeper chain
// stops with a warning rather than recursing without bound.
const maxIncludeDepth = 5

// jsoncComments matches either a quoted string (preserved) or a comment
// (removed). Copied from internal/config so this package does not depend on
// config; matching quoted strings first keeps "//" inside a value such as a URL
// intact.
var jsoncComments = regexp.MustCompile(`"(?:[^"\\]|\\.)*"|//[^\n]*|/\*[\s\S]*?\*/`)

// jsoncTrailingCommas matches either a quoted string (preserved) or a comma
// followed by optional whitespace and a closing bracket. The bracket is kept,
// the comma is dropped.
var jsoncTrailingCommas = regexp.MustCompile(`"(?:[^"\\]|\\.)*"|,\s*([}\]])`)

// RawTheme is a theme file exactly as authored, before any conversion. Colours
// keep the theme's own spelling; this layer never rewrites a value.
type RawTheme struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Include     string            `json:"include"`
	Colors      map[string]string `json:"colors"`
	TokenColors tokenColors       `json:"tokenColors"`
}

// TokenRule is one entry of a tokenColors array.
type TokenRule struct {
	Scope    scopeList     `json:"scope"`
	Settings TokenSettings `json:"settings"`
}

// TokenSettings is the style a TokenRule applies to its scopes.
type TokenSettings struct {
	Foreground string `json:"foreground"`
	Background string `json:"background"`
	FontStyle  string `json:"fontStyle"`
}

// tokenColors holds either inline syntax rules or a reference to a file that
// contains them. VS Code allows both shapes under the same key.
type tokenColors struct {
	Rules []TokenRule
	File  string
}

// UnmarshalJSON accepts the two shapes VS Code allows for "tokenColors": an
// array of rules, or a string path to another file (usually a .tmTheme plist).
// A string is recorded verbatim in File and left for the loader to resolve;
// Rules stays nil so a caller can tell "no rules" from an empty array.
func (t *tokenColors) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	switch {
	case trimmed == "" || trimmed == "null":
		*t = tokenColors{}
		return nil
	case strings.HasPrefix(trimmed, "["):
		var rules []TokenRule
		if err := json.Unmarshal(data, &rules); err != nil {
			return fmt.Errorf("decode tokenColors rules: %w", err)
		}
		*t = tokenColors{Rules: rules}
		return nil
	case strings.HasPrefix(trimmed, `"`):
		var file string
		if err := json.Unmarshal(data, &file); err != nil {
			return fmt.Errorf("decode tokenColors path: %w", err)
		}
		*t = tokenColors{File: file}
		return nil
	default:
		return fmt.Errorf("tokenColors must be an array of rules or a string path")
	}
}

// scopeList accepts either a single TextMate scope string or an array of them.
// A single string may list several scopes separated by commas, which is the
// common spelling in hand-written themes, so it is split and trimmed.
type scopeList []string

// UnmarshalJSON accepts a string or an array of strings.
func (s *scopeList) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	switch {
	case trimmed == "" || trimmed == "null":
		*s = nil
		return nil
	case strings.HasPrefix(trimmed, `"`):
		var single string
		if err := json.Unmarshal(data, &single); err != nil {
			return fmt.Errorf("decode scope: %w", err)
		}
		scopes := make([]string, 0, 1)
		for _, part := range strings.Split(single, ",") {
			if part = strings.TrimSpace(part); part != "" {
				scopes = append(scopes, part)
			}
		}
		if len(scopes) == 0 {
			*s = nil
			return nil
		}
		*s = scopes
		return nil
	default:
		var list []string
		if err := json.Unmarshal(data, &list); err != nil {
			return fmt.Errorf("decode scope list: %w", err)
		}
		*s = list
		return nil
	}
}

// DecodeJSONC decodes JSON with comments and trailing commas, the dialect VS
// Code theme files are written in. Comment markers inside quoted strings are
// preserved.
func DecodeJSONC(data []byte, v any) error {
	cleaned := stripTrailingCommas(stripJSONCComments(data))
	if err := json.Unmarshal(cleaned, v); err != nil {
		return fmt.Errorf("decode jsonc: %w", err)
	}
	return nil
}

// stripJSONCComments removes // and /* */ comments while preserving quoted
// strings, so URLs and other // sequences inside a value survive.
func stripJSONCComments(data []byte) []byte {
	return jsoncComments.ReplaceAllFunc(data, func(match []byte) []byte {
		if match[0] == '"' {
			return match
		}
		return nil
	})
}

// stripTrailingCommas removes a comma that precedes a closing bracket while
// preserving quoted strings.
func stripTrailingCommas(data []byte) []byte {
	return jsoncTrailingCommas.ReplaceAllFunc(data, func(match []byte) []byte {
		if match[0] == '"' {
			return match
		}
		return match[len(match)-1:]
	})
}

// LoadThemeTree loads the theme at path and merges its "include" chain
// depth-first: the included layer is laid down first, then the including file
// overrides it key by key.
//
// root is the containment boundary. path and every file reached through
// "include" or an external "tokenColors" reference must resolve inside root
// both lexically and after EvalSymlinks; anything else is refused.
//
// depth seeds the include counter and callers pass 0. Partial failures — an
// escaping include, a depth overflow, an unreadable or non-JSON tokenColors
// file — are returned as warnings and the load continues with what was read.
// Only a failure to read or decode path itself is returned as an error.
func LoadThemeTree(path, root string, depth int) (*RawTheme, []string, error) {
	if depth < 0 {
		depth = 0
	}
	if depth > maxIncludeDepth {
		warn := fmt.Sprintf("include depth limit %d exceeded at %q", maxIncludeDepth, path)
		return &RawTheme{}, []string{warn}, nil
	}
	// Canonicalise the root once: every resolved path handed back by
	// resolveContained is symlink-free, so resolving the root too keeps the
	// containment comparison on a single frame (macOS /var -> /private/var
	// otherwise makes an inside-tempdir candidate look like an escape).
	rootDir, err := canonicalRoot(root)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve theme root %q: %w", root, err)
	}
	resolved, err := resolveContained(rootDir, path)
	if err != nil {
		return nil, nil, fmt.Errorf("load theme %q: %w", path, err)
	}
	theme, warnings, err := loadThemeLayer(resolved, rootDir, depth)
	if err != nil {
		return nil, warnings, fmt.Errorf("load theme %q: %w", path, err)
	}
	return theme, warnings, nil
}

// loadThemeLayer reads one theme file, merges the layer it includes, and then
// overlays its own values. Problems in the included layer become warnings; only
// this file's own read or decode failure is fatal.
func loadThemeLayer(path, root string, depth int) (*RawTheme, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read theme file: %w", err)
	}
	var layer RawTheme
	if err := DecodeJSONC(data, &layer); err != nil {
		return nil, nil, fmt.Errorf("parse theme file: %w", err)
	}

	var warnings []string
	base := &RawTheme{}
	if inc := strings.TrimSpace(layer.Include); inc != "" {
		if depth >= maxIncludeDepth {
			warnings = append(warnings, fmt.Sprintf(
				"include %q skipped: include depth limit %d reached", inc, maxIncludeDepth))
		} else {
			candidate := includePath(root, inc)
			if incPath, resolveErr := resolveContained(root, candidate); resolveErr != nil {
				warnings = append(warnings, fmt.Sprintf("include %q rejected: %v", inc, resolveErr))
			} else if parent, parentWarnings, loadErr := loadThemeLayer(incPath, root, depth+1); loadErr != nil {
				warnings = append(warnings, fmt.Sprintf("include %q not loaded: %v", inc, loadErr))
			} else {
				base = parent
				warnings = append(warnings, parentWarnings...)
			}
		}
	}

	if warn := loadTokenColorsFile(&layer, path, root); warn != "" {
		warnings = append(warnings, warn)
	}

	return mergeThemes(base, &layer), warnings, nil
}

// includePath resolves an "include" value to a path candidate. Relative values
// are joined against root; absolute values are used as-is so containment can
// refuse them explicitly.
func includePath(root, inc string) string {
	candidate := filepath.FromSlash(inc)
	if !filepath.IsAbs(candidate) {
		return filepath.Join(root, candidate)
	}
	return filepath.Clean(candidate)
}

// loadTokenColorsFile loads an external "tokenColors" reference held in
// TokenColors.File. The reference is resolved relative to the theme file and
// kept inside root. Only a JSON file with the [{scope,settings}] shape is read,
// so a .tmTheme plist yields exactly one warning and no rules.
//
// It returns an empty string when there is nothing to report.
func loadTokenColorsFile(layer *RawTheme, themePath, root string) string {
	ref := strings.TrimSpace(layer.TokenColors.File)
	if ref == "" {
		return ""
	}
	if !strings.EqualFold(filepath.Ext(ref), ".json") {
		return fmt.Sprintf("tokenColors %q is not a JSON file; no syntax rules loaded", ref)
	}
	candidate := filepath.FromSlash(ref)
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(filepath.Dir(themePath), candidate)
	}
	resolved, err := resolveContained(root, candidate)
	if err != nil {
		return fmt.Sprintf("tokenColors %q rejected: %v", ref, err)
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return fmt.Sprintf("tokenColors %q not loaded: %v", ref, err)
	}
	var rules []TokenRule
	if err := DecodeJSONC(data, &rules); err != nil {
		return fmt.Sprintf("tokenColors %q not parsed: %v", ref, err)
	}
	layer.TokenColors.Rules = rules
	return ""
}

// mergeThemes lays base down first and overlays child. Colours merge per key
// with the child winning; the child's syntax rules replace the base's only when
// the child actually has some.
func mergeThemes(base, child *RawTheme) *RawTheme {
	merged := &RawTheme{}
	if base != nil {
		merged.Name = base.Name
		merged.Type = base.Type
		merged.Include = base.Include
		if len(base.Colors) > 0 {
			merged.Colors = make(map[string]string, len(base.Colors))
			for key, value := range base.Colors {
				merged.Colors[key] = value
			}
		}
		merged.TokenColors = base.TokenColors
	}
	if child == nil {
		return merged
	}
	if child.Name != "" {
		merged.Name = child.Name
	}
	if child.Type != "" {
		merged.Type = child.Type
	}
	if child.Include != "" {
		merged.Include = child.Include
	}
	if len(child.Colors) > 0 {
		if merged.Colors == nil {
			merged.Colors = make(map[string]string, len(child.Colors))
		}
		for key, value := range child.Colors {
			merged.Colors[key] = value
		}
	}
	if len(child.TokenColors.Rules) > 0 {
		merged.TokenColors.Rules = child.TokenColors.Rules
	}
	if child.TokenColors.File != "" {
		merged.TokenColors.File = child.TokenColors.File
	}
	return merged
}

// resolveContained resolves candidate and guarantees it stays inside root. A
// candidate that exists is judged on its symlink-resolved form, so a symlink
// planted inside root cannot redirect the read outside it. A candidate that
// does not exist has nothing to redirect, so it is judged lexically instead —
// that keeps "this include leaves the root" distinguishable from "this include
// is missing". Both the given and the resolved root are accepted as the
// reference frame, because a caller may pass either form.
func resolveContained(root, candidate string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve containment root %q: %w", root, err)
	}
	candAbs, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", candidate, err)
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", fmt.Errorf("resolve containment root %q: %w", rootAbs, err)
	}
	candReal, err := filepath.EvalSymlinks(candAbs)
	if err != nil {
		if !within(rootAbs, candAbs) && !within(rootReal, candAbs) {
			return "", fmt.Errorf("path %q escapes the theme containment root %q", candAbs, rootReal)
		}
		return "", fmt.Errorf("resolve path %q: %w", candAbs, err)
	}
	if !within(rootAbs, candReal) && !within(rootReal, candReal) {
		return "", fmt.Errorf("path %q escapes the theme containment root %q", candReal, rootReal)
	}
	return candReal, nil
}

// canonicalRoot returns the absolute, symlink-free form of a containment root.
func canonicalRoot(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", root, err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", abs, err)
	}
	return real, nil
}

// within reports whether target is root or lives below it.
func within(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		// Different volumes on Windows cannot be related, so treat them as
		// outside the root.
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
