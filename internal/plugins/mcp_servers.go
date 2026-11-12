package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/u007/ocode/internal/config"
)

// Plugin MCP servers.
//
// A plugin can declare MCP servers in a Claude Code-style `.mcp.json` at its
// root, or in the Claude Code manifest's `mcpServers` field. Only stdio servers
// are started: remote (url / http / sse) servers need OAuth flows and are not
// auto-started. MergeMCPServers adds them to the effective config in memory;
// nothing is written to disk.

// pluginMCPFileRel is the plugin-root MCP file read for every plugin.
const pluginMCPFileRel = ".mcp.json"

// defaultMCPTimeout is the Timeout that config.MCPConfig.UnmarshalJSON gives a
// user-configured server (5000 ms), so plugin servers time out the same way.
const defaultMCPTimeout = 5000

// rawMCPServer is one server entry in a plugin MCP document.
type rawMCPServer struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	URL     string            `json:"url"`
	// RequiresAny is an ocode key, ignored by other hosts: the server is merged
	// only when at least one entry resolves to an installed binary (see
	// requirementMet). A server with no requiresAny is merged as before.
	RequiresAny []string `json:"requiresAny"`
}

// requirementMet reports whether one requiresAny entry resolves. An entry with
// a path separator or a leading "~" is a file path (a leading "~/" is expanded
// to the home directory) and resolves when it names an existing non-directory.
// A bare name resolves through exec.LookPath, the same search a launch uses.
func requirementMet(entry string) bool {
	if strings.ContainsAny(entry, `/\`) || strings.HasPrefix(entry, "~") {
		path := entry
		if rest, ok := strings.CutPrefix(entry, "~/"); ok {
			home, err := os.UserHomeDir()
			if err != nil {
				return false
			}
			path = filepath.Join(home, rest)
		}
		info, err := os.Stat(path)
		return err == nil && !info.IsDir()
	}
	_, err := exec.LookPath(entry)
	return err == nil
}

// requirementsMet reports whether a server with these requiresAny entries may
// be merged. An empty list imposes no requirement; otherwise at least one entry
// must resolve.
func requirementsMet(requires []string) bool {
	if len(requires) == 0 {
		return true
	}
	for _, entry := range requires {
		if requirementMet(entry) {
			return true
		}
	}
	return false
}

// parseMCPServers reads a plugin MCP document in either the wrapped
// {"mcpServers": {...}} shape or the bare {"<name>": {...}} map.
func parseMCPServers(data []byte) (map[string]rawMCPServer, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, err
	}
	body := data
	if raw, ok := top["mcpServers"]; ok {
		body = raw
	}
	var servers map[string]rawMCPServer
	if err := json.Unmarshal(body, &servers); err != nil {
		return nil, err
	}
	return servers, nil
}

// stdioServer converts a declared server to an ocode config entry. ok is false
// for anything that is not a stdio server (a url, a non-stdio type, or no
// command). ${CLAUDE_PLUGIN_ROOT} and {plugin_dir} expand to absDir in the
// command, each arg and each env value; no other variable is expanded.
func stdioServer(s rawMCPServer, absDir string) (config.MCPConfig, bool) {
	if s.URL != "" || strings.TrimSpace(s.Command) == "" {
		return config.MCPConfig{}, false
	}
	if s.Type != "" && s.Type != "stdio" {
		return config.MCPConfig{}, false
	}
	expand := strings.NewReplacer("${CLAUDE_PLUGIN_ROOT}", absDir, "{plugin_dir}", absDir).Replace
	command := []string{expand(s.Command)}
	for _, arg := range s.Args {
		command = append(command, expand(arg))
	}
	var env map[string]string
	if len(s.Env) > 0 {
		env = make(map[string]string, len(s.Env))
		for k, v := range s.Env {
			env[k] = expand(v)
		}
	}
	return config.MCPConfig{
		Type:        "local",
		Command:     command,
		Environment: env,
		Enabled:     true,
		Timeout:     defaultMCPTimeout,
	}, true
}

// loggedMCPErrors dedupes the parse-failure log so a broken plugin file is
// reported once per process, not once per manifest read.
var loggedMCPErrors sync.Map

func logPluginMCPError(dir string, err error) {
	key := dir + "\x00" + err.Error()
	if _, dup := loggedMCPErrors.LoadOrStore(key, true); dup {
		return
	}
	log.Printf("plugins: %s: ignoring MCP servers: %v", dir, err)
}

// readPluginMCPFile parses one MCP file. A missing file yields nothing and no
// error; a read or parse failure is logged and yields nothing.
func readPluginMCPFile(dir, path string) map[string]rawMCPServer {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		logPluginMCPError(dir, fmt.Errorf("read %s: %w", path, err))
		return nil
	}
	servers, err := parseMCPServers(data)
	if err != nil {
		logPluginMCPError(dir, fmt.Errorf("parse %s: %w", path, err))
		return nil
	}
	return servers
}

// pluginMCPServers collects the stdio servers a plugin declares, from
// <dir>/.mcp.json first and then the Claude manifest's mcpServers field
// (manifestRaw: an inline server map, or a path relative to the plugin dir).
// A name is claimed by its first definition, so a remote first definition
// hides a later stdio one of the same name. The second result maps each server
// that declares requiresAny to its entries; the check itself runs at merge time.
func pluginMCPServers(dir string, manifestRaw json.RawMessage) (map[string]config.MCPConfig, map[string][]string) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		logPluginMCPError(dir, fmt.Errorf("resolve plugin dir: %w", err))
		return nil, nil
	}

	var sources []map[string]rawMCPServer
	sources = append(sources, readPluginMCPFile(absDir, filepath.Join(absDir, pluginMCPFileRel)))

	if len(manifestRaw) > 0 && string(manifestRaw) != "null" {
		var rel string
		if err := json.Unmarshal(manifestRaw, &rel); err == nil {
			path := filepath.Join(absDir, filepath.FromSlash(rel))
			if !isSubpath(absDir, path) {
				logPluginMCPError(absDir, fmt.Errorf("mcpServers path %q escapes the plugin dir", rel))
			} else {
				sources = append(sources, readPluginMCPFile(absDir, path))
			}
		} else if servers, err := parseMCPServers(manifestRaw); err != nil {
			logPluginMCPError(absDir, fmt.Errorf("parse inline mcpServers: %w", err))
		} else {
			sources = append(sources, servers)
		}
	}

	claimed := map[string]bool{}
	out := map[string]config.MCPConfig{}
	requires := map[string][]string{}
	for _, servers := range sources {
		for _, name := range slices.Sorted(maps.Keys(servers)) {
			if claimed[name] {
				continue
			}
			claimed[name] = true
			if mc, ok := stdioServer(servers[name], absDir); ok {
				out[name] = mc
				if len(servers[name].RequiresAny) > 0 {
					requires[name] = servers[name].RequiresAny
				}
			}
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, requires
}

// enabledFromConfig is ocode's per-plugin enabled map from a loaded config,
// the same conversion configuredEnabled applies to the saved config.
func enabledFromConfig(cfg *config.Config) map[string]bool {
	enabled := make(map[string]bool, len(cfg.Plugins))
	for name, pc := range cfg.Plugins {
		enabled[name] = pc.Enabled
	}
	return enabled
}

// MCPProvidedByPluginMessage is the refusal for enabling or disabling a server
// that exists only in memory because an enabled plugin declares it: there is
// no on-disk entry to save the toggle to.
func MCPProvidedByPluginMessage(name, plugin string) string {
	return fmt.Sprintf("MCP server %q is provided by plugin %q; disable the plugin to turn it off, or add your own %q entry to opencode.json to override it", name, plugin, name)
}

// isUnderProjectPluginDir reports whether dir lies under projectDir, the
// project's own plugin dir (projectPluginSearchPathForRoot). An empty
// projectDir means there is no project, so nothing is project-local. An
// unresolvable path counts as project-local: the gate fails closed.
func isUnderProjectPluginDir(projectDir, dir string) bool {
	if projectDir == "" {
		return false
	}
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		return true
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return true
	}
	return isSubpath(absProject, absDir)
}

// loggedProjectMCPGates dedupes the gated-server log per plugin dir, so a
// repeated merge in one process reports each plugin once.
var loggedProjectMCPGates sync.Map

func logProjectMCPGated(p Plugin) {
	if _, dup := loggedProjectMCPGates.LoadOrStore(p.Dir, true); dup {
		return
	}
	log.Printf("plugins: %s is a project-local plugin (%s); its MCP servers start only after you enable it explicitly: /plugin enable %s", p.Name, p.Dir, p.Name)
}

// MergeMCPServers adds the stdio MCP servers declared by the enabled plugins
// of projectRoot to cfg.MCP, in memory only. A server name already present in
// cfg.MCP (user config) always wins and is left untouched, including when the
// user's entry is disabled. Plugins are visited in LoadPluginsForProject
// order and server names within a plugin in sorted order, so the result is
// deterministic. It returns the merged names mapped to the providing plugin.
//
// A project-local plugin (under the project's .opencode/plugins, which a
// cloned repo can ship) contributes servers only when cfg.Plugins has an
// explicit enabled entry for it; DefaultEnabled alone is not enough. Other
// plugins follow the normal enabled rule.
//
// cfg.MCP is replaced with a new map rather than written in place, so a map
// shared with another config is never modified through the alias. Callers that
// hold cfg as a shared pointer should pass a shallow copy of the struct.
func MergeMCPServers(cfg *config.Config, projectRoot string) map[string]string {
	return mergeMCPServers(cfg, projectRoot, true)
}

// MergeSharedMCPServers is MergeMCPServers without the project-local plugins of
// projectRoot. It is for a configuration that many projects share (a server
// process-wide, or the default project of a multi-project server): a
// project-local plugin's servers belong to that one project, so they must not
// reach the shared config. Each session of a project merges its own project's
// plugins with MergeMCPServers.
func MergeSharedMCPServers(cfg *config.Config, projectRoot string) map[string]string {
	return mergeMCPServers(cfg, projectRoot, false)
}

// mergeMCPServers is the shared body of MergeMCPServers and
// MergeSharedMCPServers. includeProjectScoped controls whether a project-local
// plugin (ProjectScoped) may contribute servers, and only then does it still
// need an explicit enabled entry in cfg.Plugins.
func mergeMCPServers(cfg *config.Config, projectRoot string, includeProjectScoped bool) map[string]string {
	if cfg == nil {
		return nil
	}
	type pick struct {
		mc     config.MCPConfig
		plugin string
	}
	picks := map[string]pick{}
	for _, p := range LoadPluginsForProject(enabledFromConfig(cfg), projectRoot) {
		if len(p.MCPServers) == 0 {
			continue
		}
		if p.ProjectScoped {
			if !includeProjectScoped {
				continue
			}
			if pc, explicit := cfg.Plugins[p.Name]; !explicit || !pc.Enabled {
				logProjectMCPGated(p)
				continue
			}
		}
		for _, name := range slices.Sorted(maps.Keys(p.MCPServers)) {
			if _, userDefined := cfg.MCP[name]; userDefined {
				continue
			}
			if _, taken := picks[name]; taken {
				continue
			}
			// A server that needs a binary this machine lacks is not started,
			// so it does not claim the name either.
			if !requirementsMet(p.MCPRequires[name]) {
				continue
			}
			picks[name] = pick{mc: p.MCPServers[name], plugin: p.Name}
		}
	}
	if len(picks) == 0 {
		return nil
	}
	merged := make(map[string]config.MCPConfig, len(cfg.MCP)+len(picks))
	for name, mc := range cfg.MCP {
		merged[name] = mc
	}
	out := make(map[string]string, len(picks))
	for name, pk := range picks {
		merged[name] = pk.mc
		out[name] = pk.plugin
	}
	cfg.MCP = merged
	return out
}
