package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// A module import is never an effect; the imported FILE's calls are. These
// cases pin the extraction that lets the judges see those calls. The parser is
// advisory (a miss only leaves the pre-existing deferral), so the assertions
// cover the shapes a `python3 -c` one-liner actually produces.
func TestPythonImportedModules(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{"plain", "import a", []string{"a"}},
		{"dotted-with-alias-and-list", "import a.b as c, d", []string{"a.b", "d"}},
		// The reported command: two statements on one -c line.
		{"one-line-two-statements", "import playwright, sys; print(1)", []string{"playwright", "sys"}},
		{"from-dotted-imports-submodule", "from a.b import c", []string{"a.b", "a.b.c"}},
		{"from-parenthesised", "from a.b import (c)", []string{"a.b", "a.b.c"}},
		{"from-without-names", "from a.b import c as d", []string{"a.b", "a.b.c"}},
		// A relative import needs the enclosing package to resolve; a wrong
		// resolution would mis-attribute a file to the judge.
		{"relative-skipped", "from . import x", nil},
		{"comment-ignored", "# import evil\nprint(1)", nil},
		{"triple-quoted-body-ignored", "doc = '''\nimport evil\n'''\nprint(doc)", nil},
		{"identifier-not-a-statement", "imported_flag = 1", nil},
		{"mid-line-string", "print(\"import os\")", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pythonImportedModules(tc.src)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("pythonImportedModules(%q) = %v, want %v", tc.src, got, tc.want)
			}
		})
	}
}

// The scanner must also handle the heredoc form the agent actually uses, where
// the import is on its own line and the body is indented.
func TestPythonImportedModulesHeredocBody(t *testing.T) {
	src := "import io, sys\nfrom pathlib import Path\n\ndef main():\n    import json\n    return json.dumps({})\n"
	got := pythonImportedModules(src)
	want := []string{"io", "json", "pathlib", "pathlib.Path", "sys"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestImportedModuleScriptsForJudgeShipsProjectModules(t *testing.T) {
	a, root := newConsultAgent(t)
	helper := filepath.Join(root, "helper.py")
	if err := os.WriteFile(helper, []byte("import requests\nrequests.post('http://example.com', data={'k': 1})\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	initFile := filepath.Join(root, "pkg", "__init__.py")
	if err := os.WriteFile(initFile, []byte("VALUE = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := a.importedModuleScriptsForJudge("python", "import helper\nfrom pkg import VALUE", root, 8)
	if len(got) != 2 {
		t.Fatalf("expected helper.py and pkg/__init__.py, got %+v", got)
	}
	if got[0].Path != helper {
		t.Fatalf("first entry = %q, want %q", got[0].Path, helper)
	}
	if !strings.Contains(got[0].Text, "requests.post") {
		t.Fatalf("helper source was not shipped:\n%s", got[0].Text)
	}
	if got[1].Path != initFile {
		t.Fatalf("second entry = %q, want %q", got[1].Path, initFile)
	}
}

// An INSTALLED package resolves to no project file: it must stay inert rather
// than being reported as unknown. This is the reported bug (import playwright).
func TestImportedModuleScriptsForJudgeLeavesInstalledPackagesAlone(t *testing.T) {
	a, root := newConsultAgent(t)
	got := a.importedModuleScriptsForJudge("python", "import playwright, sys; print(playwright.__version__)", root, 8)
	if len(got) != 0 {
		t.Fatalf("expected nothing shipped for installed packages, got %+v", got)
	}
}

func TestImportedModuleScriptsForJudgeSkipsSymlinkedModule(t *testing.T) {
	a, root := newConsultAgent(t)
	secret := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(secret, []byte("TOKEN=super-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "helper.py")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if got := a.importedModuleScriptsForJudge("python", "import helper", root, 8); len(got) != 0 {
		t.Fatalf("a symlinked module must not be shipped, got %+v", got)
	}
}

func TestImportedModuleScriptsForJudgeRespectsBudgetAndLanguage(t *testing.T) {
	a, root := newConsultAgent(t)
	for _, name := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(root, name+".py"), []byte("X = 1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := a.importedModuleScriptsForJudge("python", "import a\nimport b", root, 1); len(got) != 1 {
		t.Fatalf("budget 1 should ship one module, got %+v", got)
	}
	if got := a.importedModuleScriptsForJudge("python", "import a", root, 0); got != nil {
		t.Fatalf("budget 0 should ship nothing, got %+v", got)
	}
	if got := a.importedModuleScriptsForJudge("javascript", "import a from './a.js'", root, 8); got != nil {
		t.Fatalf("non-python languages are not scanned yet, got %+v", got)
	}
}

// End to end through the state builder: this is what the judge actually
// receives, so the imported module's source has to be in executed_scripts and
// must not duplicate the interpreter entrypoint.
func TestTypesafeJudgeStateShipsImportedModuleSource(t *testing.T) {
	a, root := newConsultAgent(t)
	if err := os.WriteFile(filepath.Join(root, "helper.py"), []byte("import requests\nrequests.post('http://example.com')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	args, err := json.Marshal(map[string]string{"command": `python3 -c "import helper"`})
	if err != nil {
		t.Fatal(err)
	}
	state := a.buildTypesafePermissionState("bash", args, nil)
	raw, err := json.Marshal(state["executed_scripts"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "helper.py") || !strings.Contains(string(raw), "requests.post") {
		t.Fatalf("imported module source missing from executed_scripts: %s", raw)
	}
}

// When the command runs a script that itself imports a sibling module, both the
// entrypoint and the import travel; the entrypoint is emitted once.
func TestTypesafeJudgeStateDoesNotDoubleShipInterpreterEntrypoint(t *testing.T) {
	a, root := newConsultAgent(t)
	src := "import sibling\nprint('x')\n"
	if err := os.WriteFile(filepath.Join(root, "main.py"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sibling.py"), []byte("Y = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	args, err := json.Marshal(map[string]string{"command": "python3 main.py"})
	if err != nil {
		t.Fatal(err)
	}
	state := a.buildTypesafePermissionState("bash", args, nil)
	raw, err := json.Marshal(state["executed_scripts"])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), "main.py") != 1 {
		t.Fatalf("entrypoint shipped more than once: %s", raw)
	}
	if !strings.Contains(string(raw), "sibling.py") {
		t.Fatalf("sibling import not shipped: %s", raw)
	}
}
