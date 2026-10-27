package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/u007/ocode/internal/config"
)

// DefaultCommitMsgPrompt is the built-in prompt for AI commit-message
// generation, used when ocode.commit_msg_prompt is empty.
const DefaultCommitMsgPrompt = "Write a concise git commit message (subject line only, max 72 chars) for these changes. Output only the commit message text, nothing else:"

// commitMsgMaxDiffBytes bounds the diff sent to the model. Every surface (TUI
// git tab, web/desktop Git tab) generates through GenerateCommitMessage, so
// they cannot drift on the cap.
const commitMsgMaxDiffBytes = 8000

// ErrCommitMessageNotConfigured reports that no model can serve commit-message
// generation: none is configured, or the chosen model's provider has no client.
// Callers treat it as a configuration precondition, not a generation failure.
var ErrCommitMessageNotConfigured = errors.New("no LLM configured")

// CommitMessageModel returns the one model a commit-message generation uses: an
// explicit commit_msg_model, else the resolved small model, else the main model.
// Blank entries are skipped. A single model is returned on purpose: a failing
// model is reported, never replaced by another.
func CommitMessageModel(explicit, small, main string) string {
	for _, model := range []string{explicit, small, main} {
		if model = strings.TrimSpace(model); model != "" {
			return model
		}
	}
	return ""
}

// ResolveCommitMessageModel resolves the commit-message model for cfg. It probes
// the small model only when no explicit commit_msg_model is set, because
// resolution constructs provider clients. The probe runs under profile, so the
// profile's own credentials decide which small model resolves.
func ResolveCommitMessageModel(cfg *config.Config, profile string) string {
	if cfg == nil {
		return ""
	}
	small := ""
	if strings.TrimSpace(cfg.Ocode.CommitMsgModel) == "" {
		small = resolveCommitMessageSmallModel(cfg, profile)
	}
	return CommitMessageModel(cfg.Ocode.CommitMsgModel, small, cfg.Model)
}

// resolveCommitMessageSmallModel mirrors ResolveSmallModel, except that each
// candidate is probed under profile. ResolveSmallModel always probes with the
// process-global credentials.
func resolveCommitMessageSmallModel(cfg *config.Config, profile string) string {
	if cfg.Ocode.SmallModel != "" {
		return cfg.Ocode.SmallModel
	}
	for _, candidate := range SmallModelPriority {
		if commitMessageClient(cfg, candidate, profile) != nil {
			return candidate
		}
	}
	return ""
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

// capDiffBytes cuts diff to at most limit bytes without splitting a UTF-8
// sequence. It backs up over at most utf8.UTFMax-1 bytes, the most a valid rune
// can span before its lead byte, so input that is not valid UTF-8 cannot walk
// the cut back to an empty diff.
func capDiffBytes(diff string, limit int) string {
	if len(diff) <= limit {
		return diff
	}
	end := limit
	for i := 0; i < utf8.UTFMax-1 && !utf8.RuneStart(diff[end]); i++ {
		end--
	}
	return diff[:end]
}

// GenerateCommitMessage summarises diff as a git commit message with the one
// resolved model (see ResolveCommitMessageModel). It never falls back to another
// model: a failure is returned naming the model. ctx bounds the provider call.
// profile may be empty.
//
// This is the single implementation behind both the TUI git tab's Ctrl+G
// generator and POST /api/git/commit-message (web/desktop Git tab).
func GenerateCommitMessage(ctx context.Context, cfg *config.Config, profile, diff string) (*CommitMessageResult, error) {
	model := ResolveCommitMessageModel(cfg, profile)
	if model == "" {
		return nil, ErrCommitMessageNotConfigured
	}
	client := commitMessageClient(cfg, model, profile)
	if client == nil {
		return nil, fmt.Errorf("%w: %s has no configured provider", ErrCommitMessageNotConfigured, model)
	}
	// A client that cannot honour ctx would run past the caller's deadline, so it
	// is refused rather than called uncancellably.
	cc, ok := client.(contextChatter)
	if !ok {
		return nil, fmt.Errorf("%s: client cannot honour a request deadline", model)
	}
	diff = capDiffBytes(diff, commitMsgMaxDiffBytes)
	prompt := cfg.Ocode.CommitMsgPrompt
	if prompt == "" {
		prompt = DefaultCommitMsgPrompt
	}
	resp, err := cc.ChatWithContext(ctx, []Message{{Role: "user", Content: prompt + "\n\n" + diff}}, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", model, err)
	}
	text := ""
	if resp != nil {
		text = strings.TrimSpace(resp.Content)
	}
	if text == "" {
		return nil, fmt.Errorf("%s: empty commit message", model)
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
