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
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// maxIncludeDepth caps how many "include" hops are followed. A deeper chain
// stops with a warning rather than recursing without bound.
const maxIncludeDepth = 5

// maxThemeFileBytes bounds how much of any theme file is read. A theme is a
// small JSON document, so anything larger is a mistake or an attack. Every read
// goes through io.LimitReader, so at most maxThemeFileBytes+1 bytes are ever
// allocated; a file that turns out to be over the limit is refused once that
// bound is reached. The limit is not a pre-flight check on a stat size, which a
// file can outgrow or lie about.
const maxThemeFileBytes = 8 << 20

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
// overrides it key by key. Relative "include" and "tokenColors" references
// resolve against the directory of the file that contains them, never against
// root, so a theme inside themes/ can include "./dark_vs.json" beside it.
//
// root is the containment boundary and must be non-empty. path and every file
// reached through "include" or an external "tokenColors" reference must resolve
// inside root both lexically and after EvalSymlinks; anything else is refused.
// Every file read is capped at maxThemeFileBytes.
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
	data, err := readThemeFile(path)
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
			// VS Code resolves an "include" against the including file's
			// directory (its own "include": "./dark_vs.json" sits beside it),
			// so joining against root alone silently misses the base layer.
			candidate := includePath(filepath.Dir(path), inc)
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
// are joined against the including file's directory; absolute values are used
// as-is so containment can refuse them explicitly.
func includePath(baseDir, inc string) string {
	candidate := filepath.FromSlash(inc)
	if !filepath.IsAbs(candidate) {
		return filepath.Join(baseDir, candidate)
	}
	return filepath.Clean(candidate)
}

// readThemeFile reads a theme file or an external tokenColors reference. Only a
// regular file is read, and at most maxThemeFileBytes are returned: the bound is
// applied to the bytes actually read through io.LimitReader, never to a stat
// size, so a file that grows between the stat and the read, or one that reports
// a size it does not have, cannot allocate more than the limit.
//
// This helper is the only reader of theme files in the package, so every read
// path shares that bound. It is a size bound, not an access control: keeping the
// read inside root is resolveContained's job, and it runs before this is called.
//
// Residual: the regular-file check stats the path, so a path swapped for a FIFO
// between that stat and the open can still block in open(2).
func readThemeFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("file is not a regular file (mode %s)", info.Mode().Type())
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxThemeFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxThemeFileBytes {
		return nil, fmt.Errorf("file is at least %d bytes, over the %d-byte theme file limit", len(data), maxThemeFileBytes)
	}
	return data, nil
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
	data, err := readThemeFile(resolved)
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
	var baseTokens tokenColors
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
		baseTokens = base.TokenColors
	}
	if child == nil {
		merged.TokenColors = mergeTokenColors(tokenColors{}, baseTokens)
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
	merged.TokenColors = mergeTokenColors(baseTokens, child.TokenColors)
	return merged
}

// mergeTokenColors layers the child's tokenColors over the base's. The child's
// rules win when it has any, and the file reference follows the same precedence,
// but the two must come from the same layer: a reference is dropped when the
// winning rules were supplied by a different layer, so the impossible
// "{File != \"\", Rules from another source}" pairing cannot be produced.
func mergeTokenColors(base, child tokenColors) tokenColors {
	var out tokenColors
	rulesFrom := ""
	if len(base.Rules) > 0 {
		rulesFrom = "base"
		out.Rules = base.Rules
	}
	if len(child.Rules) > 0 {
		rulesFrom = "child"
		out.Rules = child.Rules
	}
	fileFrom := ""
	if base.File != "" {
		fileFrom = "base"
		out.File = base.File
	}
	if child.File != "" {
		fileFrom = "child"
		out.File = child.File
	}
	if rulesFrom != "" && fileFrom != "" && fileFrom != rulesFrom {
		out.File = ""
	}
	return out
}

// resolveContained resolves candidate and guarantees it stays inside root. A
// candidate that exists is judged on its symlink-resolved form, so a symlink
// planted inside root cannot redirect the read outside it.
//
// When EvalSymlinks fails the candidate is never returned. A missing leaf can
// still sit under a symlinked intermediate directory, so its lexical spelling
// says nothing about where a read would land; the lexical comparison below only
// chooses between two errors — "escapes the root" and "cannot be resolved" — and
// never authorizes a read.
//
// Both the given and the resolved root are accepted as the reference frame,
// because a caller may pass either form.
func resolveContained(root, candidate string) (string, error) {
	if err := requireContainmentRoot(root); err != nil {
		return "", err
	}
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
	if err := requireContainmentRoot(root); err != nil {
		return "", err
	}
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

// requireContainmentRoot rejects an empty root. filepath.Abs("") resolves to the
// process working directory, which would make containment a no-op, so an empty
// or whitespace-only root is an error rather than a silent widening.
func requireContainmentRoot(root string) error {
	if strings.TrimSpace(root) == "" {
		return fmt.Errorf("containment root is empty")
	}
	return nil
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
