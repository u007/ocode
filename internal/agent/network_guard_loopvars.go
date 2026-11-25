package agent

import (
	"sort"
	"strings"
)

// loopVarValueChars are the only characters a literal for-loop value may carry.
// Anything else (globs, braces, tilde, quotes, substitutions, `$`) makes the
// value unknowable, so the loop variable is poisoned.
const loopVarValueChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._/:@%+=,-"

// loopVarVariantCap bounds how many substituted variants one target may expand
// to. Above it the target is judged as written, which fails closed.
const loopVarVariantCap = 64

// literalLoopVarValues returns, for each loop variable whose every value is a
// literal from a `for NAME in w1 w2 ...` header, the list of those values. A
// name is returned only when every reference to it sits inside the body of such
// a loop and nothing on the line can rebind it. It returns nil on any doubt.
func literalLoopVarValues(command string) map[string][]string {
	// IFS changes word splitting of an unquoted $u, so one literal value could
	// become several words. Refuse any line that mentions it.
	if strings.Contains(command, "IFS") {
		return nil
	}
	// Arithmetic contexts ($[...], [[ ... ]] operands) can rebind a loop variable
	// (`$[u++]`, `[[ u++ -eq 1 ]]`) with no `=` or `((`, so refuse those lines too.
	if strings.Contains(command, "$[") || strings.Contains(command, "[[") {
		return nil
	}
	tokens, err := tokenizeShell(command)
	if err != nil {
		return nil
	}
	if hasOpaqueVariableWriter(splitShellFields(command), nil) {
		return nil
	}
	// The tokenizer strips quotes and escapes, so a quoted "do" is indistinguishable
	// from a keyword in the walk, yet bash treats it as a plain word. Refuse the line
	// when any keyword occurs in a form that is not a bare word.
	if !bareKeywordsOnly(command, tokens) {
		return nil
	}
	values := map[string][]string{}
	poisoned := map[string]bool{}
	for i, tok := range tokens {
		if tok.typ != tokWord {
			continue
		}
		// Any NAME=value word can write NAME, wherever it sits (`:;u=x`, `(u=x`).
		if name, _, ok := shellAssignmentWrite(tok.value); ok {
			poisoned[name] = true
		}
		// `source` and `.` run a file that can set any variable, at any position
		// (`command source f`).
		if tok.value == "source" || tok.value == "." {
			return nil
		}
		// A rebinding builtin, quoted or not, names its targets as bare words up to
		// the end of its command (`read u`, `printf -v u`, `local u=x`).
		if tok.value == "for" || !shellRebindingBuiltins[tok.value] {
			continue
		}
		for j := i + 1; j < len(tokens) && tokens[j].typ != tokOp; j++ {
			if tokens[j].typ != tokWord {
				continue
			}
			if name, _, ok := shellAssignmentWrite(tokens[j].value); ok {
				poisoned[name] = true
			}
			if isShellVarName(tokens[j].value) {
				poisoned[tokens[j].value] = true
			}
		}
	}
	// frames holds the loop variable of each open do-block ("" for a non-literal loop).
	var frames []string
	inFrame := func(name string) bool {
		for _, f := range frames {
			if f == name {
				return true
			}
		}
		return false
	}
	// scanRefs poisons every variable named by tok that no open frame binds.
	scanRefs := func(tok shellToken) {
		names, opaque := loopVarRefs(tok.value)
		for _, n := range opaque {
			poisoned[n] = true
		}
		for _, n := range names {
			if !inFrame(n) {
				poisoned[n] = true
			}
		}
	}
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if tok.typ == tokWord && loopVarCommandPosition(tokens, i) {
			switch tok.value {
			case "for":
				if i+2 >= len(tokens) || tokens[i+1].typ != tokWord || !isShellVarName(tokens[i+1].value) ||
					tokens[i+2].typ != tokWord || tokens[i+2].value != "in" {
					return nil
				}
				name := tokens[i+1].value
				count := 0
				j := i + 3
				for ; j < len(tokens); j++ {
					t := tokens[j]
					if t.typ == tokWord && t.value == "do" {
						break
					}
					if t.typ == tokOp && t.value == ";" && j+1 < len(tokens) && tokens[j+1].typ == tokWord && tokens[j+1].value == "do" {
						continue
					}
					if t.typ != tokWord {
						poisoned[name] = true
						scanRefs(t)
						continue
					}
					scanRefs(t)
					if !isLoopVarLiteral(t.value) {
						poisoned[name] = true
						continue
					}
					values[name] = append(values[name], t.value)
					count++
				}
				if j >= len(tokens) {
					return nil
				}
				if count == 0 {
					poisoned[name] = true
				}
				frames = append(frames, name)
				i = j
				continue
			case "select":
				return nil
			case "do":
				frames = append(frames, "")
				continue
			case "done":
				if len(frames) == 0 {
					return nil
				}
				frames = frames[:len(frames)-1]
				continue
			}
		}
		switch tok.typ {
		case tokWord, tokSubst, tokArith, tokRedir:
			scanRefs(tok)
		}
	}
	if len(frames) != 0 {
		return nil
	}
	for _, f := range splitShellFields(command) {
		if name, _, ok := shellAssignmentWrite(strings.TrimRight(f, ";")); ok {
			poisoned[name] = true
		}
	}
	out := map[string][]string{}
	for name, vals := range values {
		if poisoned[name] || len(vals) == 0 {
			continue
		}
		out[name] = vals
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// expandLoopVars returns every variant of value with the loop variables it
// references (from vars) replaced by each combination of their values. It
// returns nil when value references none of them, or when the combinations
// exceed loopVarVariantCap.
func expandLoopVars(value string, vars map[string][]string) []string {
	refs, _ := loopVarRefs(value)
	seen := map[string]bool{}
	var names []string
	for _, n := range refs {
		if _, ok := vars[n]; ok && !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	total := 1
	for _, n := range names {
		if len(vars[n]) == 0 {
			return nil
		}
		total *= len(vars[n])
		if total > loopVarVariantCap {
			return nil
		}
	}
	idx := make([]int, len(names))
	var out []string
	for {
		sub := make(map[string]string, len(names))
		for k, n := range names {
			sub[n] = vars[n][idx[k]]
		}
		out = append(out, replaceLoopVarRefs(value, sub))
		k := len(names) - 1
		for ; k >= 0; k-- {
			idx[k]++
			if idx[k] < len(vars[names[k]]) {
				break
			}
			idx[k] = 0
		}
		if k < 0 {
			break
		}
	}
	return out
}

// loopKeywords are the words the walk reads as shell keywords.
var loopKeywords = []string{"do", "done", "for", "in", "select", "then", "else", "elif", "if", "while", "until", "!", "{"}

// bareKeywordsOnly reports whether every occurrence of a loop keyword in the raw
// command is a bare word, and every keyword-valued word is one of those
// occurrences. A quoted or escaped keyword (`"do"`, `\do`, `d''o`) is a plain
// word to bash, but the tokenizer has already stripped the quotes, so the counts
// differ and the line is refused.
func bareKeywordsOnly(command string, tokens []shellToken) bool {
	for _, kw := range loopKeywords {
		tokCount := 0
		for _, t := range tokens {
			if t.typ == tokWord && t.value == kw {
				tokCount++
			}
		}
		if tokCount != bareKeywordCount(command, kw) {
			return false
		}
	}
	return true
}

// bareKeywordCount counts occurrences of kw in s that stand alone, bounded by
// the start or end of s or by whitespace or a control character.
func bareKeywordCount(s, kw string) int {
	n := 0
	for start := 0; start <= len(s); {
		i := strings.Index(s[start:], kw)
		if i < 0 {
			break
		}
		i += start
		end := i + len(kw)
		if (i == 0 || loopWordBoundary(s[i-1])) && (end == len(s) || loopWordBoundary(s[end])) {
			n++
		}
		start = i + 1
	}
	return n
}

// loopWordBoundary reports whether c ends a shell word.
func loopWordBoundary(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', ';', '&', '|', '(', ')':
		return true
	}
	return false
}

// loopVarCommandPosition reports whether tokens[i] is where a command starts.
func loopVarCommandPosition(tokens []shellToken, i int) bool {
	if i == 0 {
		return true
	}
	prev := tokens[i-1]
	switch prev.typ {
	case tokOp, tokLeftParen, tokRightParen:
		return true
	case tokWord:
		switch prev.value {
		case "do", "then", "else", "elif", "if", "while", "until", "!", "{":
			return true
		}
	}
	return false
}

// isLoopVarLiteral reports whether v is a non-empty value made only of loopVarValueChars.
func isLoopVarLiteral(v string) bool {
	if v == "" {
		return false
	}
	for i := 0; i < len(v); i++ {
		if strings.IndexByte(loopVarValueChars, v[i]) < 0 {
			return false
		}
	}
	return true
}

// isLoopIdentByte reports whether c can appear inside a shell variable name.
func isLoopIdentByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// isLoopIdentStart reports whether c can begin a shell variable name.
func isLoopIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// loopVarRefs returns the variable names s references as `$NAME` or `${NAME}`.
// Any other `${...}` form (`${u:-x}`, `${#u}`, `${!u}`) makes every identifier
// inside it an opaque name, returned separately so the caller poisons it.
func loopVarRefs(s string) (names, opaque []string) {
	for i := 0; i < len(s); i++ {
		if s[i] != '$' || i+1 >= len(s) {
			continue
		}
		if s[i+1] == '{' {
			end := strings.IndexByte(s[i+2:], '}')
			var inner string
			if end < 0 {
				inner = s[i+2:]
				i = len(s)
			} else {
				inner = s[i+2 : i+2+end]
				i = i + 2 + end
			}
			if isShellVarName(inner) {
				names = append(names, inner)
			} else {
				opaque = append(opaque, loopIdentRuns(inner)...)
			}
			continue
		}
		if !isLoopIdentStart(s[i+1]) {
			continue
		}
		k := i + 1
		for k < len(s) && isLoopIdentByte(s[k]) {
			k++
		}
		names = append(names, s[i+1:k])
		i = k - 1
	}
	return names, opaque
}

// loopIdentRuns returns every maximal run of identifier characters in s.
func loopIdentRuns(s string) []string {
	var runs []string
	start := -1
	for i := 0; i <= len(s); i++ {
		if i < len(s) && isLoopIdentByte(s[i]) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			runs = append(runs, s[start:i])
			start = -1
		}
	}
	return runs
}

// replaceLoopVarRefs replaces each `$NAME` and `${NAME}` in s whose NAME is a
// key of sub with the mapped value. Other text is copied unchanged.
func replaceLoopVarRefs(s string, sub map[string]string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '$' && i+1 < len(s) {
			if s[i+1] == '{' {
				if end := strings.IndexByte(s[i+2:], '}'); end >= 0 {
					if v, ok := sub[s[i+2:i+2+end]]; ok {
						b.WriteString(v)
						i = i + 2 + end + 1
						continue
					}
				}
			} else if isLoopIdentStart(s[i+1]) {
				k := i + 1
				for k < len(s) && isLoopIdentByte(s[k]) {
					k++
				}
				if v, ok := sub[s[i+1:k]]; ok {
					b.WriteString(v)
					i = k
					continue
				}
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
