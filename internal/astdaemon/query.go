package astdaemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const queryTimeout = 60 * time.Second

// Match represents a single AST match from sg run --json.
type Match struct {
	Text     string      `json:"text"`
	File     string      `json:"file"`
	Language string      `json:"language"`
	Range    Range       `json:"range"`
	Matches  []SubMatch  `json:"matches,omitempty"`
}

// Range represents a source range in a match.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Position is a line/column position (0-based).
type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// SubMatch is a captured group within a match.
type SubMatch struct {
	Text  string `json:"text"`
	Range Range  `json:"range"`
	Group string `json:"group,omitempty"` // meta-variable name like $NAME
}

// SearchResult holds the full result of a search operation.
type SearchResult struct {
	Matches  []Match `json:"matches,omitempty"`
	Total    int     `json:"total"`
	Limit    int     `json:"limit,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

// SearchParams are the parameters for a pattern search.
type SearchParams struct {
	Pattern  string `json:"pattern"`            // AST pattern like "fn $NAME($$$)"
	Language string `json:"lang,omitempty"`     // optional: "go", "rust", etc.
	Path     string `json:"path,omitempty"`     // optional: restrict to file/dir
	MaxResults int  `json:"max_results,omitempty"` // default 50
}

// Search runs a pattern search against the sg index.
// The index must be up to date (daemon keeps it fresh).
func Search(projectRoot string, params SearchParams) (*SearchResult, error) {
	sg, err := FindSG()
	if err != nil {
		return nil, err
	}

	limit := params.MaxResults
	if limit <= 0 {
		limit = 50
	}

	args := []string{"run", "--json"}

	if params.Pattern != "" {
		args = append(args, "--pattern", params.Pattern)
	}
	if params.Language != "" {
		args = append(args, "--lang", params.Language)
	}
	if params.Path != "" {
		args = append(args, "--path", params.Path)
	}

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, sg, args...)
	cmd.Dir = projectRoot
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			sterr := strings.TrimSpace(string(ee.Stderr))
			return nil, fmt.Errorf("sg run: %s", sterr)
		}
		return nil, fmt.Errorf("sg run: %w", err)
	}

	var rawMatches []json.RawMessage
	if err := json.Unmarshal(out, &rawMatches); err != nil {
		return nil, fmt.Errorf("sg run: parse output: %w", err)
	}

	// Parse matches, respecting limit.
	result := &SearchResult{
		Total:   len(rawMatches),
		Matches: make([]Match, 0, limit),
	}

	if len(rawMatches) > limit {
		result.Truncated = true
		result.Limit = limit
		rawMatches = rawMatches[:limit]
	}

	for _, raw := range rawMatches {
		var m Match
		if err := json.Unmarshal(raw, &m); err != nil {
			// Skip malformed match entries.
			continue
		}
		result.Matches = append(result.Matches, m)
	}

	return result, nil
}

// SymbolKind represents a type of symbol (function, class, etc.).
type SymbolKind string

const (
	SymbolFunction  SymbolKind = "function"
	SymbolClass     SymbolKind = "class"
	SymbolMethod    SymbolKind = "method"
	SymbolInterface SymbolKind = "interface"
	SymbolStruct    SymbolKind = "struct"
	SymbolEnum      SymbolKind = "enum"
	SymbolVariable  SymbolKind = "variable"
	SymbolConstant  SymbolKind = "constant"
	SymbolType      SymbolKind = "type"
	SymbolModule    SymbolKind = "module"
)

// SymbolsParams are the parameters for listing symbols.
type SymbolsParams struct {
	Kind     SymbolKind `json:"kind,omitempty"`     // filter by symbol kind
	Language string     `json:"lang,omitempty"`     // optional language filter
	Path     string     `json:"path,omitempty"`     // optional: restrict to file/dir
	MaxResults int      `json:"max_results,omitempty"` // default 50
}

// ListSymbols returns all symbols of a given kind from the sg index.
func ListSymbols(projectRoot string, params SymbolsParams) (*SearchResult, error) {
	sg, err := FindSG()
	if err != nil {
		return nil, err
	}

	limit := params.MaxResults
	if limit <= 0 {
		limit = 50
	}

	args := []string{"run", "--json"}

	if params.Kind != "" {
		// sg uses `--kind` flag for filtering by AST node kind.
		// Some sg versions use `--ast-kind` instead.
		args = append(args, "--kind", string(params.Kind))
	}
	if params.Language != "" {
		args = append(args, "--lang", params.Language)
	}
	if params.Path != "" {
		args = append(args, "--path", params.Path)
	}

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, sg, args...)
	cmd.Dir = projectRoot
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			sterr := strings.TrimSpace(string(ee.Stderr))
			return nil, fmt.Errorf("sg symbols: %s", sterr)
		}
		return nil, fmt.Errorf("sg symbols: %w", err)
	}

	var rawMatches []json.RawMessage
	if err := json.Unmarshal(out, &rawMatches); err != nil {
		return nil, fmt.Errorf("sg symbols: parse output: %w", err)
	}

	result := &SearchResult{
		Total:   len(rawMatches),
		Matches: make([]Match, 0, limit),
	}

	if len(rawMatches) > limit {
		result.Truncated = true
		result.Limit = limit
		rawMatches = rawMatches[:limit]
	}

	for _, raw := range rawMatches {
		var m Match
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		result.Matches = append(result.Matches, m)
	}

	return result, nil
}

// IndexStatus reports the freshness of the AST index.
type IndexStatus struct {
	Installed      bool   `json:"installed"`
	Version        string `json:"version,omitempty"`
	IndexExists    bool   `json:"index_exists"`
	DaemonAlive    bool   `json:"daemon_alive"`
	IndexDir       string `json:"index_dir"`
}

// GetIndexStatus returns info about the sg installation and index health.
func GetIndexStatus(projectRoot string) (*IndexStatus, error) {
	status := &IndexStatus{
		IndexDir: filepath.Join(projectRoot, indexDirRel),
	}

	// Check sg installation.
	_, err := FindSG()
	if err == nil {
		status.Installed = true
		// Get version.
		ver, err := SgVersion()
		if err == nil {
			status.Version = ver
		}

		// Check if index exists (look for sg's SQLite db).
		indexDB := filepath.Join(status.IndexDir, "index.db")
		if _, err := os.Stat(indexDB); err == nil {
			status.IndexExists = true
		}
		// sg may also use index.sqlite on some versions.
		indexSQLite := filepath.Join(status.IndexDir, "index.sqlite")
		if _, err := os.Stat(indexSQLite); err == nil {
			status.IndexExists = true
		}
	}

	// Check daemon lock.
	lockFile := filepath.Join(projectRoot, lockFileRel)
	if data, err := os.ReadFile(lockFile); err == nil {
		pidStr := strings.TrimSpace(string(data))
		if pid, err := strconv.Atoi(pidStr); err == nil && processExists(pid) {
			status.DaemonAlive = true
		}
	}

	return status, nil
}
