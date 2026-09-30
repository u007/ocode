package agent

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/u007/ocode/internal/redact"
	"github.com/u007/ocode/internal/shell"
)

// The permission judges (Jev and the chat judge) cannot run anything, so a
// command built from shell variables — MOD=$(go env GOMODCACHE); grep x "$MOD/y"
// — is opaque to them: they cannot tell where "$MOD/y" points and defer to a
// human. expandBashForJudge resolves what it safely can BEFORE the judge sees
// the command:
//
//   - variables assigned earlier in the same command (NAME=value statements),
//   - environment variables the command references (only those; a
//     secret-looking name or value is withheld as <redacted>, because Jev is a
//     remote API),
//   - $(...) substitutions whose inner command is on a fixed read-only
//     allowlist (resolveJudgeSubstitution). Anything else is never run.
//
// The judge then receives the expanded command alongside the original. The
// expansion is advisory context only: the command that runs is unchanged, and
// anything unresolved stays verbatim so the judge still sees it as opaque.

// resolvedShellVar is one expansion the judge is told about.
type resolvedShellVar struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Source string `json:"source"` // "assignment", "environment" or "command"
}

// bashExpansion is the judge-facing result: Command is the expanded text,
// Variables the expansions applied (plus withheld secrets), in order of first
// appearance.
type bashExpansion struct {
	Command   string
	Variables []resolvedShellVar
}

const redactedShellValue = "<redacted>"

// judgeSubstitutionTimeout bounds each allowlisted substitution. They are all
// local metadata queries; a slow one means something is wrong, and the judge
// then simply sees the substitution unresolved.
const judgeSubstitutionTimeout = 3 * time.Second

// Test seams: environment lookup and allowlisted-command execution.
var (
	judgeLookupEnv       = os.LookupEnv
	judgeRunSubstitution = func(command, dir string) (string, error) {
		// stdout only: tools such as npm print config warnings on stderr,
		// which must not be mistaken for the value.
		ctx, cancel := context.WithTimeout(context.Background(), judgeSubstitutionTimeout)
		defer cancel()
		c := shell.Build(ctx, command, dir)
		var stdout, stderr bytes.Buffer
		c.Stdout, c.Stderr = &stdout, &stderr
		if err := c.Run(); err != nil {
			if ctx.Err() != nil {
				return "", fmt.Errorf("timed out after %s", judgeSubstitutionTimeout)
			}
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return stdout.String(), nil
	}
)

var (
	shellVarNameRe   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*`)
	shellAssignRe    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	shellAssignAnyRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*\+?=`)
	shellIdentRe     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	goEnvVarRe       = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	secretVarNameRe  = regexp.MustCompile(`(?i)(KEY|TOKEN|SECRET|PASSW|AUTH|CREDENTIAL|COOKIE|SESSION|PRIVATE|SIGNATURE)`)
	urlUserinfoRe    = regexp.MustCompile(`://[^/@\s]+:[^/@\s]+@`)
	pythonSnippetsOK = map[string]bool{
		"import sys;print(sys.prefix)":                             true,
		"import sys;print(sys.base_prefix)":                        true,
		"import sys;print(sys.executable)":                         true,
		"import site;print(site.getusersitepackages())":            true,
		"import site;print(site.getsitepackages()[0])":             true,
		"import sysconfig;print(sysconfig.get_paths()['purelib'])": true,
		"import sysconfig;print(sysconfig.get_paths()['platlib'])": true,
	}
)

// expandBashForJudge returns the expansion of command, or ok=false when
// nothing was resolved or withheld (the judge then gets the command alone).
func (a *Agent) expandBashForJudge(command string) (bashExpansion, bool) {
	e := &judgeExpander{agent: a, vars: map[string]string{}, opaque: map[string]bool{}, seen: map[string]bool{}}
	out := e.expandCommand(command)
	if len(e.records) == 0 {
		return bashExpansion{}, false
	}
	exp := bashExpansion{Command: out, Variables: e.records}
	if reg := a.judgeMaskRegistry(); reg != nil {
		// /mask is on: resolved values must not reach the judge raw. Detect
		// on NAME=value so assignment-style detection has its keyword
		// context, then substitute everything the registry now knows.
		for i, v := range exp.Variables {
			redactText(v.Name+"="+v.Value, reg)
			exp.Variables[i].Value = reg.Substitute(v.Value)
		}
		exp.Command = reg.Substitute(redactText(exp.Command, reg))
	}
	return exp, true
}

type judgeExpander struct {
	agent   *Agent
	vars    map[string]string // resolved in-command assignments
	opaque  map[string]bool   // assigned in-command but unresolvable: never fall back to env
	records []resolvedShellVar
	seen    map[string]bool
}

func (e *judgeExpander) record(name, value, source string) {
	key := source + "\x00" + name
	if e.seen[key] {
		return
	}
	e.seen[key] = true
	e.records = append(e.records, resolvedShellVar{Name: name, Value: value, Source: source})
}

// expandCommand walks the command statement by statement. A statement made
// only of NAME=value words defines variables for the statements after it; a
// NAME=value prefix on a command does not (bash expands that command's own
// arguments before the prefix applies, and the prefix does not outlive it).
func (e *judgeExpander) expandCommand(cmd string) string {
	r := []rune(cmd)
	var out strings.Builder
	i := 0
	for i < len(r) {
		// Statement start: copy leading blanks, then collect assignments.
		for i < len(r) && (r[i] == ' ' || r[i] == '\t') {
			out.WriteRune(r[i])
			i++
		}
		type pending struct {
			name, value string
			ok          bool
		}
		var assigns []pending
		for i < len(r) {
			m := shellAssignRe.FindString(string(r[i:min(len(r), i+256)]))
			if m == "" {
				break
			}
			name := strings.TrimSuffix(m, "=")
			i += len([]rune(m))
			end := scanShellWord(r, i)
			expanded, literal, ok := e.expandWord(string(r[i:end]))
			out.WriteString(name + "=" + expanded)
			assigns = append(assigns, pending{name, literal, ok})
			i = end
			for i < len(r) && (r[i] == ' ' || r[i] == '\t') {
				out.WriteRune(r[i])
				i++
			}
		}
		// The rest of the statement, up to its terminator.
		end := scanShellStatement(r, i)
		rest := string(r[i:end])
		// A rebinding the expander does not model (export/declare/local, NAME+=,
		// for/read/unset, an assignment buried in a pipeline or group) makes the
		// variable opaque, so a later $NAME is shown unresolved rather than as
		// the stale value the shell will no longer use.
		e.markStatementRebindings(rest)
		expanded, _, _ := e.expandWord(rest)
		out.WriteString(expanded)
		pureAssignment := len(assigns) > 0 && strings.TrimSpace(string(r[i:end])) == ""
		i = end
		term := statementTerminator(r, i)
		out.WriteString(term)
		i += len([]rune(term))
		if pureAssignment && term != "|" && term != "&" {
			for _, p := range assigns {
				if p.ok {
					e.vars[p.name] = p.value
					delete(e.opaque, p.name)
					e.record(p.name, p.value, "assignment")
				} else {
					delete(e.vars, p.name)
					e.opaque[p.name] = true
				}
			}
		}
	}
	return out.String()
}

// markStatementRebindings marks variables rebound by stmt in a form
// expandCommand does not model. It is deliberately over-inclusive: a false
// positive only makes the judge see $NAME unresolved (a fail-closed extra
// confirmation), while a miss would let the judge reason about a value the
// shell will not use.
func (e *judgeExpander) markStatementRebindings(stmt string) {
	for _, m := range shellAssignAnyRe.FindAllString(stmt, -1) {
		e.markOpaque(strings.TrimSuffix(strings.TrimSuffix(m, "="), "+"))
	}
	fields := strings.Fields(stmt)
	if len(fields) == 0 {
		return
	}
	switch fields[0] {
	case "for", "read", "unset", "declare", "local", "readonly", "typeset", "mapfile", "readarray":
		for _, f := range fields[1:] {
			if strings.HasPrefix(f, "-") {
				continue
			}
			name := f
			if i := strings.IndexByte(name, '='); i >= 0 {
				name = name[:i]
			}
			e.markOpaque(name)
		}
	}
}

func (e *judgeExpander) markOpaque(name string) {
	if name == "" || !shellIdentRe.MatchString(name) {
		return
	}
	delete(e.vars, name)
	e.opaque[name] = true
}

// expandWord expands $NAME, ${NAME} and allowlisted $(...) in text, honouring
// quotes. It returns the expanded text (quotes kept, for display), its
// unquoted literal value, and whether every expansion in it resolved.
func (e *judgeExpander) expandWord(text string) (expanded, literal string, ok bool) {
	r := []rune(text)
	var out, lit strings.Builder
	ok = true
	inSingle, inDouble := false, false
	for i := 0; i < len(r); i++ {
		c := r[i]
		switch {
		case inSingle:
			out.WriteRune(c)
			if c == '\'' {
				inSingle = false
			} else {
				lit.WriteRune(c)
			}
		case c == '\\' && i+1 < len(r):
			out.WriteRune(c)
			out.WriteRune(r[i+1])
			lit.WriteRune(r[i+1])
			i++
		case c == '\'' && !inDouble:
			inSingle = true
			out.WriteRune(c)
		case c == '"':
			inDouble = !inDouble
			out.WriteRune(c)
		case c == '`':
			end := i + 1
			for end < len(r) && r[end] != '`' {
				end++
			}
			end = min(end+1, len(r))
			raw := string(r[i:end])
			out.WriteString(raw)
			lit.WriteString(raw)
			ok = false
			i = end - 1
		case c == '$':
			raw, value, resolved, n := e.expandDollar(r, i)
			if resolved {
				out.WriteString(value)
				lit.WriteString(value)
			} else {
				out.WriteString(raw)
				lit.WriteString(raw)
				ok = false
			}
			i += n - 1
		default:
			out.WriteRune(c)
			lit.WriteRune(c)
		}
	}
	return out.String(), lit.String(), ok
}

// expandDollar resolves the expansion starting at r[i] == '$'. It returns the
// raw text, the value (when resolved), and the number of runes consumed.
func (e *judgeExpander) expandDollar(r []rune, i int) (raw, value string, resolved bool, n int) {
	if i+1 >= len(r) {
		return "$", "", false, 1
	}
	switch next := r[i+1]; {
	case next == '(':
		if i+2 < len(r) && r[i+2] == '(' { // $((arithmetic))
			end := matchParen(r, i+1)
			return string(r[i:end]), "", false, end - i
		}
		end := matchParen(r, i+1)
		raw = string(r[i:end])
		inner := ""
		if end-1 > i+2 {
			inner = string(r[i+2 : end-1])
		}
		if v, ok := e.resolveJudgeSubstitution(inner); ok {
			if secretVarNameRe.MatchString(inner) || judgeValueLooksSecret(v) {
				// A substitution can name a secret (go env GITHUB_TOKEN) or return
				// one in a URL userinfo; withhold it from the remote judge exactly
				// as a secret-looking environment value is withheld.
				e.record(raw, redactedShellValue, "command")
				return raw, "", false, end - i
			}
			e.record(raw, v, "command")
			return raw, v, true, end - i
		}
		return raw, "", false, end - i
	case next == '{':
		end := i + 2
		for end < len(r) && r[end] != '}' {
			end++
		}
		if end >= len(r) {
			return string(r[i:]), "", false, len(r) - i
		}
		raw = string(r[i : end+1])
		name := string(r[i+2 : end])
		if shellVarNameRe.FindString(name) != name { // ${X:-y}, ${#X}, ...
			return raw, "", false, end + 1 - i
		}
		v, ok := e.lookup(name)
		return raw, v, ok, end + 1 - i
	default:
		name := shellVarNameRe.FindString(string(r[i+1:]))
		if name == "" { // $1, $?, $$, $@ ...
			return "$", "", false, 1
		}
		raw = "$" + name
		v, ok := e.lookup(name)
		return raw, v, ok, 1 + len([]rune(name))
	}
}

// judgeValueLooksSecret reports whether a resolved value must not reach the
// remote judge: a known secret format, or credentials embedded in a URL's
// userinfo (which QuickScan does not match — there is no keyword).
func judgeValueLooksSecret(v string) bool {
	return redact.QuickScan(v) || urlUserinfoRe.MatchString(v)
}

func (e *judgeExpander) lookup(name string) (string, bool) {
	if v, ok := e.vars[name]; ok {
		return v, true
	}
	if e.opaque[name] {
		return "", false
	}
	v, ok := judgeLookupEnv(name)
	if !ok {
		return "", false
	}
	if secretVarNameRe.MatchString(name) || judgeValueLooksSecret(v) {
		e.record(name, redactedShellValue, "environment")
		return "", false
	}
	e.record(name, v, "environment")
	return v, true
}

// resolveJudgeSubstitution runs inner only when it is exactly one of the
// read-only metadata queries below, and returns its single-line output.
// Everything else — including any other python -c code — is never executed.
func (e *judgeExpander) resolveJudgeSubstitution(inner string) (string, bool) {
	inner = strings.TrimSpace(inner)
	dir := e.agent.effectiveWorkDir()
	if inner == "pwd" {
		return dir, dir != ""
	}
	run := ""
	fields := strings.Fields(inner)
	switch {
	case len(fields) == 3 && fields[0] == "go" && fields[1] == "env" && goEnvVarRe.MatchString(fields[2]):
		run = inner
	case strings.Join(fields, " ") == "git rev-parse --show-toplevel":
		run = "git rev-parse --show-toplevel"
	case len(fields) >= 2 && len(fields) <= 3 && fields[0] == "npm" && (fields[1] == "root" || fields[1] == "prefix") &&
		(len(fields) == 2 || fields[2] == "-g"):
		run = strings.Join(fields, " ")
	default:
		if bin, snippet, ok := parsePythonSnippet(inner); ok {
			// Canonical snippets hold no double quotes or $, so double-quoting
			// is literal under both sh -c and cmd /C.
			// -I (isolated) keeps the project directory off sys.path, so a
			// repo-local sysconfig.py/site.py cannot execute during judge prep.
			run = bin + ` -I -c "` + snippet + `"`
		}
	}
	if run == "" {
		return "", false
	}
	out, err := judgeRunSubstitution(run, dir)
	if err != nil {
		e.agent.emitDebug("PERMISSION", fmt.Sprintf("tier=judge_expand_fail cmd=%q err=%v", run, err))
		return "", false
	}
	out = strings.TrimSpace(out)
	if out == "" || strings.ContainsAny(out, "\r\n") {
		e.agent.emitDebug("PERMISSION", fmt.Sprintf("tier=judge_expand_fail cmd=%q err=output is not a single line (%d bytes)", run, len(out)))
		return "", false
	}
	return out, true
}

// parsePythonSnippet matches `python|python3 -c '<snippet>'` where the snippet,
// normalised (no whitespace around ; , single-quoted keys), is one of the
// allowlisted print-a-path snippets.
func parsePythonSnippet(inner string) (bin, snippet string, ok bool) {
	fields := strings.SplitN(inner, " ", 3)
	if len(fields) != 3 || (fields[0] != "python" && fields[0] != "python3") || fields[1] != "-c" {
		return "", "", false
	}
	q := strings.TrimSpace(fields[2])
	if len(q) < 2 || (q[0] != '\'' && q[0] != '"') || q[len(q)-1] != q[0] {
		return "", "", false
	}
	s := q[1 : len(q)-1]
	s = strings.ReplaceAll(s, `"`, `'`)
	parts := strings.Split(s, ";")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	s = strings.Join(parts, ";")
	if !pythonSnippetsOK[s] {
		return "", "", false
	}
	return fields[0], s, true
}

// scanShellWord returns the end of the word starting at i: the first unquoted
// blank or statement operator outside $(...).
func scanShellWord(r []rune, i int) int {
	return scanShell(r, i, true)
}

// scanShellStatement returns the end of the statement starting at i: the first
// unquoted ; & | or newline outside $(...).
func scanShellStatement(r []rune, i int) int {
	return scanShell(r, i, false)
}

func scanShell(r []rune, i int, stopAtBlank bool) int {
	inSingle, inDouble := false, false
	for i < len(r) {
		c := r[i]
		switch {
		case inSingle:
			if c == '\'' {
				inSingle = false
			}
		case c == '\\':
			i++
		case c == '\'' && !inDouble:
			inSingle = true
		case c == '"':
			inDouble = !inDouble
		case c == '$' && i+1 < len(r) && r[i+1] == '(':
			i = matchParen(r, i+1) - 1
		case inDouble:
		case c == ';' || c == '&' || c == '|' || c == '\n':
			return i
		case stopAtBlank && (c == ' ' || c == '\t'):
			return i
		}
		i++
	}
	return len(r)
}

// matchParen returns the index just past the ')' closing the '(' at r[open],
// honouring quotes and nesting; len(r) when unbalanced.
func matchParen(r []rune, open int) int {
	depth := 0
	inSingle, inDouble := false, false
	for i := open; i < len(r); i++ {
		c := r[i]
		switch {
		case inSingle:
			if c == '\'' {
				inSingle = false
			}
		case c == '\\':
			i++
		case c == '\'' && !inDouble:
			inSingle = true
		case c == '"':
			inDouble = !inDouble
		case inDouble:
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(r)
}

// statementTerminator returns the operator at r[i] ("&&", "||", ";", "&", "|",
// "\n") or "" at the end of input.
func statementTerminator(r []rune, i int) string {
	if i >= len(r) {
		return ""
	}
	if i+1 < len(r) && (r[i] == '&' || r[i] == '|') && r[i+1] == r[i] {
		return string(r[i : i+2])
	}
	return string(r[i])
}
