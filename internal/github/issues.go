package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
)

type Issue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	State  string `json:"state"`
	Author string `json:"author"`
	Labels []string `json:"labels"`
}

type githubIssue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	State  string `json:"state"`
	User   struct {
		Login string `json:"login"`
	} `json:"user"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

func ListIssues(owner, repo string, state string) ([]Issue, error) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("GITHUB_TOKEN env var not set")
	}

	if state == "" {
		state = "open"
	}

	params := url.Values{}
	params.Set("state", state)
	params.Set("per_page", "30")

	u := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues?%s", owner, repo, params.Encode())
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	var ghIssues []githubIssue
	if err := json.NewDecoder(resp.Body).Decode(&ghIssues); err != nil {
		return nil, err
	}

	issues := make([]Issue, 0, len(ghIssues))
	for _, gi := range ghIssues {
		labels := make([]string, 0, len(gi.Labels))
		for _, l := range gi.Labels {
			labels = append(labels, l.Name)
		}
		issues = append(issues, Issue{
			Number: gi.Number,
			Title:  gi.Title,
			Body:   gi.Body,
			State:  gi.State,
			Author: gi.User.Login,
			Labels: labels,
		})
	}

	return issues, nil
}

func GetIssue(owner, repo string, number int) (*Issue, error) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("GITHUB_TOKEN env var not set")
	}

	u := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d", owner, repo, number)
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	var gi githubIssue
	if err := json.NewDecoder(resp.Body).Decode(&gi); err != nil {
		return nil, err
	}

	labels := make([]string, 0, len(gi.Labels))
	for _, l := range gi.Labels {
		labels = append(labels, l.Name)
	}

	return &Issue{
		Number: gi.Number,
		Title:  gi.Title,
		Body:   gi.Body,
		State:  gi.State,
		Author: gi.User.Login,
		Labels: labels,
	}, nil
}
