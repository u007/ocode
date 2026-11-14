package agent

import (
	"path/filepath"
	"strings"
)

// heredocOp is one heredoc operator found on a line. start and end are byte
// offsets of the operator text (for example `<<'EOF'`) within that line.
type heredocOp struct {
	start, end int
	delim      string
	quoted     bool
	stripTabs  bool
}

// heredocOpsOnLine returns the heredoc operators on one line. ok is false when
// the line holds a `<<` that cannot be classified exactly: a here-string, an
// operator inside quotes or arithmetic, or a delimiter word that runs on past
// its name. The caller then keeps the rest of the command verbatim, because it
// can no longer tell which later lines are heredoc bodies.
func heredocOpsOnLine(line string) (ops []heredocOp, ok bool) {
	if !strings.Contains(line, "<<") {
		return nil, true
	}
	if strings.Contains(line, "<<<") {
		return nil, false
	}
	matches := heredocOpRe.FindAllStringSubmatchIndex(line, -1)
	if len(matches) != strings.Count(line, "<<") {
		return nil, false
	}
	for _, m := range matches {
		op := line[m[0]:m[1]]
		quote := line[m[2]:m[3]]
		name := line[m[4]:m[5]]
		if quote != "" {
			if !strings.HasSuffix(op, quote+name+quote) {
				return nil, false
			}
		} else if !strings.HasSuffix(op, name) {
			return nil, false
		}
		if m[1] < len(line) && !strings.ContainsRune(" \t;&|<>()", rune(line[m[1]])) {
			return nil, false
		}
		ops = append(ops, heredocOp{
			start:     m[0],
			end:       m[1],
			delim:     name,
			quoted:    quote != "",
			stripTabs: strings.HasPrefix(op, "<<-"),
		})
	}
	return ops, true
}

// findHeredocEnd returns the index of the first line at or after from that
// closes op, or -1 when the heredoc is unterminated. It matches the shell's
// rule: an exact delimiter line, with leading tabs ignored for `<<-`.
func findHeredocEnd(lines []string, from int, op heredocOp) int {
	for j := from; j < len(lines); j++ {
		cmp := lines[j]
		if op.stripTabs {
			cmp = strings.TrimLeft(cmp, "\t")
		}
		if cmp == op.delim {
			return j
		}
	}
	return -1
}

// heredocScriptInterpreters are commands that run a file or a string as code.
// A heredoc body written to one of them (or to any file a later command passes
// to one) is only data until that command runs, so the body must stay visible.
var heredocScriptInterpreters = map[string]bool{
	"bash": true, "sh": true, "zsh": true, "dash": true, "ksh": true, "fish": true,
	"python": true, "python3": true, "node": true, "nodejs": true,
	"perl": true, "ruby": true, "xargs": true, "source": true, "eval": true, ".": true,
}

// heredocWrittenFileNotExecuted reports whether the lines after a heredoc's
// closing delimiter stay clear of the file the operator line writes. A body
// that is written and then run (`cat > /tmp/x.sh <<'EOF'` … `bash /tmp/x.sh`)
// is not inert even though the operator line only feeds cat, because the later
// command executes the body. Every ambiguous case returns false: hiding a body
// that might run is worse than reading a data body as a command.
//
// shortcut: only an exact appearance of the written target is checked, so
// `cd /tmp && ./x.sh` after writing `/tmp/x.sh` is not caught; upgrade by
// comparing basenames if that shape appears in practice.
func heredocWrittenFileNotExecuted(line string, op heredocOp, after []string) bool {
	if len(after) == 0 {
		return true
	}
	for _, l := range after {
		// An expansion or a substitution on a later line can build a path or a
		// command this check cannot read.
		if strings.Contains(l, "$(") || strings.Contains(l, "`") {
			return false
		}
	}
	targets, ok := heredocRedirectTargets(line[:op.start] + " " + line[op.end:])
	if !ok {
		return false
	}
	parsed, err := parseShellCommandLine(strings.Join(after, "\n"))
	if err != nil {
		return false
	}
	for _, cmd := range parsed {
		for _, words := range effectiveCommandWords(cmd.cmdWords) {
			if len(words) > 0 && heredocScriptInterpreters[filepath.Base(words[0])] {
				return false
			}
		}
	}
	text := strings.Join(after, "\n")
	for _, target := range targets {
		if strings.Contains(text, target) {
			return false
		}
	}
	return true
}

// heredocRedirectTargets returns the files the given operator line (with the
// heredoc operator removed) redirects into. ok is false when the redirections
// cannot be read exactly, in which case the caller keeps the body.
func heredocRedirectTargets(line string) (targets []string, ok bool) {
	fields := splitShellFields(line)
	for i := 0; i < len(fields); i++ {
		switch f := fields[i]; {
		case f == ">" || f == ">>" || f == ">|":
			if i+1 >= len(fields) {
				return nil, false
			}
			targets = append(targets, fields[i+1])
			i++
		case strings.HasPrefix(f, ">>"):
			targets = append(targets, strings.TrimPrefix(f, ">>"))
		case strings.HasPrefix(f, ">|"):
			targets = append(targets, strings.TrimPrefix(f, ">|"))
		case strings.HasPrefix(f, ">"):
			targets = append(targets, strings.TrimPrefix(f, ">"))
		case strings.HasSuffix(f, ">"):
			// A file-descriptor or combined redirect such as `2>` or `&>` takes
			// its target from the next field, so the field-to-target mapping is
			// no longer exact.
			return nil, false
		}
	}
	for _, target := range targets {
		if target == "" || strings.ContainsAny(target, "$`") {
			return nil, false
		}
	}
	return targets, true
}

// heredocInert reports whether the body of op on line is pure data: it is
// never executed or sent anywhere. prior holds the output lines before line,
// after the lines following the closing delimiter. Every condition fails
// toward keeping the body visible.
//
// The write-then-execute case is why after exists: `cat > /tmp/x.sh <<'EOF'`
// writes a file whose body is only data at that moment, but a later `bash
// /tmp/x.sh` runs it. Stripping the body then hides a real command from the
// egress and permission guards, so heredocWrittenFileNotExecuted keeps it.
func heredocInert(prior []string, line string, op heredocOp, body, after []string) bool {
	if !heredocWrittenFileNotExecuted(line, op, after) {
		return false
	}
	if strings.ContainsAny(line, "|`") || strings.Contains(line, "<(") ||
		strings.Contains(line, ">(") || strings.Contains(line, "$(") {
		return false
	}
	// The heredoc belongs to the command the operator sits in. The check below
	// reads the LAST command on the line, so nothing may start a new command
	// after the operator: `bash <<'EOF'; cat` feeds the body to bash, not cat.
	if strings.ContainsAny(line[op.end:], ";&") {
		return false
	}
	// A backslash continuation can join this line to a neighbour, which moves
	// the point where the body starts.
	if strings.HasSuffix(line, `\`) || (op.start > 0 && line[op.start-1] == '\\') {
		return false
	}
	if n := len(prior); n > 0 && strings.HasSuffix(prior[n-1], `\`) {
		return false
	}
	before := line[:op.start]
	// A `#` before the operator may start a comment, so the operator is not
	// a heredoc at all and the following lines are real commands.
	if strings.Contains(before, "#") {
		return false
	}
	// The operator must sit outside quotes, both across the earlier output and
	// on this line. The parser is lenient at end of input, so an open quote is
	// checked explicitly.
	head := strings.Join(prior, "\n") + "\n" + before
	if strings.Contains(head, "$(") || strings.Contains(head, "`") ||
		strings.Contains(head, "<(") || strings.Contains(head, ">(") {
		return false
	}
	if !shellQuotesClosed(head) {
		return false
	}
	removed := before + " " + line[op.end:]
	if !shellQuotesClosed(removed) {
		return false
	}
	parsed, err := parseShellCommandLine(removed)
	if err != nil || len(parsed) == 0 {
		return false
	}
	eff := effectiveCommandWords(parsed[len(parsed)-1].cmdWords)
	if len(eff) != 1 || len(eff[0]) == 0 {
		return false
	}
	switch filepath.Base(eff[0][0]) {
	case "cat", "tee":
	default:
		return false
	}
	// An unquoted delimiter expands $ and backticks in the body, and `$(...)`
	// runs commands. A quoted delimiter expands nothing.
	if !op.quoted {
		for _, l := range body {
			if strings.ContainsAny(l, "$`") {
				return false
			}
		}
	}
	return true
}

// stripInertHeredocBodies returns command with the body and closing delimiter
// line of every inert heredoc removed. An inert heredoc feeds a `cat` or `tee`
// (see heredocInert), so its body is file content and not a command. Every
// other byte is kept, including commands after the closing delimiter. A
// command with no inert heredoc comes back unchanged.
//
// A body is kept visible when a later line runs or sources the file the
// heredoc writes (`cat > /tmp/x.sh <<'EOF'` … `bash /tmp/x.sh`), or when a
// later line holds an expansion. Removing a body hides it from the analysers
// that parse the raw command, so that decision must fail toward visibility.
//
// The analysers that parse the raw command treat every body line as a shell
// command, so `"net/http"` in a Go file written by `cat > f.go <<'EOF'` looks
// like the HTTPie client. Removing the body keeps that data from being read as
// a command.
func stripInertHeredocBodies(command string) string {
	if !strings.Contains(command, "<<") {
		return command
	}
	lines := strings.Split(command, "\n")
	out := make([]string, 0, len(lines))
	stripped := false
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		ops, ok := heredocOpsOnLine(line)
		if !ok {
			// A `<<` that cannot be classified: nothing after it can be trusted
			// to be outside a heredoc body, so keep the rest verbatim.
			out = append(out, lines[i:]...)
			break
		}
		if len(ops) == 0 {
			out = append(out, line)
			continue
		}
		// The bodies of the operators on one line follow it in order, each
		// ending at its own delimiter line.
		end, terminated := i, true
		for _, op := range ops {
			j := findHeredocEnd(lines, end+1, op)
			if j < 0 {
				terminated = false
				break
			}
			end = j
		}
		if !terminated {
			out = append(out, lines[i:]...)
			break
		}
		if len(ops) == 1 && heredocInert(out, line, ops[0], lines[i+1:end], lines[end+1:]) {
			out = append(out, line)
			stripped = true
		} else {
			// Not inert: keep the operator line and its body verbatim, and skip
			// past the body so its lines are not read as operators.
			out = append(out, lines[i:end+1]...)
		}
		i = end
	}
	if !stripped {
		return command
	}
	return strings.Join(out, "\n")
}
