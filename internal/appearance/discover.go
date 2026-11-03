package appearance

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SourceVSCode is the ThemeRef.Source of every theme discovered in an installed
// VS Code extension. Builtin ocode themes use their own registry, so a source
// value of "vscode" is the discriminator the web picker uses to decide whether a
// name is resolvable by the theme registry or through /api/appearance.
const SourceVSCode = "vscode"

// maxManifestBytes bounds a package.json read. A real extension manifest is a
// few tens of kilobytes; anything larger is not a manifest we should trust (or
// spend memory decoding) during a scan of every installed extension.
const maxManifestBytes = 1 << 20

// Contribution is one theme or icon theme declared by an extension manifest.
//
// ID is the variant slug for a colour theme (`^[a-z0-9-]+$`, unique within its
// extension) and the contribution's own id for an icon theme. Path is absolute
// and inside the extension directory; Discovery verifies both before a
// contribution is listed, so a consumer never has to re-check containment.
//
// Kind is the polarity the contribution declares — a colour theme's own "type",
// falling back to its `uiTheme` hint, and dark for an icon theme (polarity for
// icons comes from the active colour theme, not the icon theme). It is carried
// here so the theme list can report a kind without converting every theme;
// ConvertTheme re-derives polarity from the theme file itself, which is
// authoritative when the two disagree.
type Contribution struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Path  string   `json:"path"`
	Kind  Polarity `json:"kind,omitempty"`
}

// Manifest is one installed extension that contributes themes or icon themes.
// ID is VS Code's `<publisher>.<name>` extension id and is the namespace half of
// every theme id the extension produces.
type Manifest struct {
	ID         string         `json:"id"`
	Namespace  string         `json:"namespace"`
	Name       string         `json:"name"`
	Version    string         `json:"version"`
	Dir        string         `json:"dir"`
	Themes     []Contribution `json:"themes,omitempty"`
	IconThemes []Contribution `json:"icon_themes,omitempty"`
}

// ExtensionRoots returns the extension directories to scan, in order: the
// caller's extra roots first (so a user can point at a portable install or a
// checkout), then the well-known editor locations under home.
//
// A leading "~/" or "~" in an extra root is expanded against home, and an empty
// entry is dropped. An empty home contributes no editor roots: a relative
// ".vscode/extensions" would silently scan whatever the process working
// directory happened to be, which is exactly the kind of ambient state this
// package must not depend on.
func ExtensionRoots(home string, extra []string) []string {
	roots := make([]string, 0, len(extra)+5)
	seen := make(map[string]struct{}, len(extra)+5)
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		roots = append(roots, p)
	}
	for _, p := range extra {
		add(expandHomeDir(p, home))
	}
	if home != "" {
		for _, dir := range []string{
			".vscode",
			".vscode-insiders",
			".vscode-oss",
			".cursor",
			".windsurf",
		} {
			add(filepath.Join(home, dir, "extensions"))
		}
	}
	return roots
}

// expandHomeDir expands a leading "~" against home. A bare "~" or "~/x" is
// expanded; "~other/x" is left alone (we have no passwd lookup and guessing
// another user's home would read the wrong tree).
func expandHomeDir(path, home string) string {
	if home == "" || path == "" || path[0] != '~' {
		return path
	}
	if path == "~" {
		return home
	}
	if len(path) > 1 && (path[1] == '/' || path[1] == filepath.Separator) {
		return filepath.Join(home, path[2:])
	}
	return path
}

// extensionManifest is the slice of an extension's package.json this package
// reads. Everything else in the file is ignored.
type extensionManifest struct {
	Name        string `json:"name"`
	Publisher   string `json:"publisher"`
	Version     string `json:"version"`
	Contributes struct {
		Themes []struct {
			Label   string `json:"label"`
			UITheme string `json:"uiTheme"`
			Path    string `json:"path"`
		} `json:"themes"`
		IconThemes []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
			Path  string `json:"path"`
		} `json:"iconThemes"`
	} `json:"contributes"`
}

// themeHead is the part of a colour theme file Discovery reads: the name it
// slugifies into the variant id and the declared polarity. The full file is
// parsed only by ConvertTheme.
type themeHead struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Discover scans roots in order and returns every extension that contributes a
// colour theme or an icon theme.
//
// A missing root is normal (not every editor is installed) and is skipped
// silently. A root that exists but cannot be read, a directory without a usable
// package.json, and a contribution whose file is missing, escapes the extension
// directory, or cannot be parsed all become warnings; none of them abort the
// scan. The returned error is reserved for a cancelled context, because a
// partial list must never be mistaken for a complete one.
//
// The first extension id wins when two roots contain the same extension, so the
// earlier root in ExtensionRoots order takes precedence over a later copy (a
// Cursor install shadowing the VS Code one, for example).
func Discover(ctx context.Context, roots []string) ([]Manifest, []string, error) {
	var manifests []Manifest
	var warnings []string
	seen := make(map[string]struct{})

	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return manifests, warnings, err
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			if !os.IsNotExist(err) {
				warnings = append(warnings, fmt.Sprintf("cannot read extensions directory %q: %v", root, err))
			}
			continue
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return manifests, warnings, err
			}
			name := entry.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			dir := filepath.Join(root, name)
			// Stat rather than entry.IsDir(): VS Code installs may be symlinked
			// from a portable location, and a symlinked extension is still one
			// we can read.
			info, statErr := os.Stat(dir)
			if statErr != nil || !info.IsDir() {
				continue
			}
			manifest, warns := readExtension(ctx, dir)
			warnings = append(warnings, warns...)
			if manifest == nil {
				continue
			}
			if _, ok := seen[manifest.ID]; ok {
				continue
			}
			seen[manifest.ID] = struct{}{}
			manifests = append(manifests, *manifest)
		}
	}
	return manifests, warnings, nil
}

// readExtension reads one extension directory. A nil manifest means the
// directory is not an extension that contributes themes; warnings describe
// contributions that had to be skipped.
func readExtension(ctx context.Context, dir string) (*Manifest, []string) {
	path := filepath.Join(dir, "package.json")
	data, err := readCappedFile(path, maxManifestBytes)
	if err != nil {
		// A directory with no package.json is not an extension (VS Code's
		// extensions folder also holds `.obsolete`, and a half-removed install
		// leaves a bare directory). That is normal and silent. A manifest that
		// is present but unreadable IS worth reporting.
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []string{fmt.Sprintf("cannot read extension manifest %q: %v", path, err)}
	}
	var pkg extensionManifest
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, []string{fmt.Sprintf("cannot parse extension manifest %q: %v", path, err)}
	}
	if pkg.Name == "" || pkg.Publisher == "" {
		return nil, nil
	}
	if len(pkg.Contributes.Themes) == 0 && len(pkg.Contributes.IconThemes) == 0 {
		return nil, nil
	}

	manifest := &Manifest{
		ID:        pkg.Publisher + "." + pkg.Name,
		Namespace: pkg.Publisher,
		Name:      pkg.Name,
		Version:   pkg.Version,
		Dir:       dir,
	}
	var warnings []string

	usedSlugs := make(map[string]int, len(pkg.Contributes.Themes))
	for i, contributed := range pkg.Contributes.Themes {
		if err := ctx.Err(); err != nil {
			return manifest, warnings
		}
		rel := strings.TrimSpace(contributed.Path)
		label := strings.TrimSpace(contributed.Label)
		if rel == "" {
			warnings = append(warnings, fmt.Sprintf(
				"extension %s: theme contribution %d has no path", manifest.ID, i))
			continue
		}
		resolved, err := resolveContained(dir, filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			warnings = append(warnings, fmt.Sprintf(
				"extension %s: theme %q is unavailable: %v", manifest.ID, rel, err))
			continue
		}
		head, err := readThemeHead(resolved)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf(
				"extension %s: theme %q is unavailable: %v", manifest.ID, rel, err))
			continue
		}
		base := strings.TrimSpace(head.Name)
		if base == "" {
			base = label
		}
		if base == "" {
			base = strings.TrimSuffix(filepath.Base(resolved), filepath.Ext(resolved))
		}
		slug := uniqueSlug(slugify(base), usedSlugs)
		if label == "" {
			label = base
		}
		manifest.Themes = append(manifest.Themes, Contribution{
			ID:    slug,
			Label: label,
			Path:  resolved,
			Kind:  contributionPolarity(head.Type, contributed.UITheme),
		})
	}

	usedIconSlugs := make(map[string]int, len(pkg.Contributes.IconThemes))
	for i, contributed := range pkg.Contributes.IconThemes {
		if err := ctx.Err(); err != nil {
			return manifest, warnings
		}
		rel := strings.TrimSpace(contributed.Path)
		label := strings.TrimSpace(contributed.Label)
		if rel == "" {
			warnings = append(warnings, fmt.Sprintf(
				"extension %s: icon theme contribution %d has no path", manifest.ID, i))
			continue
		}
		resolved, err := resolveContained(dir, filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			warnings = append(warnings, fmt.Sprintf(
				"extension %s: icon theme %q is unavailable: %v", manifest.ID, rel, err))
			continue
		}
		if _, err := os.Stat(resolved); err != nil {
			warnings = append(warnings, fmt.Sprintf(
				"extension %s: icon theme %q is unavailable: %v", manifest.ID, rel, err))
			continue
		}
		// Icon themes are resolved by Task 4's model builder, so Discovery only
		// proves the file exists. The id comes from the contribution's own id
		// (VS Code's icon theme identity), then the label, then the extension.
		base := strings.TrimSpace(contributed.ID)
		if base == "" {
			base = label
		}
		if base == "" {
			base = pkg.Name
		}
		slug := uniqueSlug(slugify(base), usedIconSlugs)
		if label == "" {
			label = slug
		}
		// Kind stays empty: an icon theme has no polarity of its own; the model
		// follows the active colour theme (spec §11.1).
		manifest.IconThemes = append(manifest.IconThemes, Contribution{
			ID:    slug,
			Label: label,
			Path:  resolved,
		})
	}

	return manifest, warnings
}

// readThemeHead decodes the top-level fields Discovery needs from a theme file.
// It requires the file to exist and be regular; a lightweight decode keeps the
// scan cheap, and ConvertTheme re-reads the full tree (including its include
// chain) when the theme is actually selected.
func readThemeHead(path string) (themeHead, error) {
	data, err := readThemeFile(path)
	if err != nil {
		return themeHead{}, err
	}
	var head themeHead
	if err := DecodeJSONC(data, &head); err != nil {
		return themeHead{}, err
	}
	return head, nil
}

// readCappedFile reads at most limit bytes from path and reports a file larger
// than the cap as an error instead of silently truncating it.
func readCappedFile(path string, limit int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%q is not a regular file", path)
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("%q is %d bytes, over the %d byte limit", path, info.Size(), limit)
	}
	return os.ReadFile(path)
}

// contributionPolarity classifies a theme contribution from the theme's own
// declared type, falling back to VS Code's `uiTheme` name, then to dark.
func contributionPolarity(declared, uiTheme string) Polarity {
	switch strings.ToLower(strings.TrimSpace(declared)) {
	case string(PolarityLight):
		return PolarityLight
	case string(PolarityDark):
		return PolarityDark
	}
	switch strings.ToLower(strings.TrimSpace(uiTheme)) {
	case "vs", "hc-light":
		return PolarityLight
	case "vs-dark", "hc-black":
		return PolarityDark
	}
	return PolarityDark
}

// slugify lowercases s and reduces every run of unsupported characters to a
// single dash, matching the variant-slug grammar `^[a-z0-9-]+$`.
func slugify(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	dash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		default:
			if b.Len() > 0 && !dash {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// uniqueSlug dedupes variant slugs inside one extension by appending -2, -3, …
// The counter is shared across calls so "Dark", "Dark" and "Dark!" become
// "dark", "dark-2", "dark-3".
func uniqueSlug(base string, used map[string]int) string {
	if base == "" {
		base = "theme"
	}
	used[base]++
	if n := used[base]; n > 1 {
		return fmt.Sprintf("%s-%d", base, n)
	}
	return base
}

// sortRefs orders theme refs by label, then id, so the list is stable and the
// picker's order does not depend on directory iteration or root order.
func sortRefs(refs []ThemeRef) {
	sort.SliceStable(refs, func(i, j int) bool {
		li, lj := strings.ToLower(refs[i].Label), strings.ToLower(refs[j].Label)
		if li != lj {
			return li < lj
		}
		return refs[i].ID < refs[j].ID
	})
}
