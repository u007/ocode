package tui

import (
	"fmt"
	"os"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/commandctx"
	"github.com/u007/ocode/internal/knowledge"
)

// docsInit delegates to the shared commandctx.DocsInit so the server's
// /api/docs/init endpoint behaves identically. Returns the result text; when a
// new bundle was created the annotation prompt is dispatched to the context
// agent here (the server returns it to the web client instead).
func (m *model) docsInit() string {
	if m.agent == nil {
		return "Error: no active agent"
	}
	text, annotate := commandctx.DocsInit(m.agent.DocPromptEnabled(), m.workDirWithDefault())
	if annotate != "" {
		return text + "\n" + m.dispatchContextAgent(annotate)
	}
	return text
}

// docsUpdate triggers a forced maintenance pass on the live agent.
func (m *model) docsUpdate(focus string) string {
	if m.agent == nil {
		return "Error: no active agent"
	}
	if !m.agent.DocPromptEnabled() {
		return "Error: knowledge system is not enabled. Run /docs on first."
	}

	wd := m.workDirWithDefault()
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

// docsCleanup delegates to the shared commandctx.DocsCleanup.
func (m *model) docsCleanup(confirm bool) string {
	return commandctx.DocsCleanup(m.workDirWithDefault(), confirm)
}

// docsStatus delegates to the shared commandctx.DocsStatus.
func (m *model) docsStatus() string {
	enabled := false
	if m.agent != nil {
		enabled = m.agent.DocPromptEnabled()
	}
	return commandctx.DocsStatus(enabled, m.workDirWithDefault())
}

// workDirWithDefault returns the model workspace dir, falling back to the
// process working directory.
func (m *model) workDirWithDefault() string {
	if m.workDir == "" {
		wd, _ := os.Getwd()
		return wd
	}
	return m.workDir
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
