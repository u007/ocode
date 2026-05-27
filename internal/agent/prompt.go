package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	promptEnvMarker       = "[ocode:environment]"
	promptProviderMarker  = "[ocode:provider]"
	promptModeMarker      = "[ocode:mode]"
	promptContextMarker   = "[ocode:context]"
	promptSelectionMarker = "[ocode:selection]"
)

// PrepareMessages prepends the stable base prompt fragments for this agent.
// It is safe to call more than once; marked fragments are not duplicated.
func (a *Agent) PrepareMessages(messages []Message, selectionContext string) []Message {
	if a == nil {
		return messages
	}
	base := a.BasePromptMessages(selectionContext)
	if len(base) == 0 {
		return messages
	}
	existing := existingPromptMarkers(messages)
	out := make([]Message, 0, len(base)+len(messages))
	for _, msg := range base {
		marker := promptMarker(msg.Content)
		if marker != "" && existing[marker] {
			continue
		}
		out = append(out, msg)
	}
	out = append(out, messages...)
	return out
}

// BasePromptMessages returns the base system fragments shared by TUI, CLI,
// server, ACP, and subagent entrypoints.
func (a *Agent) BasePromptMessages(selectionContext string) []Message {
	var msgs []Message
	if env := a.environmentPrompt(); env != "" {
		msgs = append(msgs, Message{Role: "system", Content: promptEnvMarker + "\n" + env})
	}
	if a.client != nil {
		if pp := modelFamilyPrompt(a.client.GetProvider(), a.client.GetModel()); pp != "" {
			msgs = append(msgs, Message{Role: "system", Content: promptProviderMarker + "\n" + pp})
		}
	}
	agentPrompt := a.Mode().SystemPrompt()
	if a.spec != nil && strings.TrimSpace(a.spec.SystemPrompt) != "" {
		agentPrompt = strings.TrimSpace(a.spec.SystemPrompt)
	}
	if agentPrompt != "" {
		msgs = append(msgs, Message{Role: "system", Content: promptModeMarker + "\n" + agentPrompt})
	}
	ctx := a.getPreloadedContext()
	if ctx == "" {
		ctx = LoadContext()
	}
	if strings.TrimSpace(ctx) != "" {
		msgs = append(msgs, Message{Role: "system", Content: promptContextMarker + "\nContext and rules:\n" + ctx})
	}
	if sel := strings.TrimSpace(selectionContext); sel != "" {
		msgs = append(msgs, Message{Role: "system", Content: promptSelectionMarker + "\n" + sel})
	}
	return msgs
}

func (a *Agent) environmentPrompt() string {
	cwd, _ := os.Getwd()
	root := findWorkspaceRoot(cwd)
	provider, model := "", ""
	if a.client != nil {
		provider = a.client.GetProvider()
		model = a.client.GetModel()
	}
	modelID := strings.Trim(strings.TrimPrefix(provider+"/"+model, "/"), "/")
	if modelID == "" {
		modelID = "unknown"
	}
	return strings.Join([]string{
		fmt.Sprintf("You are powered by the model named %s.", modelID),
		"Here is some useful information about the environment you are running in:",
		"<env>",
		fmt.Sprintf("  Working directory: %s", cwd),
		fmt.Sprintf("  Workspace root folder: %s", root),
		fmt.Sprintf("  Is directory a git repo: %s", yesNo(isGitRepo(root))),
		fmt.Sprintf("  Platform: %s", runtime.GOOS),
		fmt.Sprintf("  Today's date: %s", time.Now().Format("Mon Jan 2 2006")),
		"</env>",
	}, "\n")
}

func existingPromptMarkers(messages []Message) map[string]bool {
	existing := make(map[string]bool)
	for _, msg := range messages {
		if msg.Role != "system" {
			continue
		}
		if marker := promptMarker(msg.Content); marker != "" {
			existing[marker] = true
		}
	}
	return existing
}

func promptMarker(content string) string {
	line := strings.TrimSpace(strings.SplitN(content, "\n", 2)[0])
	if strings.HasPrefix(line, "[ocode:") && strings.HasSuffix(line, "]") {
		return line
	}
	return ""
}

func findWorkspaceRoot(start string) string {
	if start == "" {
		return ""
	}
	curr := start
	for {
		if _, err := os.Stat(filepath.Join(curr, ".git")); err == nil {
			return curr
		}
		if _, err := os.Stat(filepath.Join(curr, "opencode.json")); err == nil {
			return curr
		}
		if _, err := os.Stat(filepath.Join(curr, ".opencode")); err == nil {
			return curr
		}
		parent := filepath.Dir(curr)
		if parent == curr {
			return start
		}
		curr = parent
	}
}

func isGitRepo(root string) bool {
	if root == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(root, ".git"))
	return err == nil
}

func yesNo(ok bool) string {
	if ok {
		return "yes"
	}
	return "no"
}
