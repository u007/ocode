package agent

import (
	"path"
	"slices"
	"strings"
	"unicode"
)

// maxBashPrefixCandidateWords caps a derived rule at three words (binary,
// subcommand, argument). A longer candidate stops being a reusable prefix and
// starts describing one exact invocation.
const maxBashPrefixCandidateWords = 3

// IsSyntheticBashPrefix reports whether prefix names a policy gate rather than
// a literal command prefix. Synthetic scopes ("sandbox.sensitive",
// "sandbox.exfiltration_risk", "bash.interpreter.python3", "permission.escalation",
// …) are matched by their own gate in Decide, never by matchBashPrefixRule, so
// persisting one as an allow rule is a no-op that would only mislead the user.
func IsSyntheticBashPrefix(prefix string) bool {
	switch {
	case prefix == "":
		return true
	case strings.HasPrefix(prefix, "bash.interpreter."):
		return true
	case strings.HasPrefix(prefix, "sandbox."):
		return true
	case strings.HasPrefix(prefix, "permission."):
		return true
	}
	return false
}

// BashPrefixCandidates derives the word-prefix rules that could be persisted
// for a bash invocation, ordered narrowest -> broadest. Words are taken
// literally from the command because that is how matchBashPrefixRuleWords
// matches an allow rule, so every candidate here matches the invocation that
// produced it.
//
// A candidate means "commands whose first N words are exactly these"; the UI
// renders it with a trailing " *" to say so. The value stored in
// permissions.bash.prefixes never contains the star.
//
// Returns nil when no safe literal rule can be derived:
//   - the command is compound (`a && b`) — each fragment has its own head;
//   - the head is a wrapper (`env`, `sudo`, `bash -c`) — the literal head is
//     not the binary that runs, so a rule from it would read narrower than it is;
//   - the head is a shell control keyword.
//
// Git is special-cased to two words: a bare "git" allow is rejected by
// ValidateBashPrefixRule (it would auto-approve destructive subcommands), and a
// three-word git rule would leak the argument ("git commit -m").
func BashPrefixCandidates(command string) []string {
	words, ok := singleCommandWords(command)
	if !ok {
		return nil
	}
	head := path.Base(words[0])
	if ShellControlKeywords[head] {
		return nil
	}
	if head == "git" {
		idx := gitSubcommandIndex(words)
		if idx < 0 {
			return nil
		}
		return validBashPrefixCandidates([]string{"git " + words[idx]})
	}

	// A candidate may not span a non-bare token: a flag ("rm -rf build"), a path
	// ("cat README.md") or a quoted word containing whitespace is specific to this
	// invocation, so stop before the first one. Cutting here, rather than skipping
	// single candidates, keeps a later bare word from re-attaching to the rule.
	for i := 1; i < len(words); i++ {
		if !isBareCommandWord(words[i]) {
			words = words[:i]
			break
		}
	}

	limit := min(len(words), maxBashPrefixCandidateWords)
	candidates := make([]string, 0, limit)
	for n := limit; n >= 1; n-- {
		candidates = append(candidates, strings.Join(words[:n], " "))
	}
	return validBashPrefixCandidates(candidates)
}

// singleCommandWords parses command and returns its literal words when it is
// exactly one unwrapped invocation. It returns false for compound lines and for
// wrapper invocations, where the words that would be matched are not the words
// that run.
func singleCommandWords(command string) ([]string, bool) {
	parsed, err := parseShellCommandLine(command)
	if err != nil || len(parsed) != 1 {
		return nil, false
	}
	words := parsed[0].cmdWords
	if len(words) == 0 {
		return nil, false
	}
	// effectiveCommandWords peels wrappers; more than one result means the real
	// binary sits behind one, so the literal head cannot be used as a rule.
	if len(effectiveCommandWords(words)) != 1 {
		return nil, false
	}
	return words, true
}

// isBareCommandWord reports whether w can appear in a reusable prefix rule:
// not empty, not an option, free of whitespace (a quoted "my script" would
// otherwise split into two words when the rule is matched), and free of
// path/glob/expansion syntax that ties it to one invocation.
func isBareCommandWord(w string) bool {
	if w == "" || strings.HasPrefix(w, "-") || strings.ContainsFunc(w, unicode.IsSpace) {
		return false
	}
	return !strings.ContainsAny(w, "/\\.*?[]$`\"'")
}

// validBashPrefixCandidates drops candidates that cannot be stored as an allow
// rule and de-duplicates while preserving order.
func validBashPrefixCandidates(in []string) []string {
	out := make([]string, 0, len(in))
	for _, cand := range in {
		cand = strings.TrimSpace(cand)
		if cand == "" {
			continue
		}
		if ValidateBashPrefixRule(cand, PermissionAllow) != nil {
			continue
		}
		candWords := strings.Fields(cand)
		if len(candWords) == 0 || ShellControlKeywords[path.Base(candWords[0])] {
			continue
		}
		if slices.Contains(out, cand) {
			continue
		}
		out = append(out, cand)
	}
	return out
}
