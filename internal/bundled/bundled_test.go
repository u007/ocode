package bundled

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// TestExtractFromEmbedded verifies that skills and plugins are extracted from
// the embedded FS into the right on-disk layout, and that it handles both the
// root build shape ("skills/...", ".opencode/plugins/...") and the desktop
// build shape ("embedded-assets/skills/...", "embedded-assets/.opencode/...").
func TestExtractFromEmbedded(t *testing.T) {
	src := fstest.MapFS{
		"skills/foo/SKILL.md":                                 &fstest.MapFile{Data: []byte("foo skill")},
		"skills/bar/SKILL.md":                                &fstest.MapFile{Data: []byte("bar skill")},
		".opencode/plugins/orch/plugin.json":                 &fstest.MapFile{Data: []byte(`{"name":"orch"}`)},
		".opencode/plugins/orch/agents/orch.md":              &fstest.MapFile{Data: []byte("orch agent")},
		"embedded-assets/skills/baz/SKILL.md":                &fstest.MapFile{Data: []byte("baz skill")},
		"embedded-assets/.opencode/plugins/x/plugin.json":    &fstest.MapFile{Data: []byte(`{"name":"x"}`)},
		"embedded-assets/.opencode/plugins/x/agents/x.md":    &fstest.MapFile{Data: []byte("x agent")},
	}

	skillsTarget := t.TempDir()
	pluginsTarget := t.TempDir()
	if err := extractFromEmbedded(src, skillsTarget, pluginsTarget); err != nil {
		t.Fatalf("extractFromEmbedded: %v", err)
	}

	check := func(rel, want string) {
		b, err := os.ReadFile(filepath.Join(skillsTarget, rel))
		if err != nil {
			b, err = os.ReadFile(filepath.Join(pluginsTarget, rel))
		}
		if err != nil {
			t.Fatalf("missing extracted file %s: %v", rel, err)
		}
		if string(b) != want {
			t.Fatalf("%s = %q, want %q", rel, b, want)
		}
	}

	// root build shape
	check("foo/SKILL.md", "foo skill")
	check("bar/SKILL.md", "bar skill")
	check("orch/plugin.json", `{"name":"orch"}`)
	check("orch/agents/orch.md", "orch agent")
	// desktop build shape (embedded-assets/ stripped)
	check("baz/SKILL.md", "baz skill")
	check("x/plugin.json", `{"name":"x"}`)
	check("x/agents/x.md", "x agent")
}

// TestExtractFromEmbeddedIgnoresUnrelated confirms only skills/ and
// .opencode/plugins/ entries are extracted.
func TestExtractFromEmbeddedIgnoresUnrelated(t *testing.T) {
	src := fstest.MapFS{
		"skills/foo/SKILL.md":        &fstest.MapFile{Data: []byte("x")},
		"README.md":                  &fstest.MapFile{Data: []byte("y")},
		"deepseek-v4-flash.OCODE.md": &fstest.MapFile{Data: []byte("z")},
	}
	skillsTarget := t.TempDir()
	pluginsTarget := t.TempDir()
	if err := extractFromEmbedded(src, skillsTarget, pluginsTarget); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(skillsTarget, "README.md")); !os.IsNotExist(err) {
		t.Fatal("README.md should not be extracted")
	}
	if _, err := os.Stat(filepath.Join(skillsTarget, "deepseek-v4-flash.OCODE.md")); !os.IsNotExist(err) {
		t.Fatal("model config should not be extracted as a skill")
	}
}
