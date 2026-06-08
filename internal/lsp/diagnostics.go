package lsp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DiagnosticSeverity matches the LSP spec (window/showMessage and
// PublishDiagnosticsParams both use the same integer codes).
//
//	1 = Error, 2 = Warning, 3 = Information, 4 = Hint
type DiagnosticSeverity int

const (
	SeverityError       DiagnosticSeverity = 1
	SeverityWarning     DiagnosticSeverity = 2
	SeverityInformation DiagnosticSeverity = 3
	SeverityHint        DiagnosticSeverity = 4
)

// String returns the canonical lowercase name used in tool output and
// the system-prompt fragment (so the agent sees consistent labels).
func (s DiagnosticSeverity) String() string {
	switch s {
	case SeverityError:
		return "error"
	case SeverityWarning:
		return "warning"
	case SeverityInformation:
		return "info"
	case SeverityHint:
		return "hint"
	default:
		return fmt.Sprintf("severity-%d", int(s))
	}
}

// Diagnostic is the subset of LSP's PublishDiagnosticsParams.diagnostics
// we surface to the agent. It is intentionally a value type (not a raw
// json.RawMessage) so callers — both the tool layer and the system-prompt
// fragment — can format it directly without re-parsing.
type Diagnostic struct {
	URI       string             // file:// URI as received from the server
	Path      string             // local filesystem path (URI decoded); relative to wd when possible
	Range     Range              // primary span (0-based lines/cols)
	Severity  DiagnosticSeverity // 0 when the server omitted the field
	Code      string             // server-supplied code (e.g. "unusedvar"); "" if absent
	Source    string             // e.g. "gopls", "typescript"; "" if absent
	ServerCmd string             // binary name of the owning server (e.g. "gopls"); set by Manager
	Message   string             // human-readable description
}

// diagnosticParams mirrors the LSP wire shape for textDocument/publishDiagnostics.
// Kept private — callers go through parseDiagnosticsPayload.
type diagnosticParams struct {
	URI         string    `json:"uri"`
	Diagnostics []rawDiag `json:"diagnostics"`
	Version     *int      `json:"version,omitempty"`
}

type rawDiag struct {
	Range    rangeT `json:"range"`
	Severity *int   `json:"severity,omitempty"`
	Code     any    `json:"code,omitempty"`
	Source   string `json:"source,omitempty"`
	Message  string `json:"message"`
}

type rangeT struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// parseDiagnosticsPayload decodes a publishDiagnostics notification body
// into []Diagnostic. Malformed frames yield an empty slice rather than
// an error — diagnostics are best-effort and should never abort the
// read loop.
func parseDiagnosticsPayload(body []byte) []Diagnostic {
	var p diagnosticParams
	if err := json.Unmarshal(body, &p); err != nil {
		return nil
	}
	return buildDiagnostics(&p)
}

// buildDiagnostics converts a parsed publishDiagnostics payload into
// []Diagnostic. Split out so callers that already decoded the JSON
// (e.g. the readLoop) don't pay for a second json.Unmarshal.
func buildDiagnostics(p *diagnosticParams) []Diagnostic {
	if p == nil || p.URI == "" {
		return nil
	}
	if len(p.Diagnostics) == 0 {
		return nil
	}
	path := uriToPath(p.URI)
	out := make([]Diagnostic, 0, len(p.Diagnostics))
	for _, d := range p.Diagnostics {
		sev := SeverityError // LSP spec: missing severity defaults to Error
		if d.Severity != nil {
			sev = DiagnosticSeverity(*d.Severity)
		}
		out = append(out, Diagnostic{
			URI:      p.URI,
			Path:     path,
			Range:    Range{Start: d.Range.Start, End: d.Range.End},
			Severity: sev,
			Code:     stringifyCode(d.Code),
			Source:   d.Source,
			Message:  d.Message,
		})
	}
	return out
}

// extractDiagnosticsURI peeks at a publishDiagnostics payload's "uri"
// field without fully parsing the rest. Used to route the notification
// to the right per-URI slot in the store. Returns "" on a malformed
// payload; the handler then ignores the call (buildDiagnostics also
// returns nil in that case, so the store is not corrupted).
func extractDiagnosticsURI(body []byte) string {
	var p struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return ""
	}
	return p.URI
}

// stringifyCode handles both string and number code shapes (LSP allows
// either). Numbers are formatted as their base-10 representation; the
// common gopls codes are strings (e.g. "unusedvar").
func stringifyCode(v any) string {
	switch c := v.(type) {
	case nil:
		return ""
	case string:
		return c
	case float64:
		// JSON numbers decode as float64 into `any`. Format without
		// a trailing ".0" for whole values.
		if c == float64(int64(c)) {
			return fmt.Sprintf("%d", int64(c))
		}
		return fmt.Sprintf("%g", c)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// DiagnosticStore holds the most recently published diagnostics keyed by
// file URI. It is owned by a Manager and is safe for concurrent use: the
// readLoop goroutine calls SetURI after parsing each publishDiagnostics
// frame, while tool/agent callers call Snapshot/All from other goroutines.
type DiagnosticStore struct {
	mu         sync.RWMutex
	byURI      map[string][]Diagnostic
	updatedAt  time.Time // last successful update; zero value means "never"
	generation uint64
	// updatedByURI tracks per-URI timestamps so the formatter can show
	// "stale" badges for URIs the server stopped reporting on. We do not
	// expire entries on a timer — the server is expected to re-publish
	// (often with an empty list) when a document is closed.
	updatedByURI map[string]time.Time

	// notify, when non-nil, receives a non-blocking signal on every
	// diagnostics change so the TUI can proactively re-render the sidebar
	// LSP count without waiting for the next user interaction.
	notify chan struct{}
}

func newDiagnosticStore() *DiagnosticStore {
	return &DiagnosticStore{
		byURI:        make(map[string][]Diagnostic),
		updatedByURI: make(map[string]time.Time),
	}
}

// SetNotifyChan sets a channel that receives a non-blocking signal whenever
// diagnostics change. Pass nil to disable. Must be called before any LSP
// servers are started.
func (s *DiagnosticStore) SetNotifyChan(ch chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notify = ch
}

// notifyChange sends a non-blocking signal on the notify channel (if set).
func (s *DiagnosticStore) notifyChange() {
	if s.notify == nil {
		return
	}
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

// Generation reports the current generation used to gate stale diagnostics
// callbacks. When the manager is closed or a client is restarted, the
// generation is bumped so any in-flight publishDiagnostics from the previous
// lifecycle are ignored.
func (s *DiagnosticStore) Generation() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.generation
}

// BumpGeneration increments the store generation and returns the new value.
// Call this before shutting down a server to invalidate its callback.
func (s *DiagnosticStore) BumpGeneration() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generation++
	return s.generation
}

// SetURIIfGeneration replaces the cached diagnostics for a single URI only if
// the caller's generation still matches the store's current generation. This
// lets managers invalidate stale in-flight callbacks before a restart/close.
func (s *DiagnosticStore) SetURIIfGeneration(uri string, diags []Diagnostic, generation uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generation != generation {
		return false
	}
	if s.byURI == nil {
		s.byURI = make(map[string][]Diagnostic)
		s.updatedByURI = make(map[string]time.Time)
	}
	if len(diags) == 0 {
		delete(s.byURI, uri)
		s.updatedByURI[uri] = time.Now()
		s.updatedAt = time.Now()
		s.notifyChange()
		return true
	}
	cp := make([]Diagnostic, len(diags))
	copy(cp, diags)
	s.byURI[uri] = cp
	s.updatedByURI[uri] = time.Now()
	s.updatedAt = time.Now()
	s.notifyChange()
	return true
}

// SetURI replaces the cached diagnostics for a single URI. Pass nil or an
// empty slice to mark the file as "clean" (LSP publishes an empty array when
// a file's diagnostics clear).
func (s *DiagnosticStore) SetURI(uri string, diags []Diagnostic) {
	s.setURI(uri, diags)
}

// setURI replaces the cached diagnostics for a single URI. Pass nil or an
// empty slice to mark the file as "clean" (LSP publishes an empty array when
// a file's diagnostics clear).
func (s *DiagnosticStore) setURI(uri string, diags []Diagnostic) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byURI == nil {
		s.byURI = make(map[string][]Diagnostic)
		s.updatedByURI = make(map[string]time.Time)
	}
	if len(diags) == 0 {
		// Treat nil and an empty slice the same: the server is telling
		// us "no problems in this file anymore".
		delete(s.byURI, uri)
		s.updatedByURI[uri] = time.Now()
		s.updatedAt = time.Now()
		s.notifyChange()
		return
	}
	// Copy so the caller can mutate its slice without aliasing the store.
	cp := make([]Diagnostic, len(diags))
	copy(cp, diags)
	s.byURI[uri] = cp
	s.updatedByURI[uri] = time.Now()
	s.updatedAt = time.Now()
	s.notifyChange()
}

// clear empties the entire store. Called when a server is closed/restarted
// so the agent never sees stale diagnostics from a previous server life.
func (s *DiagnosticStore) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byURI = make(map[string][]Diagnostic)
	s.updatedByURI = make(map[string]time.Time)
	s.updatedAt = time.Time{}
}

// All returns a flat slice of every cached diagnostic, sorted for stable
// output: by file (path, with leading path separators normalised), then
// by line, column. The slice is freshly allocated; callers may mutate it.
func (s *DiagnosticStore) All() []Diagnostic {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Diagnostic
	for _, list := range s.byURI {
		out = append(out, list...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		pi := filepath.ToSlash(out[i].Path)
		pj := filepath.ToSlash(out[j].Path)
		if pi != pj {
			return pi < pj
		}
		if out[i].Range.Start.Line != out[j].Range.Start.Line {
			return out[i].Range.Start.Line < out[j].Range.Start.Line
		}
		return out[i].Range.Start.Character < out[j].Range.Start.Character
	})
	return out
}

// Count returns the total number of cached diagnostics.
func (s *DiagnosticStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, list := range s.byURI {
		n += len(list)
	}
	return n
}

// FileCount returns the number of files that currently have at least one
// diagnostic.
func (s *DiagnosticStore) FileCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byURI)
}

// FilteredByURI returns diagnostics for the given file URI. An empty URI
// returns all diagnostics across all files.
func (s *DiagnosticStore) FilteredByURI(uri string) []Diagnostic {
	all := s.All()
	if uri == "" {
		return all
	}
	out := all[:0]
	for _, d := range all {
		if d.URI == uri {
			out = append(out, d)
		}
	}
	return out
}

// FilteredByMinSeverity returns diagnostics with severity >= min.
// Pass 0 to keep all severities.
func FilteredByMinSeverity(in []Diagnostic, min DiagnosticSeverity) []Diagnostic {
	if min <= 0 {
		return in
	}
	out := make([]Diagnostic, 0, len(in))
	for _, d := range in {
		if d.Severity >= min {
			out = append(out, d)
		}
	}
	return out
}

// Snapshot is a compact, allocation-free-ish view returned by Manager
// helpers. Used by the auto-inject path in the agent loop, which doesn't
// need the full slice — just a count and the first N rendered lines.
type Snapshot struct {
	Total     int // total diagnostics across all files
	Files     int // number of files with at least one diagnostic
	Severity  map[DiagnosticSeverity]int
	FirstN    []Diagnostic // first N diagnostics (sorted as All()), capped
	UpdatedAt time.Time    // when the store was last modified; zero means empty
}

// Snapshot returns a point-in-time summary used by the agent's
// auto-inject. n caps the number of diagnostics included in FirstN; the
// agent's default is 50, matching the lsp_diagnostics tool default.
func (s *DiagnosticStore) Snapshot(n int) Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := Snapshot{
		UpdatedAt: s.updatedAt,
		Severity:  make(map[DiagnosticSeverity]int, 4),
	}
	if n <= 0 {
		n = 50
	}
	// Collect into a flat slice and sort so the snapshot order is stable
	// (matches All()).
	var flat []Diagnostic
	for _, list := range s.byURI {
		flat = append(flat, list...)
	}
	sort.SliceStable(flat, func(i, j int) bool {
		pi := filepath.ToSlash(flat[i].Path)
		pj := filepath.ToSlash(flat[j].Path)
		if pi != pj {
			return pi < pj
		}
		if flat[i].Range.Start.Line != flat[j].Range.Start.Line {
			return flat[i].Range.Start.Line < flat[j].Range.Start.Line
		}
		return flat[i].Range.Start.Character < flat[j].Range.Start.Character
	})
	snap.Total = len(flat)
	snap.Files = len(s.byURI)
	for _, d := range flat {
		snap.Severity[d.Severity]++
	}
	if len(flat) > n {
		snap.FirstN = flat[:n]
	} else {
		snap.FirstN = flat
	}
	return snap
}

// IsEmpty reports whether the store currently has any diagnostics at all.
// Cheap O(1) check used by the agent loop to skip the auto-inject
// entirely when there's nothing to report.
func (s *DiagnosticStore) IsEmpty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byURI) == 0
}

// renderHeader produces the "LSP diagnostics (N total across K files)"
// line that goes above the rendered list. The total and file count
// surface the "use offset/limit to read more" affordance.
func renderHeader(snap Snapshot) string {
	if snap.Total == 0 {
		return "LSP diagnostics: none."
	}
	// Show breakdown only when there's a mix of severities — otherwise
	// "5 errors across 2 files" is plenty.
	parts := []string{}
	if c := snap.Severity[SeverityError]; c > 0 {
		parts = append(parts, pluralize(c, "error", "errors"))
	}
	if c := snap.Severity[SeverityWarning]; c > 0 {
		parts = append(parts, pluralize(c, "warning", "warnings"))
	}
	if c := snap.Severity[SeverityInformation]; c > 0 {
		parts = append(parts, pluralize(c, "info", "infos"))
	}
	if c := snap.Severity[SeverityHint]; c > 0 {
		parts = append(parts, pluralize(c, "hint", "hints"))
	}
	summary := strings.Join(parts, ", ")
	if summary == "" {
		summary = fmt.Sprintf("%d diagnostics", snap.Total)
	}
	show := len(snap.FirstN)
	header := fmt.Sprintf("LSP diagnostics: %s across %d file(s).", summary, snap.Files)
	if show < snap.Total {
		header += fmt.Sprintf(" Showing first %d of %d.", show, snap.Total)
	}
	return header
}

// renderLine produces a single human-readable diagnostic line of the form
//
//	internal/foo.go:42:7  [error] unusedvar  variable 'x' is unused
//
// The path is shortened relative to the working directory when possible
// (matches formatLocations in lsp_format.go). Empty code/severity labels
// are omitted for brevity.
func renderLine(d Diagnostic) string {
	p := relPath(d.Path)
	loc := fmt.Sprintf("%s:%d:%d", p, d.Range.Start.Line+1, d.Range.Start.Character+1)
	label := d.Severity.String()
	if d.Code != "" {
		label = label + " " + d.Code
	}
	return fmt.Sprintf("%s  [%s]  %s", loc, label, d.Message)
}

// relPath shortens p relative to the current working directory when
// possible; otherwise returns p unchanged. Mirrors the helper in
// lsp_format.go (duplicated here to keep the lsp package self-contained —
// lsp_format.go imports lsp, not the other way around).
func relPath(p string) string {
	if p == "" {
		return ""
	}
	wd, err := os.Getwd()
	if err != nil {
		return p
	}
	if rel, err := filepath.Rel(wd, p); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return p
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}
