package ide

import "encoding/json"

type rawPos struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type rawRange struct {
	Start rawPos `json:"start"`
	End   rawPos `json:"end"`
}

// rawSel covers every selection shape the extension emits: the `selection_changed`
// notification (ranges[] variant or flat selection variant) and the
// getCurrentSelection tool result ({success, filePath, text, selection}).
type rawSel struct {
	Success   *bool     `json:"success"`
	FilePath  string    `json:"filePath"`
	Text      string    `json:"text"`
	Selection *rawRange `json:"selection"`
	Ranges    []struct {
		Text      string   `json:"text"`
		Selection rawRange `json:"selection"`
	} `json:"ranges"`
}

// parseSelectionParams decodes a selection from either notification params or a
// getCurrentSelection tool payload. Returns false when there is no usable
// selection (decode error, success:false, or no file).
func parseSelectionParams(data json.RawMessage) (*Selection, bool) {
	if len(data) == 0 {
		return nil, false
	}
	var r rawSel
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, false
	}
	if r.Success != nil && !*r.Success {
		return nil, false
	}
	if r.FilePath == "" {
		return nil, false
	}
	sel := &Selection{FilePath: r.FilePath}
	switch {
	case len(r.Ranges) > 0:
		for _, rg := range r.Ranges {
			sel.Ranges = append(sel.Ranges, Range{
				StartLine: rg.Selection.Start.Line,
				StartChar: rg.Selection.Start.Character,
				EndLine:   rg.Selection.End.Line,
				EndChar:   rg.Selection.End.Character,
				Text:      rg.Text,
			})
		}
	case r.Selection != nil:
		sel.Ranges = append(sel.Ranges, Range{
			StartLine: r.Selection.Start.Line,
			StartChar: r.Selection.Start.Character,
			EndLine:   r.Selection.End.Line,
			EndChar:   r.Selection.End.Character,
			Text:      r.Text,
		})
	}
	return sel, true
}

// parseMentionParams decodes an at_mentioned notification.
func parseMentionParams(data json.RawMessage) (*Mention, bool) {
	if len(data) == 0 {
		return nil, false
	}
	var r struct {
		FilePath  string `json:"filePath"`
		LineStart int    `json:"lineStart"`
		LineEnd   int    `json:"lineEnd"`
	}
	if err := json.Unmarshal(data, &r); err != nil || r.FilePath == "" {
		return nil, false
	}
	return &Mention{FilePath: r.FilePath, LineStart: r.LineStart, LineEnd: r.LineEnd}, true
}

// parseOpenEditors decodes the getOpenEditors tool payload ({tabs:[...]}).
// Untitled buffers are skipped — only real files are reported.
func parseOpenEditors(data json.RawMessage) ([]Editor, bool) {
	if len(data) == 0 {
		return nil, false
	}
	var r struct {
		Tabs []struct {
			FileName   string `json:"fileName"`
			Label      string `json:"label"`
			IsActive   bool   `json:"isActive"`
			IsDirty    bool   `json:"isDirty"`
			IsUntitled bool   `json:"isUntitled"`
		} `json:"tabs"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, false
	}
	out := make([]Editor, 0, len(r.Tabs))
	for _, t := range r.Tabs {
		if t.IsUntitled || t.FileName == "" {
			continue
		}
		out = append(out, Editor{
			FilePath: t.FileName,
			Label:    t.Label,
			Active:   t.IsActive,
			Dirty:    t.IsDirty,
		})
	}
	return out, true
}

// toolText unwraps an MCP tool result ({content:[{type:"text",text:"<json>"}]})
// and returns the inner JSON document as raw bytes.
func toolText(result json.RawMessage) json.RawMessage {
	if len(result) == 0 {
		return nil
	}
	var r struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(result, &r); err != nil {
		return nil
	}
	for _, c := range r.Content {
		if c.Type == "text" {
			return json.RawMessage(c.Text)
		}
	}
	return nil
}
