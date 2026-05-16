package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

type PullRequest struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	User   struct {
		Login string `json:"login"`
	} `json:"user"`
	Body    string `json:"body"`
	DiffURL string `json:"diff_url"`
}

func GetPRDiff(owner, repo string, prNumber int) (string, error) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return "", fmt.Errorf("GITHUB_TOKEN env var not set")
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d", owner, repo, prNumber)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	var pr PullRequest
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return "", err
	}

	diffURL := pr.DiffURL
	if diffURL == "" {
		diffURL = fmt.Sprintf("https://github.com/%s/%s/pull/%d.diff", owner, repo, prNumber)
	}

	diffReq, err := http.NewRequest("GET", diffURL, nil)
	if err != nil {
		return "", err
	}
	diffReq.Header.Set("Authorization", "Bearer "+token)
	diffReq.Header.Set("Accept", "application/vnd.github.v3.diff")

	diffResp, err := http.DefaultClient.Do(diffReq)
	if err != nil {
		return "", err
	}
	defer diffResp.Body.Close()

	if diffResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("diff request returned %d", diffResp.StatusCode)
	}

	diff, err := io.ReadAll(diffResp.Body)
	if err != nil {
		return "", err
	}

	return string(diff), nil
}

func GetPR(owner, repo string, prNumber int) (*PullRequest, error) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("GITHUB_TOKEN env var not set")
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d", owner, repo, prNumber)
	req, err := http.NewRequest("GET", url, nil)
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

	var pr PullRequest
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil, err
	}

	return &pr, nil
}
