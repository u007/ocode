package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type LearnSkillEntry struct {
	Skill
	Source string
}

type LearnInventory struct {
	Root          string
	ProjectSkills []LearnSkillEntry
}

func LoadProjectLearnInventory(root string) (LearnInventory, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return LearnInventory{}, err
	}
	inventory := LearnInventory{Root: absRoot}
	for _, dir := range ProjectLocalSkillDirs(absRoot) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			skillPath := filepath.Join(dir, entry.Name(), "SKILL.md")
			content, err := os.ReadFile(skillPath)
			if err != nil {
				continue
			}
			parsed := parseSkillMetadata(string(content))
			if parsed.Name == "" {
				parsed.Name = entry.Name()
			}
			parsed.Content = string(content)
			parsed.Source = skillPath
			inventory.ProjectSkills = append(inventory.ProjectSkills, LearnSkillEntry{
				Skill:  parsed,
				Source: skillPath,
			})
		}
	}
	sort.Slice(inventory.ProjectSkills, func(i, j int) bool {
		return strings.ToLower(inventory.ProjectSkills[i].Name) < strings.ToLower(inventory.ProjectSkills[j].Name)
	})
	return inventory, nil
}

func BuildLearnContext(inventory LearnInventory, focus string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Project root: %s\n", inventory.Root)
	if strings.TrimSpace(focus) != "" {
		fmt.Fprintf(&b, "User focus: %s\n", strings.TrimSpace(focus))
	}
	b.WriteString("\nProject-root skills\n")
	b.WriteString(strings.Repeat("-", 19) + "\n")
	if len(inventory.ProjectSkills) == 0 {
		b.WriteString("- (none found under skills/, .opencode/skills/, or .claude/skill)\n")
	} else {
		for _, s := range inventory.ProjectSkills {
			fmt.Fprintf(&b, "- %s\n", s.Name)
			if s.Description != "" {
				fmt.Fprintf(&b, "  description: %s\n", s.Description)
			}
			if s.WhenToUse != "" {
				fmt.Fprintf(&b, "  when_to_use: %s\n", s.WhenToUse)
			}
			fmt.Fprintf(&b, "  source: %s\n", makeRel(inventory.Root, s.Source))
		}
	}
	b.WriteString("\nNote\n")
	b.WriteString(strings.Repeat("-", 4) + "\n")
	b.WriteString("- /learn itself did not inspect repo modules or do gap discovery.\n")
	b.WriteString("- If module/section discovery is needed, delegate targeted read-only exploration after starting from this skill inventory.\n")
	return b.String()
}

func makeRel(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.Clean(path)
	}
	return filepath.Clean(rel)
}
