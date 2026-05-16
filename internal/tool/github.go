package tool

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jamesmercstudio/ocode/internal/github"
)

type GitHubPRTool struct{}

func (t GitHubPRTool) Name() string        { return "github_pr" }
func (t GitHubPRTool) Description() string { return "Get GitHub PR diff and details" }
func (t GitHubPRTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "github_pr",
		"description": "Fetch a GitHub pull request diff and metadata",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"owner": map[string]interface{}{
					"type":        "string",
					"description": "Repository owner",
				},
				"repo": map[string]interface{}{
					"type":        "string",
					"description": "Repository name",
				},
				"pr_number": map[string]interface{}{
					"type":        "number",
					"description": "Pull request number",
				},
			},
			"required": []string{"owner", "repo", "pr_number"},
		},
	}
}

func (t GitHubPRTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Owner    string `json:"owner"`
		Repo     string `json:"repo"`
		PRNumber int    `json:"pr_number"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	pr, err := github.GetPR(params.Owner, params.Repo, params.PRNumber)
	if err != nil {
		return "", err
	}

	diff, err := github.GetPRDiff(params.Owner, params.Repo, params.PRNumber)
	if err != nil {
		diff = "(diff unavailable)"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("PR #%d: %s\n", pr.Number, pr.Title))
	b.WriteString(fmt.Sprintf("State: %s | Author: %s\n\n", pr.State, pr.User.Login))
	if pr.Body != "" {
		b.WriteString(pr.Body + "\n\n")
	}
	b.WriteString("--- DIFF ---\n")
	b.WriteString(diff)

	return b.String(), nil
}

type GitHubIssueTool struct{}

func (t GitHubIssueTool) Name() string        { return "github_issue" }
func (t GitHubIssueTool) Description() string { return "List or get GitHub issues" }
func (t GitHubIssueTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "github_issue",
		"description": "List or fetch GitHub issues from a repository",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"owner": map[string]interface{}{
					"type":        "string",
					"description": "Repository owner",
				},
				"repo": map[string]interface{}{
					"type":        "string",
					"description": "Repository name",
				},
				"action": map[string]interface{}{
					"type":        "string",
					"description": "Action: 'list' or 'get'",
					"enum":        []string{"list", "get"},
				},
				"issue_number": map[string]interface{}{
					"type":        "number",
					"description": "Issue number (required for 'get' action)",
				},
				"state": map[string]interface{}{
					"type":        "string",
					"description": "Filter by state: 'open', 'closed', 'all' (default: 'open')",
				},
			},
			"required": []string{"owner", "repo", "action"},
		},
	}
}

func (t GitHubIssueTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Owner       string `json:"owner"`
		Repo        string `json:"repo"`
		Action      string `json:"action"`
		IssueNumber int    `json:"issue_number"`
		State       string `json:"state"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	switch params.Action {
	case "list":
		issues, err := github.ListIssues(params.Owner, params.Repo, params.State)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		for _, issue := range issues {
			labels := strings.Join(issue.Labels, ", ")
			b.WriteString(fmt.Sprintf("#%d: %s [%s] by %s", issue.Number, issue.Title, issue.State, issue.Author))
			if labels != "" {
				b.WriteString(fmt.Sprintf(" | labels: %s", labels))
			}
			b.WriteString("\n")
		}
		if len(issues) == 0 {
			return "No issues found.", nil
		}
		return b.String(), nil
	case "get":
		if params.IssueNumber == 0 {
			return "", fmt.Errorf("issue_number required for 'get' action")
		}
		issue, err := github.GetIssue(params.Owner, params.Repo, params.IssueNumber)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("#%d: %s\n", issue.Number, issue.Title))
		b.WriteString(fmt.Sprintf("State: %s | Author: %s\n", issue.State, issue.Author))
		if len(issue.Labels) > 0 {
			b.WriteString(fmt.Sprintf("Labels: %s\n", strings.Join(issue.Labels, ", ")))
		}
		b.WriteString("\n")
		b.WriteString(issue.Body)
		return b.String(), nil
	default:
		return "", fmt.Errorf("invalid action: %s (use 'list' or 'get')", params.Action)
	}
}

type GitHubWorkflowTool struct{}

func (t GitHubWorkflowTool) Name() string        { return "github_workflow" }
func (t GitHubWorkflowTool) Description() string { return "Generate GitHub Actions workflow YAML" }
func (t GitHubWorkflowTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "github_workflow",
		"description": "Generate a GitHub Actions workflow YAML file",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Workflow name: 'test', 'lint', 'build', 'deploy', or custom",
				},
			},
			"required": []string{"name"},
		},
	}
}

func (t GitHubWorkflowTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	yaml := github.GenerateWorkflow(params.Name, nil)
	return yaml, nil
}
