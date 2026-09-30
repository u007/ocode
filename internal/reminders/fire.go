package reminders

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/u007/ocode/internal/scheduler"
)

// Notifier delivers a plain, agent-free notification. The desktop host
// implements it with the OS notification service; a browser-only host can
// leave it nil, since the delivery log still records the firing.
type Notifier interface {
	NotifyReminder(title, body string) error
}

// AgentRunner executes one agent turn for an item and returns the last
// assistant message. The host injects it; the reminders package never imports
// the agent, which is what keeps internal/reminders free of the tool/session
// import graph the cron scheduler went to some trouble to avoid.
type AgentRunner interface {
	RunReminder(ctx context.Context, item *Item) (string, error)
}

// fireOnce fires one snapshot, re-checking the item's live state first.
//
// The re-check is the whole point of this function. tick() takes a snapshot
// and then fires outside the lock, because an agent turn can run for minutes.
// During that window the user can cancel, complete, delete, or re-arm the item.
// So fireOnce re-reads the item under the mutex and abandons the firing if it
// is no longer armed — otherwise a cancel would be silently overwritten by the
// completion the in-flight turn writes on its way out, and the user would see
// an item they had just cancelled flip back to done.
func (s *Service) fireOnce(snap *Item) {
	s.mu.Lock()
	idx := s.indexLocked(snap.ID)
	if idx < 0 {
		// Deleted between the snapshot and now.
		s.mu.Unlock()
		return
	}
	live := s.items[idx]
	nowMs := s.now().UnixMilli()
	if !live.Due(nowMs) {
		s.mu.Unlock()
		return
	}
	// Mark the firing BEFORE releasing the lock. This is the claim: from here
	// on the item is no longer Due(), so a concurrent tick cannot double-fire
	// it. It is written back to disk on the way out by commitFire, and a
	// crash in between leaves the item re-armed rather than lost — a duplicate
	// notification is a better failure than a dropped one.
	live.FiredAtMs = nowMs
	live.Runs++
	s.items[idx] = live
	runner, notifier, outbox, runs := s.runner, s.notifier, s.outbox, s.runs
	s.mu.Unlock()

	s.fireWith(&live, runner, notifier, outbox, runs)
}

// fireWith performs the delivery for a claimed item and records the outcome.
// It is shared by fireOnce (the scheduled path) and FireNow (the manual
// "run now" button) so the two can never diverge in what they deliver.
//
// autoCompleteTarget reports the status this firing should settle on:
//   - a reminder always settles on completed — a one-shot reminder that rang is
//     done, and the record of that is the point of keeping it
//   - a task settles on completed only when AutoComplete is set AND the agent
//     turn returned without error. A successful turn is NOT proof the work was
//     finished, so the flag is opt-in per task.
//   - anything else stays where it is, so an overdue task remains visible and
//     the user decides.
func (s *Service) fireWith(item *Item, runner AgentRunner, notifier Notifier, outbox OutboxAppender, runs RunRecorder) {
	startedAt := time.Now().UTC()
	body := item.Body()
	result := body
	runErr := error(nil)

	if item.Action == ActionAgent {
		if runner == nil {
			runErr = fmt.Errorf("reminders: no agent runner attached (host did not wire AgentRunner)")
		} else {
			// A nil context is never passed to the agent: the runner builds a
			// fresh agent per firing and a cancelled parent must not abort a
			// reminder the user already asked for.
			out, err := runner.RunReminder(context.Background(), item)
			if err != nil {
				runErr = err
				// Keep the error text as the delivered body so a notify-only
				// consumer still learns the turn failed.
				result = ""
			} else {
				result = out
				if strings.TrimSpace(out) == "" {
					result = "Agent turn returned no output."
				}
			}
		}
	}

	finishedAt := time.Now().UTC()
	appendDelivery(outbox, item, result, errString(runErr), finishedAt)
	appendRunRecord(runs, item, result, errString(runErr), startedAt, finishedAt)

	if notifier != nil && runErr == nil {
		// A notification failure must not fail the firing: the delivery log
		// and run history already recorded it, and the item must still settle
		// into its post-fire status. Log with the item id so the cause is
		// traceable, then carry on.
		if nerr := notifier.NotifyReminder(item.Title, result); nerr != nil {
			log.Printf("reminders: notify item=%s kind=%s: %v", item.ID, item.Kind, nerr)
		}
	}

	settle := Status("")
	switch {
	case item.Kind == KindReminder:
		settle = StatusCompleted
	case item.Kind == KindTask && item.AutoComplete && runErr == nil:
		settle = StatusCompleted
	}
	s.commitFire(item, result, runErr, settle)
}

// commitFire records a firing's outcome on the live item.
//
// Two writes are compare-and-set against what the item looked like when the
// firing was CLAIMED, and both guards are load-bearing:
//
//   - FiredAtMs: written only when the live item still carries our claim. The
//     user can reset the item to pending while the turn is running, and
//     SetStatus clears FiredAtMs to re-arm it. Blindly restoring the claim here
//     would silently undo that reset and the item would never fire again.
//   - Status: written only when the live status still equals the status at
//     claim time. The user can also cancel, complete, or reset the item in that
//     window, and the turn's completion must not overwrite the decision.
//
// The run bookkeeping (Runs, LastStatus, LastError) is written either way: the
// agent really did run, and hiding that would be a lie in the other direction.
func (s *Service) commitFire(item *Item, result string, runErr error, settle Status) {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := s.indexLocked(item.ID)
	if idx < 0 {
		// Deleted mid-flight. Nothing to record against; the delivery log and
		// run history already carry the result.
		return
	}
	live := &s.items[idx]
	prev := *live
	if live.FiredAtMs == item.FiredAtMs {
		// Still carries our claim: nothing re-armed it mid-flight.
		live.FiredAtMs = item.FiredAtMs
		live.Runs = item.Runs
	} else {
		// Re-armed mid-flight. Keep the run count — the turn did happen — but
		// do not resurrect the claim, or the re-armed item would be treated as
		// already fired and would never fire.
		live.Runs = item.Runs
	}
	if runErr != nil {
		live.LastStatus = "error"
		live.LastError = runErr.Error()
	} else {
		live.LastStatus = "ok"
		live.LastError = ""
	}
	live.UpdatedAtMs = s.now().UnixMilli()
	if settle != "" && live.Status == item.Status && CanTransition(live.Status, settle) {
		live.Status = settle
	}
	if err := s.persistLocked(); err != nil {
		// The run happened; only the bookkeeping failed. Roll back so memory
		// and disk agree, and log it with the item id.
		s.items[idx] = prev
		log.Printf("reminders: persist after firing item=%s: %v", item.ID, err)
	}
}

// appendDelivery records the firing in the SHARED cron outbox. That single
// choice is what gives a reminder a Telegram push, an RC-bridge system
// message, and a row in the web Outbox panel without a line of new plumbing:
// the drainer already drains this file and fans every entry out.
func appendDelivery(outbox OutboxAppender, item *Item, result, errStr string, at time.Time) {
	if outbox == nil {
		return
	}
	d := scheduler.Delivery{
		// The prefixed id keeps a reminder from colliding with a cron job id
		// in the shared log, and tells the drainer which store to look in.
		JobID:   prefixID(item.Kind, item.ID),
		JobName: item.Title,
		Owner:   item.Owner,
		Result:  result,
		Error:   errStr,
		At:      at,
	}
	if err := outbox.Append(d); err != nil {
		// The firing already happened and the run history records it, so this
		// is a delivery problem, not a run problem. Log it with the item id
		// and move on rather than failing the item.
		log.Printf("reminders: append delivery item=%s: %v", item.ID, err)
	}
}

// appendRunRecord records the firing in the SHARED per-job run history so a
// reminder or task gets the same history panel a cron job does.
func appendRunRecord(runs RunRecorder, item *Item, result, errStr string, startedAt, finishedAt time.Time) {
	if runs == nil {
		return
	}
	rec := scheduler.RunRecord{
		ID:         newRunID(),
		JobID:      prefixID(item.Kind, item.ID),
		JobName:    item.Title,
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
		DurationMs: finishedAt.Sub(startedAt).Milliseconds(),
		Status:     runStatus(errStr),
		Input:      item.Body(),
		Output:     result,
		Error:      errStr,
		Logs: []scheduler.RunLogEntry{
			newRunLog("info", fmt.Sprintf("firing %s %s", item.Kind, item.ID), startedAt),
			newRunLog("info", "fired", finishedAt),
		},
	}
	if errStr != "" {
		rec.Logs = append(rec.Logs, newRunLog("error", errStr, finishedAt))
	}
	if err := runs.Append(rec); err != nil {
		log.Printf("reminders: append run record item=%s: %v", item.ID, err)
	}
}

// newRunID returns an 8-char hex run id, mirroring scheduler.genRunID (which
// is unexported there, so it cannot be reused directly).
func newRunID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%08x", time.Now().UnixNano()&0xFFFFFFFF)
	}
	return hex.EncodeToString(b[:])
}

func newRunLog(level, msg string, at time.Time) scheduler.RunLogEntry {
	return scheduler.RunLogEntry{At: at, Level: level, Message: msg}
}

func runStatus(errStr string) string {
	if errStr != "" {
		return "error"
	}
	return "ok"
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
