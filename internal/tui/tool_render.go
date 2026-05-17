package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"charm.land/lipgloss/v2"

	"github.com/jamesmercstudio/ocode/internal/agent"
)

// formatToolCallHint returns a single-line summary of a tool call,
// pulling the most informative argument (path, command, pattern) into the line.
func formatToolCallHint(tc agent.ToolCall) string {
	name := tc.Function.Name
	var args map[string]interface{}
	_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)

	str := func(k string) string {
		if v, ok := args[k]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}
	first := func(keys ...string) string {
		for _, k := range keys {
			if v := str(k); v != "" {
				return v
			}
		}
		return ""
	}

	switch name {
	case "read":
		return fmt.Sprintf("📖 read %s", first("path", "file_path"))
	case "write":
		return fmt.Sprintf("✏  write %s", first("path", "file_path"))
	case "edit":
		return fmt.Sprintf("✏  edit %s", first("path", "file_path"))
	case "multiedit":
		return "✏  multiedit"
	case "replace_lines":
		p := first("path", "file_path")
		start := str("start_line")
		end := str("end_line")
		return fmt.Sprintf("✏  replace_lines %s:%s-%s", p, start, end)
	case "delete":
		return fmt.Sprintf("🗑  delete %s", first("path", "file_path"))
	case "bash":
		cmd := first("command")
		if len(cmd) > 80 {
			cmd = cmd[:77] + "..."
		}
		return fmt.Sprintf("$ %s", cmd)
	case "grep":
		return fmt.Sprintf("🔎 grep %q", first("pattern"))
	case "glob":
		return fmt.Sprintf("🔎 glob %s", first("pattern"))
	case "list":
		return fmt.Sprintf("📁 list %s", first("path"))
	case "web":
		return fmt.Sprintf("🌐 web %s", first("url"))
	case "agent", "task":
		p := first("prompt")
		if len(p) > 80 {
			p = p[:77] + "..."
		}
		return fmt.Sprintf("🤖 %s: %s", name, p)
	case "question":
		return fmt.Sprintf("❓ %s", first("question", "prompt"))
	}
	// Fallback: name + raw args truncated.
	a := strings.TrimSpace(tc.Function.Arguments)
	if len(a) > 80 {
		a = a[:77] + "..."
	}
	return fmt.Sprintf("🔧 %s %s", name, a)
}

// renderToolResult formats a tool result for display:
// - DIFF: prefix → colorized unified diff
// - read result → syntax-highlighted code block
// - else → plain text, truncated if huge
func renderToolResult(toolName, content string, st Styles) string {
	if strings.HasPrefix(content, "DIFF:") {
		return renderDiff(content, st)
	}
	if toolName == "read" {
		return renderReadResult(content, st)
	}
	if len(content) > 4000 {
		content = content[:4000] + "\n…(truncated)"
	}
	return st.Text.Render(content)
}

func renderDiff(content string, st Styles) string {
	lines := strings.Split(content, "\n")
	header := ""
	if strings.HasPrefix(lines[0], "DIFF:") {
		header = strings.TrimPrefix(lines[0], "DIFF:")
		lines = lines[1:]
	}
	addStyle := st.Success
	delStyle := st.Error
	metaStyle := lipgloss.NewStyle().Faint(true)

	var b strings.Builder
	if header != "" {
		b.WriteString(metaStyle.Render("⟡ " + header))
		b.WriteString("\n")
	}
	for _, line := range lines {
		if line == "" {
			b.WriteString("\n")
			continue
		}
		switch line[0] {
		case '+':
			b.WriteString(addStyle.Render(line))
		case '-':
			b.WriteString(delStyle.Render(line))
		case '@':
			b.WriteString(metaStyle.Render(line))
		default:
			b.WriteString(st.Text.Render(line))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderReadResult strips the "1\t" / "  12\t" line-number prefix the read
// tool prepends, detects the language from the first path-looking line if
// present, and applies chroma highlighting.
func renderReadResult(content string, st Styles) string {
	// The read tool returns "<path>\n<numbered lines>" — first line is the
	// path header. We syntax-highlight the body.
	path := ""
	body := content
	if nl := strings.IndexByte(content, '\n'); nl > 0 {
		first := content[:nl]
		if !strings.Contains(first, "\t") && (strings.Contains(first, "/") || strings.Contains(first, ".")) {
			path = first
			body = content[nl+1:]
		}
	}

	// Strip "<n>\t" prefix from each line if present.
	stripped := stripLineNumbers(body)

	// Show 5 lines preview; the tool already limits content via start_line/end_line.
	const previewLines = 5
	lines := strings.Split(stripped, "\n")
	truncated := ""
	if len(lines) > previewLines {
		truncated = fmt.Sprintf("\n…(%d more lines)", len(lines)-previewLines)
		stripped = strings.Join(lines[:previewLines], "\n")
	}

	highlighted := highlightCode(stripped, path)
	if path != "" {
		header := lipgloss.NewStyle().Faint(true).Render("⟡ " + path)
		return header + "\n" + highlighted + truncated
	}
	return highlighted + truncated
}

func stripLineNumbers(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	any := false
	for _, line := range lines {
		if tab := strings.IndexByte(line, '\t'); tab > 0 && tab < 8 {
			prefix := line[:tab]
			allDigits := true
			for _, r := range strings.TrimSpace(prefix) {
				if r < '0' || r > '9' {
					allDigits = false
					break
				}
			}
			if allDigits {
				out = append(out, line[tab+1:])
				any = true
				continue
			}
		}
		out = append(out, line)
	}
	if !any {
		return s
	}
	return strings.Join(out, "\n")
}

func highlightCode(code, filename string) string {
	lexer := lexers.Match(filepath.Base(filename))
	if lexer == nil {
		lexer = lexers.Analyse(code)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	style := styles.Get("monokai")
	if style == nil {
		style = styles.Fallback
	}
	formatter := formatters.Get("terminal16m")
	if formatter == nil {
		formatter = formatters.Fallback
	}
	iterator, err := lexer.Tokenise(nil, code)
	if err != nil {
		return code
	}
	var buf bytes.Buffer
	if err := formatter.Format(&buf, style, iterator); err != nil {
		return code
	}
	return buf.String()
}
