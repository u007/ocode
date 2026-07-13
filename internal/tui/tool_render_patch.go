package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/u007/ocode/internal/agent"
)

// patchOpView is a display-oriented parse of a single apply_patch operation.
type patchOpView struct {
	typ    string // "add", "update", "delete"
	path   string
	moveTo string
	hunks  []patchHunkView
}

// patchHunkView captures one @@ hunk of an update operation. Lines are stored
// in their original source order so the rendered diff interleaves context,
// removed, and added lines exactly as the patch specifies (grouping by type
// would misrepresent the change shown in the permission-approval dialog).
type patchHunkView struct {
	ctx   string
	lines []patchLine
}

// patchLine is a single hunk line tagged with its diff kind.
// kind: 0 = unchanged context, 1 = removed, 2 = added.
type patchLine struct {
	kind int
	text string
}

// parsePatchView parses an apply_patch patchText into display operations
// without depending on the tool package's internal parser. It is tolerant:
// if it cannot make sense of the text it returns nil and callers fall back
// to showing the raw arguments.
func parsePatchView(patchText string) []patchOpView {
	text := strings.TrimSpace(patchText)
	lines := strings.Split(text, "\n")

	begin, end := -1, -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "*** Begin Patch" {
			begin = i
		} else if t == "*** End Patch" {
			end = i
			break
		}
	}
	if begin < 0 || end < 0 || begin >= end {
		return nil
	}

	var ops []patchOpView
	i := begin + 1
	for i < end {
		l := lines[i]
		switch {
		case strings.HasPrefix(l, "*** Add File:"):
			p := strings.TrimSpace(strings.TrimPrefix(l, "*** Add File:"))
			var content []string
			j := i + 1
			for j < end && !strings.HasPrefix(lines[j], "***") {
				if strings.HasPrefix(lines[j], "+") {
					content = append(content, lines[j][1:])
				}
				j++
			}
			op := patchOpView{typ: "add", path: p}
			if len(content) > 0 {
				lines := make([]patchLine, len(content))
				for i, c := range content {
					lines[i] = patchLine{kind: 2, text: c}
				}
				op.hunks = []patchHunkView{{lines: lines}}
			}
			ops = append(ops, op)
			i = j

		case strings.HasPrefix(l, "*** Update File:"):
			p := strings.TrimSpace(strings.TrimPrefix(l, "*** Update File:"))
			moveTo := ""
			j := i + 1
			if j < end && strings.HasPrefix(lines[j], "*** Move to:") {
				moveTo = strings.TrimSpace(strings.TrimPrefix(lines[j], "*** Move to:"))
				j++
			}
			op := patchOpView{typ: "update", path: p, moveTo: moveTo}
			for j < end && !strings.HasPrefix(lines[j], "***") {
				if !strings.HasPrefix(lines[j], "@@") {
					j++
					continue
				}
				ctx := strings.TrimSpace(lines[j][2:])
				j++
				hk := patchHunkView{ctx: ctx}
				for j < end && !strings.HasPrefix(lines[j], "@@") && !strings.HasPrefix(lines[j], "***") {
					ln := lines[j]
					if ln == "*** End of File" {
						j++
						break
					}
					switch {
					case strings.HasPrefix(ln, " "):
						hk.lines = append(hk.lines, patchLine{kind: 0, text: ln[1:]})
					case strings.HasPrefix(ln, "-"):
						hk.lines = append(hk.lines, patchLine{kind: 1, text: ln[1:]})
					case strings.HasPrefix(ln, "+"):
						hk.lines = append(hk.lines, patchLine{kind: 2, text: ln[1:]})
					}
					j++
				}
				op.hunks = append(op.hunks, hk)
			}
			ops = append(ops, op)
			i = j

		case strings.HasPrefix(l, "*** Delete File:"):
			p := strings.TrimSpace(strings.TrimPrefix(l, "*** Delete File:"))
			ops = append(ops, patchOpView{typ: "delete", path: p})
			i++

		default:
			i++
		}
	}
	return ops
}

// formatPatchHint returns a single-line summary of an apply_patch call, e.g.
// "✏  patch apps/web/foo.tsx" or "✏  apply_patch 3 files".
func formatPatchHint(args map[string]interface{}) string {
	patchText, _ := args["patchText"].(string)
	ops := parsePatchView(patchText)
	if len(ops) == 0 {
		return "✏  apply_patch"
	}
	if len(ops) == 1 {
		op := ops[0]
		verb := "patch"
		switch op.typ {
		case "add":
			verb = "create"
		case "delete":
			verb = "delete"
		case "update":
			verb = "patch"
		}
		path := op.path
		if op.typ == "update" && op.moveTo != "" {
			path = op.path + " -> " + op.moveTo
		}
		return fmt.Sprintf("✏  %s %s", verb, path)
	}
	return fmt.Sprintf("✏  apply_patch %d files", len(ops))
}

// renderPatchRequest renders an apply_patch tool request as a readable,
// colorized diff instead of raw JSON. It reuses renderUnifiedDiff so the
// coloring matches the rest of the TUI (green additions, red deletions).
func renderPatchRequest(tc agent.ToolCall, st Styles) string {
	var args map[string]interface{}
	_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
	patchText, _ := args["patchText"].(string)
	ops := parsePatchView(patchText)
	if len(ops) == 0 {
		return formatToolCallHint(tc)
	}

	noun := "file"
	if len(ops) > 1 {
		noun = "files"
	}
	var b strings.Builder
	b.WriteString(st.Header.Render(fmt.Sprintf("✏ apply_patch · %d %s", len(ops), noun)))
	b.WriteString("\n")
	for idx, op := range ops {
		if idx > 0 {
			b.WriteString("\n")
		}
		b.WriteString(renderPatchOp(op, st))
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderPatchOp(op patchOpView, st Styles) string {
	var b strings.Builder

	label := "Update"
	switch op.typ {
	case "add":
		label = "Add"
	case "update":
		label = "Update"
	case "delete":
		label = "Delete"
	}
	path := op.path
	if op.typ == "update" && op.moveTo != "" {
		path = op.path + " -> " + op.moveTo
	}
	b.WriteString(hintStyle.Render(fmt.Sprintf("%s: %s", label, path)))
	b.WriteString("\n")

	var diff strings.Builder
	switch op.typ {
	case "add":
		diff.WriteString("--- /dev/null\n")
		diff.WriteString("+++ b/" + op.path + "\n")
		for _, hk := range op.hunks {
			for _, l := range hk.lines {
				diff.WriteString("+" + l.text + "\n")
			}
		}
	case "delete":
		diff.WriteString("--- a/" + op.path + "\n")
		diff.WriteString("+++ /dev/null\n")
	case "update":
		diff.WriteString("--- a/" + op.path + "\n")
		if op.moveTo != "" {
			diff.WriteString("+++ b/" + op.moveTo + "\n")
		} else {
			diff.WriteString("+++ b/" + op.path + "\n")
		}
		for _, hk := range op.hunks {
			if hk.ctx != "" {
				diff.WriteString("@@ " + hk.ctx + " @@\n")
			}
			for _, l := range hk.lines {
				switch l.kind {
				case 0:
					diff.WriteString(" " + l.text + "\n")
				case 1:
					diff.WriteString("-" + l.text + "\n")
				case 2:
					diff.WriteString("+" + l.text + "\n")
				}
			}
		}
	}

	colored := renderUnifiedDiff(strings.TrimRight(diff.String(), "\n"), st)
	b.WriteString(colored)
	return b.String()
}
