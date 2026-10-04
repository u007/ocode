package agent

import (
	"path"
	"strings"
)

// Wrapper unwrapping for the bash permission gates.
//
// IsHarmfulBashCommand and the user-ban matcher key off the first word of a
// command ("git"). A destructive form is trivially hidden behind a launcher
// ("env git stash", "timeout 5 git stash", "xargs git stash drop"), a path
// ("/usr/bin/git stash"), or a shell re-exec ("bash -c 'git stash'",
// "eval git stash"). effectiveCommandWords peels those layers and returns
// every command the fragment will actually run, so the gates judge the real
// binary. It never changes what auto-allows on its own: callers only use it to
// find harmful/banned forms.

// transparentWrappers run their remaining arguments as a command. The value
// is the set of flags that consume the following word.
var transparentWrappers = map[string]map[string]bool{
	"command":    {},
	"env":        {"-u": true, "--unset": true, "-C": true, "--chdir": true, "-S": true, "--split-string": true},
	"exec":       {"-a": true},
	"nohup":      {},
	"time":       {"-f": true, "--format": true, "-o": true, "--output": true},
	"nice":       {"-n": true, "--adjustment": true},
	"timeout":    {"-s": true, "--signal": true, "-k": true, "--kill-after": true},
	"xargs":      {"-I": true, "-n": true, "-L": true, "-P": true, "-s": true, "-d": true, "-E": true, "-a": true, "--arg-file": true, "--delimiter": true, "--max-args": true, "--max-lines": true, "--max-procs": true, "--replace": true},
	"stdbuf":     {"-i": true, "-o": true, "-e": true, "--input": true, "--output": true, "--error": true},
	"sudo":       {"-u": true, "--user": true, "-g": true, "--group": true, "-C": true, "-h": true, "--host": true, "-p": true, "--prompt": true, "-r": true, "-t": true, "-T": true, "-U": true},
	"doas":       {"-u": true, "-C": true},
	"unbuffer":   {},
	"caffeinate": {"-t": true, "-w": true},
	"builtin":    {},
}

// wrapperPositionals is the number of positional arguments a wrapper consumes
// before the wrapped command (timeout DURATION).
var wrapperPositionals = map[string]int{"timeout": 1}

// shellReexecBinaries run a script string given with -c.
var shellReexecBinaries = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true, "fish": true}

const maxWrapperDepth = 6

// effectiveCommandWords returns the commands a fragment actually executes,
// with launcher wrappers stripped, binary paths reduced to their basename, and
// shell re-exec / eval bodies parsed into their own fragments. The result
// always contains at least the (possibly unwrapped) input when it is
// non-empty.
func effectiveCommandWords(words []string) [][]string {
	return effectiveCommandWordsDepth(words, 0)
}

func effectiveCommandWordsDepth(words []string, depth int) [][]string {
	words = stripLeadingEnvAssignments(words)
	if len(words) == 0 {
		return nil
	}
	head := path.Base(words[0])
	if head != words[0] {
		words = append([]string{head}, words[1:]...)
	}
	if depth >= maxWrapperDepth {
		return [][]string{words}
	}

	if head == "eval" {
		if len(words) == 1 {
			return [][]string{words}
		}
		return append([][]string{words}, reparseFragments(strings.Join(words[1:], " "), depth+1)...)
	}

	if shellReexecBinaries[head] {
		if script, ok := shellDashCScript(words[1:]); ok {
			return append([][]string{words}, reparseFragments(script, depth+1)...)
		}
		return [][]string{words}
	}

	if valueFlags, ok := transparentWrappers[head]; ok {
		rest := words[1:]
		i := 0
		for i < len(rest) {
			a := rest[i]
			if a == "--" {
				i++
				break
			}
			if strings.HasPrefix(a, "-") && a != "-" {
				if valueFlags[a] && i+1 < len(rest) {
					i += 2
					continue
				}
				i++
				continue
			}
			if head == "env" && strings.Contains(a, "=") {
				i++
				continue
			}
			break
		}
		i += wrapperPositionals[head]
		if i >= len(rest) {
			return [][]string{words}
		}
		return append([][]string{words}, effectiveCommandWordsDepth(rest[i:], depth+1)...)
	}

	return [][]string{words}
}

// shellDashCScript finds the script argument of "sh [flags] -c SCRIPT"; a
// clustered flag such as "-lc" or "-ec" counts when it contains 'c'.
func shellDashCScript(args []string) (string, bool) {
	for i, a := range args {
		if !strings.HasPrefix(a, "-") || strings.HasPrefix(a, "--") {
			if strings.HasPrefix(a, "--") {
				continue
			}
			return "", false
		}
		if strings.ContainsRune(a[1:], 'c') && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

func reparseFragments(script string, depth int) [][]string {
	parsed, err := parseShellCommandLine(script)
	if err != nil {
		fields := splitShellFields(script)
		return effectiveCommandWordsDepth(fields, depth)
	}
	var out [][]string
	for _, c := range parsed {
		out = append(out, effectiveCommandWordsDepth(c.cmdWords, depth)...)
	}
	return out
}

func stripLeadingEnvAssignments(words []string) []string {
	for len(words) > 0 && isEnvAssignmentWord(words[0]) {
		words = words[1:]
	}
	return words
}

func isEnvAssignmentWord(w string) bool {
	eq := strings.IndexByte(w, '=')
	if eq <= 0 {
		return false
	}
	for i, r := range w[:eq] {
		if r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

// isOpaqueCommandHead reports whether the command word is a shell expansion
// ("$g stash", "$(which git) stash", "`…`") whose binary cannot be resolved
// statically. The sandbox gate asks for these instead of auto-allowing.
func isOpaqueCommandHead(words []string) bool {
	words = stripLeadingEnvAssignments(words)
	if len(words) == 0 {
		return false
	}
	h := words[0]
	return strings.HasPrefix(h, "$") || strings.HasPrefix(h, "`") || strings.HasPrefix(h, "${")
}

// heredocShellConsumers are command heads that execute a heredoc body as
// shell code (locally, or on a remote host for ssh), so the body must stay
// visible to the static gates.
var heredocShellConsumers = map[string]bool{"source": true, ".": true, "eval": true, "ssh": true}

// sandboxGateParseTarget returns the text the sandbox per-fragment gate should
// parse. Bodies of quoted, terminated heredocs (<<'EOF', <<"EOF") are removed:
// the shell never expands them, so they are data for the consuming program,
// not commands. Lines after the terminator are kept. The raw command is
// returned unchanged, keeping every body line under the gates, when
//   - a heredoc is unquoted (the shell expands $(...) and backticks in it),
//   - a heredoc is unterminated, or its operator may sit inside a quote,
//     comment or arithmetic expansion (then it is not a heredoc at all),
//   - any command in the line is a shell or shell-like consumer (bash <<'EOF',
//     cat <<'EOF' | sh, ssh host <<'EOF'): the body is shell code.
func sandboxGateParseTarget(command string) string {
	lines := strings.Split(command, "\n")
	var kept []string
	stripped := false
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		locs := heredocOpRe.FindAllStringSubmatchIndex(line, -1)
		if len(locs) == 0 {
			kept = append(kept, line)
			continue
		}
		if len(locs) > 1 || strings.ContainsAny(line[:locs[0][0]], "\"'`\\#(") {
			return command
		}
		m := heredocOpRe.FindStringSubmatch(line)
		if m[1] == "" {
			return command
		}
		stripTabs := strings.HasPrefix(m[0], "<<-")
		terminated := false
		for i++; i < len(lines); i++ {
			cmp := lines[i]
			if stripTabs {
				cmp = strings.TrimLeft(cmp, "\t")
			}
			if cmp == m[2] {
				terminated = true
				break
			}
		}
		if !terminated {
			return command
		}
		kept = append(kept, line)
		stripped = true
	}
	if !stripped {
		return command
	}
	target := strings.Join(kept, "\n")
	parsed, err := parseShellCommandLine(target)
	if err != nil {
		return command
	}
	for _, c := range parsed {
		for _, words := range effectiveCommandWords(c.cmdWords) {
			if len(words) > 0 && (shellReexecBinaries[words[0]] || heredocShellConsumers[words[0]]) {
				return command
			}
		}
	}
	return target
}
