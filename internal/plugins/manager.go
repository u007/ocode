package plugins

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/jamesmercstudio/ocode/internal/config"
)

// InstallGit clones a git URL into pluginsRoot/<derived-dir> and returns the
// parsed Plugin and the absolute clone directory. ref may be empty (HEAD).
func InstallGit(rawURL, pluginsRoot, ref string) (Plugin, string, error) {
	gitURL := normaliseGitURL(rawURL)
	dirName := installDirName(gitURL)
	destDir := filepath.Join(pluginsRoot, dirName)

	if _, err := os.Stat(destDir); err == nil {
		return Plugin{}, "", fmt.Errorf("plugin directory %q already exists; remove it first", destDir)
	}

	cloneOpts := &gogit.CloneOptions{
		URL:      gitURL,
		Depth:    1,
		Progress: nil,
	}
	if ref != "" {
		cloneOpts.ReferenceName = plumbing.NewTagReferenceName(ref)
	}

	if _, err := gogit.PlainClone(destDir, false, cloneOpts); err != nil {
		return Plugin{}, "", fmt.Errorf("git clone %s: %w", gitURL, err)
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
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("on_install failed: %w", err)
	}
	return nil
}

// AutoRegisterMCP writes a local MCP server entry to ocode config when the
// plugin declares mcp.auto_register = true.
func AutoRegisterMCP(pluginDir string, p Plugin) error {
	if p.MCP == nil || !p.MCP.AutoRegister || p.MCP.Server == "" || len(p.MCP.Command) == 0 {
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
	server := config.MCPConfig{
		Type:    "local",
		Command: cmd,
		Enabled: true,
	}
	return config.SaveMCPServer(p.MCP.Server, server)
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
