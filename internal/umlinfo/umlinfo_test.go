package umlinfo

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

const fixture = `// Package demo is a tiny fixture for the umlinfo analyzer.
package demo

import (
	"fmt"

	"github.com/example/demo/internal/dep"
)

// Greeter greets callers. It is the main interface.
//
// More detail after a blank line that must be dropped.
type Greeter interface {
	// Greet says hello.
	Greet(name string) string
	String() string
}

// Person is a human.
type Person struct {
	// Name is the display name.
	Name string
	age  int
	dep.Dep
}

// Greet implements Greeter.
func (p *Person) Greet(name string) string {
	return fmt.Sprintf("hello %s", name)
}

// NewPerson returns a Person.
func NewPerson(name string) *Person { return &Person{Name: name} }
`

const depFixture = `// Package dep is a dependency of demo.
package dep

// Dep is an embedded dependency.
type Dep struct{ X int }
`

func writeFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module github.com/example/demo\n\ngo 1.26\n")
	writeFile(t, root, "demo.go", fixture)
	writeFile(t, root, "internal/dep/dep.go", depFixture)
	return root
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildIndexGoSymbols(t *testing.T) {
	root := writeFixture(t)
	idx, err := BuildIndex(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Symbol{}
	for _, s := range idx.Symbols {
		byName[s.Name] = s
	}

	greet, ok := byName["Greeter"]
	if !ok {
		t.Fatalf("Greeter interface not found in %+v", idx.Symbols)
	}
	if greet.Kind != "interface" {
		t.Errorf("Greeter kind = %q, want interface", greet.Kind)
	}
	if got := greet.Doc; got != "Greeter greets callers. It is the main interface." {
		t.Errorf("Greeter doc = %q (first paragraph must be kept, trailing paragraph dropped)", got)
	}
	if len(greet.Members) != 2 {
		t.Errorf("Greeter members = %+v, want 2 methods", greet.Members)
	}

	person := byName["Person"]
	if person.Kind != "struct" {
		t.Fatalf("Person kind = %q", person.Kind)
	}
	if person.File != "demo.go" {
		t.Errorf("Person.File = %q", person.File)
	}
	foundEmbedded := false
	for _, m := range person.Members {
		if m.Kind == "field" && m.Name == "(embedded) dep.Dep" {
			foundEmbedded = true
		}
	}
	if !foundEmbedded {
		t.Errorf("embedded dep.Dep field missing: %+v", person.Members)
	}

	m, ok := byName["Greet"]
	if !ok {
		t.Fatalf("method Greet not found: %+v", idx.Symbols)
	}
	if m.Kind != "method" || m.Receiver != "Person" {
		t.Errorf("Greet kind/receiver = %q/%q, want method/Person", m.Kind, m.Receiver)
	}
	if m.Line == 0 {
		t.Error("Greet.Line not set")
	}

	np := byName["NewPerson"]
	if np.Kind != "func" {
		t.Errorf("NewPerson kind = %q", np.Kind)
	}
	if np.Sig == "" {
		t.Error("NewPerson sig empty")
	}

	foundEdge := false
	for _, e := range idx.Edges {
		if e.From == "." && e.To == "internal/dep" && e.Kind == "import" {
			foundEdge = true
		}
	}
	if !foundEdge {
		t.Errorf("internal/dep import edge missing: %+v", idx.Edges)
	}

	pkgs := map[string]Package{}
	for _, p := range idx.Packages {
		pkgs[p.ID] = p
	}
	if pkgs["internal/dep"].SymbolCount == 0 {
		t.Error("internal/dep package has no symbols")
	}
	if idx.Monorepo != "none" {
		t.Errorf("monorepo = %q, want none (go.mod only)", idx.Monorepo)
	}
	if idx.Truncated {
		t.Error("unexpected truncation")
	}
}

func TestFingerprintStableAndChanges(t *testing.T) {
	root := writeFixture(t)
	ctx := context.Background()
	fp1, err := Fingerprint(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	fp2, err := Fingerprint(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if fp1 != fp2 {
		t.Fatal("fingerprint not stable across calls")
	}
	writeFile(t, root, "demo.go", fixture+"\n// touch\n")
	fp3, err := Fingerprint(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if fp3 == fp1 {
		t.Fatal("fingerprint did not change after edit")
	}
}

func TestIndexerCacheHit(t *testing.T) {
	root := writeFixture(t)
	ctx := context.Background()
	fp, err := Fingerprint(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	idx := NewIndexer()
	first, err := idx.Build(ctx, root, fp)
	if err != nil {
		t.Fatal(err)
	}
	second, ok := idx.Get(root, fp)
	if !ok {
		t.Fatal("cache miss after build")
	}
	if second != first {
		t.Fatal("cache returned a different index instance")
	}
}

func TestIgnoreRespectsGitignore(t *testing.T) {
	root := writeFixture(t)
	writeFile(t, root, ".gitignore", "ignored/\n")
	writeFile(t, root, "ignored/secret.go", "package ignored\n\ntype Secret struct{}\n")
	idx, err := BuildIndex(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range idx.Symbols {
		if s.Name == "Secret" {
			t.Fatal("symbol from gitignored directory leaked into index")
		}
	}
}

func TestBrokenFileIsSkipped(t *testing.T) {
	root := writeFixture(t)
	writeFile(t, root, "broken.go", "package broken\n\nthis is not go\n")
	idx, err := BuildIndex(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	foundPerson := false
	for _, s := range idx.Symbols {
		if s.Name == "Person" {
			foundPerson = true
		}
	}
	if !foundPerson {
		t.Fatal("valid file's symbols lost when a sibling file fails to parse")
	}
}

func TestSymlinkedGoFileIsNotFollowed(t *testing.T) {
	root := writeFixture(t)
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.go")
	if err := os.WriteFile(secret, []byte("package secret\n\n// Leaked is outside the root.\ntype Leaked struct{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "escape.go")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	idx, err := BuildIndex(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range idx.Symbols {
		if s.Name == "Leaked" || s.File == "escape.go" {
			t.Fatalf("symlink outside the root was indexed: %+v", s)
		}
	}
}

func TestFailedBuildKeepsPreviousIndex(t *testing.T) {
	root := writeFixture(t)
	ctx := context.Background()
	fp, err := Fingerprint(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	idx := NewIndexer()
	good, err := idx.Build(ctx, root, fp)
	if err != nil {
		t.Fatal(err)
	}
	// A cancelled build is the deterministic failure both Build and
	// StartBuild route through markFailedLocked.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := idx.Build(cancelled, root, "next"); err == nil {
		t.Fatal("expected the cancelled build to fail")
	}
	idx.mu.Lock()
	kept := idx.entries[root]
	idx.mu.Unlock()
	if kept == nil || kept.index != good {
		t.Fatalf("failed build discarded the previous index (kept=%v)", kept)
	}
	// The old index stays valid for the fingerprint it was built from, but
	// the failed rebuild's fingerprint never reports a hit.
	if got, fresh := idx.Get(root, fp); !fresh || got != good {
		t.Fatal("the previous index should still serve its own fingerprint")
	}
	if _, fresh := idx.Get(root, "next"); fresh {
		t.Fatal("a failed rebuild must not report a hit for the new fingerprint")
	}
	if _, ok := idx.LastError(root); !ok {
		t.Fatal("failure was not recorded")
	}
}
