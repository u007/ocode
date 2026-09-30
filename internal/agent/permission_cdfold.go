package agent

import "strings"

// foldTopLevelCds removes top-level `cd <literal>` statements from a bash command
// and reports the working directory they establish.
//
// WHY: the auto-permission judge cannot resolve a cd target against
// allowed_roots — it does no path-containment reasoning, so it either invents an
// `outside_allowed_roots` concern (often wrongly) or simply loses confidence.
// Measured against the live API with the real 37-root / 148-prefix state, an
// ordinary read-only command scored allow@0.06 with concern
// `outside_allowed_roots`; the identical command with the cd folded away scored
// allow@1.00. Setting working_directory to the target instead of removing the cd
// only reached 0.89, so removal is the stronger form.
//
// GRANULARITY: the real reported command is
//
//	cd KAKIIT && total=$(ls …); …; for f in drizzle/*.sql; do grep -q … "$f" || echo …; done
//
// so the fold has to survive a trailing for-loop and several $(…) substitutions.
// What makes a cd unsafe is not "the command mentions control flow" but "the cd
// is not an unconditional top-level statement". So control flow only starts a
// freeze: cds before it fold normally, and once a loop/if/subshell is seen every
// remaining statement is copied verbatim. A cd that appears after that point is
// refused, because it may be conditional or scoped to a construct body.
//
// FAIL-CLOSED. This sits on the path that decides auto-approval, so it folds only
// when certain of the effect, and returns ok=false with the input unchanged for
// every ambiguous shape — the judge then sees the cd exactly as today, which
// defers to a human rather than granting. inScope is the allowed-roots predicate;
// a target resolving outside it is not folded, so an out-of-scope cd still reaches
// the human.
func foldTopLevelCds(command, workDir string, inScope func(string) bool) (folded, cwd string, ok bool) {
	if command == "" || workDir == "" || inScope == nil {
		return command, workDir, false
	}
	// Quotes and newlines are left alone entirely: a quoted token's boundaries
	// are not something splitShellFields is relied on to reproduce here, and a
	// wrong fold is an ask that became an allow. Newlines are refused for the
	// whole command because they make statement boundaries ambiguous.
	if strings.Contains(command, "\n") {
		return command, workDir, false
	}
	// Bare ( ) { } are group/subshell syntax and change what a cd affects. The
	// same characters inside $(…) or a backtick span are substitution syntax and
	// are perfectly ordinary, so they are blanked out before this check.
	if strings.ContainsAny(stripSubstitutions(command), "(){}") {
		return command, workDir, false
	}

	r := []rune(command)
	var out strings.Builder
	cur := workDir
	foldedAny, kept := false, 0
	frozen := false // set once control flow makes the simple statement model unsafe

	i := 0
	for i < len(r) {
		// Buffer the blanks preceding a statement: when the statement is folded
		// away its whitespace must go with it.
		blankStart := i
		for i < len(r) && (r[i] == ' ' || r[i] == '\t') {
			i++
		}
		blanks := string(r[blankStart:i])

		end := scanShellStatement(r, i)
		stmt := string(r[i:end])
		term := statementTerminator(r, end)

		target, isCd := simpleCdTarget(stmt)
		switch {
		case isCd && target == "":
			// `cd -`, `cd $VAR`, `cd ~/x`, bare `cd`, a glob, `cd a b`: a cd whose
			// effect cannot be established statically.
			return command, workDir, false
		case isCd && frozen:
			// A cd after control flow may be conditional or inside a construct body.
			return command, workDir, false
		case isCd:
			if term == "||" || term == "|" || term == "&" {
				// The cd may not run, or runs in a pipeline stage.
				return command, workDir, false
			}
			resolved := resolvePath(target, cur)
			if !inScope(resolved) {
				return command, workDir, false
			}
			cur = resolved
			foldedAny = true
		default:
			// Not a cd. Kept verbatim, terminator included, so a frozen tail is
			// reproduced exactly.
			if !frozen {
				// Control flow, or a pipeline/conditional join, means the next
				// statement is conditional or runs elsewhere: a cd after it is no
				// longer an unconditional top-level statement, so freeze here and
				// refuse any cd that follows.
				if statementHasControlFlow(stmt) || term == "|" || term == "||" || term == "&" {
					frozen = true
				}
			}
			out.WriteString(blanks)
			out.WriteString(stmt)
			if term != "" {
				out.WriteString(term)
			}
			kept++
		}

		i = end
		if term != "" {
			i += len([]rune(term))
		}
	}

	// Nothing to fold, or folding would leave no command to judge: both are the
	// status quo, not an improvement.
	if !foldedAny || kept == 0 {
		return command, workDir, false
	}
	return strings.TrimSpace(out.String()), cur, true
}

// simpleCdTarget reports whether stmt is a cd statement and, if so, its target.
// A target of "" with isCd true means "a cd, but not statically resolvable" — the
// caller must not fold it. A non-cd statement returns isCd false.
func simpleCdTarget(stmt string) (target string, isCd bool) {
	fields := splitShellFields(stmt)
	if len(fields) == 0 || fields[0] != "cd" {
		return "", false
	}
	// `cd` with no argument returns to $HOME; `cd a b` is an error. Neither is
	// something to fold.
	if len(fields) != 2 {
		return "", true
	}
	// A quoted target is refused rather than unquoted: a cd inside quotes may be
	// a single token this function would otherwise split, and a wrong fold is an
	// ask that became an allow. Quotes elsewhere in the command are fine, since
	// that text is copied verbatim.
	if strings.ContainsAny(stmt, "'\"") {
		return "", true
	}
	t := fields[1]
	if t == "" || t == "-" || t == "--" || t == "~" {
		return "", true
	}
	// Anything the shell would expand, glob, or interpret is not a literal we can
	// resolve here.
	if strings.ContainsAny(t, "$`*?[]{}~") {
		return "", true
	}
	return t, true
}

// statementHasControlFlow reports whether a single statement introduces shell
// control flow or a group, from which point the simple statement model no longer
// describes what a later cd would do.
func statementHasControlFlow(stmt string) bool {
	for _, kw := range []string{"if", "while", "until", "for", "case", "select", "do", "done",
		"then", "elif", "else", "fi", "esac", "{", "}"} {
		for _, f := range splitShellFields(stmt) {
			if f == kw {
				return true
			}
		}
	}
	return false
}

// stripSubstitutions blanks out $(…) and `…` spans so a caller can test for bare
// group characters without tripping over ordinary command substitution. The
// result is only ever used for a containment check, never for parsing, so
// collapsing each span to a single space is safe.
func stripSubstitutions(s string) string {
	r := []rune(s)
	var b strings.Builder
	inSingle, inDouble := false, false
	for i := 0; i < len(r); i++ {
		c := r[i]
		switch {
		case inSingle:
			if c == '\'' {
				inSingle = false
			}
			b.WriteRune(' ')
		case c == '\\':
			b.WriteRune(' ')
			if i+1 < len(r) {
				i++
				b.WriteRune(' ')
			}
		case c == '\'' && !inDouble:
			inSingle = true
			b.WriteRune(' ')
		case c == '"':
			inDouble = !inDouble
			b.WriteRune(' ')
		case c == '`':
			j := i + 1
			for j < len(r) && r[j] != '`' {
				j++
			}
			i = j
			b.WriteRune(' ')
		case c == '$' && i+1 < len(r) && r[i+1] == '(':
			i = matchParen(r, i+1) - 1
			b.WriteRune(' ')
		default:
			b.WriteRune(c)
		}
	}
	return b.String()
}
