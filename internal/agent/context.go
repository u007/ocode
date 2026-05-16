package agent

import (
	"os"
	"path/filepath"

	"github.com/jamesmercstudio/ocode/internal/plugins"
)

func LoadContext() string {
	var context string
	files := []string{"AGENTS.md", "CLAUDE.md", ".cursorrules"}

	for _, f := range files {
		if content, err := os.ReadFile(f); err == nil {
			context += "\n--- " + f + " ---\n" + string(content) + "\n"
		}
	}

	rulesDir := filepath.Join(".opencode", "rules")
	if entries, err := os.ReadDir(rulesDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && filepath.Ext(entry.Name()) == ".md" {
				if content, err := os.ReadFile(filepath.Join(rulesDir, entry.Name())); err == nil {
					context += "\n--- " + entry.Name() + " ---\n" + string(content) + "\n"
				}
			}
		}
	}

	if pluginInstr := plugins.LoadPluginInstructions(); pluginInstr != "" {
		context += pluginInstr
	}

	return context
}
