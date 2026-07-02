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
	system := titleSystemPrompt
	if def := lookupHiddenAgent("title"); def != nil && strings.TrimSpace(def.SystemPrompt) != "" {
		system = def.SystemPrompt
	}
	prompt := buildTitlePrompt(userMsg, assistantMsg)
	return a.generateTitleWithClients(a.titleClients(), system, prompt)
}

// generateTitleWithClients tries each client in order until one returns a
// non-empty sanitized title. A failing or empty result falls through to the
// next client so a rate-limited small model doesn't lose the title.
func (a *Agent) generateTitleWithClients(clients []LLMClient, system, prompt string) string {
	for _, client := range clients {
		content, err := a.titleChat(client, system, prompt)
		if err != nil {
			emitDebug("TITLE", fmt.Sprintf("%s/%s error: %v", client.GetProvider(), client.GetModel(), err))
			continue
		}
		if t := sanitizeTitle(content); t != "" {
			return t
		}
		emitDebug("TITLE", fmt.Sprintf("%s/%s returned empty title", client.GetProvider(), client.GetModel()))
	}
	return ""
}

func (a *Agent) titleChat(client LLMClient, system, prompt string) (string, error) {
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
		a.RecordSideUsageFromMessage(resp)
		done <- struct {
			content string
			err     error
		}{resp.Content, nil}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-done:
		return r.content, r.err
	}
}

// titleClients returns candidate clients in precedence order:
// registry "title" agent's Model > Ocode.RecapModel > Ocode.SmallModel > main
// client. Duplicate model strings are collapsed; the main client is always the
// final fallback.
func (a *Agent) titleClients() []LLMClient {
	var clients []LLMClient
	if a.config != nil {
		var models []string
		if def := lookupHiddenAgent("title"); def != nil {
			models = append(models, strings.TrimSpace(def.Model))
		}
		if a.config.Ocode.RecapModelEnabled {
			models = append(models, strings.TrimSpace(a.config.Ocode.RecapModel))
		}
		if a.config.Ocode.SmallModelEnabled {
			models = append(models, strings.TrimSpace(a.config.Ocode.SmallModel))
		}
		seen := map[string]bool{}
		for _, m := range models {
			if m == "" || seen[m] {
				continue
			}
			seen[m] = true
			if c := NewClient(a.config, m); c != nil {
				clients = append(clients, c)
			}
		}
	}
	if a.client != nil {
		clients = append(clients, a.client)
	}
	return clients
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
