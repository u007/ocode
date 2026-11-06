package tool

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// ViewerControlSentinel prefixes the viewer_control tool result. The web
// PreviewHost scans chat tool results for this prefix and applies the
// directive to the preview beside the chat. The directive carries a random
// id so the UI applies each call once and viewer_state can report whether
// the last control landed.
const ViewerControlSentinel = "VIEWER_CONTROL:"

// viewerControlOneOf lists, per action, the parameters it accepts. A scroll
// or select takes exactly one of its group; a find takes its query; the
// find_* actions take nothing.
var viewerControlOneOf = map[string][]string{
	"scroll":     {"page", "slide", "line", "heading", "direction"},
	"select":     {"text", "lines", "range"},
	"find":       {"query"},
	"find_next":  nil,
	"find_prev":  nil,
	"find_close": nil,
}

// viewerControlFields is the fixed order parameters are checked in, so the
// first violation reported is deterministic.
var viewerControlFields = []string{"page", "slide", "line", "heading", "direction", "text", "lines", "range", "query"}

type viewerLines struct {
	Start *int `json:"start"`
	End   *int `json:"end"`
}

// viewerControlParams holds the optional parameters. Pointers distinguish
// "absent" from a zero value, so `"page": 0` is reported, not ignored.
type viewerControlParams struct {
	Page      *int         `json:"page,omitempty"`
	Slide     *int         `json:"slide,omitempty"`
	Line      *int         `json:"line,omitempty"`
	Heading   *string      `json:"heading,omitempty"`
	Direction *string      `json:"direction,omitempty"`
	Text      *string      `json:"text,omitempty"`
	Lines     *viewerLines `json:"lines,omitempty"`
	Range     *string      `json:"range,omitempty"`
	Query     *string      `json:"query,omitempty"`
}

type viewerControlArgs struct {
	Action string `json:"action"`
	viewerControlParams
}

// viewerControlDirective is the wire payload: id first, then action, then
// only the provided parameters.
type viewerControlDirective struct {
	ID     string `json:"id"`
	Action string `json:"action"`
	viewerControlParams
}

// ViewerControlTool drives the file preview the user has open beside the
// chat: scroll, select, or find. It validates the call and emits a
// directive; the web UI applies it. It does nothing in the TUI.
type ViewerControlTool struct{}

func (t *ViewerControlTool) Name() string { return "viewer_control" }

func (t *ViewerControlTool) Description() string {
	return "Drive the file preview the user has open beside this chat (web/desktop only): scroll to a page, slide, line or heading, or by one screen; select text, a line range or a sheet range; open the preview's find bar with a query and step to the next/previous match. The web UI applies it; call viewer_state afterwards to confirm the outcome (it reports the last control's status and the current view). Does nothing in the TUI."
}

// Definition returns the canonical FLAT tool descriptor, same shape as
// PreviewOpenTool. The schema is static: no live state in it, so the tools
// array stays byte-stable for the prompt cache.
func (t *ViewerControlTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        t.Name(),
		"description": t.Description(),
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"scroll", "select", "find", "find_next", "find_prev", "find_close"},
					"description": "What to do in the preview.",
				},
				"page": map[string]interface{}{
					"type":        "integer",
					"description": "1-based page to scroll to (PDF). Use with scroll.",
				},
				"slide": map[string]interface{}{
					"type":        "integer",
					"description": "1-based slide to scroll to (PowerPoint). Use with scroll.",
				},
				"line": map[string]interface{}{
					"type":        "integer",
					"description": "1-based line to scroll to (text/code). Use with scroll.",
				},
				"heading": map[string]interface{}{
					"type":        "string",
					"description": "Heading text to scroll to (markdown/Word). Case-insensitive substring. Use with scroll.",
				},
				"direction": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"up", "down"},
					"description": "Scroll by one screen in this direction. Use with scroll.",
				},
				"text": map[string]interface{}{
					"type":        "string",
					"description": "Select the first occurrence of this text from the top. Use with select.",
				},
				"lines": map[string]interface{}{
					"type":        "object",
					"description": "1-based inclusive line range to select (text/code). Use with select.",
					"properties": map[string]interface{}{
						"start": map[string]interface{}{"type": "integer"},
						"end":   map[string]interface{}{"type": "integer"},
					},
					"required": []string{"start", "end"},
				},
				"range": map[string]interface{}{
					"type":        "string",
					"description": "A1 range to select in a spreadsheet, optionally prefixed with the sheet name (e.g. Sheet1!A1:C10). Use with select.",
				},
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Text to search for in the preview's find bar. Use with find.",
				},
			},
			"required": []string{"action"},
		},
	}
}

// Parallel is false: a control changes the one preview the user is looking
// at, so two in one turn would race each other.
func (t *ViewerControlTool) Parallel() bool { return false }

func (t *ViewerControlTool) Execute(args json.RawMessage) (string, error) {
	var a viewerControlArgs
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&a); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if err := a.validate(); err != nil {
		return "", err
	}
	id, err := newViewerControlID()
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(viewerControlDirective{ID: id, Action: a.Action, viewerControlParams: a.viewerControlParams})
	if err != nil {
		return "", fmt.Errorf("viewer_control: encode directive: %w", err)
	}
	return ViewerControlSentinel + string(raw), nil
}

func (a viewerControlArgs) validate() error {
	if a.Action == "" {
		return fmt.Errorf("viewer_control: action is required")
	}
	oneOf, ok := viewerControlOneOf[a.Action]
	if !ok {
		return fmt.Errorf("viewer_control: action must be one of scroll, select, find, find_next, find_prev, find_close (got %q)", a.Action)
	}

	allowed := make(map[string]bool, len(oneOf))
	for _, name := range oneOf {
		allowed[name] = true
	}
	var set []string
	for _, name := range viewerControlFields {
		if a.provided(name) {
			set = append(set, name)
		}
	}
	for _, name := range set {
		if !allowed[name] {
			return fmt.Errorf("viewer_control: %s does not take %s", a.Action, name)
		}
	}

	switch {
	case len(oneOf) == 0 && len(set) != 0:
		return fmt.Errorf("viewer_control: %s takes no parameters", a.Action)
	case len(oneOf) > 0 && len(set) != 1:
		return fmt.Errorf("viewer_control: %s needs exactly one of %s", a.Action, strings.Join(oneOf, ", "))
	}

	if err := a.checkValues(); err != nil {
		return err
	}
	return nil
}

// provided reports whether a named parameter was present in the call.
func (a viewerControlArgs) provided(name string) bool {
	switch name {
	case "page":
		return a.Page != nil
	case "slide":
		return a.Slide != nil
	case "line":
		return a.Line != nil
	case "heading":
		return a.Heading != nil
	case "direction":
		return a.Direction != nil
	case "text":
		return a.Text != nil
	case "lines":
		return a.Lines != nil
	case "range":
		return a.Range != nil
	case "query":
		return a.Query != nil
	}
	return false
}

// checkValues validates the value of each parameter that was provided.
func (a viewerControlArgs) checkValues() error {
	for _, v := range []struct {
		name string
		n    *int
	}{{"page", a.Page}, {"slide", a.Slide}, {"line", a.Line}} {
		if v.n != nil && *v.n < 1 {
			return fmt.Errorf("viewer_control: %s must be >= 1 (got %d)", v.name, *v.n)
		}
	}
	for _, v := range []struct {
		name string
		s    *string
	}{{"heading", a.Heading}, {"text", a.Text}, {"range", a.Range}, {"query", a.Query}} {
		if v.s != nil && strings.TrimSpace(*v.s) == "" {
			return fmt.Errorf("viewer_control: %s must be non-empty", v.name)
		}
	}
	if a.Direction != nil && *a.Direction != "up" && *a.Direction != "down" {
		return fmt.Errorf("viewer_control: direction must be up or down (got %q)", *a.Direction)
	}
	if a.Lines != nil {
		if a.Lines.Start == nil || a.Lines.End == nil {
			return fmt.Errorf("viewer_control: lines needs both start and end")
		}
		start, end := *a.Lines.Start, *a.Lines.End
		if start < 1 || end < 1 {
			return fmt.Errorf("viewer_control: lines start and end must be >= 1 (got %d..%d)", start, end)
		}
		if start > end {
			return fmt.Errorf("viewer_control: lines start must be <= end (got %d..%d)", start, end)
		}
	}
	return nil
}

// newViewerControlID returns "vc_" plus 8 hex characters from crypto/rand.
func newViewerControlID() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("viewer_control: generate id: %w", err)
	}
	return "vc_" + hex.EncodeToString(b[:]), nil
}
