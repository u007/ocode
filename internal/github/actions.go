package github

import (
	"fmt"
	"strings"
)

type WorkflowTemplate struct {
	Name    string
	Triggers map[string]interface{}
	Jobs    map[string]Job
}

type Job struct {
	RunsOn string
	Steps  []Step
}

type Step struct {
	Name string
	Uses string
	Run  string
	With map[string]string
}

func GenerateWorkflow(name string, config map[string]interface{}) string {
	var w strings.Builder
	w.WriteString("name: " + name + "\n\n")
	w.WriteString("on:\n")

	switch name {
	case "test":
		w.WriteString("  pull_request:\n")
		w.WriteString("    branches: [main]\n")
		w.WriteString("  push:\n")
		w.WriteString("    branches: [main]\n\n")
	case "lint":
		w.WriteString("  pull_request:\n")
		w.WriteString("    branches: [main]\n\n")
	case "build":
		w.WriteString("  pull_request:\n")
		w.WriteString("    branches: [main]\n")
		w.WriteString("  push:\n")
		w.WriteString("    branches: [main]\n\n")
	case "deploy":
		w.WriteString("  push:\n")
		w.WriteString("    branches: [main]\n\n")
	default:
		if triggers, ok := config["triggers"].(map[string]interface{}); ok {
			for k, v := range triggers {
				w.WriteString(fmt.Sprintf("  %s: %v\n", k, v))
			}
		} else {
			w.WriteString("  pull_request:\n")
			w.WriteString("    branches: [main]\n\n")
		}
	}

	w.WriteString("jobs:\n")
	w.WriteString("  " + strings.ToLower(strings.ReplaceAll(name, " ", "-")) + ":\n")
	w.WriteString("    runs-on: ubuntu-latest\n")
	w.WriteString("    steps:\n")

	steps := generateSteps(name, config)
	for _, step := range steps {
		w.WriteString("      - name: " + step.Name + "\n")
		if step.Uses != "" {
			w.WriteString("        uses: " + step.Uses + "\n")
		}
		if step.Run != "" {
			w.WriteString("        run: |\n")
			for _, line := range strings.Split(step.Run, "\n") {
				w.WriteString("          " + line + "\n")
			}
		}
		if len(step.With) > 0 {
			w.WriteString("        with:\n")
			for k, v := range step.With {
				w.WriteString(fmt.Sprintf("          %s: %s\n", k, v))
			}
		}
	}

	return w.String()
}

func generateSteps(name string, config map[string]interface{}) []Step {
	switch name {
	case "test":
		return []Step{
			{Name: "Checkout", Uses: "actions/checkout@v4"},
			{Name: "Setup Go", Uses: "actions/setup-go@v5", With: map[string]string{"go-version": "1.23"}},
			{Name: "Run tests", Run: "go test ./... -v -cover"},
		}
	case "lint":
		return []Step{
			{Name: "Checkout", Uses: "actions/checkout@v4"},
			{Name: "Setup Go", Uses: "actions/setup-go@v5", With: map[string]string{"go-version": "1.23"}},
			{Name: "Run linter", Uses: "golangci/golangci-lint-action@v6", With: map[string]string{"version": "v1.61"}},
		}
	case "build":
		return []Step{
			{Name: "Checkout", Uses: "actions/checkout@v4"},
			{Name: "Setup Go", Uses: "actions/setup-go@v5", With: map[string]string{"go-version": "1.23"}},
			{Name: "Build", Run: "go build ./..."},
		}
	case "deploy":
		return []Step{
			{Name: "Checkout", Uses: "actions/checkout@v4"},
			{Name: "Setup Go", Uses: "actions/setup-go@v5", With: map[string]string{"go-version": "1.23"}},
			{Name: "Build", Run: "go build -o bin/app ./..."},
			{Name: "Deploy", Run: "echo 'Deploy step not configured'"},
		}
	default:
		if stepsCfg, ok := config["steps"].([]map[string]interface{}); ok {
			var steps []Step
			for _, s := range stepsCfg {
				step := Step{}
				if n, ok := s["name"].(string); ok {
					step.Name = n
				}
				if u, ok := s["uses"].(string); ok {
					step.Uses = u
				}
				if r, ok := s["run"].(string); ok {
					step.Run = r
				}
				if w, ok := s["with"].(map[string]string); ok {
					step.With = w
				}
				steps = append(steps, step)
			}
			return steps
		}
		return []Step{
			{Name: "Checkout", Uses: "actions/checkout@v4"},
			{Name: "Run", Run: "echo 'No steps configured'"},
		}
	}
}
