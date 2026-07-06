package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/knowledge"
)

// docsInit dispatches the context subagent to bootstrap existing docs/ into an
// OKF bundle. First writes the marker index.md and log.md, then dispatches the
// context agent to annotate existing files.
func (m *model) docsInit() string {
	if m.agent == nil {
		return "Error: no active agent"
	}
	if !m.agent.DocPromptEnabled() {
		return "Error: knowledge system is not enabled. Run /docs on first."
	}

	wd := m.workDir
	if wd == "" {
		wd, _ = os.Getwd()
	}

	// Check if bundle already exists.
	if bundle, ok := knowledge.DetectBundle(wd); ok {
		// Re-run audit (idempotent).
		return m.docsReAudit(bundle)
	}

	// Init the bundle: create index.md + log.md with marker.
	if err := knowledge.InitBundle(wd); err != nil {
		return fmt.Sprintf("Error initializing bundle: %v", err)
	}

	// Now dispatch the context sub-agent to annotate existing files.
	return m.dispatchContextAgent(`You are initializing the OKF knowledge bundle for this project.

1. Scan the docs/ directory for existing markdown files (excluding index.md, log.md, and files matching default exclusion patterns: PLAN-*.md and *.OCODE.md).
2. Also check if docs/.okfignore exists; if so, read it and skip any files whose names or paths match its patterns.
3. For each included file that can be classified, add frontmatter (type, title, description, tags where appropriate) preserving all existing content.
4. Do NOT delete any content or files.
5. After classification, use doc_write for each file to add frontmatter.
6. Finally, produce a summary of what was classified and any notes about files that could not be classified.

Types of files to create:
- decisions/ — architecture decisions, technical choices
- playbooks/ — how-to guides, runbooks
- schemas/ — data models, API structures
- gotchas/ — known pitfalls, workarounds
- concepts/ — general documentation, explanations

If a file doesn't fit any category, leave it as-is (non-conforming).`)
}

func (m *model) docsReAudit(bundle *knowledge.Bundle) string {
	// Generate a fresh index under the bundle lock (M1).
	if err := knowledge.WithBundleLock(bundle.Root, func() error {
		return knowledge.GenerateIndex(bundle)
	}); err != nil {
		return fmt.Sprintf("Error regenerating index: %v", err)
	}

	// Count docs.
	allDocs, err := bundle.Docs()
	if err != nil {
		return fmt.Sprintf("Error reading bundle: %v", err)
	}

	conforming := 0
	deprecated := 0
	unclassified := 0
	for _, d := range allDocs {
		if d.Conforming {
			conforming++
		} else {
			unclassified++
		}
		if d.Status == "deprecated" {
			deprecated++
		}
	}

	return fmt.Sprintf("Knowledge bundle re-audited at `%s`.\n- Conforming docs: %d\n- Non-conforming: %d\n- Deprecated: %d\n\nIndex has been regenerated.", bundle.Root, conforming, unclassified, deprecated)
}

// docsUpdate triggers a forced maintenance pass.
func (m *model) docsUpdate(focus string) string {
	if m.agent == nil {
		return "Error: no active agent"
	}
	if !m.agent.DocPromptEnabled() {
		return "Error: knowledge system is not enabled. Run /docs on first."
	}

	wd := m.workDir
	if wd == "" {
		wd, _ = os.Getwd()
	}
	if _, ok := knowledge.DetectBundle(wd); !ok {
		return "No OKF knowledge bundle found. Run /docs init first."
	}

	m.agent.QueueDocMaintenance(agent.DocMaintenanceRequest{
		WorkDir:        wd,
		Forced:         true,
		Focus:          focus,
		RecentMessages: m.memoryMaintenanceContext(),
	})

	if focus != "" {
		return fmt.Sprintf("Maintenance pass queued with focus: %q. Check /docs status for updates.", focus)
	}
	return "Maintenance pass queued. Check /docs status for updates."
}

// docsCleanup lists deprecated docs. When confirm is true, deletes them
// under WithBundleLock, updates the log, and regenerates the index.
func (m *model) docsCleanup(confirm bool) string {
	if m.agent == nil {
		return "Error: no active agent"
	}
	wd := m.workDir
	if wd == "" {
		wd, _ = os.Getwd()
	}
	bundle, ok := knowledge.DetectBundle(wd)
	if !ok {
		return "No OKF knowledge bundle found. Run /docs init first."
	}

	allDocs, err := bundle.Docs()
	if err != nil {
		return fmt.Sprintf("Error reading bundle: %v", err)
	}

	var deprecated []*knowledge.Doc
	for _, d := range allDocs {
		if d.Status == "deprecated" {
			deprecated = append(deprecated, d)
		}
	}
	sort.Slice(deprecated, func(i, j int) bool {
		return deprecated[i].Path < deprecated[j].Path
	})

	if len(deprecated) == 0 {
		return "No deprecated documents to clean up."
	}

	// Build a summary for display.
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d deprecated document(s):\n\n", len(deprecated)))
	for _, d := range deprecated {
		b.WriteString(fmt.Sprintf("- `%s`", d.Path))
		if d.DeprecatedReason != "" {
			b.WriteString(fmt.Sprintf(" — %s", d.DeprecatedReason))
		}
		b.WriteString("\n")
	}

	if !confirm {
		b.WriteString(fmt.Sprintf("\nRun `/docs cleanup --yes` to delete these %d file(s). This cannot be undone.", len(deprecated)))
		return b.String()
	}

	// Perform deletion under lock.
	deleted := 0
	err = knowledge.WithBundleLock(bundle.Root, func() error {
		for _, d := range deprecated {
			fullPath := filepath.Join(bundle.Root, d.Path)
			if err := os.Remove(fullPath); err != nil {
				// Log but continue — don't let one failure abort the batch.
				agent.DebugAppendf("DOCS", "cleanup: failed to remove %s: %v", fullPath, err)
				continue
			}
			if err := knowledge.AppendLog(bundle, "Deletion", d.Path, fmt.Sprintf("Deleted deprecated doc: %s", d.Title)); err != nil {
				agent.DebugAppendf("DOCS", "cleanup: failed to append log for %s: %v", d.Path, err)
			}
			deleted++
		}
		// Regenerate index.
		if err := knowledge.GenerateIndex(bundle); err != nil {
			return fmt.Errorf("regenerate index after cleanup: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Sprintf("Error during cleanup: %v", err)
	}

	return fmt.Sprintf("Deleted %d deprecated document(s). Index and log updated.", deleted)
}

// docsStatus returns rich status about the knowledge system.
func (m *model) docsStatus() string {
	if m.agent == nil {
		return "No active agent."
	}

	enabled := m.agent.DocPromptEnabled()
	wd := m.workDir
	if wd == "" {
		wd, _ = os.Getwd()
	}

	bundle, bundleOK := knowledge.DetectBundle(wd)

	var b strings.Builder
	b.WriteString("## Knowledge System Status\n\n")
	b.WriteString(fmt.Sprintf("**Doc prompt enabled:** %v\n", enabled))

	if !enabled {
		b.WriteString("\n**Active:** No (DocPromptEnabled is off)\n")
		b.WriteString("Run `/docs on` to enable the doc-first prompt, then `/docs init` to set up the OKF bundle.\n")
		return b.String()
	}

	if !bundleOK {
		b.WriteString("\n**Bundle present:** No\n")
		b.WriteString("Run `/docs init` to create an OKF knowledge bundle.\n")
		return b.String()
	}

	b.WriteString(fmt.Sprintf("**Bundle root:** `%s`\n", bundle.Root))
	b.WriteString(fmt.Sprintf("**OKF version:** %s\n", bundle.OKFVersion))

	allDocs, err := bundle.Docs()
	conforming := 0
	deprecated := 0
	unclassified := 0
	lastLogDate := ""
	if err == nil {
		for _, d := range allDocs {
			if d.Conforming {
				conforming++
			} else {
				unclassified++
			}
			if d.Status == "deprecated" {
				deprecated++
			}
		}
	}

	// Read last log entry date.
	logPath := filepath.Join(bundle.Root, "log.md")
	if data, err := os.ReadFile(logPath); err == nil {
		content := string(data)
		// Find latest "## YYYY-MM-DD" heading.
		for _, line := range strings.Split(content, "\n") {
			if strings.HasPrefix(line, "## ") && len(line) > 3 {
				lastLogDate = strings.TrimPrefix(line, "## ")
			}
		}
	}

	b.WriteString(fmt.Sprintf("**Conforming docs:** %d\n", conforming))
	b.WriteString(fmt.Sprintf("**Non-conforming:** %d\n", unclassified))
	b.WriteString(fmt.Sprintf("**Deprecated:** %d\n", deprecated))
	if lastLogDate != "" {
		b.WriteString(fmt.Sprintf("**Last log entry:** %s\n", lastLogDate))
	}
	b.WriteString("\n**Active:** Yes\n")

	return b.String()
}

// dispatchContextAgent sends a prompt to the context subagent synchronously
// via the task tool, and returns the result as a string.
func (m *model) dispatchContextAgent(prompt string) string {
	if m.agent == nil {
		return "Error: no active agent"
	}
	taskTool, ok := m.agent.GetTool("task")
	if !ok {
		return "Error: task tool not available"
	}
	task, ok := taskTool.(*agent.TaskTool)
	if !ok {
		return "Error: task tool has unexpected type"
	}

	result, err := task.ExecuteRaw("context", prompt, false)
	if err != nil {
		return fmt.Sprintf("Error dispatching context agent: %v", err)
	}
	return result
}
