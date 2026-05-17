package tool

import (
	"strings"
)

// FormatDiff returns a fenced diff block the TUI can recognize and colorize.
// Prefix "DIFF:" on the first line carries the path; subsequent lines start
// with "-", "+", or " " in classic unified-diff style.
func FormatDiff(path, before, after string) string {
	var b strings.Builder
	b.WriteString("DIFF:")
	b.WriteString(path)
	b.WriteString("\n")

	if before == "" && after != "" {
		b.WriteString("@@ new file @@\n")
		for _, line := range strings.Split(strings.TrimRight(after, "\n"), "\n") {
			b.WriteString("+")
			b.WriteString(line)
			b.WriteString("\n")
		}
		return b.String()
	}

	beforeLines := strings.Split(strings.TrimRight(before, "\n"), "\n")
	afterLines := strings.Split(strings.TrimRight(after, "\n"), "\n")

	// Trim shared prefix/suffix to keep diff focused.
	prefix := 0
	for prefix < len(beforeLines) && prefix < len(afterLines) && beforeLines[prefix] == afterLines[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(beforeLines)-prefix && suffix < len(afterLines)-prefix &&
		beforeLines[len(beforeLines)-1-suffix] == afterLines[len(afterLines)-1-suffix] {
		suffix++
	}

	ctx := 2
	startCtx := prefix - ctx
	if startCtx < 0 {
		startCtx = 0
	}
	endCtxB := len(beforeLines) - suffix + ctx
	if endCtxB > len(beforeLines) {
		endCtxB = len(beforeLines)
	}
	endCtxA := len(afterLines) - suffix + ctx
	if endCtxA > len(afterLines) {
		endCtxA = len(afterLines)
	}

	b.WriteString("@@\n")
	for i := startCtx; i < prefix; i++ {
		b.WriteString(" ")
		b.WriteString(beforeLines[i])
		b.WriteString("\n")
	}
	for i := prefix; i < len(beforeLines)-suffix; i++ {
		b.WriteString("-")
		b.WriteString(beforeLines[i])
		b.WriteString("\n")
	}
	for i := prefix; i < len(afterLines)-suffix; i++ {
		b.WriteString("+")
		b.WriteString(afterLines[i])
		b.WriteString("\n")
	}
	for i := len(beforeLines) - suffix; i < endCtxB; i++ {
		b.WriteString(" ")
		b.WriteString(beforeLines[i])
		b.WriteString("\n")
	}
	return b.String()
}
