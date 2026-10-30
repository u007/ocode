package dbconnect

import (
	"fmt"
	"strings"
)

// ExpandEnv substitutes environment-variable references in a template.
//
// A reference is written ${NAME} or $NAME. The result is the template with
// every reference replaced by the value lookup returns.
//
// A BRACED reference ("${NAME}") that does not resolve is an ERROR, never an
// empty string: a template like "postgres://user:${DB_PASS}@host/db" that
// silently lost its password would connect to the wrong place with the wrong
// credentials and look like a successful connection.
//
// A BARE reference ("$NAME") that does not resolve stays LITERAL text,
// because a Postgres password may legitimately contain a dollar sign
// ("postgres://app:p$ssword@host/db"). Use IsTemplate to decide which kind a
// value is before expanding it.
//
// A reference that resolves to an EMPTY value is not an error — an empty
// value is a legitimate value, and the caller decides whether it is
// acceptable.
//
// The substitution happens on the raw template string, BEFORE any URL
// parsing. That ordering is load-bearing: net/url rejects a "${" in the
// userinfo section ("invalid userinfo"), so a URL that embeds a reference
// cannot be parsed first and substituted after. Substituting into an
// already-parsed URL is unsafe for a second reason — a value carrying
// URL-significant characters ("/", "@", ":") would silently re-interpret
// the structure around it.
//
// A "$" that does not begin a valid reference is left as-is, so a literal
// dollar sign in a password or a path survives.
func ExpandEnv(template string, lookup func(string) (string, bool)) (string, error) {
	if !strings.ContainsRune(template, '$') {
		return template, nil
	}
	var b strings.Builder
	b.Grow(len(template))
	for i := 0; i < len(template); {
		c := template[i]
		if c != '$' {
			b.WriteByte(c)
			i++
			continue
		}
		name, next, braced, ok := readReference(template, i)
		if !ok {
			// Not a reference: a lone "$", "$ ", "$-" or a trailing "$".
			b.WriteByte('$')
			i++
			continue
		}
		value, found := lookup(name)
		if !found {
			if braced {
				// A braced reference is an explicit opt-in, so an unresolved
				// one is a mistake worth reporting: the user wrote
				// "${DB_PASS}" and it is not set.
				return "", fmt.Errorf("environment variable %q is not set", name)
			}
			// A bare reference that does not resolve is literal text. This is
			// what keeps a Postgres password containing "$" working.
			b.WriteByte('$')
			i++
			continue
		}
		b.WriteString(value)
		i = next
	}
	return b.String(), nil
}

// readReference reads a variable reference at template[start] (which must be
// a '$'). It returns the variable name, the index just past the reference,
// and whether the reference was braced. ok is false when the text at start is
// not a reference.
//
// The accepted forms are "${NAME}" and "$NAME", where NAME is a shell-like
// identifier: a letter or underscore followed by letters, digits or
// underscores.
func readReference(template string, start int) (name string, next int, braced, ok bool) {
	i := start + 1
	if i >= len(template) {
		return "", 0, false, false
	}
	if template[i] == '{' {
		end := strings.IndexByte(template[i:], '}')
		if end < 0 {
			// An unterminated "${" is not a reference.
			return "", 0, false, false
		}
		inner := template[i+1 : i+end]
		if !validVarName(inner) {
			return "", 0, false, false
		}
		return inner, i + end + 1, true, true
	}
	// Bare $NAME.
	j := i
	for j < len(template) && isVarChar(template[j], j == i) {
		j++
	}
	if j == i {
		return "", 0, false, false
	}
	return template[i:j], j, false, true
}

// validVarName reports whether name is a shell-like identifier.
func validVarName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		if !isVarChar(name[i], i == 0) {
			return false
		}
	}
	return true
}

// isVarChar reports whether c may appear in a variable name. The first
// character may not be a digit.
func isVarChar(c byte, first bool) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
		return true
	case c >= '0' && c <= '9':
		return !first
	}
	return false
}

// IsTemplate reports whether s is an environment template that must be
// expanded.
//
// Only a BRACED reference ("${NAME}") makes a value a template, and an
// unresolved one is then an error. A BARE reference ("$NAME") never does, and
// the process environment plays no part in the decision. A Postgres password
// such as "pa$HOME" or "p$ssword" is a literal secret. Classifying it by
// whether "HOME" happens to be set would save the password in plaintext as a
// template instead of sealing it.
//
// Once a value is a template, every reference in it is expanded (ExpandEnv),
// bare ones included: "postgres://${U}:$P@h/db" expands both. A bare
// reference that does not resolve inside a template stays literal text.
func IsTemplate(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != '$' {
			continue
		}
		if _, _, braced, ok := readReference(s, i); ok && braced {
			return true
		}
	}
	return false
}

// nonReferenceText returns the part of s that is not an environment reference.
// A bare "$" with no valid identifier after it stays in the result.
func nonReferenceText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '$' {
			if _, next, _, ok := readReference(s, i); ok {
				i = next
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// TemplateHasLiteralSecret reports whether s carries a literal credential even
// though it also contains an environment reference.
//
// A template is saved in the clear (it names variables, not a credential), so a
// literal password in it would be persisted unencrypted. The check is
// deliberately conservative: the userinfo password, and the password /
// sslpassword query values, must consist ONLY of references. A partial
// reference such as "pa$HOME" is refused too — the user can write ${DB_PASS}
// instead.
//
// A URL with no reference is not a template; the caller seals it. A URL whose
// template reference is outside the credential (for example a literal user with
// ${DB_PASS}) is a valid template.
func TemplateHasLiteralSecret(s string) bool {
	const schemeSep = "://"
	sep := strings.Index(s, schemeSep)
	if sep < 0 {
		return false
	}
	rest := s[sep+len(schemeSep):]
	authorityEnd := strings.IndexAny(rest, "/?#")
	authority := rest
	if authorityEnd >= 0 {
		authority = rest[:authorityEnd]
	}
	if at := strings.LastIndexByte(authority, '@'); at >= 0 {
		userinfo := authority[:at]
		if colon := strings.IndexByte(userinfo, ':'); colon >= 0 {
			if nonReferenceText(userinfo[colon+1:]) != "" {
				return true
			}
		}
	}
	query := ""
	if q := strings.IndexByte(s, '?'); q >= 0 {
		query = s[q+1:]
		if hash := strings.IndexByte(query, '#'); hash >= 0 {
			query = query[:hash]
		}
	}
	for _, pair := range strings.Split(query, "&") {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		switch strings.ToLower(key) {
		case "password", "sslpassword":
			if nonReferenceText(value) != "" {
				return true
			}
		}
	}
	return false
}
