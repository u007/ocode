package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/u007/ocode/internal/config"
)

// Claude Code plugin support.
//
// A plugin directory is recognised when it holds an ocode manifest
// (plugin.json) OR a Claude Code manifest (.claude-plugin/plugin.json), so one
// plugin repo installs in both tools. When both manifests exist the ocode one
// is authoritative and the Claude Code one only fills blank fields.
//
// Besides ocode's own plugin dirs, plugins installed by Claude Code itself
// (~/.claude/plugins/installed_plugins.json) are discovered too. They are
// searched LAST, so an ocode plugin with the same name always wins and the
// Claude Code copy is used only when ocode has none.

const (
	// FormatOcode marks a plugin that has an ocode plugin.json.
	FormatOcode = "ocode"
	// FormatClaude marks a plugin that has only a Claude Code manifest.
	FormatClaude = "claude"

	// SourceOcode marks a plugin found in ocode's plugin dirs (global,
	// project, bundled).
	SourceOcode = "ocode"
	// SourceClaudeCode marks a plugin installed by Claude Code.
	SourceClaudeCode = "claude-code"
)

// claudeManifestRel is the Claude Code manifest path inside a plugin dir.
var claudeManifestRel = filepath.Join(".claude-plugin", "plugin.json")

// claudeManifest is the subset of Claude Code's plugin.json ocode reads.
type claudeManifest struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Version     string          `json:"version"`
	Skills      json.RawMessage `json:"skills"`
}

// readPluginManifest reads the plugin manifest(s) in dir. ok is false when the
// directory holds neither an ocode nor a Claude Code manifest, or the one it
// holds does not parse.
func readPluginManifest(dir string) (Plugin, bool) {
	var p Plugin
	hasOcode := false
	if data, err := os.ReadFile(filepath.Join(dir, "plugin.json")); err == nil {
		if err := json.Unmarshal(data, &p); err != nil {
			return Plugin{}, false
		}
		hasOcode = true
		p.Format = FormatOcode
	}

	var cm claudeManifest
	hasClaude := false
	if data, err := os.ReadFile(filepath.Join(dir, claudeManifestRel)); err == nil {
		if err := json.Unmarshal(data, &cm); err == nil {
			hasClaude = true
		} else if !hasOcode {
			return Plugin{}, false
		}
	}
	if !hasOcode && !hasClaude {
		return Plugin{}, false
	}

	if hasClaude {
		p.HasClaudeManifest = true
		if !hasOcode {
			p.Format = FormatClaude
		}
		if p.Name == "" {
			p.Name = cm.Name
		}
		if p.Description == "" {
			p.Description = cm.Description
		}
		if p.Version == "" {
			p.Version = cm.Version
		}
	}
	p.SkillDirs = pluginSkillDirs(dir, cm.Skills)
	return p, true
}

// pluginSkillDirs returns the skill roots of a plugin: the default skills/
// dir plus any extra paths from the Claude Code manifest's "skills" field
// (a string or an array; it ADDS to the default). Paths that escape the
// plugin dir or do not exist are dropped.
func pluginSkillDirs(dir string, raw json.RawMessage) []string {
	var dirs []string
	seen := map[string]bool{}
	add := func(p string) {
		if seen[p] {
			return
		}
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			seen[p] = true
			dirs = append(dirs, p)
		}
	}
	add(filepath.Join(dir, "skills"))

	var extra []string
	if len(raw) > 0 {
		var one string
		if err := json.Unmarshal(raw, &one); err == nil {
			extra = []string{one}
		} else {
			_ = json.Unmarshal(raw, &extra)
		}
	}
	for _, rel := range extra {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if !isSubpath(dir, p) {
			continue
		}
		add(p)
	}
	return dirs
}

// claudeConfigDir is Claude Code's config dir: $CLAUDE_CONFIG_DIR, else
// ~/.claude.
func claudeConfigDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

// claudeInstalledEntry is one install record in installed_plugins.json.
type claudeInstalledEntry struct {
	ID          string `json:"id"`
	Scope       string `json:"scope"`
	InstallPath string `json:"installPath"`
	ProjectPath string `json:"projectPath"`
}

// parseClaudeInstalled reads installed_plugins.json. It accepts the shapes
// Claude Code has used: "plugins" as a map of id → record or id → [records],
// or as an array of records carrying their own "id".
func parseClaudeInstalled(data []byte) []claudeInstalledEntry {
	var doc struct {
		Plugins json.RawMessage `json:"plugins"`
	}
	if err := json.Unmarshal(data, &doc); err != nil || len(doc.Plugins) == 0 {
		return nil
	}

	var list []claudeInstalledEntry
	if err := json.Unmarshal(doc.Plugins, &list); err == nil {
		return list
	}

	var byID map[string]json.RawMessage
	if err := json.Unmarshal(doc.Plugins, &byID); err != nil {
		return nil
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	// Sorted so the result (and any prompt text derived from it) is stable.
	sort.Strings(ids)
	var out []claudeInstalledEntry
	for _, id := range ids {
		raw := byID[id]
		var many []claudeInstalledEntry
		if err := json.Unmarshal(raw, &many); err != nil {
			var one claudeInstalledEntry
			if err := json.Unmarshal(raw, &one); err != nil {
				continue
			}
			many = []claudeInstalledEntry{one}
		}
		for _, e := range many {
			if e.ID == "" {
				e.ID = id
			}
			out = append(out, e)
		}
	}
	return out
}

// claudeEnabledPlugins merges Claude Code's enabledPlugins maps: user
// settings first, then the project's shared and local settings, later files
// overriding earlier ones — the same layering Claude Code applies.
func claudeEnabledPlugins(configDir, projectRoot string) map[string]bool {
	files := []string{filepath.Join(configDir, "settings.json")}
	if projectRoot != "" {
		files = append(files,
			filepath.Join(projectRoot, ".claude", "settings.json"),
			filepath.Join(projectRoot, ".claude", "settings.local.json"),
		)
	}
	out := map[string]bool{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var s struct {
			EnabledPlugins map[string]bool `json:"enabledPlugins"`
		}
		if json.Unmarshal(data, &s) != nil {
			continue
		}
		for id, on := range s.EnabledPlugins {
			out[id] = on
		}
	}
	return out
}

// claudeCodePlugin is one plugin Claude Code has installed.
type claudeCodePlugin struct {
	dir string
	// enabled is Claude Code's own state for it (enabledPlugins; enabled
	// when unset, Claude Code's default). It only seeds ocode's default.
	enabled bool
}

// claudeCodePlugins returns the plugins Claude Code has installed for
// projectRoot, with Claude Code's enabled state. Project/local-scoped
// installs count only for the project they were installed in.
func claudeCodePlugins(projectRoot string) []claudeCodePlugin {
	configDir := claudeConfigDir()
	if configDir == "" {
		return nil
	}
	pluginsRoot := filepath.Join(configDir, "plugins")
	data, err := os.ReadFile(filepath.Join(pluginsRoot, "installed_plugins.json"))
	if err != nil {
		return nil
	}
	enabled := claudeEnabledPlugins(configDir, projectRoot)

	var out []claudeCodePlugin
	seen := map[string]bool{}
	for _, e := range parseClaudeInstalled(data) {
		if e.InstallPath == "" {
			continue
		}
		switch e.Scope {
		case "project", "local":
			if e.ProjectPath == "" || projectRoot == "" ||
				filepath.Clean(e.ProjectPath) != filepath.Clean(projectRoot) {
				continue
			}
		}
		dir := e.InstallPath
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(pluginsRoot, dir)
		}
		dir = filepath.Clean(dir)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		on, ok := enabled[e.ID]
		out = append(out, claudeCodePlugin{dir: dir, enabled: on || !ok})
	}
	return out
}

// pluginCandidate is one directory that may hold a plugin, tagged with where
// it was found.
type pluginCandidate struct {
	dir            string
	source         string
	defaultEnabled bool
}

// pluginCandidatesForProject lists every plugin directory in precedence
// order: ocode's global, project and bundled plugin dirs (children of each
// root, in directory order), then Claude Code's installed plugins.
func pluginCandidatesForProject(projectRoot string) []pluginCandidate {
	var out []pluginCandidate
	for _, root := range pluginSearchPathsForProject(projectRoot) {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				out = append(out, pluginCandidate{filepath.Join(root, e.Name()), SourceOcode, true})
			}
		}
	}
	claudeRoot := projectRoot
	if claudeRoot == "" {
		claudeRoot = findProjectRoot()
	}
	for _, cp := range claudeCodePlugins(claudeRoot) {
		out = append(out, pluginCandidate{cp.dir, SourceClaudeCode, cp.enabled})
	}
	return out
}

// SkillRoot is one plugin skill directory. Skills found under it are exposed
// as "<Plugin>:<skill>", mirroring Claude Code's plugin skill namespacing.
type SkillRoot struct {
	Plugin string
	Dir    string
}

// SkillRootsForProject returns the skill directories of every enabled plugin
// (ocode's plugin config applied), in plugin precedence order.
func SkillRootsForProject(projectRoot string) []SkillRoot {
	var roots []SkillRoot
	for _, p := range LoadPluginsForProject(configuredEnabled(), projectRoot) {
		for _, d := range p.SkillDirs {
			roots = append(roots, SkillRoot{Plugin: p.Name, Dir: d})
		}
	}
	return roots
}

// enabledCacheTTL bounds how stale configuredEnabled may be; the skill loader
// calls it on every skill lookup.
const enabledCacheTTL = 3 * time.Second

var (
	enabledCacheMu sync.Mutex
	enabledCache   map[string]bool
	enabledCacheAt time.Time
)

// configuredEnabled returns ocode's per-plugin enabled map from the saved
// config, for callers (the skill loader) that have no config in scope.
func configuredEnabled() map[string]bool {
	enabledCacheMu.Lock()
	defer enabledCacheMu.Unlock()
	if enabledCache != nil && time.Since(enabledCacheAt) < enabledCacheTTL {
		return enabledCache
	}
	m := map[string]bool{}
	if cfg, err := config.Load(); err == nil && cfg != nil {
		for name, pc := range cfg.Plugins {
			m[name] = pc.Enabled
		}
	}
	enabledCache, enabledCacheAt = m, time.Now()
	return m
}
