package contextbudget

import (
	"fmt"
	"strings"
)

// RenderText renders the report in the TUI's terminal layout. The web SPA
// renders the same Report as markdown, so both surfaces show the same sections
// and numbers.
func (r Report) RenderText() string {
	var b strings.Builder
	b.WriteString("Context Budget\n")
	b.WriteString(strings.Repeat("═", 38) + "\n")
	for _, sec := range r.Sections {
		if sec.Title != "" {
			b.WriteString("\n" + sec.Title + "\n")
		}
		if sec.Note != "" {
			b.WriteString(sec.Note + "\n")
		}
		for _, row := range sec.Rows {
			if row.Subhead {
				b.WriteString("\n  " + row.Label + "\n")
				continue
			}
			if row.Value != "" {
				fmt.Fprintf(&b, "  %-28s %s\n", row.Label, row.Value)
			} else {
				b.WriteString("  " + row.Label + "\n")
			}
			for _, line := range row.Lines {
				if row.Raw {
					b.WriteString("  │ " + line + "\n")
				} else {
					b.WriteString("    " + line + "\n")
				}
			}
		}
	}
	for _, note := range r.Notes {
		b.WriteString("\nNote: " + note + "\n")
	}
	return b.String()
}
