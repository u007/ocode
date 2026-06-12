package plugins

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/u007/ocode/internal/config"
)

// looksLikeCommitSHA returns true if ref looks like a full (40-char) or
// abbreviated (≥7) hex commit hash.
var commitSHAPat = regexp.MustCompile(`^[a-f0-9]{7,40}$`)

func looksLikeCommitSHA(ref string) bool {
	return commitSHAPat.MatchString(strings.ToLower(strings.TrimSpace(ref)))
}

func resolveCommitHash(repo *gogit.Repository, ref string) (plumbing.Hash, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	if ref == "" {
		return plumbing.Hash{}, fmt.Errorf("empty commit ref")
	}
	if len(ref) == 40 {
		return plumbing.NewHash(ref), nil
	}

	iter, err := repo.Log(&gogit.LogOptions{All: true})
	if err != nil {
		return plumbing.Hash{}, fmt.Errorf("list commits: %w", err)
	}
	defer iter.Close()

	var match plumbing.Hash
	count := 0
	err = iter.ForEach(func(c *object.Commit) error {
		if strings.HasPrefix(c.Hash.String(), ref) {
			match = c.Hash
			count++
			if count > 1 {
				return fmt.Errorf("ambiguous commit ref %q", ref)
			}
		}
		return nil
	})
	if err != nil {
		return plumbing.Hash{}, err
	}
	if count == 0 {
		return plumbing.Hash{}, fmt.Errorf("commit %q not found", ref)
	}
	return match, nil
}

// InstallGit clones a git URL into pluginsRoot/<derived-dir> and returns the
// parsed Plugin and the absolute clone directory. ref may be empty (HEAD).
func InstallGit(rawURL, pluginsRoot, ref string) (Plugin, string, error) {
	gitURL := normaliseGitURL(rawURL)
	dirName := installDirName(gitURL)
	destDir := filepath.Join(pluginsRoot, dirName)

	if _, err := os.Stat(destDir); err == nil {
		return Plugin{}, "", fmt.Errorf("plugin directory %q already exists; remove it first", destDir)
	}

	var repo *gogit.Repository
	if ref != "" && looksLikeCommitSHA(ref) {
		// For commit SHAs, clone the full repository so abbreviated hashes can
		// be resolved locally, then checkout the resolved commit.
		cloneOpts := &gogit.CloneOptions{
			URL:      gitURL,
			Progress: nil,
		}
		var err error
		repo, err = gogit.PlainClone(destDir, false, cloneOpts)
		if err != nil {
			return Plugin{}, "", fmt.Errorf("git clone %s: %w", gitURL, err)
		}
		wt, err := repo.Worktree()
		if err != nil {
			_ = os.RemoveAll(destDir)
			return Plugin{}, "", fmt.Errorf("worktree: %w", err)
		}
		hash, err := resolveCommitHash(repo, ref)
		if err != nil {
			_ = os.RemoveAll(destDir)
			return Plugin{}, "", err
		}
		if err := wt.Checkout(&gogit.CheckoutOptions{
			Hash: hash,
		}); err != nil {
			_ = os.RemoveAll(destDir)
			return Plugin{}, "", fmt.Errorf("checkout %s: %w", ref, err)
		}
	} else {
		cloneOpts := &gogit.CloneOptions{
			URL:      gitURL,
			Depth:    1,
			Progress: nil,
		}
		if ref != "" {
			// Try tag first, then branch.
			cloneOpts.ReferenceName = plumbing.NewTagReferenceName(ref)
		}
		var err error
		repo, err = gogit.PlainClone(destDir, false, cloneOpts)
		if err != nil && ref != "" {
			// Tag not found — try as a branch reference.
			_ = os.RemoveAll(destDir)
			cloneOpts.ReferenceName = plumbing.NewBranchReferenceName(ref)
			repo, err = gogit.PlainClone(destDir, false, cloneOpts)
		}
		if err != nil {
			return Plugin{}, "", fmt.Errorf("git clone %s @ %s: %w", gitURL, ref, err)
		}
	}

	p, err := readManifest(destDir)
	if err != nil {
		_ = os.RemoveAll(destDir)
		return Plugin{}, "", fmt.Errorf("read plugin manifest: %w", err)
	}
	if p.Name == "" {
		p.Name = dirName
	}
	abs, err := filepath.Abs(destDir)
	if err != nil {
		_ = os.RemoveAll(destDir)
		return Plugin{}, "", fmt.Errorf("resolve clone dir: %w", err)
	}
	return p, abs, nil
}

// InstallLocal copies a local directory into destDir and returns the parsed Plugin.
func InstallLocal(srcDir, destDir string) (Plugin, error) {
	p, err := readManifest(srcDir)
	if err != nil {
		return Plugin{}, fmt.Errorf("read plugin manifest: %w", err)
	}
	if err := copyDir(srcDir, destDir); err != nil {
		return Plugin{}, fmt.Errorf("copy plugin directory: %w", err)
	}
	return p, nil
}

// Remove deletes a plugin directory from disk.
func Remove(pluginDir string) error {
	if err := os.RemoveAll(pluginDir); err != nil {
		return fmt.Errorf("remove plugin dir %q: %w", pluginDir, err)
	}
	return nil
}

// PluginInstallDir returns the canonical global plugins root directory.
func PluginInstallDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "opencode", "plugins"), nil
}

// RunOnInstall executes the plugin's on_install command if present.
// The command is executed directly via exec — no shell — preventing injection.
// Tokens containing {plugin_dir} are replaced with the absolute plugin path.
func RunOnInstall(pluginDir string, p Plugin) error {
	if len(p.OnInstall) == 0 {
		return nil
	}
	abs, err := filepath.Abs(pluginDir)
	if err != nil {
		return fmt.Errorf("resolve plugin dir: %w", err)
	}
	args := make([]string, len(p.OnInstall))
	for i, tok := range p.OnInstall {
		args[i] = strings.ReplaceAll(tok, "{plugin_dir}", abs)
	}
	if strings.ContainsAny(args[0], ";|&$`\\\"'") {
		return fmt.Errorf("on_install command[0] contains disallowed characters: %q", args[0])
	}
	cmd := exec.Command(args[0], args[1:]...) //nolint:gosec — validated above; no shell
	cmd.Dir = abs
	// Capture rather than inherit the terminal: the TUI runs in an alt-screen, so
	// inherited stdout/stderr would paint the subprocess output over the frame.
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("on_install failed: %w\n%s", err, out.String())
	}
	log.Printf("plugin on_install %s: %s", p.Name, out.String())
	return nil
}

// validatePluginName rejects names that could escape the plugins root via
// path traversal or absolute path components.
func validatePluginName(name string) error {
	if name == "" {
		return fmt.Errorf("plugin name must not be empty")
	}
	if filepath.IsAbs(name) {
		return fmt.Errorf("plugin name must not be an absolute path")
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("plugin name must not contain path separators")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("plugin name %q is not allowed", name)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("plugin name must not contain '..' path segments")
	}
	return nil
}

// ScaffoldPlugin creates a new plugin directory with a basic plugin.json manifest
// and a commands/ subdirectory under the global plugins root. It returns the
// absolute path to the created directory. Returns an error if the directory
// already exists or the name is invalid.
func ScaffoldPlugin(name, description string) (string, error) {
	if err := validatePluginName(name); err != nil {
		return "", err
	}
	root, err := PluginInstallDir()
	if err != nil {
		return "", fmt.Errorf("plugin install dir: %w", err)
	}
	dir := filepath.Join(root, name)
	// Verify the resolved path is still inside the plugins root.
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve plugins root: %w", err)
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve plugin dir: %w", err)
	}
	if !strings.HasPrefix(absDir, absRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("plugin name %q resolves outside the plugins root", name)
	}
	if _, err := os.Stat(dir); err == nil {
		return "", fmt.Errorf("plugin %q already exists at %s", name, dir)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create plugin dir: %w", err)
	}
	// Create commands/ subdirectory.
	cmdsDir := filepath.Join(dir, "commands")
	if err := os.MkdirAll(cmdsDir, 0755); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("create commands dir: %w", err)
	}
	// Write plugin.json.
	manifest := Plugin{
		Name:        name,
		Description: description,
		Version:     "1.0.0",
		Commands:    []string{},
		Tools:       []string{},
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("marshal manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), data, 0644); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("write plugin.json: %w", err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("resolve plugin dir: %w", err)
	}
	return abs, nil
}

// AutoRegisterMCP adds the plugin's MCP server config to ocode config if configured.
func AutoRegisterMCP(pluginDir string, p Plugin) error {
	if p.MCP == nil || !p.MCP.AutoRegister || len(p.MCP.Command) == 0 {
		return nil
	}
	abs, err := filepath.Abs(pluginDir)
	if err != nil {
		return fmt.Errorf("resolve plugin dir: %w", err)
	}
	cmd := make([]string, len(p.MCP.Command))
	for i, tok := range p.MCP.Command {
		cmd[i] = strings.ReplaceAll(tok, "{plugin_dir}", abs)
	}
	cfg := config.MCPConfig{
		Type:    "local",
		Command: cmd,
		Enabled: true,
	}
	return config.SaveMCPServer(p.MCP.Server, cfg)
}

// UnregisterMCP removes the auto-registered MCP server from ocode config.
func UnregisterMCP(p Plugin) error {
	if p.MCP == nil || !p.MCP.AutoRegister || p.MCP.Server == "" {
		return nil
	}
	return config.RemoveMCPServer(p.MCP.Server)
}

func normaliseGitURL(raw string) string {
	if strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "http://") {
		return raw
	}
	return "https://" + raw
}

func installDirName(gitURL string) string {
	u, err := url.Parse(gitURL)
	if err != nil {
		return strings.NewReplacer("/", "-", ".", "-").Replace(gitURL)
	}
	path := strings.Trim(u.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	return strings.ReplaceAll(path, "/", "-")
}

func readManifest(dir string) (Plugin, error) {
	data, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return Plugin{}, fmt.Errorf("plugin.json not found in %s: %w", dir, err)
	}
	var p Plugin
	if err := json.Unmarshal(data, &p); err != nil {
		return Plugin{}, fmt.Errorf("parse plugin.json: %w", err)
	}
	return p, nil
}

func copyDir(src, dest string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0644)
	})
}
