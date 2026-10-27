package agent

import (
	"fmt"
	"strings"

	"github.com/u007/ocode/internal/config"
)

// DefaultCommitMsgPrompt is the built-in prompt for AI commit-message
// generation, used when ocode.commit_msg_prompt is empty.
const DefaultCommitMsgPrompt = "Write a concise git commit message (subject line only, max 72 chars) for these changes. Output only the commit message text, nothing else:"

// commitMsgMaxDiffBytes bounds the diff sent to the model. Every surface (TUI
// git tab, web/desktop Git tab) generates through GenerateCommitMessage, so
// they cannot drift on the cap.
const commitMsgMaxDiffBytes = 8000

// CommitMessageModelChain returns the models a commit-message generation
// attempt should try, in order. An explicit commit_msg_model is authoritative
// and is tried alone; otherwise the small model is preferred and the main model
// is the fallback, so generation still works when no small model resolves or
// its request fails. Empty and duplicate entries are dropped.
func CommitMessageModelChain(explicit, small, main string) []string {
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		return []string{explicit}
	}
	models := make([]string, 0, 2)
	seen := make(map[string]bool, 2)
	for _, model := range []string{strings.TrimSpace(small), strings.TrimSpace(main)} {
		if model == "" || seen[model] {
			continue
		}
		seen[model] = true
		models = append(models, model)
	}
	return models
}

// ResolveCommitMessageModelChain resolves the commit-message model chain for
// cfg. It probes the small model only when no explicit commit_msg_model is set,
// because resolution constructs provider clients.
func ResolveCommitMessageModelChain(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	small := ""
	if strings.TrimSpace(cfg.Ocode.CommitMsgModel) == "" {
		small = ResolveSmallModel(cfg)
	}
	return CommitMessageModelChain(cfg.Ocode.CommitMsgModel, small, cfg.Model)
}

// CommitMessageResult is one successful commit-message generation.
type CommitMessageResult struct {
	Text  string
	Model string
	Usage *TokenUsage
	Spend *float64
}

// commitMessageClient builds the side client for model under profile. An empty
// profile keeps the process-global (or TUI) credential behavior.
func commitMessageClient(cfg *config.Config, model, profile string) LLMClient {
	if profile != "" {
		return newClientWithProfileFn(cfg, model, profile)
	}
	return newClientFn(cfg, model)
}

// GenerateCommitMessage summarises diff as a git commit message, trying the
// resolved model chain in order and falling back when a client cannot be built,
// the request fails, or the reply is empty. profile may be empty.
//
// This is the single implementation behind both the TUI git tab's Ctrl+G
// generator and POST /api/git/commit-message (web/desktop Git tab).
func GenerateCommitMessage(cfg *config.Config, profile, diff string) (*CommitMessageResult, error) {
	if cfg == nil {
		return nil, fmt.Errorf("no LLM configured")
	}
	models := ResolveCommitMessageModelChain(cfg)
	if len(models) == 0 {
		return nil, fmt.Errorf("no LLM configured")
	}
	if len(diff) > commitMsgMaxDiffBytes {
		diff = diff[:commitMsgMaxDiffBytes]
	}
	prompt := cfg.Ocode.CommitMsgPrompt
	if prompt == "" {
		prompt = DefaultCommitMsgPrompt
	}
	var lastErr error
	for _, model := range models {
		client := commitMessageClient(cfg, model, profile)
		if client == nil {
			lastErr = fmt.Errorf("%s: no client (provider not configured)", model)
			continue
		}
		resp, err := client.Chat([]Message{{Role: "user", Content: prompt + "\n\n" + diff}}, nil)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", model, err)
			continue
		}
		text := ""
		if resp != nil {
			text = strings.TrimSpace(resp.Content)
		}
		if text == "" {
			lastErr = fmt.Errorf("%s: empty commit message", model)
			continue
		}
		res := &CommitMessageResult{Text: text, Model: model}
		if resp.Usage != nil {
			res.Usage = resp.Usage
		}
		if resp.Spend != nil {
			s := *resp.Spend
			res.Spend = &s
		} else if resp.Usage != nil && resp.Model != "" && promptAndCompletionTokens(resp.Usage) > 0 {
			res.Spend = resp.Usage.Spend(resp.Model)
		}
		return res, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no LLM configured")
	}
	return nil, lastErr
}

func promptAndCompletionTokens(u *TokenUsage) int64 {
	if u == nil {
		return 0
	}
	var n int64
	if u.PromptTokens != nil {
		n += *u.PromptTokens
	}
	if u.CompletionTokens != nil {
		n += *u.CompletionTokens
	}
	return n
}
