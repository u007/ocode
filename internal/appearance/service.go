package appearance

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// discoveryTTL is how long a discovery snapshot is reused. Spec §6: discovery is
// on demand but memoised for five minutes, and Refresh forces it fresh after an
// install/remove or a settings change.
const discoveryTTL = 5 * time.Minute

// ThemeRef is one selectable colour theme in the unified list: builtin themes
// use their registry name, VS Code themes use the `vscode:` id.
type ThemeRef struct {
	ID           string   `json:"id"`
	Label        string   `json:"label"`
	Source       string   `json:"source"`
	Kind         Polarity `json:"kind"`
	Extension    string   `json:"extension,omitempty"`
	HasIconTheme bool     `json:"has_icon_theme"`
}

// ErrThemeNotFound reports an id that is well-formed but is not contributed by
// any discovered extension. Handlers map it to 404; a malformed id is a 400.
var ErrThemeNotFound = errors.New("theme not found")

// validThemeID matches `vscode:<namespace>.<extension>/<variant-slug>`.
//
// The extension segment is one or more dot-separated tokens of letters, digits,
// "_" and "-", which is what VS Code allows for publisher and extension names.
// A bare "." or ".." token can never match, so an id is safe to compare against
// discovered manifests before anything touches the filesystem. The slug is
// `[a-z0-9-]+` exactly as spec §5 requires.
var validThemeID = regexp.MustCompile(`^vscode:[A-Za-z0-9_-]+(?:\.[A-Za-z0-9_-]+)*/[a-z0-9-]+$`)

// ValidThemeID reports whether id is a syntactically valid VS Code theme id. It
// says nothing about whether the theme exists.
func ValidThemeID(id string) bool {
	return validThemeID.MatchString(id)
}

// splitThemeID splits a validated id into its extension id and variant slug.
func splitThemeID(id string) (extension, slug string, ok bool) {
	if !ValidThemeID(id) {
		return "", "", false
	}
	rest := strings.TrimPrefix(id, "vscode:")
	slash := strings.LastIndex(rest, "/")
	if slash <= 0 || slash == len(rest)-1 {
		return "", "", false
	}
	return rest[:slash], rest[slash+1:], true
}

// nowFn is the clock used for the discovery TTL. It is a variable only so tests
// can advance time without sleeping.
var nowFn = time.Now

// convertThemeFn is the conversion the service performs on a cache miss. It is a
// variable so tests can count conversions without a fake filesystem.
var convertThemeFn = ConvertTheme

// discoverFn is the walk the service performs on a cache miss. It is a variable
// so a test can interleave a Refresh (or a settings change) with a scan: the
// generation guard in snapshot is what makes such a scan non-authoritative.
var discoverFn = Discover

// cacheEntry is one converted theme, valid only while the source file keeps the
// same modification time and size.
type cacheEntry struct {
	modTime time.Time
	size    int64
	theme   ConvertedTheme
}

// Service discovers colour themes in installed editor extensions and converts
// them on demand, caching both the discovery snapshot and each conversion.
//
// It is safe for concurrent use: the discovery snapshot and the conversion cache
// are guarded by mu, and a scan runs under scanMu so concurrent first requests
// share one filesystem walk instead of each starting their own. No lock is held
// across a conversion.
type Service struct {
	home string

	mu        sync.Mutex
	extra     []string
	manifests []Manifest
	warnings  []string
	scanned   bool
	scannedAt time.Time
	cache     map[string]cacheEntry
	// gen is bumped by every Refresh. A scan captures it before walking and only
	// installs its result when the generation is unchanged: a walk that started
	// before a settings change describes roots (and a memo state) that no longer
	// apply, so its result must not win over the refresh that replaced them.
	gen uint64

	// scanMu serialises discovery. It is deliberately a separate lock from mu so
	// a long walk does not block Resolve for a theme already in the cache.
	scanMu sync.Mutex
}

// NewService returns a service scanning the well-known editor extension
// directories under home plus extra. home is explicit rather than read from the
// environment so a caller (and a test) controls exactly which tree is scanned.
func NewService(home string, extra []string) *Service {
	return &Service{
		home:  home,
		extra: append([]string(nil), extra...),
		cache: make(map[string]cacheEntry),
	}
}

// SetExtraRoots replaces the configured extra extension roots and refreshes the
// snapshot when they actually changed. It is called after a settings write,
// because a new root must be visible immediately rather than after the
// five-minute memo expires.
//
// A save that only changes the colour theme must not drop the snapshot or the
// conversion cache: rescanning every installed editor for an unrelated field
// would make a trivial settings change cost a full filesystem walk, and bumping
// the generation would discard a conversion that is still valid.
func (s *Service) SetExtraRoots(extra []string) {
	next := append([]string(nil), extra...)
	s.mu.Lock()
	changed := !slices.Equal(s.extra, next)
	s.extra = next
	s.mu.Unlock()
	if changed {
		s.Refresh()
	}
}

// Home returns the home directory the service resolves well-known editor
// extension locations against. It is immutable after NewService, so a caller can
// build a throwaway service with the same home (the settings handler validates a
// candidate root set without mutating this one).
func (s *Service) Home() string {
	return s.home
}

// ExtraRoots returns a copy of the configured extra extension roots, in order.
func (s *Service) ExtraRoots() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.extra...)
}

// Refresh drops the discovery snapshot and every cached conversion, forcing the
// next request to rescan and reconvert. Called after an install, a removal, or a
// settings change.
func (s *Service) Refresh() {
	s.mu.Lock()
	s.gen++
	s.manifests = nil
	s.warnings = nil
	s.scanned = false
	s.scannedAt = time.Time{}
	s.cache = make(map[string]cacheEntry)
	s.mu.Unlock()
}

// Themes returns every discovered VS Code colour theme as a ref, sorted by
// label, together with the discovery warnings. The list is complete: callers
// paginate it (spec §15.1).
func (s *Service) Themes(ctx context.Context) ([]ThemeRef, []string, error) {
	manifests, warnings, err := s.snapshot(ctx)
	if err != nil {
		return nil, nil, err
	}
	refs := make([]ThemeRef, 0, len(manifests))
	for _, manifest := range manifests {
		hasIcons := len(manifest.IconThemes) > 0
		for _, contributed := range manifest.Themes {
			refs = append(refs, ThemeRef{
				ID:           "vscode:" + manifest.ID + "/" + contributed.ID,
				Label:        contributed.Label,
				Source:       SourceVSCode,
				Kind:         contributed.Kind,
				Extension:    manifest.ID,
				HasIconTheme: hasIcons,
			})
		}
	}
	sortRefs(refs)
	return refs, warnings, nil
}

// Resolve converts the theme with the given `vscode:` id.
//
// The id is validated before any filesystem access, then matched against the
// memoised discovery snapshot: an id is only ever resolved through a
// contribution Discovery already verified, so no caller-supplied path reaches
// the disk. Conversions are cached under the theme ID and revalidated against
// the contribution file's modification time and size, so touching that file
// invalidates it (spec §6). Files pulled in through `include` or an external
// `tokenColors` reference are NOT part of that key. A conversion failure is
// returned as-is; only a missing contribution is ErrThemeNotFound.
func (s *Service) Resolve(ctx context.Context, id string) (ConvertedTheme, error) {
	extension, slug, ok := splitThemeID(id)
	if !ok {
		return ConvertedTheme{}, fmt.Errorf("invalid theme id %q", id)
	}
	manifests, _, err := s.snapshot(ctx)
	if err != nil {
		return ConvertedTheme{}, err
	}
	for _, manifest := range manifests {
		if manifest.ID != extension {
			continue
		}
		for _, contributed := range manifest.Themes {
			if contributed.ID != slug {
				continue
			}
			return s.convert(id, manifest.Dir, contributed)
		}
	}
	return ConvertedTheme{}, fmt.Errorf("%w: %s", ErrThemeNotFound, id)
}

// convert loads and converts one contribution, reusing a cached conversion when
// the source file has not changed.
func (s *Service) convert(id, root string, contributed Contribution) (ConvertedTheme, error) {
	info, err := os.Stat(contributed.Path)
	if err != nil {
		return ConvertedTheme{}, fmt.Errorf("theme %s is unavailable: %w", id, err)
	}

	s.mu.Lock()
	entry, ok := s.cache[id]
	s.mu.Unlock()
	if ok && entry.modTime.Equal(info.ModTime()) && entry.size == info.Size() {
		return entry.theme, nil
	}

	raw, warnings, err := LoadThemeTree(contributed.Path, root, 0)
	if err != nil {
		return ConvertedTheme{}, fmt.Errorf("theme %s: %w", id, err)
	}
	converted, err := convertThemeFn(id, contributed.Label, raw)
	if err != nil {
		return ConvertedTheme{}, fmt.Errorf("theme %s: %w", id, err)
	}
	converted.Warnings = append(converted.Warnings, warnings...)

	s.mu.Lock()
	s.cache[id] = cacheEntry{
		modTime: info.ModTime(),
		size:    info.Size(),
		theme:   converted,
	}
	s.mu.Unlock()
	return converted, nil
}

// snapshot returns the discovery result, rescanning when the memo has expired.
// A scan error is never cached: a cancelled request must not poison the next one.
func (s *Service) snapshot(ctx context.Context) ([]Manifest, []string, error) {
	if manifests, warnings, ok := s.cachedSnapshot(); ok {
		return manifests, warnings, nil
	}

	s.scanMu.Lock()
	defer s.scanMu.Unlock()
	// Another request may have completed the scan while we waited.
	if manifests, warnings, ok := s.cachedSnapshot(); ok {
		return manifests, warnings, nil
	}

	// A Refresh (or a SetExtraRoots, which calls it) between capturing the roots
	// and installing the result invalidates the walk: its roots, and the memo
	// state it is about to install, belong to a generation that no longer
	// applies. Retry with the current generation so the caller still gets a
	// fresh list; the bound keeps a pathological Refresh loop from spinning here
	// forever (the last walk's result is then returned uncached).
	var lastManifests []Manifest
	var lastWarnings []string
	for attempt := 0; attempt < 3; attempt++ {
		s.mu.Lock()
		extra := append([]string(nil), s.extra...)
		roots := ExtensionRoots(s.home, s.extra)
		gen := s.gen
		s.mu.Unlock()

		// A relative extra root is refused by ExtensionRoots; warn about the
		// stored value instead of failing, because an unloadable config must
		// degrade (spec §13).
		warnings := invalidExtraRootWarnings(extra)
		manifests, scanWarnings, err := discoverFn(ctx, roots)
		warnings = append(warnings, scanWarnings...)
		if err != nil {
			return nil, warnings, err
		}

		s.mu.Lock()
		if s.gen != gen {
			s.mu.Unlock()
			lastManifests, lastWarnings = manifests, warnings
			continue
		}
		s.manifests = manifests
		s.warnings = warnings
		s.scanned = true
		s.scannedAt = nowFn()
		s.mu.Unlock()
		return manifests, warnings, nil
	}
	return lastManifests, lastWarnings, nil
}

// cachedSnapshot reports the memoised result when it is still inside the TTL.
func (s *Service) cachedSnapshot() ([]Manifest, []string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.scanned || nowFn().Sub(s.scannedAt) >= discoveryTTL {
		return nil, nil, false
	}
	return s.manifests, s.warnings, true
}
