package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/skill"
)

func buildLearnPrompt(workDir string, args []string) (string, error) {
	inventory, err := skill.LoadProjectLearnInventory(workDir)
	if err != nil {
		return "", err
	}
	focus := strings.TrimSpace(strings.Join(args, " "))
	context := skill.BuildLearnContext(inventory, focus)

	var b strings.Builder
	b.WriteString("You are the /learn command for this repository.\n\n")
	b.WriteString("Goal: keep project-local skills useful, small, and easy to update so future repo work needs less rediscovery.\n\n")
	b.WriteString("Operating rules:\n")
	b.WriteString("- Start only from the provided project-root skill inventory.\n")
	b.WriteString("- /learn itself did not inspect repo modules or sections; do not pretend that it did.\n")
	b.WriteString("- Reuse the skill tool to inspect existing skills before proposing changes.\n")
	b.WriteString("- Load the skill-creator skill before proposing any new SKILL.md shape or rewrite.\n")
	b.WriteString("- If a dedicated skill-update helper skill is available in the catalog, load and use it too.\n")
	b.WriteString("- Only if gap analysis is necessary, delegate narrow read-only reconnaissance to the explore subagent for the exact module(s) you need.\n")
	b.WriteString("- Prefer a few high-ROI project-local skills over many tiny skills.\n")
	b.WriteString("- Keep the maintenance plan low-touch: lightweight frontmatter, narrow references, and simple update triggers.\n\n")
	b.WriteString("Return these sections exactly:\n")
	b.WriteString("1. Current skill inventory\n")
	b.WriteString("2. Proposed discovery plan\n")
	b.WriteString("3. High-value skills to create or update\n")
	b.WriteString("4. Likely outdated skills to review\n")
	b.WriteString("5. Minimal-maintenance workflow\n")
	b.WriteString("6. Ready-to-use context-manager prompt\n")
	b.WriteString("7. Recommended next actions\n\n")
	if focus != "" {
		fmt.Fprintf(&b, "User focus: %s\n\n", focus)
	}
	b.WriteString("Use concise bullets. When proposing a new or updated skill, include: skill name, target module/section, why it saves discovery time, what references/resources it should own, and what signal should trigger an update. If you need missing-module discovery, say so explicitly and keep it targeted.\n\n")
	b.WriteString("Repository learn context\n")
	b.WriteString("========================\n")
	b.WriteString(context)
	return b.String(), nil
}

func (m *model) handleLearnCmd(args []string) tea.Cmd {
	prompt, err := buildLearnPrompt(m.workDir, args)
	if err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("/learn: %v", err)})
		return nil
	}
	if m.agent != nil {
		m.agent.ResetSubagentDispatch()
	}
	m.rerenderTranscriptAndMaybeScroll()
	return m.sendCustomCommandPrompt(prompt)
}
