// Package sysperm is the cross-platform OS permission manager behind the
// settings UI's "System Permissions" section.
//
// On macOS the grants it tracks are TCC (Transparency, Consent, and Control)
// permissions — Full Disk Access, Files & Folders, Accessibility, Screen
// Recording, and Automation. TCC grants are keyed to the application's code
// signature, so every rebuild produces a new cdhash and macOS forgets the
// grant; the persisted on/off intent plus Reconcile exist to re-request what
// the user already approved after a rebuild.
//
// On Windows and Linux there is no per-application consent gate, so the
// catalog is informational (Supported=false, Status=not_required) and the
// settings section still renders.
//
// The package is stdlib-only on purpose: internal/config imports it for the
// persisted config type, so it must never depend on config, server, or agent.
package sysperm

import (
	"context"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Status is the live, platform-reported state of one permission area. It is
// computed at read time and never persisted.
type Status string

const (
	// StatusGranted means the OS reports the grant as present.
	StatusGranted Status = "granted"
	// StatusDenied means the OS reports the grant as absent, or the probe was
	// refused with a permission error.
	StatusDenied Status = "denied"
	// StatusNotDetermined means macOS has not yet been asked (no recorded
	// request) or the probe could not distinguish "never asked" from "denied".
	StatusNotDetermined Status = "not_determined"
	// StatusUnknown means the platform exposes no scriptable check for this
	// area (e.g. Screen Recording, Automation on macOS).
	StatusUnknown Status = "unknown"
	// StatusNotRequired means the platform has no per-application grant.
	StatusNotRequired Status = "not_required"
)

// Kind distinguishes a permission area (Full Disk Access, Accessibility) from
// a concrete filesystem path (Desktop, Documents, or a user-added directory).
type Kind string

const (
	KindCategory Kind = "category"
	KindPath     Kind = "path"
)

// Source records where an entry came from: a platform built-in, a directory
// discovered from ocode's saved projects, or a path the user added.
type Source string

const (
	SourceBuiltin    Source = "builtin"
	SourceDiscovered Source = "discovered"
	SourceCustom     Source = "custom"
)

// Entry is one row in the System Permissions list. Label/Detail are display
// strings; Status/Supported are live platform facts; Enabled/Requested carry
// the persisted user intent.
type Entry struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Detail    string `json:"detail,omitempty"`
	Kind      Kind   `json:"kind"`
	Platform  string `json:"platform"`
	Supported bool   `json:"supported"`
	Status    Status `json:"status"`
	Enabled   bool   `json:"enabled"`
	// Requested records whether the user has ever asked for this grant. It
	// gates prompt-triggering folder probes on read so opening the settings
	// page never raises a dialog by itself.
	Requested bool   `json:"requested"`
	Path      string `json:"path,omitempty"`
	Source    Source `json:"source"`
}

// EntryConfig is the persisted intent for one entry. Only user intent is
// stored; live status is always recomputed.
type EntryConfig struct {
	// Enabled is the user's on/off intent.
	Enabled bool `json:"enabled,omitempty"`
	// Requested records that the user has asked for this grant at least once.
	Requested bool `json:"requested,omitempty"`
	// Path and Label are set only for user-added custom path entries.
	Path  string `json:"path,omitempty"`
	Label string `json:"label,omitempty"`
}

// Config is the persisted system-permissions sub-tree.
type Config struct {
	Entries map[string]EntryConfig `json:"entries,omitempty"`
}

// DefaultConfig returns an empty configuration. Nothing is enabled by default.
func DefaultConfig() Config {
	return Config{Entries: map[string]EntryConfig{}}
}

// Get returns the persisted config for an entry id, or the zero value.
func (c Config) Get(id string) EntryConfig {
	if c.Entries == nil {
		return EntryConfig{}
	}
	return c.Entries[id]
}

// Set stores the persisted config for an entry id, initializing the map.
func (c *Config) Set(id string, ec EntryConfig) {
	if c.Entries == nil {
		c.Entries = map[string]EntryConfig{}
	}
	c.Entries[id] = ec
}

// Delete removes an entry id. The map is left non-nil.
func (c *Config) Delete(id string) {
	if c.Entries == nil {
		return
	}
	delete(c.Entries, id)
}

// PathID returns the stable entry id for a filesystem path. The absolute,
// cleaned path is embedded so a custom path and a discovered path for the same
// directory collapse to one row.
func PathID(path string) string {
	return "path:" + filepath.Clean(path)
}

// Options carries inputs the catalog needs beyond the persisted config.
type Options struct {
	// DiscoveredPaths are directories ocode has accessed before (saved
	// projects, the server workdir) surfaced as extra path rows.
	DiscoveredPaths []string
}

// Catalog returns the full entry list for the current platform, merging the
// platform's built-ins with persisted intent and discovered/custom paths, then
// computing each entry's live status.
func Catalog(ctx context.Context, cfg Config, opts Options) []Entry {
	entries := buildCatalog(cfg, opts, builtinEntries())
	for i := range entries {
		entries[i].Status = detectEntry(ctx, entries[i])
	}
	return entries
}

// buildCatalog is the pure merge half of Catalog (no OS probing), split out so
// it can be tested deterministically with a fake builtin set.
func buildCatalog(cfg Config, opts Options, builtins []Entry) []Entry {
	entries := make([]Entry, 0, len(builtins)+len(opts.DiscoveredPaths))
	for _, b := range builtins {
		ec := cfg.Get(b.ID)
		b.Enabled = ec.Enabled
		b.Requested = ec.Requested
		if b.Source == "" {
			b.Source = SourceBuiltin
		}
		entries = append(entries, b)
	}
	entries = append(entries, pathEntries(cfg, opts.DiscoveredPaths)...)
	return entries
}

// pathEntries merges user-added custom paths (which carry a label and live in
// config) with directories discovered from saved projects, de-duplicating by
// cleaned path and sorting the result.
func pathEntries(cfg Config, discovered []string) []Entry {
	seen := map[string]bool{}
	out := make([]Entry, 0, len(discovered))

	add := func(path, label string, source Source, enabled, requested bool) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		norm := filepath.Clean(path)
		if seen[norm] {
			return
		}
		seen[norm] = true
		if label == "" {
			label = filepath.Base(norm)
		}
		out = append(out, Entry{
			ID:        PathID(norm),
			Label:     label,
			Detail:    norm,
			Kind:      KindPath,
			Platform:  runtime.GOOS,
			Supported: PlatformSupported(),
			Status:    StatusUnknown,
			Enabled:   enabled,
			Requested: requested,
			Path:      norm,
			Source:    source,
		})
	}

	// Custom entries first so a user-authored label wins for a path that also
	// shows up as a discovered project. Iterate ids in sorted order so the
	// result is deterministic (map iteration is randomized).
	ids := make([]string, 0, len(cfg.Entries))
	for id := range cfg.Entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		ec := cfg.Entries[id]
		if ec.Path == "" {
			continue
		}
		add(ec.Path, ec.Label, SourceCustom, ec.Enabled, ec.Requested)
	}
	for _, p := range discovered {
		ec := cfg.Get(PathID(p))
		add(p, ec.Label, SourceDiscovered, ec.Enabled, ec.Requested)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// RequestResult is the outcome of requesting one entry's OS grant.
type RequestResult struct {
	ID             string `json:"id"`
	Status         Status `json:"status"`
	Message        string `json:"message"`
	OpenedSettings bool   `json:"opened_settings"`
}

// Request triggers the OS permission request for one entry and returns its
// re-detected status. It never returns an error: where the platform has no
// consent gate it returns an informational result.
func Request(ctx context.Context, e Entry) RequestResult {
	if ctx == nil {
		ctx = context.Background()
	}
	return requestEntry(ctx, e)
}

// Reconcile requests every enabled, supported entry whose live status is not
// granted, using the supplied requester. It is the desktop startup path: after
// a rebuild invalidates TCC grants, this re-raises the prompts the user already
// opted into. The requester is a parameter so hosts can inject a stub (and so
// the handler's request path is exercised end-to-end in tests).
func Reconcile(ctx context.Context, entries []Entry, request func(context.Context, Entry) RequestResult) []RequestResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if request == nil {
		request = Request
	}
	var out []RequestResult
	for _, e := range entries {
		if !e.Enabled || !e.Supported {
			continue
		}
		if e.Status == StatusGranted || e.Status == StatusNotRequired {
			continue
		}
		out = append(out, request(ctx, e))
	}
	return out
}
