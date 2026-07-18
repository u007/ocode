package server

import (
	"context"
	"fmt"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/scheduler"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)

// schedulerRunner implements scheduler.AgentRunner for the server host. It
// builds a fresh agent for each job (so the per-job permission mode is bound
// cleanly), loads/seeds the persistent cron:<id> session, runs one Step, and
// persists the transcript with a retention cap.
type schedulerRunner struct {
	cfg *config.Config
	// MaxContextTokens is the per-job session cap. The persisted transcript
	// is trimmed so the seeded context + recent turns fit within this budget
	// (heuristic: 1 token ≈ 4 chars, industry standard for English). Default
	// 60_000 when zero — large enough to cover 2-3 hours of dense
	// conversation at most model context windows (200K).
	MaxContextTokens int
}

// RunScheduledJob satisfies scheduler.AgentRunner.
func (r *schedulerRunner) RunScheduledJob(ctx context.Context, job *scheduler.Job) (string, error) {
	cfg := r.cfg
	if cfg == nil {
		return "", fmt.Errorf("scheduler runner: no config")
	}
	model := cfg.Model
	if model == "" {
		return "", fmt.Errorf("no model configured")
	}
	client := agent.NewClient(cfg, model)
	if client == nil {
		return "", fmt.Errorf("failed to create LLM client")
	}
	tools, lspMgr := tool.LoadBuiltins(cfg, nil)
	ag := agent.NewAgent(client, tools, cfg, lspMgr)
	ag.LoadExternalToolsWithMCP(cfg)
	ag.SetAdvisorEnabled(false)
	if job.Payload.PermMode != "" {
		ag.Permissions().SetMode(agent.PermissionMode(job.Payload.PermMode))
	}

	sessionID := "cron:" + job.ID
	prefix := scheduler.ContextPrefix(job)
	msgs, err := loadOrSeedCronSession(sessionID, prefix, job.Payload.Message)
	if err != nil {
		return "", err
	}
	result, err := ag.Step(msgs)
	if err != nil {
		return "", err
	}
	budget := r.MaxContextTokens
	if budget <= 0 {
		budget = 60_000
	}
	result = capCronTokens(result, budget)
	if err := session.Save(sessionID, job.Name, result, map[string]any{
		"scheduled": true,
		"job_id":    job.ID,
	}); err != nil {
		return "", err
	}
	return lastCronAssistantContent(result), nil
}

// loadOrSeedCronSession loads the per-job session or seeds a new one with
// the context prefix + prompt as the first user message.
func loadOrSeedCronSession(id, prefix, prompt string) ([]agent.Message, error) {
	s, err := session.Load(id)
	if err == nil && len(s.Messages) > 0 {
		return append(s.Messages, agent.Message{Role: "user", Content: prefix + "\n" + prompt}), nil
	}
	return []agent.Message{{Role: "user", Content: prefix + "\n" + prompt}}, nil
}

// capCronTokens trims a transcript to fit within the given token budget,
// always keeping the seeded first message (which carries the schedule
// context) and the most recent turns. Token estimation uses the
// chars/4 heuristic — cheap, no tokenizer dependency, accurate enough for
// retention decisions (we'd rather over-trim by ~10% than under-trim and
// risk blowing the context window on the next firing).
func capCronTokens(msgs []agent.Message, tokenBudget int) []agent.Message {
	if len(msgs) == 0 || tokenBudget <= 0 {
		return msgs
	}
	// Quick path: total already fits.
	if estimateCronTokens(msgs) <= tokenBudget {
		return msgs
	}
	head := msgs[0]
	tail := make([]agent.Message, 0, len(msgs))
	budgetChars := tokenBudget * 4
	used := len(head.Content) + 1 // +1 for joining newline
	for i := len(msgs) - 1; i >= 1; i-- {
		c := len(msgs[i].Content) + 1
		if used+c > budgetChars {
			break
		}
		tail = append([]agent.Message{msgs[i]}, tail...)
		used += c
	}
	out := make([]agent.Message, 0, 1+len(tail))
	out = append(out, head)
	out = append(out, tail...)
	return out
}

// estimateCronTokens returns the rough token count of msgs using the
// chars/4 heuristic.
func estimateCronTokens(msgs []agent.Message) int {
	total := 0
	for _, m := range msgs {
		total += len(m.Content) + 1
	}
	return total / 4
}

// lastCronAssistantContent returns the content of the most recent assistant message.
func lastCronAssistantContent(msgs []agent.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			return msgs[i].Content
		}
	}
	return ""
}

// RunScheduledJob is the public entry point used by main.go's
// serverSchedulerRunner shim. It builds a fresh schedulerRunner with cfg and
// runs the job. Exposed so main.go (which depends on this package) does not
// need to know about the unexported schedulerRunner struct.
func RunScheduledJob(ctx context.Context, cfg *config.Config, job *scheduler.Job) (string, error) {
	return (&schedulerRunner{cfg: cfg}).RunScheduledJob(ctx, job)
}
