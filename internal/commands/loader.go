package commands

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/jamesmercstudio/ocode/internal/plugins"
)

type Command struct {
	Name        string
	Description string
	Prompt      string
	HasArgs     bool
}

func LoadCommands() []Command {
	var cmds []Command
	seen := make(map[string]bool)

	for _, dir := range commandSearchPaths() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if filepath.Ext(entry.Name()) != ".md" {
				continue
			}
			name := strings.TrimSuffix(entry.Name(), ".md")
			if seen[name] {
				continue
			}

			path := filepath.Join(dir, entry.Name())
			cmd, err := parseCommandFile(path)
			if err != nil {
				continue
			}

			if cmd.Name == "" {
				cmd.Name = name
			}

			seen[name] = true
			cmds = append(cmds, cmd)
		}
	}

	return cmds
}

func commandSearchPaths() []string {
	var paths []string

	home, err := os.UserHomeDir()
	if err == nil {
		if runtime.GOOS == "windows" {
			paths = append(paths, filepath.Join(os.Getenv("APPDATA"), "opencode", "commands"))
		} else {
			paths = append(paths, filepath.Join(home, ".config", "opencode", "commands"))
		}
	}

	cwd, err := os.Getwd()
	if err == nil {
		paths = append(paths, filepath.Join(cwd, ".opencode", "commands"))
		paths = append(paths, filepath.Join(cwd, "commands"))
	}

	paths = append(paths, plugins.LoadPluginCommandDirPaths()...)

	return paths
}

func parseCommandFile(path string) (Command, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Command{}, err
	}

	content := string(data)
	var name, description, prompt string
	inFrontmatter := false
	frontmatterEnded := false

	lines := strings.SplitN(content, "\n", 50)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "---" {
			if !inFrontmatter {
				inFrontmatter = true
				continue
			}
			frontmatterEnded = true
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
		if strings.HasPrefix(trimmed, "has_args:") {
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, "has_args:"))
			if val == "true" {
				// HasArgs will be set below
			}
		}
	}

	if frontmatterEnded {
		idx := strings.Index(content, "---")
		if idx != -1 {
			second := strings.Index(content[idx+3:], "---")
			if second != -1 {
				prompt = strings.TrimSpace(content[idx+3+second+3:])
			}
		}
	}

	hasArgs := strings.Contains(prompt, "{{args}}") || strings.Contains(prompt, "{args}")

	return Command{
		Name:        name,
		Description: description,
		Prompt:      prompt,
		HasArgs:     hasArgs,
	}, nil
}

func LoadCommand(name string) (*Command, error) {
	for _, dir := range commandSearchPaths() {
		candidates := []string{
			filepath.Join(dir, name+".md"),
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
			cmd, err := parseCommandFile(abs)
			if err != nil {
				continue
			}
			if cmd.Name == "" {
				cmd.Name = name
			}
			return &cmd, nil
		}
	}
	return nil, nil
}
