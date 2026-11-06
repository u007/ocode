package tool

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

var viewerControlIDPattern = regexp.MustCompile(`^vc_[0-9a-f]{8}$`)

// viewerControlPayload runs Execute on raw JSON args and returns the result.
func viewerControlPayload(t *testing.T, raw string) (string, error) {
	t.Helper()
	return (&ViewerControlTool{}).Execute(json.RawMessage(raw))
}

// TestViewerControlHappyPath: each action yields one sentinel-prefixed,
// single-line JSON directive with a vc_ id, and echoes exactly the provided
// parameters.
func TestViewerControlHappyPath(t *testing.T) {
	cases := []struct {
		name string
		args string
	}{
		{"scroll page", `{"action":"scroll","page":3}`},
		{"scroll slide", `{"action":"scroll","slide":2}`},
		{"scroll line", `{"action":"scroll","line":10}`},
		{"scroll heading", `{"action":"scroll","heading":"Install"}`},
		{"scroll direction", `{"action":"scroll","direction":"down"}`},
		{"select text", `{"action":"select","text":"needle"}`},
		{"select lines", `{"action":"select","lines":{"start":2,"end":5}}`},
		{"select range", `{"action":"select","range":"Sheet1!A1:C3"}`},
		{"find", `{"action":"find","query":"TODO"}`},
		{"find_next", `{"action":"find_next"}`},
		{"find_prev", `{"action":"find_prev"}`},
		{"find_close", `{"action":"find_close"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := viewerControlPayload(t, tc.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.HasPrefix(got, ViewerControlSentinel) {
				t.Fatalf("result %q missing sentinel", got)
			}
			body := strings.TrimPrefix(got, ViewerControlSentinel)
			if strings.Contains(body, "\n") {
				t.Fatalf("payload must be single-line: %q", body)
			}
			var directive map[string]interface{}
			if err := json.Unmarshal([]byte(body), &directive); err != nil {
				t.Fatalf("payload is not JSON: %v", err)
			}
			id, _ := directive["id"].(string)
			if !viewerControlIDPattern.MatchString(id) {
				t.Fatalf("id %q does not match vc_ + 8 hex", id)
			}
			delete(directive, "id")

			var want map[string]interface{}
			if err := json.Unmarshal([]byte(tc.args), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(directive, want) {
				t.Fatalf("directive fields = %v, want %v", directive, want)
			}
		})
	}
}

// TestViewerControlIDsDiffer: two calls never share an id, so the UI can
// apply each one once.
func TestViewerControlIDsDiffer(t *testing.T) {
	a, err := viewerControlPayload(t, `{"action":"find_next"}`)
	if err != nil {
		t.Fatal(err)
	}
	b, err := viewerControlPayload(t, `{"action":"find_next"}`)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatalf("two calls produced identical directives: %q", a)
	}
}

// TestViewerControlRejects: every violation is an error with no directive.
func TestViewerControlRejects(t *testing.T) {
	cases := []struct {
		name string
		args string
	}{
		{"invalid json", `{"action":`},
		{"wrong type for page", `{"action":"scroll","page":"3"}`},
		{"fractional page", `{"action":"scroll","page":1.5}`},
		{"unknown field", `{"action":"find_next","bogus":1}`},
		{"missing action", `{"page":1}`},
		{"empty action", `{"action":"","page":1}`},
		{"unknown action", `{"action":"zoom"}`},
		{"scroll with nothing", `{"action":"scroll"}`},
		{"scroll with two targets", `{"action":"scroll","page":1,"line":2}`},
		{"scroll with a select param", `{"action":"scroll","page":1,"query":"x"}`},
		{"scroll bad direction", `{"action":"scroll","direction":"left"}`},
		{"scroll page zero", `{"action":"scroll","page":0}`},
		{"scroll page negative", `{"action":"scroll","page":-2}`},
		{"scroll slide zero", `{"action":"scroll","slide":0}`},
		{"scroll line zero", `{"action":"scroll","line":0}`},
		{"scroll blank heading", `{"action":"scroll","heading":"  "}`},
		{"select with nothing", `{"action":"select"}`},
		{"select with two targets", `{"action":"select","text":"x","range":"A1"}`},
		{"select with a scroll param", `{"action":"select","text":"x","page":1}`},
		{"select blank text", `{"action":"select","text":""}`},
		{"select lines start after end", `{"action":"select","lines":{"start":5,"end":2}}`},
		{"select lines missing end", `{"action":"select","lines":{"start":2}}`},
		{"select lines start zero", `{"action":"select","lines":{"start":0,"end":2}}`},
		{"select lines unknown key", `{"action":"select","lines":{"start":1,"end":2,"sheet":"x"}}`},
		{"find without query", `{"action":"find"}`},
		{"find blank query", `{"action":"find","query":" "}`},
		{"find_next with query", `{"action":"find_next","query":"x"}`},
		{"find_prev with page", `{"action":"find_prev","page":1}`},
		{"find_close with direction", `{"action":"find_close","direction":"up"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := viewerControlPayload(t, tc.args)
			if err == nil {
				t.Fatalf("expected error, got directive %q", got)
			}
			if got != "" {
				t.Fatalf("error path must return empty result, got %q", got)
			}
		})
	}
}

// TestViewerControlDefinitionIsFlatAndSequential: the tool is sequential and
// uses the flat descriptor shape the Anthropic transport reads directly.
func TestViewerControlDefinitionIsFlatAndSequential(t *testing.T) {
	tl := &ViewerControlTool{}
	if tl.Parallel() {
		t.Error("viewer_control must not run in parallel")
	}
	def := tl.Definition()
	if def["name"] != "viewer_control" {
		t.Errorf("definition name = %v, want viewer_control", def["name"])
	}
	params, ok := def["parameters"].(map[string]interface{})
	if !ok {
		t.Fatalf("parameters missing or wrong type: %T", def["parameters"])
	}
	if !reflect.DeepEqual(params["required"], []string{"action"}) {
		t.Errorf("required = %v, want [action]", params["required"])
	}
}
