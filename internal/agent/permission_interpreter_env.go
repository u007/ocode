package agent

import (
	"regexp"
	"sort"
	"strings"
)

// Interpreter environment resolution, the python counterpart of the shell
// expander (permission_shellvars.go). The shell path resolves NAME=value and
// $NAME references before the judge sees the command, so a variable that holds
// a loopback URL is judgeable rather than opaque. Inline interpreter source has
// no such pass, so `requests.get(os.environ['API_URL'])` was unprovable and had
// to be treated as off-host even when API_URL was http://127.0.0.1:8080.
//
// This resolves the NAMES the source reads, never rewriting the source: the
// resolved values travel beside it as interpreter.resolved_env, an ocode
// verified fact, so the judge applies the destination rule to the value instead
// of guessing at the expression.
//
// Safety is inherited from the shell expander, not reinvented:
//   - a secret-looking NAME (secretVarNameRe) or a secret-looking VALUE
//     (judgeValueLooksSecret: known secret format, or credentials in URL
//     userinfo) is recorded as <redacted> and NOT resolved;
//   - PWD/OLDPWD are skipped: they describe ocode's process cwd, not the
//     directory the command runs in, and resolving them hands the judge a
//     confidently wrong path;
//   - when the command itself mentions the name (an assignment, export, unset,
//     or a wrapper the shell expander could not model) the environment lookup is
//     NOT used. The shell expander marks such a name opaque, and falling back to
//     the process environment would show the judge a value the child will not
//     use;
//   - a name that cannot be resolved is simply absent, which leaves the
//     pre-existing "not proven" deferral in place.
//
// Additive: nothing here allows or denies on its own.
func (a *Agent) interpreterResolvedEnv(command, source string, shellVars []resolvedShellVar) []resolvedShellVar {
	names := pythonReferencedEnv(source)
	if len(names) == 0 {
		return nil
	}
	byName := make(map[string]resolvedShellVar, len(shellVars))
	for _, v := range shellVars {
		// "command" records name a $(...) substitution, not an environment variable.
		if v.Source == "command" {
			continue
		}
		byName[v.Name] = v
	}
	var out []resolvedShellVar
	for _, name := range names {
		if v, ok := byName[name]; ok {
			out = append(out, v)
			continue
		}
		if name == "PWD" || name == "OLDPWD" {
			continue
		}
		// `API_URL=… python3 -c …` exports the variable to THIS child only, so the
		// shell expander deliberately does not record it as a statement assignment
		// (bash expands the command's own args before a prefix applies). For an
		// interpreter the prefix IS the child's environment, so read it here.
		if v, ok := interpreterPrefixEnv(command, name); ok {
			if secretVarNameRe.MatchString(name) || judgeValueLooksSecret(v) {
				out = append(out, resolvedShellVar{Name: name, Value: redactedShellValue, Source: "environment"})
				continue
			}
			out = append(out, resolvedShellVar{Name: name, Value: v, Source: "environment"})
			continue
		}
		// The command mentions this name but the expander produced no record:
		// it was marked opaque (unset/export/+=/a wrapper it cannot model).
		// Never substitute the process environment for the child's.
		if shellCommandTouchesName(command, name) {
			continue
		}
		val, ok := judgeLookupEnv(name)
		if !ok {
			continue
		}
		if secretVarNameRe.MatchString(name) || judgeValueLooksSecret(val) {
			out = append(out, resolvedShellVar{Name: name, Value: redactedShellValue, Source: "environment"})
			continue
		}
		out = append(out, resolvedShellVar{Name: name, Value: val, Source: "environment"})
	}
	return out
}

// interpreterEnvNamePatterns match the python forms that read an environment
// variable by a LITERAL name. A computed name (os.environ[key]) is deliberately
// not matched: there is nothing to resolve.
var interpreterEnvNamePatterns = []*regexp.Regexp{
	// os.environ.get("NAME", ...) / environ.get('NAME')
	regexp.MustCompile(`(?:os\.)?environ\.get\(\s*["']([A-Za-z_][A-Za-z0-9_]*)["']`),
	// os.environ["NAME"] / environ['NAME']
	regexp.MustCompile(`(?:os\.)?environ\[\s*["']([A-Za-z_][A-Za-z0-9_]*)["']\s*\]`),
	// os.getenv("NAME", ...) / getenv('NAME')
	regexp.MustCompile(`(?:os\.)?getenv\(\s*["']([A-Za-z_][A-Za-z0-9_]*)["']`),
}

// pythonReferencedEnv returns the environment variable names a python source
// reads by literal name, sorted and deduplicated for a stable judge state.
func pythonReferencedEnv(source string) []string {
	if source == "" {
		return nil
	}
	src := stripPythonTripleQuotedBodies(source)
	seen := make(map[string]bool)
	var names []string
	for _, re := range interpreterEnvNamePatterns {
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			if len(m) < 2 || seen[m[1]] {
				continue
			}
			seen[m[1]] = true
			names = append(names, m[1])
		}
	}
	sort.Strings(names)
	return names
}

// interpreterPrefixEnv returns the value of a NAME=value environment prefix on
// the interpreter invocation (`API_URL=… python3 -c …`, optionally after a
// leading cd or wrapped in env). Only the words BEFORE the interpreter token are
// considered, so a `NAME=value` string inside the python source cannot be
// mistaken for it, and a value containing a shell expansion is refused rather
// than guessed.
func interpreterPrefixEnv(command, name string) (string, bool) {
	cut := len(command)
	for _, tok := range []string{"python3", "python"} {
		if i := strings.Index(command, tok); i >= 0 && i < cut {
			cut = i
		}
	}
	if cut == 0 || cut >= len(command) {
		return "", false
	}
	for _, field := range strings.Fields(command[:cut]) {
		key, value, ok := strings.Cut(field, "=")
		if !ok || key != name {
			continue
		}
		value = strings.Trim(value, `"'`)
		if value == "" || strings.ContainsAny(value, "$`") {
			return "", false
		}
		return value, true
	}
	return "", false
}

// shellCommandTouchesName reports whether the SHELL part of the command binds or
// references the variable, in which case the shell expander is authoritative and
// the process environment must not be consulted.
//
// It must not match the interpreter BODY: `python3 -c "… os.environ['API_URL'] …"`
// embeds the very name we are asking about, so a plain substring test skipped
// every resolution. Only shell forms count — $NAME, ${NAME}, NAME=, and the
// rebinding keywords (unset/export/read/for/…), which are over-inclusive on
// purpose: a false positive only leaves the destination unproven.
func shellCommandTouchesName(command, name string) bool {
	if command == "" || name == "" {
		return false
	}
	if strings.Contains(command, "$"+name) || strings.Contains(command, "${"+name) {
		return true
	}
	if strings.Contains(command, name+"=") {
		return true
	}
	pattern := `(?:^|[\s;&|(])(?:unset|export|declare|local|readonly|typeset|read|for|mapfile|readarray)\s+(?:-\S+\s+)*` + regexp.QuoteMeta(name) + `\b`
	return regexp.MustCompile(pattern).MatchString(command)
}
