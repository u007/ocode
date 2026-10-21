// Package envfile reads dotenv files (.env, .env.local) into a key/value map.
//
// ocode uses it to resolve environment-variable references in database
// connection templates, so a connection can be configured by a project file
// instead of a hard-coded URL.
//
// The format is the common dotenv subset: KEY=VALUE lines, an optional
// "export " prefix, "#" comments, blank lines, and single- or double-quoted
// values. There are no multi-line values, no variable interpolation inside a
// file, and no command substitution.
package envfile

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// File names read by Load, in the order they are applied. A later file
// overrides an earlier one.
const (
	FileName      = ".env"
	LocalFileName = ".env.local"
)

// Parse reads dotenv content and returns its key/value pairs.
//
// A line that is blank, a comment, or has no "=" is skipped. A line whose quoted
// value is unterminated is an error, as is a key that appears twice in the same
// file (the file is ambiguous, so nothing from it is trusted). A value may be
// wrapped in matching single or double quotes, which are stripped; inside
// double quotes, "\n", "\t", "\r" and "\\" are unescaped. A quoted value may
// contain "="; the first "=" outside a quoted value separates key from value.
//
// Keys are trimmed of surrounding whitespace. Values are trimmed of
// surrounding whitespace unless they are quoted, in which case the quotes
// define the exact value.
func Parse(r io.Reader) (map[string]string, error) {
	out := make(map[string]string)
	sc := bufio.NewScanner(r)
	// A .env line is short in practice, but a pasted value can be long.
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		key, value, ok, err := parseLine(line)
		if err != nil {
			// The error names the line and the defect, never the value.
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		if !ok {
			continue
		}
		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("line %d: duplicate key %q", lineNo, key)
		}
		out[key] = value
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read env file: %w", err)
	}
	return out, nil
}

// parseLine splits one dotenv line into a key and a value. ok is false for a
// line that carries no assignment (blank, comment, or keyless). err is non-nil
// for a line whose quoted value is malformed: such a line is never skipped, so
// a connection cannot silently lose a secret.
func parseLine(line string) (key, value string, ok bool, err error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false, nil
	}
	// "export KEY=VALUE" is accepted; the prefix is dropped.
	if rest, found := strings.CutPrefix(trimmed, "export "); found {
		trimmed = strings.TrimSpace(rest)
	} else if rest, found := strings.CutPrefix(trimmed, "export\t"); found {
		trimmed = strings.TrimSpace(rest)
	}
	eq := indexUnquoted(trimmed, '=')
	if eq < 0 {
		return "", "", false, nil
	}
	key = strings.TrimSpace(trimmed[:eq])
	if key == "" {
		return "", "", false, nil
	}
	value, err = unquote(strings.TrimSpace(trimmed[eq+1:]))
	if err != nil {
		return "", "", false, fmt.Errorf("key %q: %w", key, err)
	}
	return key, value, true, nil
}

// indexUnquoted returns the index of the first "=" that is not inside a quoted
// section, or -1.
func indexUnquoted(s string, target byte) int {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == target:
			return i
		}
	}
	return -1
}

// unquote strips matching surrounding quotes and unescapes the escapes that
// matter inside a double-quoted value. An unquoted value is returned as-is
// (already trimmed by the caller).
func unquote(v string) (string, error) {
	if v == "" {
		return v, nil
	}
	switch v[0] {
	case '\'':
		// A lone quote (len 1) is unterminated too, not a literal value.
		if len(v) < 2 || v[len(v)-1] != '\'' {
			return "", fmt.Errorf("unterminated single quote")
		}
		// Single quotes are literal: no escape processing.
		return v[1 : len(v)-1], nil
	case '"':
		if len(v) < 2 || v[len(v)-1] != '"' {
			return "", fmt.Errorf("unterminated double quote")
		}
		return unescapeDouble(v[1 : len(v)-1]), nil
	}
	return v, nil
}

// unescapeDouble expands the escapes that are meaningful inside a
// double-quoted dotenv value. An unknown escape keeps its backslash.
func unescapeDouble(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '\\':
			b.WriteByte('\\')
		default:
			b.WriteByte('\\')
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// Load reads .env and .env.local from dir and returns their combined values.
//
// .env is read first and .env.local second, so a key in .env.local overrides
// the same key in .env. A missing file is not an error: its absence simply
// contributes no values. A file that exists but cannot be read IS an error.
//
// The returned map contains only file values. It does not include the process
// environment; the caller layers that on top (see Lookup).
func Load(dir string) (map[string]string, error) {
	out := make(map[string]string)
	for _, name := range []string{FileName, LocalFileName} {
		path := filepath.Join(dir, name)
		values, err := loadFile(path)
		if err != nil {
			return nil, err
		}
		for k, v := range values {
			out[k] = v
		}
	}
	return out, nil
}

// loadFile reads one dotenv file. A missing file yields no values and no
// error.
func loadFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	defer f.Close()
	values, err := Parse(f)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return values, nil
}

// Lookup returns the value of name, preferring the process environment over
// the file values.
//
// The precedence is process environment > .env.local > .env, so a value
// exported in the shell (or injected by CI) always beats the file, while
// .env.local still overrides the committed .env.
//
// A variable that is set but EMPTY counts as set: an empty value is a
// deliberate value, and treating it as missing would turn an intentional
// empty password into a confusing "not set" error.
func Lookup(name string, fileValues map[string]string) (string, bool) {
	if v, ok := os.LookupEnv(name); ok {
		return v, true
	}
	v, ok := fileValues[name]
	return v, ok
}
