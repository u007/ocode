package scheduler

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// AgentRunner executes one agent turn for a scheduled job and returns the last
// assistant message content. The host (server/desktop) supplies a concrete
// implementation; the scheduler package stays decoupled from
// internal/agent/tool/session to avoid the import cycle that would otherwise
// be created through internal/tool (which now imports internal/scheduler for
// the cron tool).
type AgentRunner interface {
	RunScheduledJob(ctx context.Context, job *Job) (string, error)
}

// Dispatcher is the host-supplied OnJobFunc for the scheduler. It builds the
// context prefix and delegates the actual agent turn to a host-injected
// AgentRunner. The runner is responsible for:
//   - building an agent for the job (NewAgent with cloned cfg if needed),
//   - loading or seeding the per-job session (cron:<id>),
//   - binding the job's perm_mode via ag.Permissions().SetMode,
//   - calling ag.Step and returning the last assistant content,
//   - persisting the transcript with a retention cap,
//   - delivering the result if a sink is configured.
type Dispatcher struct {
	Runner AgentRunner
	// Outbox is an optional result sink that records every successful (or
	// failed) run to a per-project JSONL file. External integrations
	// (Telegram bot, RC client, web poll endpoint) read from this outbox
	// to push the result to the user. See deliver.go.
	Outbox *Outbox
	// Deliver is an additional, optional in-process callback invoked after a
	// successful run with the job and the result string. The runner's
	// returned string is passed; Outbox + Deliver can coexist.
	Deliver func(job *Job, result string)
}

// OnJob satisfies scheduler.OnJobFunc: run the job via the runner and report
// any error so the scheduler can record it on the job's state.
func (d *Dispatcher) OnJob(ctx context.Context, job *Job) error {
	if d == nil || d.Runner == nil {
		return fmt.Errorf("scheduler: no agent runner attached (host did not wire Dispatcher.Runner)")
	}
	result, err := d.Runner.RunScheduledJob(ctx, job)
	// Always record to the outbox (when configured) — both successes and
	// failures. The outbox is the durable receipt the user/agent can
	// consult to know whether the job ran.
	if d.Outbox != nil {
		rec := Delivery{
			JobID:       job.ID,
			JobName:     job.Name,
			Owner:       job.Payload.Owner,
			DeliveredTo: job.Payload.DeliverTo,
			Result:      result,
		}
		if err != nil {
			rec.Error = err.Error()
			rec.Result = ""
		}
		if aerr := d.Outbox.Append(rec); aerr != nil {
			// Don't fail the job because the outbox couldn't be written;
			// the work already happened. Log via the return path so the
			// scheduler records the outbox failure on the job state.
			err = fmt.Errorf("run ok but outbox write failed: %w (run err: %v)", aerr, err)
		}
	}
	if err != nil {
		return err
	}
	if d.Deliver != nil && result != "" {
		d.Deliver(job, result)
	}
	return nil
}

// ContextPrefix builds the "[Scheduled job context]" block used to seed a
// scheduled turn. Exposed so host implementations of AgentRunner can reuse
// the exact format the scheduler package defines.
func ContextPrefix(job *Job) string {
	var b strings.Builder
	b.WriteString("[Scheduled job context]\n")
	fmt.Fprintf(&b, "Job: %s\n", job.Name)
	if job.Payload.Notes != "" {
		fmt.Fprintf(&b, "Purpose: %s\n", job.Payload.Notes)
	}
	if job.Payload.Owner != "" {
		fmt.Fprintf(&b, "Scheduled by: %s\n", job.Payload.Owner)
	}
	if job.CreatedAtMs > 0 {
		created := time.UnixMilli(job.CreatedAtMs).UTC()
		fmt.Fprintf(&b, "Created: %s\n", created.Format("2006-01-02 15:04 UTC"))
	}
	fmt.Fprintf(&b, "Schedule: %s\n", formatSchedule(job.Schedule))
	b.WriteString("---\n")
	return b.String()
}

// formatSchedule produces a human-readable schedule summary.
func formatSchedule(sc Schedule) string {
	switch sc.Kind {
	case KindAt:
		return "at " + time.UnixMilli(sc.AtMs).Format(time.RFC1123)
	case KindEvery:
		return fmt.Sprintf("every %s", time.Duration(sc.EveryMs)*time.Millisecond)
	case KindCron:
		if sc.TZ != "" {
			return sc.Expr + " (tz " + sc.TZ + ")"
		}
		return sc.Expr
	}
	return string(sc.Kind)
}
