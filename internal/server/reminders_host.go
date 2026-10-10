package server

import (
	"encoding/json"
	"fmt"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/reminders"
	"github.com/u007/ocode/internal/scheduler"
)

// ReminderFiredEvent is the SSE event type published when a reminder or task
// fires. It is a PROJECT-level event (no session id), because a reminder
// belongs to the project, not to a conversation — there may be no chat open at
// all when it rings.
//
// It is deliberately not in sessionScopedEvents: a fired reminder has no
// session to scope to, and the bus rejects a session event published without
// one.
const ReminderFiredEvent = "reminder_fired"

// busNotifier satisfies reminders.Notifier by publishing ReminderFiredEvent.
// The web app turns that into a toast, which is the one channel that behaves
// identically in the desktop WKWebView and in a browser, needs no OS
// permission prompt, and is testable.
//
// The fired item is NOT re-read from the service here: NotifyReminder is
// called from the firing goroutine, and a second lookup would race the
// post-fire status write that commitFire performs immediately afterwards. The
// engine already resolved the item and passes the settled title and body.
type busNotifier struct {
	bus     *EventBus
	project string
}

func (n *busNotifier) NotifyReminder(title, body string) error {
	if n == nil || n.bus == nil {
		return fmt.Errorf("reminders: no event bus attached")
	}
	payload := map[string]any{
		"title": title,
		"body":  body,
	}
	// json.RawMessage so the envelope carries a structured object rather than
	// a Go map that would serialise per subscriber.
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("reminders: marshal fired payload: %w", err)
	}
	n.bus.Publish(ReminderFiredEvent, n.project, "", json.RawMessage(data))
	return nil
}

// AttachReminders starts the reminders engine for workDir and attaches it to
// the server, wiring:
//
//   - the shared cron Outbox and RunHistory, so a reminder result shows in the
//     same web Outbox panel, the same runs.jsonl, and the same Telegram/RC
//     fan-out as a cron job — one delivery log, one drainer;
//   - the project's workdir as the notifier's event-bus scope;
//   - the REMINDER'S OWN AgentRunner, so a reminder that runs an agent reuses
//     the shared cron runner (and its per-firing LSP/agent cleanup) instead of
//     growing a second one.
//
// It is order-independent with respect to SetScheduler: when the cron outbox
// is not attached yet, this builds its own instances over the SAME store
// directory, so both engines still write one file.
//
// Errors are returned for the host to log; a broken reminder list must never
// stop the server from serving.
//
// This function deliberately does NOT log on success. The caller owns that
// line, because the prefix is host-specific ("serve:" vs "ocode-desktop:") and
// logging here produced the same message twice with the wrong prefix on one
// host.
func (s *Server) AttachReminders(workDir string, cfg *config.Config, notifier reminders.Notifier) error {
	if s == nil {
		return fmt.Errorf("reminders: nil server")
	}
	storePath, err := reminders.DefaultStorePath(workDir)
	if err != nil {
		return err
	}
	runner := reminderAgentRunner(cfg)
	svc, err := reminders.StartForHost(workDir, runner, notifier)
	if err != nil {
		return err
	}
	outbox, runs := s.schedulerOutbox, s.schedulerRuns
	if outbox == nil {
		// Same directory as the cron store, so NewOutbox/NewRunHistory resolve
		// to the same deliveries.jsonl and runs.jsonl.
		outbox = scheduler.NewOutbox(storePath)
		s.schedulerOutbox = outbox
	}
	if runs == nil {
		runs = scheduler.NewRunHistory(storePath)
		s.schedulerRuns = runs
	}
	svc.SetOutbox(outbox)
	svc.SetRunHistory(runs)
	// Seed the default project's per-project entry with THIS engine, and record
	// the config + notifier so other projects can be started on demand. Seeding
	// (rather than letting the registry start a twin) preserves the wiring the
	// host already did — the event-bus notifier below, and the cron drainer.
	s.markRemindersAttached()
	// s.scheduler is typed `any` so the server does not force a scheduler
	// dependency on hosts that never attach one; take the concrete service
	// through the existing typed accessor.
	s.setCronScopeConfig(cfg, notifier, s.Scheduler(), svc, workDir, outbox, runs, s.schedulerTargets)

	if notifier == nil && s.handler != nil && s.handler.bus != nil {
		svc.SetNotifier(&busNotifier{bus: s.handler.bus, project: workDir})
	}
	s.attachReminders(svc)
	return nil
}
