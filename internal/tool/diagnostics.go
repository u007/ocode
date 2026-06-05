package tool

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/u007/ocode/internal/lsp"
)

// LSPDiagnosticsTool lets the agent read the latest LSP diagnostics
// (errors / warnings / hints) reported by the project's language servers.
//
// The tool is a thin wrapper around the Manager-owned DiagnosticStore —
// it never blocks on a server, it never sends an LSP request. The server
// is expected to have already published its diagnostics in response to
// didOpen/didChange; we just render what's cached.
//
// The default behaviour (no params) returns the first DefaultLimit
// diagnostics. With `offset`/`limit` the agent can paginate through the
// full list. With `path` the list is filtered to a single file (and the
// tool will EnsureOpen that file so the server is forced to re-validate
// and republish). With `severity` the list is filtered to a minimum
// severity (`error` >= `warning` >= `info` >= `hint`).
type LSPDiagnosticsTool struct {
	// Mgr, if set, is the shared LSP manager (so this tool reuses the
	// same gopls/rust-analyzer instance the `lsp` tool is using). When
	// nil the tool creates a private manager (tests).
	Mgr *lsp.Manager
}

const lspDiagnosticsDefaultLimit = 50
const lspDiagnosticsMaxLimit = 1000

func (t *LSPDiagnosticsTool) Name() string { return "lsp_diagnostics" }
func (t *LSPDiagnosticsTool) Description() string {
	return "Read LSP diagnostics (errors/warnings/info/hints) reported by the project's language servers"
}
func (t *LSPDiagnosticsTool) Parallel() bool { return true }

func (t *LSPDiagnosticsTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name": "lsp_diagnostics",
		"description": fmt.Sprintf(
			"Read LSP diagnostics (errors, warnings, infos, hints) reported by the project's language servers (gopls, rust-analyzer, typescript-language-server, etc). "+
				"Returns the first %d diagnostics by default, sorted by file then line. "+
				"Use `offset` and `limit` to paginate, `path` to filter to a single file (the file is also re-opened with the server so fresh diagnostics are pulled), and `severity` to filter to a minimum level "+
				"(one of %q). The output header always shows the total count and file count so you can decide whether to paginate. "+
				"This tool reads the same cache that is auto-injected into the system message on every turn; call it when you need entries beyond the first %d, or want to filter by file/severity.",
			lspDiagnosticsDefaultLimit,
			[]string{"error", "warning", "info", "hint"},
			lspDiagnosticsDefaultLimit,
		),
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Optional file path. If set, only diagnostics for this file are returned AND the file is opened with its language server so the server re-publishes fresh diagnostics.",
				},
				"severity": map[string]interface{}{
					"type":        "string",
					"description": "Minimum severity to include. One of: error, warning, info, hint. Defaults to showing all severities.",
					"enum":        []string{"error", "warning", "info", "hint"},
				},
				"offset": map[string]interface{}{
					"type":        "integer",
					"description": fmt.Sprintf("0-based starting index in the sorted diagnostic list. Defaults to 0. Use with `limit` to paginate beyond the first %d.", lspDiagnosticsDefaultLimit),
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": fmt.Sprintf("Maximum number of diagnostics to return. Defaults to %d, capped at %d.", lspDiagnosticsDefaultLimit, lspDiagnosticsMaxLimit),
				},
			},
		},
	}
}

func (t *LSPDiagnosticsTool) manager() *lsp.Manager {
	if t.Mgr != nil {
		return t.Mgr
	}
	return lsp.NewManager(".")
}

func (t *LSPDiagnosticsTool) Execute(args json.RawMessage) (string, error) {
	var input struct {
		Path     string `json:"path"`
		Severity string `json:"severity"`
		Offset   int    `json:"offset"`
		Limit    int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return "", err
	}

	if input.Limit < 0 {
		return "", fmt.Errorf("lsp_diagnostics: limit must be non-negative (got %d)", input.Limit)
	}
	if input.Offset < 0 {
		return "", fmt.Errorf("lsp_diagnostics: offset must be non-negative (got %d)", input.Offset)
	}
	if input.Limit == 0 {
		input.Limit = lspDiagnosticsDefaultLimit
	}
	if input.Limit > lspDiagnosticsMaxLimit {
		input.Limit = lspDiagnosticsMaxLimit
	}

	minSev := parseSeverity(input.Severity)

	mgr := t.manager()
	store := mgr.Diagnostics()
	if store == nil {
		return "LSP diagnostics: none (no diagnostic store).", nil
	}

	// If the agent asks for diagnostics on a single file, force the
	// server to re-validate by opening it. Without this the cache could
	// be stale (gopls only re-publishes on didChange / didOpen). We do
	// NOT block waiting for the server to publish — the next call to
	// this tool (a few hundred ms later, usually) will see the fresh
	// state. The store's previous contents for the file are still
	// returned in the meantime so the agent isn't left with empty data.
	if input.Path != "" {
		_ = mgr.EnsureOpen(input.Path) // non-fatal: see LSPTool.Execute
	}

	// Pull the full sorted list, then apply filters and pagination.
	all := store.All()
	if minSev > 0 {
		all = lsp.FilteredByMinSeverity(all, minSev)
	}
	if input.Path != "" {
		all = filterByPath(all, input.Path)
	}
	total := len(all)
	if total == 0 {
		// Distinguish "no diagnostics anywhere" from "no diagnostics match
		// the filter" so the agent doesn't think the server is healthy
		// when the filter just wiped everything out.
		if store.IsEmpty() {
			return "LSP diagnostics: none.", nil
		}
		return fmt.Sprintf("LSP diagnostics: 0 match the filter (severity=%q path=%q). Total in cache: %d across %d file(s).",
			input.Severity, input.Path, store.Count(), store.FileCount()), nil
	}
	return RenderDiagnosticsPage(all, input.Offset, input.Limit), nil
}

// RenderDiagnosticsPage renders a page of diagnostics in the canonical
// header + rows format used by the tool output and the agent's transient
// system-message injection.
func RenderDiagnosticsPage(filtered []lsp.Diagnostic, offset, limit int) string {
	total := len(filtered)
	if total == 0 {
		return "LSP diagnostics: none."
	}
	if limit <= 0 {
		limit = lspDiagnosticsDefaultLimit
	}
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	page := filtered[offset:end]

	// Compose the response. Header shows the total + file count so the
	// agent can decide whether to paginate; the rendered lines are
	// identical to the format injected into the system message.
	header := renderDiagnosticsHeader(filtered, len(page), offset)
	var b strings.Builder
	b.WriteString(header)
	b.WriteByte('\n')
	for i, d := range page {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(renderDiagnosticLine(d))
	}
	return b.String()
}

// parseSeverity converts the tool's severity string into a
// DiagnosticSeverity. Unknown values return 0 (meaning: "no filter").
func parseSeverity(s string) lsp.DiagnosticSeverity {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "error":
		return lsp.SeverityError
	case "warning", "warn":
		return lsp.SeverityWarning
	case "info", "information":
		return lsp.SeverityInformation
	case "hint":
		return lsp.SeverityHint
	default:
		return 0
	}
}

// filterByPath returns the subset of diags whose Path matches target.
// Matches are case-insensitive (LSP URIs are case-sensitive but
// Windows filesystems are not) and tolerant of relative vs. absolute
// forms by comparing both as cleaned absolute paths.
func filterByPath(diags []lsp.Diagnostic, target string) []lsp.Diagnostic {
	absTarget, _ := filepath.Abs(target)
	absTarget = filepath.Clean(absTarget)
	out := make([]lsp.Diagnostic, 0, len(diags))
	for _, d := range diags {
		ap := filepath.Clean(d.Path)
		if d.Path == target || ap == target {
			out = append(out, d)
			continue
		}
		if absTarget != "" && ap == absTarget {
			out = append(out, d)
			continue
		}
		// Also accept a matching suffix so "internal/foo.go" matches
		// diagnostics stored as "/abs/path/to/repo/internal/foo.go".
		if strings.HasSuffix(ap, string(filepath.Separator)+target) {
			out = append(out, d)
		}
	}
	return out
}

// renderDiagnosticsHeader produces the "LSP diagnostics (N total across
// K files)" line. Inputs:
//   - filtered: the (filtered + sorted) list we're about to render from
//   - shown: how many of `filtered` we're actually rendering (after paging)
//   - offset: where the page starts in `filtered`
//
// The severity breakdown and the file count are both computed from
// `filtered` (not the unfiltered store), so a path/severity filter
// narrows the header too — showing "0 errors" is correct when the
// filter excluded them.
func renderDiagnosticsHeader(filtered []lsp.Diagnostic, shown, offset int) string {
	total := len(filtered)
	if total == 0 {
		return "LSP diagnostics: none."
	}
	severity := make(map[lsp.DiagnosticSeverity]int, 4)
	for _, d := range filtered {
		severity[d.Severity]++
	}
	parts := []string{}
	if c := severity[lsp.SeverityError]; c > 0 {
		parts = append(parts, pluralize(c, "error", "errors"))
	}
	if c := severity[lsp.SeverityWarning]; c > 0 {
		parts = append(parts, pluralize(c, "warning", "warnings"))
	}
	if c := severity[lsp.SeverityInformation]; c > 0 {
		parts = append(parts, pluralize(c, "info", "infos"))
	}
	if c := severity[lsp.SeverityHint]; c > 0 {
		parts = append(parts, pluralize(c, "hint", "hints"))
	}
	summary := strings.Join(parts, ", ")
	if summary == "" {
		summary = fmt.Sprintf("%d diagnostics", total)
	}
	// File count: the number of distinct files represented in `filtered`
	// (so a path filter narrows this to 1).
	seen := make(map[string]struct{}, total)
	for _, d := range filtered {
		seen[d.URI] = struct{}{}
	}
	header := fmt.Sprintf("LSP diagnostics: %s across %d file(s).", summary, len(seen))
	switch {
	case total > shown && offset > 0:
		header += fmt.Sprintf(" Showing %d-%d of %d (offset=%d).", offset+1, offset+shown, total, offset)
	case total > shown:
		header += fmt.Sprintf(" Showing first %d of %d.", shown, total)
	case offset > 0:
		header += fmt.Sprintf(" Showing %d-%d of %d (offset=%d).", offset+1, offset+shown, total, offset)
	}
	return header
}

// renderDiagnosticLine produces the canonical line for a single
// diagnostic, e.g.:
//
//	internal/foo.go:42:7  [error unusedvar]  variable 'x' is unused
//
// Used by both the tool (full output) and the agent-loop auto-inject
// (system message). Kept as a small helper so the two paths can never
// drift in formatting.
func renderDiagnosticLine(d lsp.Diagnostic) string {
	loc := fmt.Sprintf("%s:%d:%d", displayPath(d.Path), d.Range.Start.Line+1, d.Range.Start.Character+1)
	label := d.Severity.String()
	if d.Code != "" {
		label = label + " " + d.Code
	}
	return fmt.Sprintf("%s  [%s]  %s", loc, label, d.Message)
}

// displayPath shortens d.Path relative to the current working
// directory, falling back to the absolute path. Wraps the same logic
// used by formatLocations in lsp_format.go so the tool output and the
// system-prompt fragment stay consistent.
func displayPath(p string) string {
	if p == "" {
		return ""
	}
	if r := relPath(p); r != "" {
		return r
	}
	return p
}

// pluralize returns "N word" or "N words" based on count. Used in the
// rendered header to keep the severity breakdown grammatical.
func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}
