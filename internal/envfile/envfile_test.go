package envfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseReadsCommonDotenvForms(t *testing.T) {
	const content = `
# a comment
DATABASE_URL=postgres://u:p@host:5432/db

export EXPORTED=yes
PLAIN=simple
SPACED =  padded
QUOTED="double quoted"
SINGLE='single quoted'
EQUALS_IN_VALUE=key=value
EMPTY=
QUOTED_EMPTY=""
`
	got, err := Parse(strings.NewReader(content))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := map[string]string{
		"DATABASE_URL":    "postgres://u:p@host:5432/db",
		"EXPORTED":        "yes",
		"PLAIN":           "simple",
		"SPACED":          "padded",
		"QUOTED":          "double quoted",
		"SINGLE":          "single quoted",
		"EQUALS_IN_VALUE": "key=value",
		"EMPTY":           "",
		"QUOTED_EMPTY":    "",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("Parse()[%q] = %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("Parse() returned %d keys, want %d: %v", len(got), len(want), got)
	}
}

func TestParseKeepsSpecialCharactersInValues(t *testing.T) {
	// A password with URL-significant characters must survive verbatim: the
	// value is substituted into a URL, and a mangled password fails at connect
	// time with a confusing error.
	const content = `PASSWORD=p@ss/w:rd#frag?query
WITH_SPACE="a b c"
`
	got, err := Parse(strings.NewReader(content))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got["PASSWORD"] != "p@ss/w:rd#frag?query" {
		t.Errorf("PASSWORD = %q, want %q", got["PASSWORD"], "p@ss/w:rd#frag?query")
	}
	if got["WITH_SPACE"] != "a b c" {
		t.Errorf("WITH_SPACE = %q, want %q", got["WITH_SPACE"], "a b c")
	}
}

func TestParseUnescapesDoubleQuotedValues(t *testing.T) {
	got, err := Parse(strings.NewReader(`A="line1\nline2"` + "\n" + `B="tab\there"` + "\n" + `C="back\\slash"` + "\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got["A"] != "line1\nline2" {
		t.Errorf("A = %q, want %q", got["A"], "line1\nline2")
	}
	if got["B"] != "tab\there" {
		t.Errorf("B = %q, want %q", got["B"], "tab\there")
	}
	if got["C"] != `back\slash` {
		t.Errorf("C = %q, want %q", got["C"], `back\slash`)
	}
}

func TestParseSingleQuotesAreLiteral(t *testing.T) {
	got, err := Parse(strings.NewReader(`A='no \n escape'` + "\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got["A"] != `no \n escape` {
		t.Errorf("A = %q, want %q", got["A"], `no \n escape`)
	}
}

func TestParseSkipsLinesWithoutAssignment(t *testing.T) {
	got, err := Parse(strings.NewReader("# comment\n\nnot an assignment\n=novalue\nGOOD=1\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 1 || got["GOOD"] != "1" {
		t.Errorf("Parse() = %v, want only GOOD=1", got)
	}
}

func TestParseRejectsDuplicateKey(t *testing.T) {
	_, err := Parse(strings.NewReader("A=1\nA=2\n"))
	if err == nil {
		t.Fatal("Parse() with a duplicate key: want an error, got nil")
	}
	if !strings.Contains(err.Error(), "A") {
		t.Errorf("error %q does not name the duplicate key", err)
	}
}

// A repeated IDENTICAL assignment is not ambiguous, and appending one is how
// real .env files acquire duplicates. Rejecting the whole file over it voids
// every other key, which surfaces as a phantom "environment variable X is not
// set" for a value that is right there.
func TestParseAcceptsAnIdenticalDuplicateKey(t *testing.T) {
	got, err := Parse(strings.NewReader("A=1\nB=2\nA=1\n"))
	if err != nil {
		t.Fatalf("Parse() with an identical duplicate: %v", err)
	}
	if len(got) != 2 || got["A"] != "1" || got["B"] != "2" {
		t.Errorf("Parse() = %v, want A=1 B=2", got)
	}
}

func TestParseQuotedValueMayContainEquals(t *testing.T) {
	got, err := Parse(strings.NewReader(`URL="postgres://u:p@h/db?x=1"` + "\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got["URL"] != "postgres://u:p@h/db?x=1" {
		t.Errorf("URL = %q", got["URL"])
	}
}

func TestLoadAppliesEnvThenEnvLocal(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, FileName, "SHARED=from-dotenv\nONLY_DOTENV=1\n")
	writeFile(t, dir, LocalFileName, "SHARED=from-dotenv-local\n")

	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got["SHARED"] != "from-dotenv-local" {
		t.Errorf("SHARED = %q, want %q (.env.local must win)", got["SHARED"], "from-dotenv-local")
	}
	if got["ONLY_DOTENV"] != "1" {
		t.Errorf("ONLY_DOTENV = %q, want 1", got["ONLY_DOTENV"])
	}
}

func TestLoadMissingFilesAreNotAnError(t *testing.T) {
	got, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load on an empty dir: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Load() = %v, want empty", got)
	}
}

func TestLookupPrefersProcessEnvOverFile(t *testing.T) {
	t.Setenv("OCODE_TEST_ENV_LOOKUP", "from-process")
	files := map[string]string{
		"OCODE_TEST_ENV_LOOKUP": "from-file",
		"ONLY_IN_FILE":          "file-value",
	}
	got, ok := Lookup("OCODE_TEST_ENV_LOOKUP", files)
	if !ok || got != "from-process" {
		t.Errorf("Lookup = (%q, %v), want (from-process, true)", got, ok)
	}
	got, ok = Lookup("ONLY_IN_FILE", files)
	if !ok || got != "file-value" {
		t.Errorf("Lookup = (%q, %v), want (file-value, true)", got, ok)
	}
	if _, ok := Lookup("ABSENT", files); ok {
		t.Error("Lookup of an absent variable reported found")
	}
}

func TestLookupTreatsEmptyProcessValueAsSet(t *testing.T) {
	// An explicitly exported empty value is a deliberate value, not a
	// missing one: the caller must not turn it into a "not set" error.
	t.Setenv("OCODE_TEST_ENV_EMPTY", "")
	if _, ok := Lookup("OCODE_TEST_ENV_EMPTY", nil); !ok {
		t.Error("Lookup of an exported empty variable reported missing")
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// A malformed quoted value must fail the parse, naming the line, and must not
// echo the value. Before this, the line was silently skipped.
func TestParseRejectsMalformedQuotedValue(t *testing.T) {
	bad := map[string]string{
		"unterminated double": `DB_PASS="abc`,
		"unterminated single": `DB_PASS='abc`,
		"lone double quote":   `DB_PASS="`,
		"lone single quote":   `DB_PASS='`,
	}
	for name, line := range bad {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(strings.NewReader("GOOD=1\n" + line + "\n"))
			if err == nil {
				t.Fatalf("Parse(%q): want an error, got nil", line)
			}
			if !strings.Contains(err.Error(), "line 2") {
				t.Errorf("error %q does not name line 2", err)
			}
			if strings.Contains(err.Error(), "abc") {
				t.Errorf("error %q echoes the secret value", err)
			}
		})
	}
}

// An empty quoted value is a real value, not a malformed one.
func TestParseAcceptsEmptyQuotedValue(t *testing.T) {
	got, err := Parse(strings.NewReader("A=\"\"\nB=''\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got["A"] != "" || got["B"] != "" {
		t.Fatalf("got %v, want A and B empty", got)
	}
}

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		in, want string
		wantErr  bool
	}{
		{"", "", false},
		{"staging", ".env.staging", false},
		{".staging", ".env.staging", false},
		{".env.staging", ".env.staging", false},
		{".env", ".env", false},
		{"  production  ", ".env.production", false},
		{"../.env", "", true},
		{"/etc/passwd", "", true},
		{".env.a/b", "", true},
		{"a b", "", true},
		{".env.", "", true},
		{".env.a..b", "", true},
		{".env..", "", true},
		{"..env", "", true},
		{".env.a\\b", "", true},
	}
	for _, tt := range tests {
		got, err := NormalizeName(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("NormalizeName(%q) = %q, want an error", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizeName(%q): %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("NormalizeName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestLoadNamedReadsOnlyTheNamedFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(FileName, "DATABASE_URL=from-base\nBASE_ONLY=yes\n")
	write(LocalFileName, "DATABASE_URL=from-local\n")
	write(".env.staging", "DATABASE_URL=postgres://u:p@staging:5432/db\nSTAGING_ONLY=yes\n")

	got, err := LoadNamed(dir, ".staging")
	if err != nil {
		t.Fatalf("LoadNamed: %v", err)
	}
	if got["DATABASE_URL"] != "postgres://u:p@staging:5432/db" || got["STAGING_ONLY"] != "yes" {
		t.Fatalf("LoadNamed values = %v, want only the staging file", got)
	}
	if _, ok := got["BASE_ONLY"]; ok {
		t.Fatalf("LoadNamed leaked a key from .env: %v", got)
	}
}

func TestLoadNamedMissingFileIsAnError(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadNamed(dir, ".env.production")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("LoadNamed with a missing named file: want fs.ErrNotExist, got %v", err)
	}
}

func TestListSelectsEnvFileNames(t *testing.T) {
	cases := []struct {
		name    string
		listed  bool
		comment string
	}{
		{".env", true, "the committed file"},
		{".env.local", true, "the local override"},
		{".env.production", true, "a named environment"},
		{".env.staging", true, "a named environment"},
		{".env.", false, "empty suffix"},
		{".env.a.b", false, "a dot is not a suffix character"},
		{".env.a b", false, "space is not a suffix character"},
		{"env", false, "no leading dot"},
		{".envrc", false, "a different dotfile"},
		{"README.md", false, "unrelated file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, tc.name, "K=v\n")
			got, err := List(dir)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if tc.listed && (len(got) != 1 || got[0] != tc.name) {
				t.Fatalf("List = %v, want [%s] (%s)", got, tc.name, tc.comment)
			}
			if !tc.listed && len(got) != 0 {
				t.Fatalf("List = %v, want none (%s)", got, tc.comment)
			}
		})
	}
}

func TestListExcludesDirectoriesAndSortsOutput(t *testing.T) {
	dir := t.TempDir()
	// Written out of order: List must sort, not rely on creation order.
	for _, name := range []string{".env.staging", ".env", ".env.production", ".env.local"} {
		writeFile(t, dir, name, "K=v\n")
	}
	// A directory named like an env file is not a file to list.
	if err := os.Mkdir(filepath.Join(dir, ".env.d"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{".env", ".env.local", ".env.production", ".env.staging"}
	if len(got) != len(want) {
		t.Fatalf("List = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("List = %v, want %v", got, want)
		}
	}
}

func TestListEmptyDirReturnsEmptySlice(t *testing.T) {
	got, err := List(t.TempDir())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("List = %#v, want a non-nil empty slice", got)
	}
}

func TestListMissingDirIsAnError(t *testing.T) {
	_, err := List(filepath.Join(t.TempDir(), "absent"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("List on a missing dir: want fs.ErrNotExist, got %v", err)
	}
}
