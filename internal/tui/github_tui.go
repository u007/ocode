package tui

import (
	"github.com/jamesmercstudio/ocode/internal/github"
)

func ghGetPR(owner, repo string, prNumber int) (*github.PullRequest, error) {
	return github.GetPR(owner, repo, prNumber)
}

func ghGetPRDiff(owner, repo string, prNumber int) (string, error) {
	return github.GetPRDiff(owner, repo, prNumber)
}

func ghListIssues(owner, repo, state string) ([]github.Issue, error) {
	return github.ListIssues(owner, repo, state)
}

func ghGetIssue(owner, repo string, number int) (*github.Issue, error) {
	return github.GetIssue(owner, repo, number)
}

func ghGenerateWorkflow(name string, config map[string]interface{}) string {
	return github.GenerateWorkflow(name, config)
}
