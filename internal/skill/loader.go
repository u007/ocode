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
	"github.com/u007/ocode/internal/plugins"
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
	// model + stack (see universalStacks / stackActive) and must NEVER appear
	// in an ungated listing.
	TunedFor string
	// Stack is the Kaizen `stack` frontmatter (e.g. "react", "conduct"). The
	// universal corpora (see universalStacks) and an empty value are active in
	// every repo; any other value gates on stackdetect.Detect(root).
	Stack string
	// Digest is the compact directive block carved from a SKILL.md between the
	// `<!-- kaizen:digest -->` … `<!-- /kaizen:digest -->` markers. For a Kaizen
	// tuning skill it is force-injected into the base prompt on model match
	// (KaizenDigestBlock) — advertising alone doesn't reliably make an
	// overconfident model load the body. Empty when the skill has no such
	// section (all normal skills, and any tuning skill that omits it).
	Digest string
	// Plugin is the name of the plugin that ships this skill, empty for
	// skills from the ordinary skill dirs. A plugin skill's Name is
	// "<Plugin>:<skill>", Claude Code's namespacing, so it never collides
	// with (or shadows) a same-named user or project skill.
	Plugin string
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
	pluginRoots := plugins.SkillRootsForProject(root)
	keyParts := append([]string(nil), paths...)
	for _, r := range pluginRoots {
		keyParts = append(keyParts, r.Plugin+"="+r.Dir)
	}
	key := strings.Join(keyParts, "\x00")

	skillCacheMu.Lock()
	if e, ok := skillCache[key]; ok && time.Since(e.ts) < skillCacheTTL {
		out := append([]Skill(nil), e.skills...)
		skillCacheMu.Unlock()
		return out
	}
	skillCacheMu.Unlock()

	skills := append(loadSkillsFromPaths(paths), loadPluginSkills(pluginRoots)...)
	sortSkills(skills)

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
			// entry.IsDir() is false for a symlink (its DirEntry.Type() carries
			// ModeSymlink, not ModeDir) even when the target is a directory —
			// stat through the link so skill dirs installed as symlinks (the
			// common layout under ~/.config/opencode/skills) aren't skipped.
			isDir := entry.IsDir()
			if !isDir && entry.Type()&os.ModeSymlink != 0 {
				if info, err := os.Stat(filepath.Join(dir, entry.Name())); err == nil {
					isDir = info.IsDir()
				}
			}
			if !isDir {
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

	sortSkills(skills)
	return skills
}

func sortSkills(skills []Skill) {
	sort.Slice(skills, func(i, j int) bool {
		return strings.ToLower(skills[i].Name) < strings.ToLower(skills[j].Name)
	})
}

// loadPluginSkills loads the skills shipped by plugins, named
// "<plugin>:<skill>". Roots arrive in plugin precedence order, so the first
// plugin to provide a namespaced name wins.
func loadPluginSkills(roots []plugins.SkillRoot) []Skill {
	var skills []Skill
	seen := make(map[string]bool)
	for _, r := range roots {
		for _, s := range loadSkillsFromPaths([]string{r.Dir}) {
			s.Name = r.Plugin + ":" + s.Name
			if seen[s.Name] {
				continue
			}
			seen[s.Name] = true
			s.Plugin = r.Plugin
			skills = append(skills, s)
		}
	}
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

// KaizenDigestAdmittedForModels reports, for each given model id, whether at
// least one Kaizen tuning skill carrying a force-injected digest
// (`<!-- kaizen:digest -->`) is admitted for that model in root. The repo stack
// is detected ONCE per call, so a UI can badge every listed model (the web
// model picker) without paying stack detection per model. Skill loading is
// served by the cached loader; matching rules stay encapsulated in the private
// helpers above. A model not present in the map is simply not admitted.
func KaizenDigestAdmittedForModels(root string, models []string) map[string]bool {
	out := make(map[string]bool, len(models))
	if len(models) == 0 {
		return out
	}
	detected := stackdetect.Detect(root)
	for _, s := range LoadSkillsForRoot(root) {
		if s.TunedFor == "" || s.Digest == "" {
			continue
		}
		for _, id := range models {
			if out[id] {
				continue
			}
			if kaizenAdmitted(s, id, detected) {
				out[id] = true
			}
		}
	}
	return out
}

// KaizenSkillsForModel returns ONLY the Kaizen (per-model tuned) skills admitted
// for the active model + detected stack — normal skills are excluded (use
// LoadSkills for those). The discovery corpus calls this so a gated tuning skill
// is ALWAYS listed in the always-visible names-index regardless of embedding
// rank; its full SKILL.md body still loads on demand via the skill tool.
func KaizenSkillsForModel(root, activeModel string) []Skill {
	all := LoadSkillsForRoot(root)
	detected := stackdetect.Detect(root)

	out := make([]Skill, 0, 2)
	for _, s := range all {
		if s.TunedFor == "" {
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

// universalStacks are the Kaizen corpora admitted on an exact model-id match
// ALONE, with no stackdetect gate. Two reasons a corpus earns a place here:
//
//   - "conduct" / "hallucination" are not tech stacks at all. They probe model
//     behaviour that applies in every repo, so no marker file could detect them
//     (docs/okf/hallucination/meta.yaml sets `detection: mode: universal`).
//   - "pdf" is a real document format, but its only marker is a *.pdf file at the
//     repo root or one/two levels down (internal/stackdetect). That is a proxy for
//     "a PDF already exists here", and it cannot see the tasks these corrections
//     exist for: generating a PDF from scratch (nothing on disk yet), a PDF
//     attached from outside the repo, or a PDF deeper than the glob limit. It also
//     misses the session that first writes a PDF, because the prompt context is
//     built once and cached for prefix stability before the request exists. The
//     result was a create-a-PDF task running with no catalogue line and no digest:
//     the model had no way to learn the corrections were there.
//
// A corpus listed here has its digest force-injected into EVERY session for models
// that have a tuning skill in that corpus (KaizenDigestBlock), at ~1.7KB for pdf.
// That is deliberate: cheap next to the defects it prevents, and a
// request-conditional gate is not implementable without re-breaking prefix
// stability.
var universalStacks = map[string]bool{
	"conduct":       true,
	"hallucination": true,
	"pdf":           true,
}

// kaizenAdmitted reports whether a Kaizen skill is admitted for the session:
// the active model must match the skill's tuned_for AND the skill's stack must
// be active (a universal corpus, an empty stack, or a detected stack).
func kaizenAdmitted(s Skill, activeModel string, detected []string) bool {
	if !modelMatchesTuned(activeModel, s.TunedFor) {
		return false
	}
	return stackActive(s.Stack, detected)
}

// stackActive reports whether a Kaizen skill's stack is active for this repo.
// The universal corpora (see universalStacks) and an empty stack are always
// active; any other stack must appear in the detected set.
func stackActive(stack string, detected []string) bool {
	if strings.TrimSpace(stack) == "" || universalStacks[strings.ToLower(strings.TrimSpace(stack))] {
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
//
// OpenRouter-style route variants after a colon (":free", ":nitro",
// ":extended", …) are stripped before compare so
// "openrouter/tencent/hy3:free" matches tuned_for "tencent/hy3".
//
// opencode-zen marks free-tier models with a hyphenated "-free" suffix on the
// base id instead of a colon variant (a different provider's convention living
// in the same function). That suffix is also stripped, so
// "opencode/deepseek-v4-flash-free" matches tuned_for "deepseek-v4-flash".
// Only the literal trailing "-free" token is removed — never everything after
// the last hyphen — so "deepseek-v4-flash" is never mangled to "deepseek-v4".
func modelMatchesTuned(activeModel, tunedFor string) bool {
	a := strings.ToLower(strings.TrimSpace(activeModel))
	t := strings.ToLower(strings.TrimSpace(tunedFor))
	if a == "" || t == "" {
		return false
	}
	// Strip OpenRouter (and similar) variant suffixes: "id:free" → "id".
	if i := strings.IndexByte(a, ':'); i >= 0 {
		a = a[:i]
	}
	if a == t || strings.HasSuffix(a, "/"+t) {
		return true
	}
	// Retry with an opencode-zen "-free" tier suffix stripped. Raw match is
	// tried first (above) so a tuned_for that genuinely ends in "-free" still
	// matches without the suffix being dropped.
	if base := strings.TrimSuffix(a, "-free"); base != a {
		return base == t || strings.HasSuffix(base, "/"+t)
	}
	return false
}

// ProjectLocalSkillDirs returns the project-root skill directories that should
// be scanned for project-local skills. root is the project root (absolute path).
func ProjectLocalSkillDirs(root string) []string {
	return []string{
		filepath.Join(root, ".opencode", "skills"),
		filepath.Join(root, ".claude", "skills"),
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
//
// Precedence is first-wins-on-directory-name (see loadSkillsFromPaths), so the
// order below decides which copy of a duplicated skill is served:
//
//  1. ~/.config/opencode/skills — ocode's native global dir, and the installer's
//     write target (see globalSkillsDir), so it must stay first.
//  2. ~/.agents/skills — shared agent skills tree.
//  3. ~/.claude/skills — Claude Code's user-global skills dir. Listed after the
//     two above so an ocode-native copy still wins, but scanned so skills
//     installed only by Claude Code are discoverable. Project-local
//     <root>/.claude/skills was already supported (ProjectLocalSkillDirs), so
//     omitting the user-global counterpart was an asymmetry, not a policy.
//  4. Project-local dirs, then bundled.
//
// Each base root also gets a "<root>/kaizen" entry appended, because
// loadSkillsFromPaths descends only a single level (<path>/<name>/SKILL.md).
func SkillSearchPathsForRoot(root string) []string {
	var paths []string

	home, err := os.UserHomeDir()
	if err == nil {
		paths = append(paths, filepath.Join(home, ".config", "opencode", "skills"))
		paths = append(paths, filepath.Join(home, ".agents", "skills"))
		paths = append(paths, filepath.Join(home, ".claude", "skills"))
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

// digestStart / digestEnd delimit the force-injected directive block in a
// SKILL.md body. HTML comments so they are invisible in rendered markdown.
const (
	digestStart = "<!-- kaizen:digest -->"
	digestEnd   = "<!-- /kaizen:digest -->"
)

// extractDigest returns the trimmed text between the digest markers, or "" if
// the markers are absent or malformed (start without end). It never returns a
// header-only/whitespace block: an empty body yields "".
func extractDigest(content string) string {
	i := strings.Index(content, digestStart)
	if i < 0 {
		return ""
	}
	rest := content[i+len(digestStart):]
	j := strings.Index(rest, digestEnd)
	if j < 0 {
		return "" // unterminated marker: treat as no digest rather than swallow the rest of the file
	}
	return strings.TrimSpace(rest[:j])
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
	skill.Digest = extractDigest(content)

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

// KaizenDigestBlock renders the force-injected directive digests of every Kaizen
// skill admitted for (root, activeModel) that carries a `<!-- kaizen:digest -->`
// section. Unlike the catalog (which advertises a skill the model MAY load),
// this puts the skill's hard rules directly in the base prompt as authoritative
// instructions — the fix for an overconfident model that never loads the body.
//
// Returns exactly "" when no admitted tuning skill has a digest, so a
// non-matching model's cached prefix is byte-identical to having no tuning skill
// at all. Kaizen skills without a digest section contribute nothing.
func KaizenDigestBlock(root, activeModel string) string {
	skills := KaizenSkillsForModel(root, activeModel)
	var parts []string
	for _, s := range skills {
		if s.Digest != "" {
			parts = append(parts, s.Digest)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n--- Model-Specific Directives (always in effect) ---\n")
	b.WriteString("These are corrective rules tuned for the active model. Follow them as hard requirements, not optional guidance.\n")
	for _, d := range parts {
		b.WriteString(d)
		b.WriteString("\n")
	}
	return b.String()
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
	all := LoadSkillsForRoot(root)
	// Exact name first ("superpowers:brainstorming" for a plugin skill), then
	// the directory name, preferring an ordinary skill over a plugin one so a
	// bare "brainstorming" still resolves to a plugin's skill only when no
	// user/project skill has that name.
	for _, s := range all {
		if s.Name == name {
			skill := s
			return &skill, nil
		}
	}
	for _, wantPlugin := range []bool{false, true} {
		for _, s := range all {
			if (s.Plugin != "") == wantPlugin && filepath.Base(filepath.Dir(s.Source)) == name {
				skill := s
				return &skill, nil
			}
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
