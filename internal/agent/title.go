package agent

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	titleSystemPrompt = `Generate a concise 3-7 word title for this conversation.

Rules:
- Plain text only, no quotes, no punctuation at the end
- Title-case, no trailing period
- Describe the task/topic, not the participants
- If unclear, prefer the user's apparent goal
- Output ONLY the title, nothing else`

	titleTimeoutSeconds = 15
	titleMaxInputChars  = 4000
	titleMaxOutputChars = 80
)

// GenerateTitleAsync runs a one-shot title-generation LLM call in a background
// goroutine and delivers the result (or empty string on failure) via onResult.
//
// It uses the configured small model when available, falling back to the main
// client. This avoids burning the primary model on a 5-word string.
func (a *Agent) GenerateTitleAsync(userMsg, assistantMsg string, onResult func(string)) {
	if onResult == nil {
		return
	}
	if strings.TrimSpace(userMsg) == "" {
		onResult("")
		return
	}
	go func() {
		title := a.generateTitle(userMsg, assistantMsg)
		onResult(title)
	}()
}

func (a *Agent) generateTitle(userMsg, assistantMsg string) string {
	client := a.titleClient()
	if client == nil {
		return ""
	}

	system := titleSystemPrompt
	if def := lookupHiddenAgent("title"); def != nil && strings.TrimSpace(def.SystemPrompt) != "" {
		system = def.SystemPrompt
	}

	prompt := buildTitlePrompt(userMsg, assistantMsg)
	ctx, cancel := context.WithTimeout(context.Background(), titleTimeoutSeconds*time.Second)
	defer cancel()

	done := make(chan struct {
		content string
		err     error
	}, 1)
	go func() {
		resp, err := client.Chat([]Message{
			{Role: "system", Content: system},
			{Role: "user", Content: prompt},
		}, nil)
		if err != nil {
			done <- struct {
				content string
				err     error
			}{"", err}
			return
		}
		done <- struct {
			content string
			err     error
		}{resp.Content, nil}
	}()

	select {
	case <-ctx.Done():
		emitDebug("TITLE", fmt.Sprintf("timeout: %v", ctx.Err()))
		return ""
	case r := <-done:
		if r.err != nil {
			emitDebug("TITLE", fmt.Sprintf("error: %v", r.err))
			return ""
		}
		return sanitizeTitle(r.content)
	}
}

func (a *Agent) titleClient() LLMClient {
	if a.config == nil {
		return a.client
	}
	// Precedence: registry "title" agent's Model > Ocode.SmallModel > main client.
	if def := lookupHiddenAgent("title"); def != nil {
		if m := strings.TrimSpace(def.Model); m != "" {
			if c := NewClient(a.config, m); c != nil {
				return c
			}
		}
	}
	small := strings.TrimSpace(a.config.Ocode.SmallModel)
	if small == "" {
		return a.client
	}
	if client := NewClient(a.config, small); client != nil {
		return client
	}
	return a.client
}

// lookupHiddenAgent fetches a hidden agent definition by name from the default
// registry, or returns nil if the registry is uninitialised or the name is
// unknown. Hidden agents drive runtime helpers (title generation, compaction)
// and can be overridden by users via markdown files in .opencode/agents/.
func lookupHiddenAgent(name string) *AgentDefinition {
	if DefaultAgentRegistry == nil {
		return nil
	}
	def := DefaultAgentRegistry.Get(name)
	if def == nil || !def.Hidden {
		return nil
	}
	return def
}

func buildTitlePrompt(userMsg, assistantMsg string) string {
	u := truncateForTitle(strings.TrimSpace(userMsg), titleMaxInputChars)
	var b strings.Builder
	b.WriteString("User: ")
	b.WriteString(u)
	if a := strings.TrimSpace(assistantMsg); a != "" {
		b.WriteString("\n\nAssistant: ")
		b.WriteString(truncateForTitle(a, titleMaxInputChars))
	}
	b.WriteString("\n\nTitle:")
	return b.String()
}

func truncateForTitle(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func sanitizeTitle(s string) string {
	s = strings.TrimSpace(s)
	// Strip wrapping quotes or angle brackets the model may add.
	for _, pair := range [][2]string{{"\"", "\""}, {"'", "'"}, {"`", "`"}, {"<", ">"}, {"[", "]"}} {
		if strings.HasPrefix(s, pair[0]) && strings.HasSuffix(s, pair[1]) && len(s) >= 2 {
			s = strings.TrimSpace(s[1 : len(s)-1])
		}
	}
	// Take only the first non-empty line.
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = strings.TrimSpace(s[:idx])
	}
	s = strings.TrimRight(s, ".!?,:;")
	if len(s) > titleMaxOutputChars {
		s = strings.TrimSpace(s[:titleMaxOutputChars])
	}
	return s
}
