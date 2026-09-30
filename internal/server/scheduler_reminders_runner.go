package server

import (
	"context"
	"fmt"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/reminders"
	"github.com/u007/ocode/internal/scheduler"
)

// reminderAgentRunner adapts a reminder/task to the EXISTING cron agent runner
// rather than growing a second one.
//
// The two need exactly the same thing — build a fresh agent for the configured
// model, bind the item's permission mode, run one Step in a per-item session,
// persist the transcript with a token cap, and return the last assistant
// message — and that machinery already exists in schedulerRunner, down to the
// comment explaining why the LSP manager and the agent must be closed per
// firing or they leak goroutines. A second copy would be free to drift from
// those fixes, so the item is projected onto a scheduler.Job and handed over.
//
// The projection is lossless for the runner's purposes and the mapping is
// pinned by TestReminderItemAsCronJob:
//
//	Session id   "cron:" + "rt-" + id   — the "rt-" namespace keeps a
//	              reminder transcript from colliding with a cron job's, whose
//	              id is a bare 8-hex string.
//	Schedule     KindAt at the due time — the runner reads nothing else, and
//	              KindAt is the honest description of a one-shot firing.
//	Payload      Message is the prompt, PermMode the permission bound, Owner
//	              the workdir hint the Telegram resolver reads.
func reminderAgentRunner(cfg *config.Config) *reminderRunnerShim {
	return &reminderRunnerShim{cfg: cfg}
}

type reminderRunnerShim struct {
	cfg *config.Config
}

// RunReminder satisfies reminders.AgentRunner.
func (r *reminderRunnerShim) RunReminder(ctx context.Context, it *reminders.Item) (string, error) {
	if r == nil || r.cfg == nil {
		return "", fmt.Errorf("reminder runner: no config")
	}
	return RunScheduledJob(ctx, r.cfg, reminderItemAsCronJob(it))
}

// reminderSessionPrefix namespaces a reminder transcript. Exported through the
// id projection below; see TestReminderItemAsCronJob for the collision argument.
const reminderSessionPrefix = "rt-"

// reminderItemAsCronJob projects a reminder/task onto the scheduler.Job shape
// the shared runner consumes. It is a pure function so the mapping is directly
// testable without building an agent.
func reminderItemAsCronJob(it *reminders.Item) *scheduler.Job {
	if it == nil {
		return nil
	}
	name := it.Title
	if name == "" {
		name = it.Body()
	}
	return &scheduler.Job{
		ID:   reminderSessionPrefix + it.ID,
		Name: name,
		Schedule: scheduler.Schedule{
			Kind: scheduler.KindAt,
			AtMs: it.DueAtMs,
		},
		Payload: scheduler.Payload{
			Message:  it.Body(),
			Notes:    it.Notes,
			Owner:    it.Owner,
			PermMode: it.PermMode,
		},
		State: scheduler.JobState{
			NextRunAtMs: it.DueAtMs,
		},
		CreatedAtMs: it.CreatedAtMs,
		Enabled:     true,
	}
}
