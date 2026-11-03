package appearance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
// A leading "~/" in an extra root is expanded against home, and an empty entry
// is dropped. A bare "~" is refused like any other non-absolute root: expanding
// it would make the user's whole home directory an extension root, so every
// unrelated directory (and every installed editor's tree) would be walked.
// An extra root that is neither absolute nor "~/"-prefixed is dropped too: a
// bare relative path would scan whatever the process working directory happened
// to be, which is exactly the kind of ambient state this package must not depend
// on. Such an entry is reported by InvalidExtraRoots so the caller can warn
// (discovery) or reject it (settings write). An empty home contributes no editor
// roots for the same reason.
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
		p = strings.TrimSpace(p)
		if p == "" || !validExtraRoot(p) {
			continue
		}
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

// expandHomeDir expands a leading "~/" against home. A bare "~" is deliberately
// NOT expanded: it is refused by validExtraRoot, and expanding it here would
// turn the entire home directory into an extension root. "~other/x" is left
// alone too (we have no passwd lookup and guessing another user's home would
// read the wrong tree).
func expandHomeDir(path, home string) string {
	if home == "" || path == "" || path[0] != '~' {
		return path
	}
	if len(path) > 1 && (path[1] == '/' || path[1] == filepath.Separator) {
		return filepath.Join(home, path[2:])
	}
	return path
}

// validExtraRoot reports whether dir is an acceptable extra extension root: an
// absolute path, or one with a leading "~/" that expandHomeDir knows how to
// expand. A bare "~" is refused because it would mean the user's whole home, and
// a plain relative path is refused because scanning it would read whatever
// directory the process started in — a settings value must never resolve against
// the process CWD.
func validExtraRoot(dir string) bool {
	if filepath.IsAbs(dir) {
		return true
	}
	return len(dir) > 1 && dir[0] == '~' && (dir[1] == '/' || dir[1] == filepath.Separator)
}

// InvalidExtraRoots returns the non-empty entries of extra that are neither
// absolute nor "~/"-prefixed, in declaration order. ExtensionRoots drops such an
// entry; the caller decides whether that is a settings-write error (HTTP PUT) or
// a discovery warning (a value already on disk must never block a load).
func InvalidExtraRoots(extra []string) []string {
	var invalid []string
	for _, dir := range extra {
		dir = strings.TrimSpace(dir)
		if dir == "" || validExtraRoot(dir) {
			continue
		}
		invalid = append(invalid, dir)
	}
	return invalid
}

// invalidExtraRootWarnings explains every refused extra root. It runs on the
// stored config, so it must warn rather than fail: spec §13 requires a bad
// stored value to degrade, never to block discovery.
func invalidExtraRootWarnings(extra []string) []string {
	invalid := InvalidExtraRoots(extra)
	if len(invalid) == 0 {
		return nil
	}
	warnings := make([]string, 0, len(invalid))
	for _, dir := range invalid {
		warnings = append(warnings, fmt.Sprintf(
			"ignoring extra extension directory %q: use an absolute path or a ~/ path (a relative path would resolve against the process working directory)", dir))
	}
	return warnings
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
			manifest, warns, readErr := readExtension(ctx, dir)
			warnings = append(warnings, warns...)
			if readErr != nil {
				// A cancelled context must reach the caller no matter where it
				// fired. Swallowing it here would let the LAST extension's
				// partial manifest stand as a complete result, which the service
				// would then memoise for five minutes.
				return manifests, warnings, readErr
			}
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
	// Re-check after the walk: a cancellation during the last entry has no
	// following iteration to notice it.
	if err := ctx.Err(); err != nil {
		return manifests, warnings, err
	}
	return manifests, warnings, nil
}

// readExtension reads one extension directory. A nil manifest means the
// directory is not an extension that contributes themes; warnings describe
// contributions that had to be skipped. A non-nil error is a cancelled context:
// the manifest may still be returned alongside it, but the caller must not treat
// the result as complete.
func readExtension(ctx context.Context, dir string) (*Manifest, []string, error) {
	// The manifest read is contained the same way a contribution path is: a
	// symlinked package.json must not let an extension point the scan at a file
	// outside its own directory.
	path, err := resolveContained(dir, filepath.Join(dir, "package.json"))
	if err != nil {
		// A directory with no package.json is not an extension (VS Code's
		// extensions folder also holds `.obsolete`, and a half-removed install
		// leaves a bare directory). That is normal and silent. A manifest that
		// is present but escapes the extension directory IS worth reporting.
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, []string{fmt.Sprintf("cannot read extension manifest in %q: %v", dir, err)}, nil
	}
	data, err := readCappedFile(path, maxManifestBytes)
	if err != nil {
		return nil, []string{fmt.Sprintf("cannot read extension manifest %q: %v", path, err)}, nil
	}
	var pkg extensionManifest
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, []string{fmt.Sprintf("cannot parse extension manifest %q: %v", path, err)}, nil
	}
	if pkg.Name == "" || pkg.Publisher == "" {
		return nil, nil, nil
	}
	if len(pkg.Contributes.Themes) == 0 && len(pkg.Contributes.IconThemes) == 0 {
		return nil, nil, nil
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
			return manifest, warnings, err
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
			return manifest, warnings, err
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

	return manifest, warnings, nil
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
// than the cap as an error instead of silently truncating it. The cap is
// enforced while reading (io.LimitReader), not from a stat size a file can
// outgrow between the check and the read.
func readCappedFile(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%q is not a regular file", path)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%q is at least %d bytes, over the %d byte limit", path, len(data), limit)
	}
	return data, nil
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
