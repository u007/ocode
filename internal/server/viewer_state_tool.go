package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/u007/ocode/internal/tool"
)

const viewerStateDescription = "Read what the user currently sees in the file preview open beside this chat (web/desktop only): file, kind, page/slide/sheet range/heading/visible lines, the text they selected, the text visible on screen, the preview find state, and the outcome of the last viewer_control call. Returns 'no preview open' when nothing is attached."

// viewerStateTool exposes the file-preview record the SPA pushed for one
// session. It is server-only: the record lives in the Handler, not the tool
// package, so it is registered by buildAgentSession, never by the builtins.
type viewerStateTool struct {
	h         *Handler
	sessionID string
}

// viewerStateRecord mirrors the JSON the SPA pushes. Unknown fields are ignored.
type viewerStateRecord struct {
	Path        string           `json:"path"`
	Kind        string           `json:"kind"`
	Location    *viewerLocation  `json:"location"`
	Selection   *string          `json:"selection"`
	VisibleText *string          `json:"visibleText"`
	Find        *viewerFind      `json:"find"`
	Control     *viewerControlOp `json:"control"`
}

type viewerLocation struct {
	Page         int             `json:"page"`
	PageCount    int             `json:"pageCount"`
	Slide        int             `json:"slide"`
	SlideCount   int             `json:"slideCount"`
	Sheet        string          `json:"sheet"`
	Range        string          `json:"range"`
	Heading      string          `json:"heading"`
	Lines        *viewerLineSpan `json:"lines"`
	VisibleLines *viewerLineSpan `json:"visibleLines"`
}

type viewerLineSpan struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type viewerFind struct {
	Query   string `json:"query"`
	Count   int    `json:"count"`
	Current int    `json:"current"`
}

type viewerControlOp struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  string `json:"error"`
}

func (v *viewerStateTool) Name() string   { return "viewer_state" }
func (v *viewerStateTool) Parallel() bool { return true }

func (v *viewerStateTool) Description() string { return viewerStateDescription }

func (v *viewerStateTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "viewer_state",
		"description": viewerStateDescription,
		"parameters": map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}
}

// Execute reads the snapshot and formats it. No preview is a normal result, not
// an error: the agent is told the state directly. A record that does not parse
// into the expected shapes is an error, because the SPA sent something the tool
// cannot read.
func (v *viewerStateTool) Execute(_ json.RawMessage) (string, error) {
	snap, ok := v.h.viewerState(v.sessionID)
	if !ok {
		return "no preview open for this session", nil
	}
	var rec viewerStateRecord
	if err := json.Unmarshal(snap.Raw, &rec); err != nil {
		return "", fmt.Errorf("viewer state: record does not match the expected shape: %w", err)
	}
	return formatViewerState(rec, time.Since(snap.ReceivedAt)), nil
}

// formatViewerState renders the record as plain lines. Zero numbers and empty
// strings count as absent, so a field the SPA leaves out prints nothing.
func formatViewerState(rec viewerStateRecord, age time.Duration) string {
	var lines []string

	head := "Preview: " + rec.Path
	if rec.Kind != "" {
		head += " (" + rec.Kind + ")"
	}
	lines = append(lines, head)

	if loc := viewerLocationText(rec.Location); loc != "" {
		lines = append(lines, "Location: "+loc)
	}
	if rec.Selection != nil && *rec.Selection != "" {
		lines = append(lines, fmt.Sprintf("Selection: %q", *rec.Selection))
	}
	if rec.VisibleText != nil && *rec.VisibleText != "" {
		lines = append(lines, "Visible text:", strings.TrimRight(*rec.VisibleText, "\n"))
	}
	if rec.Find != nil {
		lines = append(lines, fmt.Sprintf("Find: %q %d matches, current %d", rec.Find.Query, rec.Find.Count, rec.Find.Current))
	}
	if rec.Control != nil {
		ctl := fmt.Sprintf("Last control: %s %s", rec.Control.ID, rec.Control.Status)
		if rec.Control.Error != "" {
			ctl += " (" + rec.Control.Error + ")"
		}
		lines = append(lines, ctl)
	}
	lines = append(lines, fmt.Sprintf("Updated: %ds ago", int(age.Seconds())))
	return strings.Join(lines, "\n")
}

// viewerLocationText joins the location parts that are present, in reading
// order: page, slide, sheet range, heading, selected lines, visible lines.
func viewerLocationText(loc *viewerLocation) string {
	if loc == nil {
		return ""
	}
	var parts []string
	if loc.Page > 0 {
		parts = append(parts, viewerOfText("p.", loc.Page, loc.PageCount))
	}
	if loc.Slide > 0 {
		parts = append(parts, viewerOfText("slide ", loc.Slide, loc.SlideCount))
	}
	switch {
	case loc.Sheet != "" && loc.Range != "":
		parts = append(parts, loc.Sheet+"!"+loc.Range)
	case loc.Sheet != "":
		parts = append(parts, loc.Sheet)
	case loc.Range != "":
		parts = append(parts, loc.Range)
	}
	if loc.Heading != "" {
		parts = append(parts, "§ "+loc.Heading)
	}
	if loc.Lines != nil {
		parts = append(parts, fmt.Sprintf("L%d-L%d", loc.Lines.Start, loc.Lines.End))
	}
	if loc.VisibleLines != nil {
		parts = append(parts, fmt.Sprintf("view L%d-L%d", loc.VisibleLines.Start, loc.VisibleLines.End))
	}
	return strings.Join(parts, ", ")
}

// viewerOfText renders "p.3/12" or "slide 2/8", with the total only when known.
func viewerOfText(prefix string, n, total int) string {
	if total > 0 {
		return fmt.Sprintf("%s%d/%d", prefix, n, total)
	}
	return fmt.Sprintf("%s%d", prefix, n)
}

// Compile-time check that the tool satisfies the agent's tool interface.
var _ tool.Tool = (*viewerStateTool)(nil)
