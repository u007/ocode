package dbconnect

import (
	"net/url"
	"strings"
	"testing"
)

func lookupFrom(m map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := m[name]
		return v, ok
	}
}

func TestExpandEnvSubstitutesBracedAndBareReferences(t *testing.T) {
	vars := map[string]string{
		"DATABASE_URL": "postgres://u:p@host:5432/db",
		"DB_USER":      "appuser",
	}
	got, err := ExpandEnv("${DATABASE_URL}", lookupFrom(vars))
	if err != nil {
		t.Fatalf("ExpandEnv: %v", err)
	}
	if got != "postgres://u:p@host:5432/db" {
		t.Errorf("ExpandEnv = %q", got)
	}

	got, err = ExpandEnv("postgres://$DB_USER@host/db", lookupFrom(vars))
	if err != nil {
		t.Fatalf("ExpandEnv: %v", err)
	}
	if got != "postgres://appuser@host/db" {
		t.Errorf("ExpandEnv = %q", got)
	}
}

func TestExpandEnvUnresolvedReferenceIsAnError(t *testing.T) {
	// A missing variable must NOT expand to an empty string: a template that
	// silently lost its password would connect to the wrong place and look
	// like it worked.
	_, err := ExpandEnv("postgres://user:${DB_PASS}@host/db", lookupFrom(nil))
	if err == nil {
		t.Fatal("ExpandEnv with an unresolved variable: want an error, got nil")
	}
	if !strings.Contains(err.Error(), "DB_PASS") {
		t.Errorf("error %q does not name the missing variable", err)
	}
}

func TestExpandEnvEmptyValueIsNotAnError(t *testing.T) {
	got, err := ExpandEnv("postgres://user:${DB_PASS}@host/db", lookupFrom(map[string]string{"DB_PASS": ""}))
	if err != nil {
		t.Fatalf("ExpandEnv: %v", err)
	}
	if got != "postgres://user:@host/db" {
		t.Errorf("ExpandEnv = %q", got)
	}
}

func TestExpandEnvLeavesAnUnresolvedBareReferenceAlone(t *testing.T) {
	// A bare "$NAME" that does not resolve is LITERAL text, not an error.
	// This is what keeps a Postgres URL whose password contains a dollar
	// sign working: "postgres://app:p$ssword@host/db" must survive intact
	// when "ssword" is not set.
	const raw = "postgres://app:p$ssword@host/db"
	got, err := ExpandEnv(raw, lookupFrom(nil))
	if err != nil {
		t.Fatalf("ExpandEnv: %v", err)
	}
	if got != raw {
		t.Fatalf("ExpandEnv = %q, want %q unchanged", got, raw)
	}
}

func TestExpandEnvResolvesABareReferenceWhenItIsSet(t *testing.T) {
	got, err := ExpandEnv("postgres://app:$PASS@host/db", lookupFrom(map[string]string{"PASS": "secret"}))
	if err != nil {
		t.Fatalf("ExpandEnv: %v", err)
	}
	if got != "postgres://app:secret@host/db" {
		t.Fatalf("ExpandEnv = %q", got)
	}
}

func TestExpandEnvABracedReferenceStillErrorsWhenUnset(t *testing.T) {
	// A braced reference is an explicit opt-in, so an unset one is an error
	// even though a bare one is not.
	_, err := ExpandEnv("postgres://app:${PASS}@host/db", lookupFrom(nil))
	if err == nil {
		t.Fatal("ExpandEnv with an unset braced reference: want an error, got nil")
	}
	if !strings.Contains(err.Error(), "PASS") {
		t.Errorf("error %q does not name the variable", err)
	}
}

func TestExpandEnvLeavesNonReferencesAlone(t *testing.T) {
	vars := map[string]string{"A": "1"}
	for _, tmpl := range []string{"cost $5", "trailing $", "$ ", "$-x", "${unterminated", "100$"} {
		got, err := ExpandEnv(tmpl, lookupFrom(vars))
		if err != nil {
			t.Errorf("ExpandEnv(%q): %v", tmpl, err)
			continue
		}
		if got != tmpl {
			t.Errorf("ExpandEnv(%q) = %q, want it unchanged", tmpl, got)
		}
	}
}

func TestExpandEnvIsARawTextSubstitution(t *testing.T) {
	// Expansion is a RAW substitution on the template string, before any URL
	// parsing. That is what makes the dominant case work: the value of
	// ${DATABASE_URL} is a complete, already-valid URL, and inserting it
	// verbatim must not re-interpret anything.
	//
	// The corollary is that a value carrying URL-significant characters
	// ("/", "@", ":") lands in the text as-is. A Postgres URL therefore
	// requires the percent-encoded form for such a password, exactly as a
	// hand-written URL does: "%40" is "@" and "%2F" is "/". An UNENCODED "@"
	// in a password is ambiguous in a URL, whether it is typed by hand or
	// substituted.
	const encoded = "p%40ss%2Fw%3Ard"
	got, err := ExpandEnv("postgres://appuser:${SECRET}@db.internal:5432/aims",
		lookupFrom(map[string]string{"SECRET": encoded}))
	if err != nil {
		t.Fatalf("ExpandEnv: %v", err)
	}
	if got != "postgres://appuser:"+encoded+"@db.internal:5432/aims" {
		t.Fatalf("ExpandEnv = %q", got)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", got, err)
	}
	if u.Hostname() != "db.internal" {
		t.Errorf("host = %q, want db.internal", u.Hostname())
	}
	pass, _ := u.User.Password()
	if pass != "p@ss/w:rd" {
		t.Errorf("decoded password = %q, want %q", pass, "p@ss/w:rd")
	}
}

func TestExpandEnvSubstitutesTheWholeURLVerbatim(t *testing.T) {
	// The dominant case: ${DATABASE_URL} holds a complete URL. It must land
	// in the result byte for byte, with no re-interpretation.
	const raw = "postgres://appuser:sup3r%40secret@db.internal:5432/aims?sslmode=require"
	got, err := ExpandEnv("${DATABASE_URL}", lookupFrom(map[string]string{"DATABASE_URL": raw}))
	if err != nil {
		t.Fatalf("ExpandEnv: %v", err)
	}
	if got != raw {
		t.Fatalf("ExpandEnv = %q, want %q", got, raw)
	}
}

func TestExpandEnvWholeURLVariable(t *testing.T) {
	// The dominant case: the variable holds the ENTIRE URL, so the template
	// is exactly "${DATABASE_URL}".
	const raw = "postgres://appuser:sup3r%40secret@db.internal:5432/aims?sslmode=require"
	got, err := ExpandEnv("${DATABASE_URL}", lookupFrom(map[string]string{"DATABASE_URL": raw}))
	if err != nil {
		t.Fatalf("ExpandEnv: %v", err)
	}
	if got != raw {
		t.Fatalf("ExpandEnv = %q, want %q", got, raw)
	}
	if _, err := ParseURL(got); err != nil {
		t.Errorf("ParseURL of the expanded URL: %v", err)
	}
}

func TestExpandEnvComposesSeveralVariables(t *testing.T) {
	vars := map[string]string{
		"DB_USER": "appuser",
		"DB_PASS": "secret",
		"DB_HOST": "db.internal",
		"DB_NAME": "aims",
	}
	got, err := ExpandEnv("postgres://${DB_USER}:${DB_PASS}@${DB_HOST}:5432/${DB_NAME}", lookupFrom(vars))
	if err != nil {
		t.Fatalf("ExpandEnv: %v", err)
	}
	want := "postgres://appuser:secret@db.internal:5432/aims"
	if got != want {
		t.Fatalf("ExpandEnv = %q, want %q", got, want)
	}
	if _, err := ParseURL(got); err != nil {
		t.Errorf("ParseURL of the expanded URL: %v", err)
	}
}

func TestIsTemplate(t *testing.T) {
	// A braced reference is an explicit opt-in: it always makes the value a
	// template, even when the variable is not set.
	yes := []string{"${DATABASE_URL}", "postgres://${U}:${P}@h/db", "${UNSET_BUT_BRACED}"}
	for _, s := range yes {
		if !IsTemplate(s) {
			t.Errorf("IsTemplate(%q) = false, want true", s)
		}
	}
	// A bare reference never makes a value a template, whether or not it
	// resolves. A Postgres password like "p$ssword" or "pa$HOME" is a literal
	// secret, and a file named "we$ird.db" is a plain path.
	no := []string{
		"",
		"postgres://u:p@h/db",
		"cost $5",
		"trailing $",
		"$-",
		"${unterminated",
		"postgres://app:p$ssword@host/db",
		"postgres://app:pa$HOME@db/prod",
		"we$ird.db",
		"$DATABASE_URL",
	}
	for _, s := range no {
		if IsTemplate(s) {
			t.Errorf("IsTemplate(%q) = true, want false", s)
		}
	}
}

func TestExpandEnvDoesNotExpandInsideAnInvalidReference(t *testing.T) {
	// "${1BAD}" is not a valid name: the reference is not recognised, so the
	// text is left alone rather than half-expanded.
	got, err := ExpandEnv("${1BAD}", lookupFrom(map[string]string{"BAD": "x"}))
	if err != nil {
		t.Fatalf("ExpandEnv: %v", err)
	}
	if got != "${1BAD}" {
		t.Errorf("ExpandEnv = %q, want ${1BAD}", got)
	}
}

func TestTemplateHasLiteralSecret(t *testing.T) {
	// A template is stored in the clear, so its password must come entirely
	// from references.
	literal := []string{
		"postgres://app:hunter2@host/db?sslmode=${SM}",
		"postgres://app:pa$HOME@host/db?sslmode=${SM}",
		"postgres://app:${DB_PASS}x@host/db",
		"postgres://${DB_USER}:hunter2@host/db",
		"postgres://app@host/db?sslmode=${SM}&password=hunter2",
		"postgres://app@host/db?sslpassword=hunter2&sslmode=${SM}",
	}
	for _, s := range literal {
		if !TemplateHasLiteralSecret(s) {
			t.Errorf("TemplateHasLiteralSecret(%q) = false, want true", s)
		}
	}
	safe := []string{
		"postgres://app:${DB_PASS}@host/db",
		"postgres://${DB_USER}:${DB_PASS}@host/db",
		"postgres://app@host/db?sslmode=${SM}",
		"postgres://app@${DB_HOST}:${DB_PORT}/db",
		"postgres://app@host/db?password=${DB_PASS}",
		"postgres://app@host/db",
	}
	for _, s := range safe {
		if TemplateHasLiteralSecret(s) {
			t.Errorf("TemplateHasLiteralSecret(%q) = true, want false", s)
		}
	}
}
