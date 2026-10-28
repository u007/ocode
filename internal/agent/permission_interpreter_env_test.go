package agent

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestPythonReferencedEnv(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{"subscript-single", `u = os.environ['API_URL']`, []string{"API_URL"}},
		{"subscript-double", `u = os.environ["API_URL"]`, []string{"API_URL"}},
		{"get", `u = os.environ.get('API_URL', 'http://127.0.0.1:1')`, []string{"API_URL"}},
		{"getenv", `u = os.getenv("BASE")`, []string{"BASE"}},
		{"bare-environ", `u = environ['API_URL']`, []string{"API_URL"}},
		// A computed name cannot be resolved, so it must not be claimed.
		{"computed-name-ignored", `u = os.environ[key]`, nil},
		{"dynamic-get-ignored", `u = os.environ.get(key)`, nil},
		{"docstring-ignored", "doc = '''os.environ['HIDDEN']'''\n", nil},
		{"sorted-deduped", `a = os.environ['B']; b = os.environ['A']; c = os.environ['B']`, []string{"A", "B"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pythonReferencedEnv(tc.src)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("pythonReferencedEnv(%q) = %v, want %v", tc.src, got, tc.want)
			}
		})
	}
}

// stubJudgeEnv swaps the expander's environment lookup for the duration of a test.
func stubJudgeEnv(t *testing.T, values map[string]string) {
	t.Helper()
	prev := judgeLookupEnv
	judgeLookupEnv = func(name string) (string, bool) {
		v, ok := values[name]
		return v, ok
	}
	t.Cleanup(func() { judgeLookupEnv = prev })
}

func TestInterpreterResolvedEnvResolvesLoopbackAndWithholdsSecrets(t *testing.T) {
	a, _ := newConsultAgent(t)
	stubJudgeEnv(t, map[string]string{
		"API_URL":             "http://127.0.0.1:8080",
		"REMOTE_URL":          "https://example.com",
		"API_KEY":             "sk-live-abc123",
		"INNOCENT":            "http://user:pass@127.0.0.1:8080",
		"PWD":                 "/somewhere",
		"UNSET_BY_COMMAND":    "http://127.0.0.1:1",
		"ASSIGNED_IN_COMMAND": "",
	})
	src := strings.Join([]string{
		`import os, requests`,
		`a = os.environ['API_URL']`,
		`b = os.environ['REMOTE_URL']`,
		`c = os.environ['API_KEY']`,
		`d = os.environ['INNOCENT']`,
		`e = os.environ['PWD']`,
		`f = os.environ['UNSET_BY_COMMAND']`,
		`g = os.environ['ASSIGNED_IN_COMMAND']`,
	}, "\n")
	got := a.interpreterResolvedEnv("unset UNSET_BY_COMMAND; ASSIGNED_IN_COMMAND= python3 -c x", src, nil)

	byName := map[string]resolvedShellVar{}
	for _, v := range got {
		byName[v.Name] = v
	}
	if v, ok := byName["API_URL"]; !ok || v.Value != "http://127.0.0.1:8080" {
		t.Fatalf("API_URL not resolved: %+v", got)
	}
	if v := byName["REMOTE_URL"]; v.Value != "https://example.com" {
		t.Fatalf("REMOTE_URL = %q, want it resolved (so the judge can judge the destination)", v.Value)
	}
	// Secret-looking NAME and a secret-looking VALUE are both withheld.
	if v := byName["API_KEY"]; v.Value != redactedShellValue {
		t.Fatalf("API_KEY = %q, want %q", v.Value, redactedShellValue)
	}
	if v := byName["INNOCENT"]; v.Value != redactedShellValue {
		t.Fatalf("userinfo value = %q, want %q", v.Value, redactedShellValue)
	}
	// PWD describes ocode's process cwd, never the child's.
	if _, ok := byName["PWD"]; ok {
		t.Fatal("PWD must not be resolved")
	}
	// A name the command itself touches must not fall back to the process env.
	if _, ok := byName["UNSET_BY_COMMAND"]; ok {
		t.Fatal("a name the command unsets must not be resolved from the environment")
	}
	if _, ok := byName["ASSIGNED_IN_COMMAND"]; ok {
		t.Fatal("a name the command assigns/unsets must not be resolved from the environment")
	}
}

// A NAME=value prefix on the same command is authoritative and comes from the
// shell expander's assignment record, not the process environment.
func TestInterpreterResolvedEnvPrefersSameCommandAssignment(t *testing.T) {
	a, _ := newConsultAgent(t)
	stubJudgeEnv(t, map[string]string{
		"API_URL": "https://example.com",
	})
	shellVars := []resolvedShellVar{{Name: "API_URL", Value: "http://127.0.0.1:8080", Source: "assignment"}}
	got := a.interpreterResolvedEnv("API_URL=http://127.0.0.1:8080 python3 -c x", "u = os.environ['API_URL']", shellVars)
	if len(got) != 1 || got[0].Value != "http://127.0.0.1:8080" || got[0].Source != "assignment" {
		t.Fatalf("got %+v, want the same-command assignment", got)
	}
}

// End to end: the judge state carries interpreter.resolved_env, and a secret is
// never sent in the clear.
func TestTypesafeJudgeStateCarriesResolvedEnv(t *testing.T) {
	a, _ := newConsultAgent(t)
	stubJudgeEnv(t, map[string]string{
		"API_URL": "http://127.0.0.1:8080",
		"API_KEY": "sk-live-abc123",
	})
	args, err := json.Marshal(map[string]string{"command": `python3 -c "import os, requests; print(requests.get(os.environ['API_URL'], headers={'k': os.environ['API_KEY']}).status_code)"`})
	if err != nil {
		t.Fatal(err)
	}
	state := a.buildTypesafePermissionState("bash", args, nil)
	interp, ok := state["interpreter"].(map[string]any)
	if !ok {
		t.Fatalf("no interpreter block: %+v", state)
	}
	raw, err := json.Marshal(interp["resolved_env"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "http://127.0.0.1:8080") {
		t.Fatalf("loopback URL missing from resolved_env: %s", raw)
	}
	if strings.Contains(string(raw), "sk-live-abc123") {
		t.Fatalf("secret value leaked into the judge state: %s", raw)
	}
	// encoding/json escapes < and >, so match the payload rather than the marker.
	if !strings.Contains(string(raw), "redacted") || !strings.Contains(string(raw), "API_KEY") {
		t.Fatalf("API_KEY should be present but redacted: %s", raw)
	}
}

// With nothing referenced there is no fact at all, so the judge keeps the
// pre-existing "not proven" rule.
func TestInterpreterResolvedEnvEmptyWhenUnreferenced(t *testing.T) {
	a, _ := newConsultAgent(t)
	if got := a.interpreterResolvedEnv("", "print(1)", nil); got != nil {
		t.Fatalf("unexpected env facts: %+v", got)
	}
	_ = os.Getenv("PATH")
}
