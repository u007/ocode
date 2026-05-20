package tool

import (
	"encoding/json"
	"fmt"
	urlpkg "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// repoCacheDir returns the directory where cloned repositories are cached.
// Override with OPENCODE_REPO_CACHE or $XDG_STATE_HOME env vars.
func repoCacheDir() (string, error) {
	if env := os.Getenv("OPENCODE_REPO_CACHE"); env != "" {
		return env, nil
	}
	if env := os.Getenv("XDG_STATE_HOME"); env != "" {
		return filepath.Join(env, "opencode", "repos"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "opencode", "repos"), nil
}

type RepoCloneTool struct{}

func (t RepoCloneTool) Name() string { return "repo_clone" }
func (t RepoCloneTool) Description() string {
	return "Clone or refresh a repository into a managed cache for read-only research"
}
func (t RepoCloneTool) Parallel() bool { return false }

func (t RepoCloneTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "repo_clone",
		"description": "Clone or refresh a repository into OpenCode's managed cache under the data directory. Accepts git URLs, host/path references, or GitHub owner/repo shorthand. Returns the cached absolute local path so other tools can explore the cloned source. This tool is intended for dependency and documentation research workflows, not for modifying the user's workspace.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"repository": map[string]interface{}{
					"type":        "string",
					"description": "Repository to clone, as a git URL, host/path reference, or GitHub owner/repo shorthand",
				},
				"refresh": map[string]interface{}{
					"type":        "boolean",
					"description": "When true, fetches the latest remote state into the managed cache",
				},
				"branch": map[string]interface{}{
					"type":        "string",
					"description": "Branch or ref to clone and inspect",
				},
			},
			"required": []string{"repository"},
		},
	}
}

func (t RepoCloneTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Repository string `json:"repository"`
		Refresh    bool   `json:"refresh"`
		Branch     string `json:"branch"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid params: %w", err)
	}

	remote, host, relPath, err := normalizeRepoURL(params.Repository)
	if err != nil {
		return "", err
	}

	cacheDir, err := repoCacheDir()
	if err != nil {
		return "", fmt.Errorf("failed to get cache dir: %w", err)
	}

	localPath := filepath.Join(cacheDir, host, relPath)
	status := "cached"
	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
			return "", fmt.Errorf("failed to create cache dir: %w", err)
		}
		gitArgs := []string{"clone", "--depth", "1"}
		if params.Branch != "" {
			gitArgs = append(gitArgs, "--branch", params.Branch)
		}
		gitArgs = append(gitArgs, remote, localPath)
		if err := runGit(gitArgs...); err != nil {
			return "", fmt.Errorf("clone failed: %w", err)
		}
		status = "cloned"
	} else if err != nil {
		return "", fmt.Errorf("failed to inspect cache path: %w", err)
	} else if params.Refresh {
		if err := runGit("-C", localPath, "fetch", "--all", "--prune"); err != nil {
			return "", fmt.Errorf("refresh failed: %w", err)
		}
		status = "refreshed"
	}

	if params.Branch != "" && status != "cloned" {
		if err := runGit("-C", localPath, "checkout", params.Branch); err != nil {
			return "", fmt.Errorf("checkout failed: %w", err)
		}
	}

	head, branch := gitStatus(localPath)
	repoName := filepath.Join(host, relPath)

	var out strings.Builder
	fmt.Fprintf(&out, "Repository ready: %s\n", repoName)
	fmt.Fprintf(&out, "Status: %s\n", status)
	fmt.Fprintf(&out, "Local path: %s\n", localPath)
	fmt.Fprintf(&out, "Remote: %s\n", remote)
	if branch != "" {
		fmt.Fprintf(&out, "Branch: %s\n", branch)
	}
	if head != "" {
		fmt.Fprintf(&out, "HEAD: %s\n", head)
	}
	return out.String(), nil
}

type RepoOverviewTool struct{}

func (t RepoOverviewTool) Name() string { return "repo_overview" }
func (t RepoOverviewTool) Description() string {
	return "Summarize structure and ecosystems of a repository or directory"
}
func (t RepoOverviewTool) Parallel() bool { return true }

func (t RepoOverviewTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "repo_overview",
		"description": "Summarize the structure and likely entrypoints of a cloned repository or local directory. Reports detected ecosystems, dependency files, package manager, likely entrypoints, and a compact structure tree.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"repository": map[string]interface{}{
					"type":        "string",
					"description": "Cached repository to inspect, as a git URL, host/path reference, or GitHub owner/repo shorthand",
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Directory path to inspect instead of a cached repository",
				},
				"depth": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum structure depth to include. Defaults to 3.",
				},
			},
		},
	}
}

func (t RepoOverviewTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Repository string `json:"repository"`
		Path       string `json:"path"`
		Depth      int    `json:"depth"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid params: %w", err)
	}
	if params.Depth <= 0 {
		params.Depth = 3
	}

	target := params.Path
	if target != "" {
		safe, err := confinedPath(target)
		if err != nil {
			return "", err
		}
		target = safe
	}
	if target == "" && params.Repository != "" {
		_, host, relPath, err := normalizeRepoURL(params.Repository)
		if err != nil {
			return "", err
		}
		cacheDir, err := repoCacheDir()
		if err != nil {
			return "", fmt.Errorf("failed to get cache dir: %w", err)
		}
		target = filepath.Join(cacheDir, host, relPath)
	}
	if target == "" {
		return "", fmt.Errorf("either 'repository' or 'path' is required")
	}

	target, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	st, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("path does not exist: %s", target)
	}
	if !st.IsDir() {
		return "", fmt.Errorf("path is not a directory: %s", target)
	}

	info := analyzeDirectory(target, params.Depth)
	return formatOverview(target, info), nil
}

var ignoredDirs = map[string]struct{}{
	".git": {}, "node_modules": {}, "__pycache__": {}, ".venv": {},
	"dist": {}, "build": {}, ".next": {}, "target": {}, "vendor": {},
}

var dependencyFiles = map[string]struct{}{
	"package.json": {}, "package-lock.json": {}, "bun.lock": {},
	"bun.lockb": {}, "pnpm-lock.yaml": {}, "yarn.lock": {},
	"requirements.txt": {}, "pyproject.toml": {}, "go.mod": {},
	"Cargo.toml": {}, "Gemfile": {}, "build.gradle": {},
	"build.gradle.kts": {}, "pom.xml": {}, "composer.json": {},
}

var entrypointPatterns = []string{
	"main.go", "main.rs", "main.py", "main.ts", "main.js", "main.tsx",
	"index.ts", "index.js", "index.tsx", "index.py",
	"app.ts", "app.js", "app.py", "app.tsx",
	"server.ts", "server.js", "server.py",
	"lib.rs", "mod.rs", "mod.py",
	"startup.cs", "program.cs", "program.fs",
}

type dirInfo struct {
	path        string
	depth       int
	files       map[string]bool
	entries     []string
	truncated   bool
	entrypoints []string
	depFiles    []string
	ecosystems  []string
	packageMgr  string
}

func analyzeDirectory(root string, maxDepth int) dirInfo {
	info := dirInfo{path: root, depth: maxDepth, files: make(map[string]bool)}
	walkDir(root, "", maxDepth, &info)
	return info
}

func walkDir(dir, rel string, maxDepth int, info *dirInfo) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	sort.Slice(ents, func(i, j int) bool { return ents[i].Name() < ents[j].Name() })

	for _, ent := range ents {
		if info.truncated {
			continue
		}
		name := ent.Name()
		if ent.IsDir() {
			if _, ok := ignoredDirs[name]; ok {
				continue
			}
			childRel := rel + name + "/"
			info.entries = append(info.entries, childRel)
			if len(info.entries) >= 200 {
				info.truncated = true
				continue
			}
			if strings.Count(childRel, "/") < maxDepth {
				walkDir(filepath.Join(dir, name), childRel, maxDepth, info)
			}
			continue
		}

		entryRel := rel + name
		info.entries = append(info.entries, entryRel)
		if len(info.entries) >= 200 {
			info.truncated = true
			continue
		}
		info.files[name] = true
		if _, ok := dependencyFiles[name]; ok {
			info.depFiles = append(info.depFiles, entryRel)
		}
		if stringSliceContains(entrypointPatterns, name) {
			info.entrypoints = append(info.entrypoints, entryRel)
		}
	}
}

func (d *dirInfo) detect() {
	files := make(map[string]struct{})
	for f := range d.files {
		files[f] = struct{}{}
	}
	d.packageMgr = detectPackageManager(files)
	d.ecosystems = detectEcosystems(files)
	sort.Strings(d.depFiles)
	sort.Strings(d.entrypoints)
}

func hasFile(files map[string]struct{}, name string) bool {
	_, ok := files[name]
	return ok
}

func detectPackageManager(files map[string]struct{}) string {
	if hasFile(files, "bun.lock") || hasFile(files, "bun.lockb") {
		return "bun"
	}
	if hasFile(files, "pnpm-lock.yaml") {
		return "pnpm"
	}
	if hasFile(files, "yarn.lock") {
		return "yarn"
	}
	if hasFile(files, "package-lock.json") {
		return "npm"
	}
	return ""
}

func detectEcosystems(files map[string]struct{}) []string {
	var eco []string
	if hasFile(files, "package.json") {
		eco = append(eco, "Node.js")
	}
	if hasFile(files, "go.mod") {
		eco = append(eco, "Go")
	}
	if hasFile(files, "pyproject.toml") || hasFile(files, "requirements.txt") {
		eco = append(eco, "Python")
	}
	if hasFile(files, "Cargo.toml") {
		eco = append(eco, "Rust")
	}
	if hasFile(files, "Gemfile") {
		eco = append(eco, "Ruby")
	}
	if hasFile(files, "build.gradle") || hasFile(files, "build.gradle.kts") || hasFile(files, "pom.xml") {
		eco = append(eco, "Java/Kotlin")
	}
	if hasFile(files, "composer.json") {
		eco = append(eco, "PHP")
	}
	if hasFile(files, "Package.swift") {
		eco = append(eco, "Swift")
	}
	if hasFile(files, "Makefile") {
		eco = append(eco, "C/C++")
	}
	return eco
}

func formatOverview(target string, info dirInfo) string {
	info.detect()
	var out strings.Builder
	fmt.Fprintf(&out, "# Overview: %s\n\n", target)
	if len(info.ecosystems) > 0 {
		fmt.Fprintf(&out, "**Ecosystems:** %s\n\n", strings.Join(info.ecosystems, ", "))
	}
	if info.packageMgr != "" {
		fmt.Fprintf(&out, "**Package Manager:** %s\n\n", info.packageMgr)
	}
	if len(info.depFiles) > 0 {
		fmt.Fprintf(&out, "**Dependency Files:** %s\n\n", strings.Join(info.depFiles, ", "))
	}
	if len(info.entrypoints) > 0 {
		fmt.Fprintf(&out, "**Entrypoints:** %s\n\n", strings.Join(info.entrypoints, ", "))
	}
	fmt.Fprint(&out, "**Structure:**\n```\n")
	for _, entry := range info.entries {
		fmt.Fprintln(&out, entry)
	}
	fmt.Fprint(&out, "```\n")
	if info.truncated {
		fmt.Fprint(&out, "(truncated after 200 entries)\n")
	}
	return out.String()
}

func normalizeRepoURL(input string) (remote, host, repoPath string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", "", fmt.Errorf("repository is required")
	}
	if !strings.HasPrefix(input, "http://") && !strings.HasPrefix(input, "https://") && !strings.HasPrefix(input, "git@") && !strings.HasPrefix(input, "ssh://") {
		input = "https://github.com/" + input
	}

	if strings.HasPrefix(input, "git@") {
		idx := strings.Index(input, ":")
		if idx == -1 {
			return "", "", "", fmt.Errorf("invalid git URL: %s", input)
		}
		host = input[4:idx]
		repoPath = input[idx+1:]
		remote = input
	} else if strings.HasPrefix(input, "ssh://") {
		u, err := urlpkg.Parse(input)
		if err != nil || u.Host == "" || u.Path == "" {
			return "", "", "", fmt.Errorf("invalid ssh URL: %s", input)
		}
		host = u.Host
		repoPath = strings.TrimPrefix(u.Path, "/")
		remote = input
	} else {
		u, err := urlpkg.Parse(input)
		if err != nil || u.Host == "" || u.Path == "" {
			return "", "", "", fmt.Errorf("invalid URL: %s", input)
		}
		host = u.Host
		repoPath = strings.TrimPrefix(u.Path, "/")
		remote = input
	}

	repoPath = strings.Trim(repoPath, "/")
	repoPath = strings.TrimSuffix(repoPath, ".git")
	if repoPath == "" || strings.Contains(repoPath, "..") {
		return "", "", "", fmt.Errorf("invalid repository path: %s", repoPath)
	}
	return remote, host, repoPath + ".git", nil
}

func runGit(args ...string) error {
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %s (%w)", strings.Join(args, " "), strings.TrimSpace(string(out)), err)
	}
	return nil
}

func gitStatus(path string) (head, branch string) {
	headBytes, _ := exec.Command("git", "-C", path, "rev-parse", "HEAD").Output()
	head = strings.TrimSpace(string(headBytes))
	branchBytes, _ := exec.Command("git", "-C", path, "rev-parse", "--abbrev-ref", "HEAD").Output()
	branch = strings.TrimSpace(string(branchBytes))
	return
}

func stringSliceContains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
