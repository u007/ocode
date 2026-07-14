package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"

	"github.com/u007/ocode/internal/agent"
)

const (
	toolOutputPreviewLines = 20
	readToolPreviewLines   = 5
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
			if f, ok := v.(float64); ok {
				return fmt.Sprintf("%g", f)
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
		p := first("path", "file_path", "filePath")
		offset := first("offset", "start_line")
		limit := first("limit")
		if offset != "" && limit != "" {
			return fmt.Sprintf("≫ read %s offset=%s limit=%s", p, offset, limit)
		}
		if offset != "" {
			return fmt.Sprintf("≫ read %s offset=%s", p, offset)
		}
		if limit != "" {
			return fmt.Sprintf("≫ read %s limit=%s", p, limit)
		}
		return fmt.Sprintf("≫ read %s", p)
	case "write":
		return fmt.Sprintf("✏  write %s", first("path", "file_path"))
	case "edit":
		return fmt.Sprintf("✏  edit %s", first("path", "file_path"))
	case "multiedit":
		return fmt.Sprintf("✏  multiedit %s", first("path", "file_path"))
	case "multi_file_edit":
		return "✏  multi_file_edit"
	case "replace_lines":
		p := first("path", "file_path")
		start := str("start_line")
		end := str("end_line")
		return fmt.Sprintf("✏  replace_lines %s:%s-%s", p, start, end)
	case "delete":
		return fmt.Sprintf("∅  delete %s", first("path", "file_path"))
	case "bash":
		cmd := first("command")
		return fmt.Sprintf("$ %s", cmd)
	case "grep":
		return fmt.Sprintf("⌾ grep %q", first("pattern"))
	case "glob":
		return fmt.Sprintf("⌾ glob %s", first("pattern"))
	case "list":
		return fmt.Sprintf("▸ list %s", first("path"))
	case "web":
		return fmt.Sprintf("⊕ web %s", first("url"))
	case "agent", "task":
		p := first("prompt")
		if p != "" {
			lines := strings.Split(p, "\n")
			const maxLines = 5
			if len(lines) > maxLines {
				lines = lines[:maxLines]
				p = strings.Join(lines, "\n") + "\n..."
			} else {
				p = strings.Join(lines, "\n")
			}
		}
		return fmt.Sprintf("@ %s:\n%s", name, p)
	case "advisor":
		p := first("prompt")
		if p != "" {
			p = strings.TrimSpace(p)
		}
		return fmt.Sprintf("◆ advisor:\n%s", p)
	case "question":
		return fmt.Sprintf("❓ %s", first("question", "prompt"))
	case "skill":
		return fmt.Sprintf("→ Skill %q", first("name"))
	case "apply_patch":
		return formatPatchHint(args)
	}
	// Fallback: name + raw args.
	a := strings.TrimSpace(tc.Function.Arguments)
	return fmt.Sprintf("⚙ %s %s", name, a)
}

func makeToolCall(name, argsJSON string) agent.ToolCall {
	tc := agent.ToolCall{}
	tc.Function.Name = name
	tc.Function.Arguments = argsJSON
	return tc
}

// renderThinkingContent replaces <tool_call> XML blocks embedded in thinking
// text (emitted by some models) with formatted hints via formatToolCallHint.
// It handles two common formats:
//
//	<tool_call><function=name><parameter=k>v</parameter></function></tool_call>
//	<tool_call><name>name</name><parameters><k>v</k></parameters></tool_call>
//
// collapseBlankLines reduces runs of 2+ blank lines (3+ consecutive newlines)
// to a single blank line. Reasoning streams from Anthropic and OpenAI commonly
// emit paragraph breaks as `\n\n\n` which visually doubles spacing in the TUI.
func collapseBlankLines(s string) string {
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return s
}

func renderThinkingContent(text string, st Styles) string {
	const open = "<tool_call>"
	const close = "</tool_call>"
	text = collapseBlankLines(text)
	if !strings.Contains(text, open) {
		return text
	}
	var b strings.Builder
	for {
		start := strings.Index(text, open)
		if start < 0 {
			b.WriteString(text)
			break
		}
		b.WriteString(text[:start])
		text = text[start+len(open):]
		end := strings.Index(text, close)
		var block string
		if end < 0 {
			block = text
			text = ""
		} else {
			block = text[:end]
			text = text[end+len(close):]
		}
		hint := parseThinkingToolCall(block)
		b.WriteString(st.Thinking.Copy().Render("  " + hint))
		if end >= 0 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// parseThinkingToolCall parses the inner content of a <tool_call> block and
// returns a formatted hint string.
func parseThinkingToolCall(block string) string {
	block = strings.TrimSpace(block)

	// Format 1: <function=name><parameter=k>v</parameter>...
	if strings.HasPrefix(block, "<function=") {
		end := strings.Index(block, ">")
		if end < 0 {
			return "⚙ " + block
		}
		name := block[len("<function="):end]
		rest := block[end+1:]
		args := map[string]interface{}{}
		for {
			ps := strings.Index(rest, "<parameter=")
			if ps < 0 {
				break
			}
			pe := strings.Index(rest[ps:], ">")
			if pe < 0 {
				break
			}
			key := rest[ps+len("<parameter=") : ps+pe]
			rest = rest[ps+pe+1:]
			closeTag := "</parameter>"
			ce := strings.Index(rest, closeTag)
			var val string
			if ce >= 0 {
				val = rest[:ce]
				rest = rest[ce+len(closeTag):]
			} else {
				val = rest
				rest = ""
			}
			args[key] = strings.TrimSpace(val)
		}
		argsJSON, _ := json.Marshal(args)
		return formatToolCallHint(makeToolCall(name, string(argsJSON)))
	}

	// Format 2: <name>name</name><parameters><k>v</k>...</parameters>
	name := extractXMLTag(block, "name")
	args := map[string]interface{}{}
	params := extractXMLTag(block, "parameters")
	if params != "" {
		rest := params
		for {
			ts := strings.Index(rest, "<")
			if ts < 0 {
				break
			}
			te := strings.Index(rest[ts:], ">")
			if te < 0 {
				break
			}
			key := rest[ts+1 : ts+te]
			if strings.HasPrefix(key, "/") {
				rest = rest[ts+te+1:]
				continue
			}
			rest = rest[ts+te+1:]
			closeTag := "</" + key + ">"
			ce := strings.Index(rest, closeTag)
			var val string
			if ce >= 0 {
				val = rest[:ce]
				rest = rest[ce+len(closeTag):]
			} else {
				val = rest
				rest = ""
			}
			args[key] = strings.TrimSpace(val)
		}
	}
	if name == "" {
		return "⚙ " + strings.TrimSpace(block)
	}
	argsJSON, _ := json.Marshal(args)
	return formatToolCallHint(makeToolCall(name, string(argsJSON)))
}

func extractXMLTag(s, tag string) string {
	open := "<" + tag + ">"
	close := "</" + tag + ">"
	start := strings.Index(s, open)
	if start < 0 {
		return ""
	}
	start += len(open)
	end := strings.Index(s[start:], close)
	if end < 0 {
		return s[start:]
	}
	return s[start : start+end]
}

// renderToolResult formats a tool result for display:
// - DIFF: prefix → colorized unified diff
// - read result → syntax-highlighted code block
// - else → plain text, with truncation footer hidden from rendered display
//
// Sanitization runs first. Tool output is untrusted — it comes from
// subprocesses (bash, git, grep, …) whose authors may emit cursor
// controls, OSC sequences, raw CRs, or other control bytes that would
// overwrite the alt-screen frame the TUI has already rendered. We keep
// SGR color escapes (so chroma and `git diff --color` still work) and
// drop everything else that can move the cursor, change the window
// title, ring the bell, or break column math in the selection walker.
// See sanitizeForTUI for the full policy.
func renderToolResult(toolName, content string, st Styles) string {
	content = sanitizeForTUI(content)
	content = stripTruncationFooter(content)
	if strings.HasPrefix(content, "DIFF:") {
		return renderDiff(content, st)
	}
	if toolName == "read" {
		return renderReadResult(content, st, readToolPreviewLines)
	}
	if looksLikeUnifiedDiff(content) {
		return renderUnifiedDiff(content, st)
	}
	return st.Text.Render(content)
}

func stripTruncationFooter(content string) string {
	marker := "\n\n" + agent.TruncationMarkerPrefix
	idx := strings.Index(content, marker)
	if idx >= 0 {
		return content[:idx]
	}
	if strings.HasPrefix(content, agent.TruncationMarkerPrefix) {
		return ""
	}
	return content
}

func looksLikeUnifiedDiff(content string) bool {
	for _, line := range strings.SplitN(content, "\n", 6) {
		if strings.HasPrefix(line, "diff --git ") || strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") {
			return true
		}
	}
	return false
}

func renderDiff(content string, st Styles) string {
	lines := strings.Split(content, "\n")
	header := ""
	if len(lines) > 0 && strings.HasPrefix(lines[0], "DIFF:") {
		header = strings.TrimPrefix(lines[0], "DIFF:")
		lines = lines[1:]
	}

	var b strings.Builder
	if header != "" {
		b.WriteString(st.Header.Render("⟡ " + header))
		b.WriteString("\n")
	}
	b.WriteString(renderUnifiedDiff(strings.Join(lines, "\n"), st))
	return strings.TrimRight(b.String(), "\n")
}

func renderUnifiedDiff(content string, st Styles) string {
	addStyle := st.Success.Copy().Background(lipgloss.Color("#17361f")).Bold(true)
	delStyle := st.Error.Copy().Background(lipgloss.Color("#3a1717")).Bold(true)
	hunkStyle := lipgloss.NewStyle().Foreground(st.Header.GetForeground()).Background(lipgloss.Color("#1f2430")).Bold(true)
	metaStyle := lipgloss.NewStyle().Foreground(st.Hint.GetForeground()).Background(lipgloss.Color("#141821")).Faint(true)
	fileStyle := lipgloss.NewStyle().Foreground(st.Header.GetForeground()).Background(lipgloss.Color("#1a2233")).Bold(true)

	lines := strings.Split(content, "\n")
	var b strings.Builder
	for i, line := range lines {
		if line == "" {
			if i < len(lines)-1 {
				b.WriteString("\n")
			}
			continue
		}

		switch {
		case strings.HasPrefix(line, "diff --git "):
			b.WriteString(fileStyle.Render(line))
		case strings.HasPrefix(line, "@@"):
			b.WriteString(hunkStyle.Render(line))
		case strings.HasPrefix(line, "+"):
			b.WriteString(addStyle.Render(line))
		case strings.HasPrefix(line, "-"):
			b.WriteString(delStyle.Render(line))
		case strings.HasPrefix(line, "index ") || strings.HasPrefix(line, "new file mode ") || strings.HasPrefix(line, "deleted file mode ") || strings.HasPrefix(line, "similarity index ") || strings.HasPrefix(line, "rename from ") || strings.HasPrefix(line, "rename to "):
			b.WriteString(metaStyle.Render(line))
		default:
			b.WriteString(st.Text.Render(line))
		}
		if i < len(lines)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// renderReadResult strips the "1\t" / "  12\t" line-number prefix the read
// tool prepends, detects the language from the first path-looking line if
// present, and applies chroma highlighting. previewLines controls the inline
// truncation applied to read-tool output; pass 0 to render the full result.
func renderReadResult(content string, st Styles, previewLines int) string {
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

	// Extract the continuation footer from the read tool (e.g. "…(use start_line=51, limit=50 to continue)")
	// before stripping line numbers, since it has no \t prefix.
	continuation := ""
	bodyLines := strings.Split(body, "\n")
	if len(bodyLines) > 0 {
		last := bodyLines[len(bodyLines)-1]
		if strings.HasPrefix(last, "…(") && strings.Contains(last, "start_line=") {
			continuation = last
			bodyLines = bodyLines[:len(bodyLines)-1]
			body = strings.Join(bodyLines, "\n")
		}
	}

	// Strip "<n>\t" prefix from each line if present.
	stripped := stripLineNumbers(body)

	lines := strings.Split(stripped, "\n")
	previewTruncated := ""
	if previewLines > 0 && len(lines) > previewLines {
		previewTruncated = fmt.Sprintf("\n…(%d more lines)", len(lines)-previewLines)
		stripped = strings.Join(lines[:previewLines], "\n")
	}

	highlighted := highlightCode(stripped, path)

	var parts []string
	if path != "" {
		parts = append(parts, lipgloss.NewStyle().Faint(true).Render("⟡ "+path))
	}
	parts = append(parts, highlighted)
	if continuation != "" {
		parts = append(parts, lipgloss.NewStyle().Faint(true).Render(continuation))
	} else if previewTruncated != "" {
		parts = append(parts, previewTruncated)
	}
	return strings.Join(parts, "\n")
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
