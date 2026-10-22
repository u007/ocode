package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type spliceVal struct {
	Disabled []string `json:"disabled,omitempty"`
}

func TestSpliceTopLevelKey(t *testing.T) {
	v := spliceVal{Disabled: []string{"a", "b"}}
	for _, tc := range []struct {
		name, in, want string
		remove         bool
	}{
		{
			name: "replace keeps everything else byte for byte",
			in:   "{\n    \"zeta\": {\"k\":   1},\n    \"skills\": {\"disabled\": [\"old\"]},\n    \"alpha\": [1,2,  3]\n}\n",
			want: "{\n    \"zeta\": {\"k\":   1},\n    \"skills\": {\n      \"disabled\": [\n        \"a\",\n        \"b\"\n      ]\n    },\n    \"alpha\": [1,2,  3]\n}\n",
		},
		{
			name: "insert appends after the last member with its indent",
			in:   "{\n\t\"zeta\": 1\n}\n",
			want: "{\n\t\"zeta\": 1,\n\t\"skills\": {\n\t  \"disabled\": [\n\t    \"a\",\n\t    \"b\"\n\t  ]\n\t}\n}\n",
		},
		{
			name: "insert into single-line object",
			in:   `{"zeta": 1}`,
			want: `{"zeta": 1, "skills": {"disabled":["a","b"]}}`,
		},
		{
			name: "insert into empty object",
			in:   "{}",
			want: "{\n  \"skills\": {\n    \"disabled\": [\n      \"a\",\n      \"b\"\n    ]\n  }\n}",
		},
		{
			name:   "remove middle member",
			in:     "{\n  \"a\": 1,\n  \"skills\": {\"disabled\": [\"x\"]},\n  \"b\": 2\n}\n",
			want:   "{\n  \"a\": 1,\n  \"b\": 2\n}\n",
			remove: true,
		},
		{
			name:   "remove last member drops the dangling comma",
			in:     "{\n  \"a\": 1,\n  \"skills\": {\"disabled\": [\"x\"]}\n}\n",
			want:   "{\n  \"a\": 1\n}\n",
			remove: true,
		},
		{
			name:   "remove only member",
			in:     "{\n  \"skills\": {}\n}\n",
			want:   "{}\n",
			remove: true,
		},
		{
			name:   "remove absent key is a no-op",
			in:     "{\n  \"a\": 1\n}\n",
			want:   "{\n  \"a\": 1\n}\n",
			remove: true,
		},
		{
			name: "comments and url strings survive",
			in:   "{\n  // keep me\n  \"url\": \"https://x.test/a\", /* and me */\n  \"skills\": {\"disabled\": [\"old\"]}\n}\n",
			want: "{\n  // keep me\n  \"url\": \"https://x.test/a\", /* and me */\n  \"skills\": {\n    \"disabled\": [\n      \"a\",\n      \"b\"\n    ]\n  }\n}\n",
		},
		{
			name: "escaped quote in an earlier key",
			in:   "{\n  \"we\\\"ird\": 1,\n  \"skills\": 0\n}\n",
			want: "{\n  \"we\\\"ird\": 1,\n  \"skills\": {\n    \"disabled\": [\n      \"a\",\n      \"b\"\n    ]\n  }\n}\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := spliceTopLevelKey([]byte(tc.in), "skills", v, tc.remove)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("got:\n%q\nwant:\n%q", got, tc.want)
			}
			// Whatever we produce must still be valid JSON once comments go.
			var m map[string]json.RawMessage
			if err := json.Unmarshal(blankJSONCComments(got), &m); err != nil {
				t.Fatalf("result does not parse: %v\n%s", err, got)
			}
		})
	}
}

func TestSpliceTopLevelKeyRejectsMalformed(t *testing.T) {
	for _, in := range []string{`[1,2]`, `{"a": }`, `{"a": 1,}`, `not json`} {
		if _, err := spliceTopLevelKey([]byte(in), "skills", spliceVal{}, false); err == nil {
			t.Errorf("expected an error for %q", in)
		}
	}
}

func TestBlankJSONCCommentsPreservesOffsets(t *testing.T) {
	in := []byte("{\n // c1 \"x\"\n \"u\": \"http://a//b\", /* m\nl */ \"k\": 1\n}")
	out := blankJSONCComments(in)
	if len(out) != len(in) {
		t.Fatalf("length changed: %d -> %d", len(in), len(out))
	}
	if strings.Count(string(out), "\n") != strings.Count(string(in), "\n") {
		t.Fatal("newlines must be preserved")
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil || m["u"] != "http://a//b" || m["k"] != float64(1) {
		t.Fatalf("blanked output parse: %v %v", m, err)
	}
}

// End to end through the real save path: a hand-formatted settings file with a
// comment is toggled and only the skills lines change.
func TestSaveProjectSkillPrefsPreservesFileFormatting(t *testing.T) {
	root := t.TempDir()
	path := ProjectSettingsPathForRoot(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := "{\n  // project allow-list\n  \"extra_allowed_paths\": [ \"/srv/a\",\n                           \"/srv/b\" ],\n  \"aaa\": true\n}\n"
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SaveSkillEnabled(root, "pdf", false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	got, _ := os.ReadFile(path)
	prefix := strings.TrimSuffix(orig, "\n}\n") + ",\n  \"skills\":"
	if !strings.HasPrefix(string(got), prefix) {
		t.Fatalf("existing bytes were rewritten:\n%s", got)
	}
	prefs, err := loadProjectSkillPrefs(root)
	if err != nil || len(prefs.Disabled) != 1 || prefs.Disabled[0] != "pdf" {
		t.Fatalf("prefs after disable = %+v, %v", prefs, err)
	}

	if err := SaveSkillEnabled(root, "pdf", true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	got, _ = os.ReadFile(path)
	if !strings.HasPrefix(string(got), prefix) {
		t.Fatalf("existing bytes were rewritten on re-enable:\n%s", got)
	}
}

func TestSaveProjectSkillPrefsDropsEmptyBlockAndLeavesFileAsFound(t *testing.T) {
	root := t.TempDir()
	path := ProjectSettingsPathForRoot(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := "{\n  \"b\": 1,\n  \"a\": 2\n}\n"
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveSkillEnabled(root, "x", false); err != nil {
		t.Fatal(err)
	}
	// Enabling adds x to "enabled" and removes it from "disabled"; clear both.
	if err := saveProjectSkillPrefs(root, SkillsConfig{}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != orig {
		t.Fatalf("file not restored to its original bytes:\n%q", got)
	}
}

func TestSaveProjectSkillPrefsRefusesMalformedSettings(t *testing.T) {
	root := t.TempDir()
	path := ProjectSettingsPathForRoot(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	bad := "{\"a\": 1,}\n"
	if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveSkillEnabled(root, "x", false); err == nil {
		t.Fatal("expected an error for a malformed settings file")
	}
	if got, _ := os.ReadFile(path); string(got) != bad {
		t.Fatalf("malformed file was modified: %q", got)
	}
}
