package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Skill enable/disable preferences.
//
// Layering (the "global default + per-project override" model the sidebar
// exposes):
//
//	effectiveDisabled = (global.Disabled ∪ project.Disabled)
//	                    − (global.Enabled ∪ project.Enabled)
//
// So a project can ADD a disable without repeating the global list, and can
// RE-ENABLE a globally disabled skill by naming it in its own "enabled" list.
// The global "enabled" list exists for symmetry (a user editing
// ocodeconfig.json), but the sidebar writes only project scope.
//
// Names are compared exactly (skill names are case-sensitive identifiers, and
// plugin skills are namespaced "<plugin>:<name>"). Entries are trimmed, empties
// dropped, and each list deduped + sorted on load and before every save so the
// on-disk form is deterministic and two writers cannot leave duplicates.

// projectSettingsSkillsKey is the .ocode/settings.json key holding a project's
// skill preferences. It has the same shape as the global ocodeconfig.json
// "skills" key, so one decoder serves both.
const projectSettingsSkillsKey = "skills"

// ProjectSettingsPathForRoot returns the .ocode/settings.json path for a
// project root. Unlike getProjectSettingsPath it never consults the process cwd:
// a web/desktop server hosts many projects (and boots with cwd "/"), so the
// caller's session root is the only correct anchor.
func ProjectSettingsPathForRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	return filepath.Join(root, ".ocode", "settings.json")
}

// NormalizeSkillNames trims, drops empties, dedupes and sorts a skill-name list.
// Returns nil for an effectively empty input so callers can compare with len().
func NormalizeSkillNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

// ApplySkillsConfig replaces dst from src, normalizing both lists.
//
// Replace-not-merge is deliberate: this runs on config LOAD, so a value removed
// from the file must actually disappear. Merging here would make deletion
// impossible across a reload.
func ApplySkillsConfig(dst *SkillsConfig, src *skillsConfigFile) {
	if dst == nil {
		return
	}
	if src == nil {
		dst.Disabled = nil
		dst.Enabled = nil
		return
	}
	dst.Disabled = NormalizeSkillNames(src.Disabled)
	dst.Enabled = NormalizeSkillNames(src.Enabled)
}

// applySkillsConfig is the file-local spelling used by loadOcodeConfigFile.
func applySkillsConfig(dst *SkillsConfig, src *skillsConfigFile) {
	ApplySkillsConfig(dst, src)
}

// loadProjectSkillPrefs reads the "skills" block out of a project's
// .ocode/settings.json. A missing file (or missing block) is not an error.
func loadProjectSkillPrefs(root string) (SkillsConfig, error) {
	var prefs SkillsConfig
	path := ProjectSettingsPathForRoot(root)
	if path == "" {
		return prefs, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return prefs, nil
		}
		return prefs, fmt.Errorf("read project settings: %w", err)
	}
	// Decode into a raw map so a malformed sibling key cannot block skill
	// toggling. Comments are tolerated (blanked, not removed, to keep offsets).
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(blankJSONCComments(data), &raw); err != nil {
		return prefs, fmt.Errorf("parse project settings: %w", err)
	}
	block, ok := raw[projectSettingsSkillsKey]
	if !ok {
		return prefs, nil
	}
	var file skillsConfigFile
	if err := json.Unmarshal(block, &file); err != nil {
		return prefs, fmt.Errorf("parse project skills: %w", err)
	}
	ApplySkillsConfig(&prefs, &file)
	return prefs, nil
}

// saveProjectSkillPrefs writes ONLY the "skills" block of a project's
// .ocode/settings.json. It splices that one member into the file's own bytes
// (spliceTopLevelKey), so key order, indentation, formatting and // comments of
// everything else survive a toggle and a git-tracked settings file diffs by
// just the skills lines. A file that is not a JSON(C) object is refused, never
// overwritten.
func saveProjectSkillPrefs(root string, prefs SkillsConfig) error {
	path := ProjectSettingsPathForRoot(root)
	if path == "" {
		return fmt.Errorf("empty project root")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create .ocode directory: %w", err)
	}

	var existing []byte
	exists := true
	if data, err := os.ReadFile(path); err == nil {
		existing = data
	} else if os.IsNotExist(err) {
		exists = false
	} else {
		return fmt.Errorf("read settings file: %w", err)
	}

	block := skillsConfigFile{
		Disabled: NormalizeSkillNames(prefs.Disabled),
		Enabled:  NormalizeSkillNames(prefs.Enabled),
	}
	// An empty block is deleted rather than written as {} so a project that
	// ends up all-default leaves settings.json exactly as it found it.
	empty := len(block.Disabled) == 0 && len(block.Enabled) == 0
	if empty && !exists {
		return nil
	}
	out, err := spliceTopLevelKey(existing, projectSettingsSkillsKey, block, empty)
	if err != nil {
		return fmt.Errorf("parse existing settings: %w", err)
	}
	if exists && bytes.Equal(out, existing) {
		return nil
	}
	if len(bytes.TrimSpace(existing)) == 0 {
		out = append(out, '\n') // a file we just created ends with a newline
	}
	return writeFileAtomic(path, out, 0o644)
}

// writeFileAtomic writes via a temp file + rename so a reader (the skill loader,
// which re-reads this file per cache miss) never observes a half-written
// settings.json. The temp file is created in the same directory so the rename
// stays on one filesystem.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".settings-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp settings: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp settings: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp settings: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp settings: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("chmod temp settings: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace settings: %w", err)
	}
	return nil
}

// EffectiveDisabledSkills resolves the two-layer preference set for a project
// root and returns it as a lookup set. root may be empty (no project layer).
//
// The two layers load independently and the result is ALWAYS non-nil: a layer
// that cannot be read contributes nothing while the other stays in force. This
// sits on the skill loading path, so failing closed would silently empty every
// skill catalog, and letting a bad global config hide a readable project layer
// would silently re-enable that project's disabled skills. Every layer error is
// still returned (joined) so callers that care can tell the set is degraded:
// the sidebar surfaces it, and the cache in internal/skill declines to keep it.
func EffectiveDisabledSkills(root string) (map[string]bool, error) {
	var global SkillsConfig
	globalErr := loadGlobalSkills(&global)
	if globalErr != nil {
		global = SkillsConfig{}
	}
	project, projectErr := loadProjectSkillPrefs(root)
	if projectErr != nil {
		project = SkillsConfig{}
	}
	return resolveDisabledSet(global, project), errors.Join(globalErr, projectErr)
}

// loadGlobalSkills reads only the global "skills" block, through the same
// full-config loader the rest of ocode uses, so profiles/migration stay in one
// place.
func loadGlobalSkills(dst *SkillsConfig) error {
	full, err := loadFullOcodeConfig()
	if err != nil {
		return err
	}
	*dst = full.Skills
	return nil
}

// resolveDisabledSet is the single place the layering algebra lives, so every
// consumer agrees. Kept pure for tests.
func resolveDisabledSet(global, project SkillsConfig) map[string]bool {
	disabled := make(map[string]bool, len(global.Disabled)+len(project.Disabled))
	for _, name := range global.Disabled {
		disabled[name] = true
	}
	for _, name := range project.Disabled {
		disabled[name] = true
	}
	// Enabled wins from either scope: a project can rescue a globally disabled
	// skill, and a global enable is an explicit statement of intent.
	for _, name := range global.Enabled {
		delete(disabled, name)
	}
	for _, name := range project.Enabled {
		delete(disabled, name)
	}
	return disabled
}

// SaveSkillEnabled persists one skill's toggle.
//
// Scope follows the caller: a non-empty root writes the PROJECT layer
// (.ocode/settings.json), which is what the chat sidebar uses because a skill
// preference is naturally a property of that repo. An empty root writes the
// GLOBAL layer (ocodeconfig.json) for chats with no project.
//
// The write is a targeted load-modify-write under the config lock — never a
// full in-memory snapshot save — so a concurrent toggle from another session is
// not clobbered.
func SaveSkillEnabled(root, name string, enabled bool) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("empty skill name")
	}
	if strings.TrimSpace(root) == "" {
		return saveGlobalSkillEnabled(name, enabled)
	}
	return withProjectSkillsLock(root, func(prefs *SkillsConfig) {
		if enabled {
			prefs.Disabled = removeSkillName(prefs.Disabled, name)
			prefs.Enabled = appendUniqueSkillName(prefs.Enabled, name)
			return
		}
		prefs.Enabled = removeSkillName(prefs.Enabled, name)
		prefs.Disabled = appendUniqueSkillName(prefs.Disabled, name)
	})
}

// saveGlobalSkillEnabled applies the same move to the global layer. Written
// through withOcodeConfigLock so it reloads the current file before mutating.
func saveGlobalSkillEnabled(name string, enabled bool) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		if cfg == nil {
			return fmt.Errorf("nil ocode config")
		}
		if enabled {
			cfg.Skills.Disabled = removeSkillName(cfg.Skills.Disabled, name)
			cfg.Skills.Enabled = appendUniqueSkillName(cfg.Skills.Enabled, name)
		} else {
			cfg.Skills.Enabled = removeSkillName(cfg.Skills.Enabled, name)
			cfg.Skills.Disabled = appendUniqueSkillName(cfg.Skills.Disabled, name)
		}
		cfg.Skills.Disabled = NormalizeSkillNames(cfg.Skills.Disabled)
		cfg.Skills.Enabled = NormalizeSkillNames(cfg.Skills.Enabled)
		return nil
	})
}

// withProjectSkillsLock performs a read-modify-write of the project settings
// under the same cross-process lock the global config uses. The lock key is
// derived from the project path, so two projects never contend and two sessions
// on one project serialize.
func withProjectSkillsLock(root string, fn func(prefs *SkillsConfig)) error {
	path := ProjectSettingsPathForRoot(root)
	if path == "" {
		return fmt.Errorf("empty project root")
	}
	// The lock lives next to settings.json, so .ocode/ must exist before the
	// exclusive-create can succeed (same ordering as lockOcodeConfig).
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create .ocode directory: %w", err)
	}
	unlock, err := lockFilePath(path + ".lock")
	if err != nil {
		return err
	}
	defer unlock()

	prefs, err := loadProjectSkillPrefs(root)
	if err != nil {
		// A malformed file must not block the toggle, but it also must not be
		// silently overwritten — refuse and let the caller surface it.
		return err
	}
	fn(&prefs)
	if err := saveProjectSkillPrefs(root, prefs); err != nil {
		return err
	}
	OnConfigSaved.Fire()
	return nil
}

func removeSkillName(names []string, name string) []string {
	if len(names) == 0 {
		return nil
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if n != name {
			out = append(out, n)
		}
	}
	return out
}

func appendUniqueSkillName(names []string, name string) []string {
	for _, n := range names {
		if n == name {
			return names
		}
	}
	return append(names, name)
}
