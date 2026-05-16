package skill

import (
	"os"
	"path/filepath"
	"strings"
)

type Skill struct {
	Name        string
	Description string
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

			sName, sDesc := parseSkillMetadata(string(content))
			if sName == "" {
				sName = name
			}

			seen[name] = true
			skills = append(skills, Skill{
				Name:        sName,
				Description: sDesc,
				Content:     string(content),
				Source:      skillPath,
			})
		}
	}

	return skills
}

func skillSearchPaths() []string {
	var paths []string

	home, err := os.UserHomeDir()
	if err == nil {
		paths = append(paths, filepath.Join(home, ".config", "opencode", "skills"))
	}

	cwd, err := os.Getwd()
	if err == nil {
		paths = append(paths, filepath.Join(cwd, ".opencode", "skills"))
		paths = append(paths, filepath.Join(cwd, "skills"))
	}

	return paths
}

func parseSkillMetadata(content string) (name, description string) {
	lines := strings.SplitN(content, "\n", 20)
	inFrontmatter := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			if !inFrontmatter {
				inFrontmatter = true
				continue
			}
			break
		}
		if !inFrontmatter {
			continue
		}
		if strings.HasPrefix(trimmed, "name:") {
			name = strings.TrimSpace(strings.TrimPrefix(trimmed, "name:"))
		}
		if strings.HasPrefix(trimmed, "description:") {
			description = strings.TrimSpace(strings.TrimPrefix(trimmed, "description:"))
		}
	}

	return
}

func LoadSkill(name string) (*Skill, error) {
	for _, dir := range skillSearchPaths() {
		candidates := []string{
			filepath.Join(dir, name+".md"),
			filepath.Join(dir, name, "SKILL.md"),
		}
		for _, p := range candidates {
			abs, err := filepath.Abs(p)
			if err != nil {
				continue
			}
			absDir, err := filepath.Abs(dir)
			if err != nil {
				continue
			}
			if !strings.HasPrefix(abs, absDir+string(filepath.Separator)) && abs != absDir {
				continue
			}
			content, err := os.ReadFile(abs)
			if err != nil {
				continue
			}
			sName, sDesc := parseSkillMetadata(string(content))
			if sName == "" {
				sName = name
			}
			return &Skill{
				Name:        sName,
				Description: sDesc,
				Content:     string(content),
				Source:      abs,
			}, nil
		}
	}
	return nil, nil
}
