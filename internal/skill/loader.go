package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/bundled"
	"github.com/u007/ocode/internal/stackdetect"
)

type Skill struct {
	Name        string
	Description string
	WhenToUse   string
	Content     string
	Source      string
	// TunedFor is the Kaizen `tuned_for` frontmatter: the provider-stripped
	// canonical model id this skill was derived for (e.g. "tencent/hy3"). A
	// non-empty TunedFor marks this as a Kaizen skill, which is gated by
	// model + stack and must NEVER appear in an ungated listing.
	TunedFor string
	// Stack is the Kaizen `stack` frontmatter (e.g. "react", "conduct"). The
	// special value "conduct" (and an empty value) is universal — active in
	// every repo; any other value gates on stackdetect.Detect(root).
	Stack string
}

// skillCache caches LoadSkillsForRoot results keyed by the search-path set, so
// repeated command dispatch does not re-scan every SKILL.md on disk each time.
// A short TTL bounds staleness; skills installed/upgraded at runtime invalidate
// the cache via InvalidateSkillCache.
var (
	skillCacheMu sync.Mutex
	skillCache   = map[string]skillCacheEntry{}
)

type skillCacheEntry struct {
	skills []Skill
	ts     time.Time
}

const skillCacheTTL = 3 * time.Second

// LoadSkillsForRoot loads and parses every skill discoverable from the given
// project root (matching SkillSearchPathsForRoot). An empty root falls back to
// the current working directory.
func LoadSkillsForRoot(root string) []Skill {
	paths := SkillSearchPathsForRoot(root)
	key := strings.Join(paths, "\x00")

	skillCacheMu.Lock()
	if e, ok := skillCache[key]; ok && time.Since(e.ts) < skillCacheTTL {
		out := append([]Skill(nil), e.skills...)
		skillCacheMu.Unlock()
		return out
	}
	skillCacheMu.Unlock()

	skills := loadSkillsFromPaths(paths)

	skillCacheMu.Lock()
	skillCache[key] = skillCacheEntry{skills: skills, ts: time.Now()}
	skillCacheMu.Unlock()
	return skills
}

// InvalidateSkillCache clears the skill-load cache. Call after skills are
// installed, upgraded, or removed on disk so subsequent loads reflect the
// change immediately instead of waiting for the TTL to expire.
func InvalidateSkillCache() {
	skillCacheMu.Lock()
	skillCache = map[string]skillCacheEntry{}
	skillCacheMu.Unlock()
}

func loadSkillsFromPaths(paths []string) []Skill {
	var skills []Skill
	seen := make(map[string]bool)

	for _, dir := range paths {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			if seen[name] {
				continue
			}

			skillPath := filepath.Join(dir, name, "SKILL.md")
			content, err := os.ReadFile(skillPath)
			if err != nil {
				continue
			}

			skill := parseSkillMetadata(string(content))
			if skill.Name == "" {
				skill.Name = name
			}
			skill.Content = string(content)
			skill.Source = skillPath

			seen[name] = true
			skills = append(skills, skill)
		}
	}

	sort.Slice(skills, func(i, j int) bool {
		return strings.ToLower(skills[i].Name) < strings.ToLower(skills[j].Name)
	})

	return skills
}

// LoadSkills loads skills discoverable from the current working directory,
// EXCLUDING all Kaizen (per-model tuned) skills. This is the default ungated
// listing path: no Kaizen skill may leak into a catalog that is not model-aware.
// Callers that want the tuned skills gated in must use LoadSkillsForModel.
func LoadSkills() []Skill {
	root := ""
	if cwd, err := os.Getwd(); err == nil {
		root = cwd
	}
	return excludeKaizen(LoadSkillsForRoot(root))
}

// LoadSkillsForModel loads every skill discoverable from root and returns the
// set admissible for a session running activeModel: all normal skills, plus any
// Kaizen skill whose gate passes (model matches its tuned_for AND its stack is
// active). stackdetect.Detect(root) is computed ONCE here so the result is
// stable for a fixed (root, activeModel) — respecting the prefix-cache contract.
func LoadSkillsForModel(root, activeModel string) []Skill {
	all := LoadSkillsForRoot(root)
	detected := stackdetect.Detect(root)

	out := make([]Skill, 0, len(all))
	for _, s := range all {
		if s.TunedFor == "" {
			out = append(out, s) // normal skill: always admitted
			continue
		}
		if kaizenAdmitted(s, activeModel, detected) {
			out = append(out, s)
		}
	}
	return out
}

// excludeKaizen returns the subset of skills that are NOT Kaizen skills
// (empty TunedFor). It never mutates the input slice.
func excludeKaizen(in []Skill) []Skill {
	out := make([]Skill, 0, len(in))
	for _, s := range in {
		if s.TunedFor != "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

// kaizenAdmitted reports whether a Kaizen skill is admitted for the session:
// the active model must match the skill's tuned_for AND the skill's stack must
// be active (universal "conduct"/"" or present in the detected stacks).
func kaizenAdmitted(s Skill, activeModel string, detected []string) bool {
	if !modelMatchesTuned(activeModel, s.TunedFor) {
		return false
	}
	return stackActive(s.Stack, detected)
}

// stackActive reports whether a Kaizen skill's stack is active for this repo.
// The universal conduct corpus (and an empty stack) is always active; any other
// stack must appear in the detected set.
func stackActive(stack string, detected []string) bool {
	if strings.TrimSpace(stack) == "" || strings.EqualFold(stack, "conduct") {
		return true
	}
	for _, d := range detected {
		if strings.EqualFold(d, stack) {
			return true
		}
	}
	return false
}

// modelMatchesTuned reports whether the active model corresponds to a Kaizen
// skill's tuned_for canonical id. Matching is case-insensitive and provider-
// aware: the active model matches when it equals tunedFor exactly, or when it
// carries a provider prefix and ends in "/"+tunedFor. So both
// "novita-ai/tencent/hy3" and "openrouter/tencent/hy3" match "tencent/hy3",
// but a bare "hy3" does NOT match "tencent/hy3" (no "/" boundary).
func modelMatchesTuned(activeModel, tunedFor string) bool {
	a := strings.ToLower(strings.TrimSpace(activeModel))
	t := strings.ToLower(strings.TrimSpace(tunedFor))
	if a == "" || t == "" {
		return false
	}
	return a == t || strings.HasSuffix(a, "/"+t)
}

// ProjectLocalSkillDirs returns the project-root skill directories that should
// be scanned for project-local skills. root is the project root (absolute path).
func ProjectLocalSkillDirs(root string) []string {
	return []string{
		filepath.Join(root, ".opencode", "skills"),
		filepath.Join(root, ".claude", "skill"),
		filepath.Join(root, "skills"),
	}
}

func skillSearchPaths() []string {
	root := ""
	if cwd, err := os.Getwd(); err == nil {
		root = cwd
	}
	return SkillSearchPathsForRoot(root)
}

// SkillSearchPathsForRoot returns the ordered list of directories searched for
// skills, using root as the project root (may be empty).
func SkillSearchPathsForRoot(root string) []string {
	var paths []string

	home, err := os.UserHomeDir()
	if err == nil {
		paths = append(paths, filepath.Join(home, ".config", "opencode", "skills"))
		paths = append(paths, filepath.Join(home, ".agents", "skills"))
	}

	if root != "" {
		paths = append(paths, ProjectLocalSkillDirs(root)...)
	} else if cwd, err := os.Getwd(); err == nil {
		paths = append(paths, ProjectLocalSkillDirs(cwd)...)
	}

	// Embedded (bundled) skills — appended LAST so disk skills (global and
	// project, above) win via loadSkillsFromPaths' first-wins-on-name rule.
	if bundled.SkillsDir != "" {
		paths = append(paths, bundled.SkillsDir)
	}

	// Kaizen (per-model tuned) skills live one level deeper, under a `kaizen/`
	// subtree of each skills root (skills/kaizen/<name>/SKILL.md). Because they
	// are gate-filtered separately, they are grouped there rather than mixed in
	// with normal skills. loadSkillsFromPaths only descends a single level
	// (<path>/<name>/SKILL.md), so each `kaizen` subtree must be its own search
	// path or the tuned skills would never load. Append after the base roots so
	// the same first-wins-on-name precedence holds.
	for _, p := range append([]string(nil), paths...) {
		paths = append(paths, filepath.Join(p, "kaizen"))
	}

	return paths
}

func parseSkillMetadata(content string) Skill {
	var skill Skill
	lines := strings.Split(content, "\n")
	frontmatter := parseFrontmatter(lines)
	if len(frontmatter) > 0 {
		skill.Name = cleanMetadataValue(frontmatter["name"])
		skill.Description = firstNonEmpty(
			cleanMetadataValue(frontmatter["description"]),
			cleanMetadataValue(frontmatter["purpose"]),
		)
		skill.WhenToUse = firstNonEmpty(
			cleanMetadataValue(frontmatter["when_to_use"]),
			cleanMetadataValue(frontmatter["when-to-use"]),
			cleanMetadataValue(frontmatter["when"]),
		)
		// Kaizen (per-model tuned) frontmatter. A non-empty TunedFor promotes
		// this skill to gated-only; see LoadSkillsForModel / the exclusion in
		// LoadSkills.
		skill.TunedFor = firstNonEmpty(
			cleanMetadataValue(frontmatter["tuned_for"]),
			cleanMetadataValue(frontmatter["tuned-for"]),
		)
		skill.Stack = cleanMetadataValue(frontmatter["stack"])
	}

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if skill.Name == "" && strings.HasPrefix(line, "#") {
			skill.Name = cleanHeading(line)
			continue
		}
		if skill.Description == "" {
			skill.Description = descriptionFromLine(line)
			if skill.Description != "" {
				continue
			}
		}
		if skill.WhenToUse == "" {
			key, value := splitMetadataLikeLine(line)
			switch strings.ToLower(key) {
			case "when to use", "when-to-use", "use when", "when":
				skill.WhenToUse = cleanMetadataValue(value)
			}
		}
		if skill.Description != "" && skill.WhenToUse != "" && skill.Name != "" {
			break
		}
	}

	skill.Description = clampSentence(skill.Description, 400)
	skill.WhenToUse = clampSentence(skill.WhenToUse, 400)
	return skill
}

// BuildCatalog renders the ungated skill catalog. It never lists Kaizen skills
// (LoadSkills excludes them); use BuildCatalogForModel for the model-aware
// catalog that gates the tuned skills in.
func BuildCatalog() string {
	return renderCatalog(LoadSkills())
}

// BuildCatalogForModel renders the catalog for a session running activeModel:
// identical to BuildCatalog, but built from the model-aware set that admits any
// Kaizen skill whose model+stack gate passes.
func BuildCatalogForModel(root, activeModel string) string {
	return renderCatalog(LoadSkillsForModel(root, activeModel))
}

func renderCatalog(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n--- Skill Catalog ---\n")
	b.WriteString("Compact skill summaries available in this workspace. Use the skill tool to load full SKILL.md contents on demand when relevant.\n")
	for _, s := range skills {
		b.WriteString("- ")
		b.WriteString(s.Name)
		if s.Description != "" {
			b.WriteString(": ")
			b.WriteString(s.Description)
		}
		if s.WhenToUse != "" {
			b.WriteString(" When to use: ")
			b.WriteString(s.WhenToUse)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// LoadSkill resolves a skill by exact name (or containing directory name). It
// scans the UNFILTERED set (LoadSkillsForRoot), so an explicit load-by-name can
// still resolve a Kaizen skill — that is an explicit request, distinct from
// advertising it in an ungated catalog.
func LoadSkill(name string) (*Skill, error) {
	root := ""
	if cwd, err := os.Getwd(); err == nil {
		root = cwd
	}
	for _, s := range LoadSkillsForRoot(root) {
		if s.Name == name || filepath.Base(filepath.Dir(s.Source)) == name {
			skill := s
			return &skill, nil
		}
	}
	return nil, nil
}

func parseFrontmatter(lines []string) map[string]string {
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil
	}
	frontmatter := make(map[string]string)
	for _, raw := range lines[1:] {
		line := strings.TrimSpace(raw)
		if line == "---" {
			return frontmatter
		}
		key, value := splitMetadataLikeLine(line)
		if key == "" {
			continue
		}
		frontmatter[strings.ToLower(key)] = value
	}
	return nil
}

func splitMetadataLikeLine(line string) (key, value string) {
	idx := strings.Index(line, ":")
	if idx <= 0 {
		return "", ""
	}
	return strings.TrimSpace(line[:idx]), strings.TrimSpace(line[idx+1:])
}

func cleanMetadataValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `"'`)
	return strings.Join(strings.Fields(value), " ")
}

func cleanHeading(line string) string {
	line = strings.TrimLeft(line, "#")
	return cleanMetadataValue(line)
}

func descriptionFromLine(line string) string {
	if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "#") {
		return ""
	}
	key, value := splitMetadataLikeLine(line)
	switch strings.ToLower(key) {
	case "description", "purpose", "summary", "overview":
		return cleanMetadataValue(value)
	case "when to use", "when-to-use", "use when", "when":
		return ""
	}
	if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
		return ""
	}
	return cleanMetadataValue(line)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func clampSentence(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	trimmed := strings.TrimSpace(value[:max-3])
	return fmt.Sprintf("%s...", trimmed)
}
