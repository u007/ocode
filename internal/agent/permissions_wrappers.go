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
