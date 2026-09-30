// Package reminders implements a persistent, disk-backed reminder and task
// engine for ocode.
//
// It is a deliberate SIBLING of internal/scheduler, not a `kind` field on
// scheduler.Job, because the lifecycles genuinely differ:
//
//   - A scheduler Job is an agent prompt that RECURS (at/every/cron) and whose
//     KindAt firing deletes the job. executeJob hardcodes exactly that.
//   - A Reminder is a one-shot nudge with no recurrence. It fires once and is
//     then done; it is never silently deleted from the list, because the user
//     needs to see that it rang (or didn't).
//   - A Task is a checklist item that may have NO due time at all, and whose
//     status the user drives by hand (pending / completed / cancelled, and
//     back to pending).
//
// The engine persists items, wakes when one is due, and hands each firing to
// an injected Notifier (a plain notification) and/or AgentRunner (a real agent
// turn). Delivery deliberately REUSES scheduler.Outbox and scheduler.RunHistory
// so a reminder or task result lands in the same delivery log and run history
// as a cron job — which means the existing Telegram/RC fan-out and the web
// Outbox panel pick them up for free.
//
// Why this is NOT internal/tool's todo store: that store is the agent's
// in-turn plan, one markdown file per SESSION at
// <project-root>/.ocode/todo/<session-id>.md, with a single-file lifetime and
// no HTTP surface. A reminder/task list is per PROJECT, outlives any session,
// and needs REST + a list endpoint. They are different lifetimes, so they are
// different stores; what they share (delivery log, run history, targets) is
// reused by import rather than duplicated.
package reminders

import (
	"fmt"
	"strings"
	"time"

	"github.com/u007/ocode/internal/scheduler"
)

// Kind distinguishes a reminder (time-driven nudge) from a task (checklist
// item). It is fixed at creation; converting one into the other is a delete
// plus an add, because the two have different required fields and different
// post-fire behaviour.
type Kind string

const (
	// KindReminder fires once at its due time and is then complete.
	KindReminder Kind = "reminder"
	// KindTask is a checklist item. It fires only if DueAtMs is set, and it
	// stays pending afterwards unless AutoComplete is set.
	KindTask Kind = "task"
)

// Status is the user-visible lifecycle state of an item.
type Status string

const (
	// StatusPending is the only state from which an item may fire.
	StatusPending Status = "pending"
	// StatusInProgress marks work that has been started but not finished. It
	// still fires if the item is due — a due in-progress task is exactly the
	// one you want nudged — and it can move on to completed or back to pending.
	StatusInProgress Status = "in_progress"
	// StatusCompleted means "done" — either marked by the user, or auto-set
	// when a fired agent turn succeeded on an AutoComplete task.
	StatusCompleted Status = "completed"
	// StatusCancelled means the user abandoned it. It is NOT deleted, so the
	// list keeps a record, and it can be restored to pending.
	StatusCancelled Status = "cancelled"
)

// allStatuses is the single ordered registry of accepted statuses. Every
// validation, error message, and test table iterates THIS list rather than
// restating the set, so adding a status cannot leave one of them behind.
var allStatuses = []Status{StatusPending, StatusInProgress, StatusCompleted, StatusCancelled}

// Statuses returns the accepted statuses, in lifecycle order.
func Statuses() []Status {
	out := make([]Status, len(allStatuses))
	copy(out, allStatuses)
	return out
}

// Action selects what a firing does.
type Action string

const (
	// ActionNotify delivers a plain notification (desktop notification plus
	// the shared delivery log). No LLM turn, no token cost. The default.
	ActionNotify Action = "notify"
	// ActionAgent runs a real agent turn with Message as the prompt, exactly
	// like a cron job, and delivers that turn's result.
	ActionAgent Action = "agent"
)

// Item is one reminder or task.
type Item struct {
	ID      string `json:"id"`
	Kind    Kind   `json:"kind"`
	Title   string `json:"title"`
	Message string `json:"message,omitempty"`
	Notes   string `json:"notes,omitempty"`
	Owner   string `json:"owner,omitempty"`

	// Status starts at StatusPending and is only ever changed through
	// Service.SetStatus, which validates the transition.
	Status Status `json:"status"`
	// Action is what a firing does. Defaults to ActionNotify.
	Action Action `json:"action"`
	// AutoComplete applies to a KindTask with ActionAgent only: when the agent
	// turn returns without error, the task moves to StatusCompleted. A
	// successful turn is NOT proof the work was done, so this is opt-in.
	AutoComplete bool `json:"auto_complete"`
	// DueAtMs is 0 for a task that has no due date (a pure checklist item).
	// A KindReminder always requires one.
	DueAtMs int64 `json:"due_at_ms,omitempty"`

	// PermMode bounds the agent turn for ActionAgent. Blank resolves to
	// normal, matching resolveCronPermissionMode in internal/server.
	PermMode scheduler.PermissionMode `json:"perm_mode,omitempty"`

	CreatedAtMs int64 `json:"created_at_ms"`
	UpdatedAtMs int64 `json:"updated_at_ms"`
	// FiredAtMs is the one-shot CLAIM, and it gates re-firing. It is written
	// under the lock at the START of a firing, not at the end, so a concurrent
	// tick cannot double-fire the item — which means a reader can legitimately
	// observe fired_at_ms set while the status has not settled yet. It is "this
	// item has been claimed", not "this item has finished".
	//
	// SetStatus clears it when moving back to pending, which re-arms a one-shot
	// item: unmarking a reminder that already rang makes it ring again as soon
	// as the loop next ticks.
	FiredAtMs int64 `json:"fired_at_ms,omitempty"`
	Runs      int   `json:"runs"`
	// LastStatus is "ok" | "error" | "" — mirrors scheduler.JobState so the
	// same table styling works in the web UI.
	LastStatus string `json:"last_status,omitempty"`
	LastError  string `json:"last_error,omitempty"`
}

// Store is the on-disk representation of every item for one project.
type Store struct {
	Version int    `json:"version"`
	Items   []Item `json:"items"`
}

const (
	// maxItems caps a project's list. A cron session is capped at 50, but a
	// checklist is expected to be longer than a schedule; 500 is generous
	// while still bounding the store and the per-tick scan.
	maxItems = 500
	// maxTitleLen bounds the one-line label. The message is unbounded.
	maxTitleLen = 120
	// maxPollLeg is the longest the timer sleeps before re-checking the wall
	// clock, so a throttled VM or a clock change cannot permanently drift the
	// due times. Matches scheduler.maxPollLeg.
	maxPollLeg = 60 * time.Second
	// idleDelay is how long the loop sleeps when nothing is pending, so a
	// mis-set future due time cannot busy-spin. AddItem/SetStatus/RemoveItem
	// all call wake(), so a newly-armed item does not wait this out.
	idleDelay = 24 * time.Hour
	// idLen is the length of generated item ids (matches scheduler).
	idLen = 8
)

// DefaultStorePath returns the per-project on-disk path for the reminders
// store. It sits NEXT TO the scheduler's jobs.json (same slug, same directory)
// so both engines share one delivery log and one targets registry:
//
//	<GlobalDataDir>/scheduler/<project-slug>/reminders.json
func DefaultStorePath(workDir string) (string, error) {
	jobsPath, err := scheduler.DefaultStorePath(workDir)
	if err != nil {
		return "", err
	}
	return siblingPath(jobsPath, "reminders.json"), nil
}

// ValidKind reports whether k is a kind this package accepts.
func ValidKind(k Kind) bool { return k == KindReminder || k == KindTask }

// ValidStatus reports whether s is a status this package accepts. It is the
// ONLY definition of the set: an unrecognised value is rejected, never coerced.
func ValidStatus(s Status) bool {
	for _, known := range allStatuses {
		if s == known {
			return true
		}
	}
	return false
}

// ValidAction reports whether a is an action this package accepts.
func ValidAction(a Action) bool { return a == ActionNotify || a == ActionAgent }

// Body returns the text a notification should carry: the message when one is
// set, otherwise the title. It never returns "" for an item that passed
// validate, so a notification is never blank.
func (i *Item) Body() string {
	if strings.TrimSpace(i.Message) != "" {
		return i.Message
	}
	return i.Title
}

// Active reports whether the item is in a state from which it may still fire —
// that is, not completed and not cancelled.
//
// This is the SHARED predicate behind both halves of the engine: Due uses it to
// decide whether to fire, and Service.nextDelayLocked uses it to decide what to
// wait for. The two MUST agree: when they did not — the loop computed a zero
// delay from an item that Due() then refused to fire — the item was never
// claimed, the delay stayed zero, and run() spun at full CPU forever.
// TestNextDelayAgreesWithDue pins that agreement.
func (i *Item) Active() bool {
	return i.Status == StatusPending || i.Status == StatusInProgress
}

// Due reports whether the item is armed to fire at or before nowMs. The three
// conditions are the whole firing gate and are checked in ONE place so the run
// loop, the auto-complete compare-and-set, and the tests cannot disagree:
//
//	active   — pending or in_progress; a completed or cancelled item never
//	           fires, whatever its clock says
//	due      — DueAtMs > 0 and <= now; 0 means "no due date", never "due now"
//	unfired  — FiredAtMs == 0, so a one-shot item rings exactly once
//
// in_progress is deliberately still active: a task that is due AND started is
// precisely the one worth nudging.
func (i *Item) Due(nowMs int64) bool {
	return i.Active() && i.DueAtMs > 0 && i.DueAtMs <= nowMs && i.FiredAtMs == 0
}

// statusList renders the accepted statuses for an error message. Derived
// from allStatuses so the message can never drift from the validator.
func statusList() string {
	parts := make([]string, 0, len(allStatuses))
	for _, s := range allStatuses {
		parts = append(parts, string(s))
	}
	return strings.Join(parts, "/")
}

// validate normalises defaults IN PLACE and rejects an unusable item.
//
// The pointer receiver is load-bearing: this function's whole job includes
// rewriting blank Status/Action to their defaults and clearing a meaningless
// AutoComplete, and a value receiver would silently discard every one of those
// edits. Callers must pass the exact copy they are about to store.
func (i *Item) validate() error {
	i.Title = strings.TrimSpace(i.Title)
	i.Message = strings.TrimSpace(i.Message)
	i.Notes = strings.TrimSpace(i.Notes)
	i.Owner = strings.TrimSpace(i.Owner)

	if !ValidKind(i.Kind) {
		return fmt.Errorf("kind must be %q or %q, got %q", KindReminder, KindTask, i.Kind)
	}
	if i.Status == "" {
		i.Status = StatusPending
	}
	if !ValidStatus(i.Status) {
		return fmt.Errorf("status must be one of %s, got %q", statusList(), i.Status)
	}
	if i.Action == "" {
		i.Action = ActionNotify
	}
	if !ValidAction(i.Action) {
		return fmt.Errorf("action must be %q or %q, got %q", ActionNotify, ActionAgent, i.Action)
	}
	if i.Title == "" {
		return fmt.Errorf("title is required")
	}
	if len([]rune(i.Title)) > maxTitleLen {
		return fmt.Errorf("title must be at most %d characters", maxTitleLen)
	}
	if i.Kind == KindReminder && i.DueAtMs <= 0 {
		return fmt.Errorf("a reminder needs a due time; use a task for a checklist item with no due date")
	}
	// AutoComplete only means something for a task whose firing runs an agent.
	// Normalising it to false elsewhere keeps the stored item honest instead of
	// leaving a flag that quietly does nothing.
	if i.Kind != KindTask || i.Action != ActionAgent {
		i.AutoComplete = false
	}
	return nil
}
