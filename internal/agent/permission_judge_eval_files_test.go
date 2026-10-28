package agent

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// evalCorpusDir must match permissionJudgeEvalDir in the integration-tagged
// permission_judge_eval_test.go, which this file cannot import.
const evalCorpusDir = "testdata/permission_judge_eval"

// The live judge eval (permission_judge_eval_test.go) is integration-tagged and
// billed, so a malformed case file or a duplicated id was only discovered by
// whoever next ran `OCODE_JEV_EVAL=1`. This parses the committed corpus without
// any network access: it is the guard for the FILES, not for the judge.
func TestPermissionJudgeEvalCorpusParses(t *testing.T) {
	for _, name := range []string{"must_ask.yaml", "should_allow.yaml"} {
		path := filepath.Join(evalCorpusDir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		var doc struct {
			WorkDir string `yaml:"working_directory"`
			Cases   []struct {
				ID      string `yaml:"id"`
				Command string `yaml:"command"`
			} `yaml:"cases"`
		}
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s does not parse: %v", name, err)
		}
		if doc.WorkDir == "" {
			t.Fatalf("%s has no working_directory", name)
		}
		if len(doc.Cases) == 0 {
			t.Fatalf("%s has no cases", name)
		}
	}
}

// Ids address a case in the scorecard and in review notes, so a duplicate makes
// one of the two cases unaddressable and silently drops it from analysis.
func TestPermissionJudgeEvalCorpusIDsAreUnique(t *testing.T) {
	seen := make(map[string]string)
	for _, name := range []string{"must_ask.yaml", "should_allow.yaml"} {
		raw, err := os.ReadFile(filepath.Join(evalCorpusDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		var doc struct {
			Cases []struct {
				ID      string `yaml:"id"`
				Command string `yaml:"command"`
			} `yaml:"cases"`
		}
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s does not parse: %v", name, err)
		}
		for _, c := range doc.Cases {
			if c.ID == "" || c.Command == "" {
				t.Fatalf("%s: case with empty id or command (%+v)", name, c)
			}
			if prev, dup := seen[c.ID]; dup {
				t.Fatalf("duplicate case id %q in %s and %s", c.ID, prev, name)
			}
			seen[c.ID] = name
		}
	}
}
