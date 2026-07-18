package tool

import "strings"

// bashUndoCommands is the whitelist of bash commands whose target path(s)
// this can safely identify and back up before execution, so undo_file_change
// can revert them afterward. Anything not on this list — including any
// mutation hidden inside a compound command (pipes, &&, redirection,
// substitution) — is simply not backed up; it is never misclassified as
// backed-up when it is not.
var bashUndoCommands = map[string]bool{
	"rm": true, "mv": true, "cp": true, "sed": true, "truncate": true,
}

// destructiveBashBackupPaths returns the file paths a bash command is about
// to overwrite or delete, for the narrow set of commands above. It only
// matches simple, single-command invocations with no pipes, chains,
// redirection, or substitution.
func destructiveBashBackupPaths(command string) []string {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return nil
	}
	if strings.ContainsAny(trimmed, "|;`<>") || strings.Contains(trimmed, "&&") ||
		strings.Contains(trimmed, "||") || strings.Contains(trimmed, "$(") {
		return nil
	}
	fields, ok := splitShellWords(trimmed)
	if !ok || len(fields) < 2 {
		return nil
	}
	name := fields[0]
	if !bashUndoCommands[name] {
		return nil
	}

	switch name {
	case "rm":
		return stripFlagsWithValues(fields[1:], nil)
	case "truncate":
		return stripFlagsWithValues(fields[1:], map[string]bool{"-s": true, "-r": true})
	case "cp":
		args := stripFlagsWithValues(fields[1:], nil)
		if len(args) != 2 {
			return nil // only the simple `cp SRC DEST` form is supported
		}
		return args[1:] // only the destination is overwritten
	case "mv":
		args := stripFlagsWithValues(fields[1:], nil)
		if len(args) != 2 {
			return nil // only the simple `mv SRC DEST` form is supported
		}
		return args // source disappears, dest is overwritten/created
	case "sed":
		return sedInPlacePaths(fields[1:])
	}
	return nil
}

// stripFlagsWithValues removes flag tokens from args, returning the
// remaining positional tokens. flagsWithValue names flags that consume the
// following token as their value (also removed); all other "-"-prefixed
// tokens are treated as boolean flags.
func stripFlagsWithValues(args []string, flagsWithValue map[string]bool) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			if flagsWithValue[a] && i+1 < len(args) {
				i++
			}
			continue
		}
		out = append(out, a)
	}
	return out
}

// sedInPlacePaths returns the file path(s) sed will edit in place, or nil if
// the invocation has no -i/--in-place flag (a plain sed only prints to
// stdout and mutates nothing).
func sedInPlacePaths(args []string) []string {
	inPlace := false
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-i":
			inPlace = true
			// BSD sed requires a (possibly empty) backup-suffix arg next.
			if i+1 < len(args) && args[i+1] == "" {
				i++
			}
		case strings.HasPrefix(a, "-i"):
			inPlace = true // GNU -iSUFFIX form
		case a == "--in-place" || strings.HasPrefix(a, "--in-place="):
			inPlace = true
		case strings.HasPrefix(a, "-"):
			// other flag, ignored
		default:
			rest = append(rest, a)
		}
	}
	if !inPlace || len(rest) < 2 {
		return nil // need at least a script/expression plus one file
	}
	return rest[1:] // rest[0] is the sed script, the remainder are files
}

// splitShellWords is a minimal quote-aware word splitter for the narrow set
// of commands destructiveBashBackupPaths recognizes. It bails (ok=false) on
// backslash escapes or unclosed quotes rather than risk a wrong split.
func splitShellWords(s string) ([]string, bool) {
	var words []string
	var cur strings.Builder
	inSingle, inDouble, hasContent := false, false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inSingle:
			if c == '\'' {
				inSingle = false
			} else {
				cur.WriteByte(c)
			}
		case inDouble:
			if c == '"' {
				inDouble = false
			} else {
				cur.WriteByte(c)
			}
		case c == '\'':
			inSingle, hasContent = true, true
		case c == '"':
			inDouble, hasContent = true, true
		case c == ' ' || c == '\t':
			if hasContent {
				words = append(words, cur.String())
				cur.Reset()
				hasContent = false
			}
		case c == '\\':
			return nil, false
		default:
			cur.WriteByte(c)
			hasContent = true
		}
	}
	if inSingle || inDouble {
		return nil, false
	}
	if hasContent {
		words = append(words, cur.String())
	}
	return words, true
}
