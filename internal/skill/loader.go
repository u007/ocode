package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Skill struct {
	Name        string
	Description string
	WhenToUse   string
	Content     string
	Source      string
}

func LoadSkills() []Skill {
	var skills []Skill
	seen := make(map[string]bool)

	for _, dir := range skillSearchPaths() {
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
	var paths []string

	home, err := os.UserHomeDir()
	if err == nil {
		paths = append(paths, filepath.Join(home, ".config", "opencode", "skills"))
		paths = append(paths, filepath.Join(home, ".agents", "skills"))
	}

	if cwd, err := os.Getwd(); err == nil {
		paths = append(paths, ProjectLocalSkillDirs(cwd)...)
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

	skill.Description = clampSentence(skill.Description, 160)
	skill.WhenToUse = clampSentence(skill.WhenToUse, 160)
	return skill
}

func BuildCatalog() string {
	skills := LoadSkills()
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

func LoadSkill(name string) (*Skill, error) {
	for _, s := range LoadSkills() {
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
