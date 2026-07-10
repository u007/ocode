package stackdetect

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// write creates root/name with body, making parent dirs as needed.
func write(t *testing.T, root, name, body string) {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDetect(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{
			name:  "empty repo",
			files: map[string]string{},
			want:  nil,
		},
		{
			name:  "go.mod → golang",
			files: map[string]string{"go.mod": "module x\n"},
			want:  []string{"golang"},
		},
		{
			name:  "Cargo.toml → rust",
			files: map[string]string{"Cargo.toml": "[package]\n"},
			want:  []string{"rust"},
		},
		{
			name:  "react dep in dependencies",
			files: map[string]string{"package.json": `{"dependencies":{"react":"^19.0.0"}}`},
			want:  []string{"react"},
		},
		{
			name:  "react dep in devDependencies also counts",
			files: map[string]string{"package.json": `{"devDependencies":{"react":"^18"}}`},
			want:  []string{"react"},
		},
		{
			name: "nextjs via dep implies react too",
			files: map[string]string{
				"package.json": `{"dependencies":{"react":"^19","next":"15.0.0"}}`,
			},
			want: []string{"nextjs", "react"},
		},
		{
			name: "nextjs via config file, no dep",
			files: map[string]string{
				"next.config.mjs": "export default {}\n",
			},
			want: []string{"nextjs"},
		},
		{
			name: "tanstack query OR router (router only)",
			files: map[string]string{
				"package.json": `{"dependencies":{"react":"^19","@tanstack/react-router":"^1"}}`,
			},
			want: []string{"react", "tanstack"},
		},
		{
			name: "tanstack query variant",
			files: map[string]string{
				"package.json": `{"dependencies":{"react":"^19","@tanstack/react-query":"^5"}}`,
			},
			want: []string{"react", "tanstack"},
		},
		{
			name: "malformed package.json still detects file-marker stacks",
			files: map[string]string{
				"package.json": `{ this is not json`,
				"go.mod":       "module y\n",
			},
			want: []string{"golang"},
		},
		{
			name: "polyglot monorepo: go + rust + react",
			files: map[string]string{
				"go.mod":       "module z\n",
				"Cargo.toml":   "[package]\n",
				"package.json": `{"dependencies":{"react":"^19"}}`,
			},
			want: []string{"golang", "react", "rust"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for name, body := range tc.files {
				write(t, root, name, body)
			}
			got := Detect(root)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Detect() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDetect_emptyRoot(t *testing.T) {
	if got := Detect(""); got != nil {
		t.Fatalf("Detect(\"\") = %v, want nil", got)
	}
}

func TestDetect_missingRoot(t *testing.T) {
	// A root that doesn't exist must be treated as "no stacks", not a panic.
	if got := Detect(filepath.Join(t.TempDir(), "does-not-exist")); got != nil {
		t.Fatalf("Detect(missing) = %v, want nil", got)
	}
}
