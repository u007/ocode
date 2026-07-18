// Package scheduler implements a persistent, disk-backed cron/scheduled-job
// engine for ocode. It is modeled on nanobot's CronService (a scheduler-only
// engine that fires an injected onJob callback) and on Claude Code's scheduled
// task dispatch semantics (low-priority fire between turns, jitter, local
// timezone, one-shot auto-delete, 7-day expiry for interval jobs).
//
// The engine is intentionally decoupled from the agent: it knows how to
// persist jobs, compute next-run times, and wake when a job is due, but the
// actual "run an agent turn" work is delegated to an injected OnJob callback
// supplied by the long-lived host (server/desktop). See dispatch.go for the
// headless agent runner that hosts wire in.
package scheduler

import "time"

// ScheduleKind enumerates how a job repeats.
type ScheduleKind string

const (
	// KindAt is a one-shot job that fires once at a specific time, then deletes
	// itself.
	KindAt ScheduleKind = "at"
	// KindEvery fires on a fixed interval (EveryMs).
	KindEvery ScheduleKind = "every"
	// KindCron fires on a standard 5-field cron expression (+ optional TZ).
	KindCron ScheduleKind = "cron"
)

// PermissionMode bounds how autonomously a job may act when it fires.
type PermissionMode string

const (
	// PermNormal is the safe default: destructive tools are denied/asked, like a
	// normal interactive session.
	PermNormal PermissionMode = "normal"
	// PermYOLO auto-allows everything. Explicit opt-in only — never the default.
	PermYOLO PermissionMode = "yolo"
	// PermLocked denies everything (read-only).
	PermLocked PermissionMode = "locked"
)

// Schedule describes when a job fires.
type Schedule struct {
	Kind    ScheduleKind `json:"kind"`
	AtMs    int64        `json:"at_ms,omitempty"`    // KindAt: epoch ms of the single fire
	EveryMs int64        `json:"every_ms,omitempty"` // KindEvery: interval in ms
	Expr    string       `json:"expr,omitempty"`     // KindCron: 5-field expression
	TZ      string       `json:"tz,omitempty"`       // KindCron: IANA tz; empty = host local
}

// Payload is what the job runs when it fires.
type Payload struct {
	Message   string         `json:"message"`              // the prompt to run
	Notes     string         `json:"notes,omitempty"`      // purpose / description
	Owner     string         `json:"owner,omitempty"`      // who scheduled it
	DeliverTo string         `json:"deliver_to,omitempty"` // reserved: where to send the result
	PermMode  PermissionMode `json:"perm_mode,omitempty"`  // default PermNormal
}

// JobState is runtime status (not authored; recomputed on load).
type JobState struct {
	NextRunAtMs int64  `json:"next_run_at_ms,omitempty"`
	LastRunAtMs int64  `json:"last_run_at_ms,omitempty"`
	LastStatus  string `json:"last_status,omitempty"` // "ok" | "error" | ""
	LastError   string `json:"last_error,omitempty"`
	Runs        int    `json:"runs,omitempty"`
}

// Job is a single scheduled task.
type Job struct {
	ID          string   `json:"id"`   // 8-char id
	Name        string   `json:"name"` // human label
	Schedule    Schedule `json:"schedule"`
	Payload     Payload  `json:"payload"`
	State       JobState `json:"state"`
	CreatedAtMs int64    `json:"created_at_ms"`
	Enabled     bool     `json:"enabled"`
}

// Store is the on-disk representation of all jobs for one project.
type Store struct {
	Version int   `json:"version"`
	Jobs    []Job `json:"jobs"`
}

// sevenDaysMs is the recurring-job expiry window (Claude Code uses 7 days for
// /loop-style interval tasks).
const sevenDaysMs = int64(7 * 24 * 60 * 60 * 1000)

// maxJitterMs is the random per-job stagger applied to recurring schedules so
// many jobs don't all hit the API on the same wall second.
const maxJitterMs = int64(30 * 1000)

// maxPollLeg is the longest the timer sleeps before re-checking the wall clock,
// so VM throttling / clock changes don't permanently drift the schedule.
const maxPollLeg = 60 * time.Second

// idLen is the length of generated job ids (Claude Code uses 8-char ids).
const idLen = 8
