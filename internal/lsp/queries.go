package lsp

import (
	"encoding/json"
	"fmt"
)

// Position is a 0-based line/character location.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is a span between two positions.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location is a range within a document.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// SymbolInformation is a workspace/document symbol entry.
type SymbolInformation struct {
	Name     string   `json:"name"`
	Kind     int      `json:"kind"`
	Location Location `json:"location"`
	// documentSymbol returns a nested form with selectionRange instead of
	// location; we normalise to Location in DocumentSymbols.
}

func (c *Client) posParams(uri string, pos Position) map[string]interface{} {
	return map[string]interface{}{
		"textDocument": map[string]interface{}{"uri": uri},
		"position":     map[string]interface{}{"line": pos.Line, "character": pos.Character},
	}
}

// HoverParams builds textDocument/position params for a file path + position,
// using a proper file:// URI (handles paths with spaces/special chars).
func (c *Client) HoverParams(path string, pos Position) map[string]interface{} {
	uri, _ := absURI(path)
	return c.posParams(uri, pos)
}

// Definition resolves textDocument/definition. path is opened automatically.
func (c *Client) Definition(path string, pos Position) ([]Location, error) {
	if err := c.EnsureOpen(path); err != nil {
		return nil, err
	}
	abs, _ := absURI(path)
	res, err := c.Call("textDocument/definition", c.posParams(abs, pos))
	if err != nil {
		return nil, err
	}
	return decodeLocations(res)
}

// References resolves textDocument/references (including the declaration).
func (c *Client) References(path string, pos Position) ([]Location, error) {
	if err := c.EnsureOpen(path); err != nil {
		return nil, err
	}
	abs, _ := absURI(path)
	params := c.posParams(abs, pos)
	params["context"] = map[string]interface{}{"includeDeclaration": true}
	res, err := c.Call("textDocument/references", params)
	if err != nil {
		return nil, err
	}
	return decodeLocations(res)
}

// Implementation resolves textDocument/implementation.
func (c *Client) Implementation(path string, pos Position) ([]Location, error) {
	if err := c.EnsureOpen(path); err != nil {
		return nil, err
	}
	abs, _ := absURI(path)
	res, err := c.Call("textDocument/implementation", c.posParams(abs, pos))
	if err != nil {
		return nil, err
	}
	return decodeLocations(res)
}

// WorkspaceSymbols resolves workspace/symbol for a name query.
func (c *Client) WorkspaceSymbols(query string) ([]SymbolInformation, error) {
	res, err := c.Call("workspace/symbol", map[string]interface{}{"query": query})
	if err != nil {
		return nil, err
	}
	var syms []SymbolInformation
	if err := json.Unmarshal(res, &syms); err != nil {
		return nil, fmt.Errorf("parse workspace symbols: %w", err)
	}
	return syms, nil
}

// DocumentSymbolNode is the hierarchical LSP DocumentSymbol response: every
// node carries a SelectionRange and a (possibly empty) Children slice.
type DocumentSymbolNode struct {
	Name           string               `json:"name"`
	Kind           int                  `json:"kind"`
	Range          Range                `json:"range"`
	SelectionRange Range                `json:"selectionRange"`
	Children       []DocumentSymbolNode `json:"children,omitempty"`
}

// flattenDocument walks a hierarchical documentSymbol tree into a flat list,
// prefixing nested names with their parent so callers (and humans) can see
// the structure ("Type.Method", "Type.NestedType.Field"). The Kind is taken
// from the deepest node and the Location is the SelectionRange.
func flattenDocument(roots []DocumentSymbolNode, prefix string) []SymbolInformation {
	var out []SymbolInformation
	for _, n := range roots {
		name := n.Name
		if prefix != "" {
			name = prefix + "." + n.Name
		}
		out = append(out, SymbolInformation{
			Name:     name,
			Kind:     n.Kind,
			Location: Location{Range: n.SelectionRange},
		})
		if len(n.Children) > 0 {
			out = append(out, flattenDocument(n.Children, name)...)
		}
	}
	return out
}

// isHierarchicalSymbolShape peeks at the first element of a documentSymbol
// response and reports whether it's the hierarchical form (has a "range" key)
// or the flat SymbolInformation[] form (has a "location" key). Both shapes
// decode as a top-level JSON array; the disambiguator is the per-element key.
func isHierarchicalSymbolShape(raw json.RawMessage) bool {
	var peek []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &peek); err != nil || len(peek) == 0 {
		return false
	}
	_, hasRange := peek[0]["range"]
	_, hasLocation := peek[0]["location"]
	return hasRange && !hasLocation
}

// DocumentSymbols resolves textDocument/documentSymbol, normalising both the
// flat SymbolInformation[] and hierarchical DocumentSymbol[] response shapes.
// Nested children are flattened with dotted names so the consumer can see
// e.g. "Type.Method" instead of losing the inner scope.
func (c *Client) DocumentSymbols(path string) ([]SymbolInformation, error) {
	if err := c.EnsureOpen(path); err != nil {
		return nil, err
	}
	abs, _ := absURI(path)
	res, err := c.Call("textDocument/documentSymbol", map[string]interface{}{
		"textDocument": map[string]interface{}{"uri": abs},
	})
	if err != nil {
		return nil, err
	}
	if isHierarchicalSymbolShape(res) {
		var hier []DocumentSymbolNode
		if err := json.Unmarshal(res, &hier); err != nil {
			return nil, fmt.Errorf("parse document symbols: %w", err)
		}
		flat := flattenDocument(hier, "")
		// Stamp the document URI onto every location so the formatter can
		// show path:line without re-resolving the original file.
		for i := range flat {
			flat[i].Location.URI = abs
		}
		return flat, nil
	}
	var flat []SymbolInformation
	if err := json.Unmarshal(res, &flat); err != nil {
		return nil, fmt.Errorf("parse document symbols: %w", err)
	}
	return flat, nil
}

// CallHierarchyItem is a node in the call hierarchy.
type CallHierarchyItem struct {
	Name           string `json:"name"`
	Kind           int    `json:"kind"`
	URI            string `json:"uri"`
	Range          Range  `json:"range"`
	SelectionRange Range  `json:"selectionRange"`
}

// IncomingCalls returns the callers of the symbol at pos (best-effort: requires
// the server to support callHierarchy; gopls does, many others do not).
func (c *Client) IncomingCalls(path string, pos Position) ([]Location, error) {
	if err := c.EnsureOpen(path); err != nil {
		return nil, err
	}
	abs, _ := absURI(path)
	prep, err := c.Call("textDocument/prepareCallHierarchy", c.posParams(abs, pos))
	if err != nil {
		return nil, err
	}
	var items []CallHierarchyItem
	if err := json.Unmarshal(prep, &items); err != nil || len(items) == 0 {
		return nil, fmt.Errorf("no call hierarchy at position (server may not support it)")
	}
	res, err := c.Call("callHierarchy/incomingCalls", map[string]interface{}{"item": items[0]})
	if err != nil {
		return nil, err
	}
	var calls []struct {
		From       CallHierarchyItem `json:"from"`
		FromRanges []Range           `json:"fromRanges"`
	}
	if err := json.Unmarshal(res, &calls); err != nil {
		return nil, fmt.Errorf("parse incoming calls: %w", err)
	}
	out := make([]Location, 0, len(calls))
	for _, call := range calls {
		rng := call.From.SelectionRange
		if len(call.FromRanges) > 0 {
			rng = call.FromRanges[0]
		}
		out = append(out, Location{URI: call.From.URI, Range: rng})
	}
	return out, nil
}

// decodeLocations handles the three shapes definition/references can return:
// null, a single Location, or a Location[].
func decodeLocations(res json.RawMessage) ([]Location, error) {
	trimmed := string(res)
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var arr []Location
	if err := json.Unmarshal(res, &arr); err == nil {
		return arr, nil
	}
	var single Location
	if err := json.Unmarshal(res, &single); err != nil {
		return nil, fmt.Errorf("parse locations: %w", err)
	}
	return []Location{single}, nil
}
