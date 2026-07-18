package tool

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/u007/ocode/internal/scheduler"
)

// newCronToolFromService resolves an `any` (passed through InitBuiltinTools to
// avoid a tool ↔ scheduler import cycle) into a *CronTool wired to the
// scheduler service. Returns nil if the value isn't a *scheduler.Service.
func newCronToolFromService(svc any) *CronTool {
	// Fast path: direct *scheduler.Service.
	if s, ok := svc.(*scheduler.Service); ok {
		return &CronTool{Service: s}
	}
	// Indirect path: a thin wrapper exposing Svc() — for callers that don't
	// want to take a hard dependency on the concrete type.
	if w, ok := svc.(cronServiceWrapper); ok {
		if s := w.CronService(); s != nil {
			return &CronTool{Service: s}
		}
	}
	// Reflection fallback: accept any value whose pointer-element type is
	// *scheduler.Service. Used by tests and the dispatch helper.
	v := reflect.ValueOf(svc)
	if v.Kind() == reflect.Ptr && v.Type().Elem() == reflect.TypeOf((*scheduler.Service)(nil)) {
		return &CronTool{Service: v.Interface().(*scheduler.Service)}
	}
	return nil
}

// cronServiceWrapper is an optional interface that hosts can implement to
// hand a *scheduler.Service to InitBuiltinTools without naming the concrete
// type. main.go uses it to keep the wiring decoupled.
type cronServiceWrapper interface {
	CronService() *scheduler.Service
}

// CronTool lets the LLM manage scheduled jobs. It mirrors Claude Code's
// CronCreate / CronList / CronDelete surface (add / list / remove / describe
// actions, 8-char ids) and operates on the project's scheduler.Service
// attached via SetService.
//
// Safety: registration defaults the tool to "allow" in NewPermissionManager
// (see internal/agent/permissions.go). The dispatcher that fires the job runs
// the per-job permission mode (default: normal) — so even when this tool is
// always-allowed, the agent turn that actually executes the prompt still goes
// through the permission layer.
type CronTool struct {
	Service *scheduler.Service
}

func (t *CronTool) Name() string { return "cron" }
func (t *CronTool) Description() string {
	return "Manage scheduled jobs that run a prompt on a clock (at / every / cron). Actions: add, list, remove, describe."
}
func (t *CronTool) Parallel() bool { return false }

func (t *CronTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "cron",
		"description": "Manage scheduled jobs. Create a job (action=add) to run a prompt at a future time, repeatedly, or on a cron schedule. Use action=list to enumerate jobs, action=describe to inspect one, action=remove to cancel. The job's permission mode (perm_mode) bounds what the fired agent turn is allowed to do; the safe default is 'normal' (asks/denies destructive tools).",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"add", "list", "remove", "describe"},
					"description": "What to do: add a new job, list existing ones, remove by id, or describe one in full.",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "(add) Optional human-readable label. Defaults to a truncated version of the message.",
				},
				"message": map[string]interface{}{
					"type":        "string",
					"description": "(add) The prompt that will be run as an agent turn when the job fires.",
				},
				"notes": map[string]interface{}{
					"type":        "string",
					"description": "(add) Optional purpose / description shown in the job context.",
				},
				"owner": map[string]interface{}{
					"type":        "string",
					"description": "(add) Who scheduled it (recorded in the job context).",
				},
				"deliver_to": map[string]interface{}{
					"type":        "string",
					"description": "(add) Reserved: where to deliver the fired-job result.",
				},
				"perm_mode": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"normal", "yolo", "locked"},
					"description": "(add) Permission mode the fired agent turn will use. Default 'normal' is safe (asks/denies). 'yolo' auto-allows everything (explicit opt-in only).",
				},
				"schedule": map[string]interface{}{
					"type":        "object",
					"description": "(add) When to fire.",
					"properties": map[string]interface{}{
						"kind": map[string]interface{}{
							"type":        "string",
							"enum":        []string{"at", "every", "cron"},
							"description": "Schedule kind.",
						},
						"at_ms": map[string]interface{}{
							"type":        "integer",
							"description": "(at) Epoch milliseconds of the single fire.",
						},
						"every_ms": map[string]interface{}{
							"type":        "integer",
							"description": "(every) Interval in milliseconds.",
						},
						"expr": map[string]interface{}{
							"type":        "string",
							"description": "(cron) 5-field cron expression (minute hour dom month dow). E.g. '0 9 * * 1-5' for weekdays 9am.",
						},
						"tz": map[string]interface{}{
							"type":        "string",
							"description": "(cron) IANA timezone (e.g. 'America/Los_Angeles'). Empty = host local time.",
						},
					},
					"required": []string{"kind"},
				},
				"id": map[string]interface{}{
					"type":        "string",
					"description": "(remove, describe) The 8-char job id returned by add/list.",
				},
			},
			"required": []string{"action"},
		},
	}
}

func (t *CronTool) Execute(args json.RawMessage) (string, error) {
	if t == nil || t.Service == nil {
		return "", fmt.Errorf("cron: scheduler not attached (only available when running in serve/web/desktop hosts)")
	}
	var p struct {
		Action    string                   `json:"action"`
		Name      string                   `json:"name,omitempty"`
		Message   string                   `json:"message,omitempty"`
		Notes     string                   `json:"notes,omitempty"`
		Owner     string                   `json:"owner,omitempty"`
		DeliverTo string                   `json:"deliver_to,omitempty"`
		PermMode  scheduler.PermissionMode `json:"perm_mode,omitempty"`
		Schedule  struct {
			Kind    scheduler.ScheduleKind `json:"kind"`
			AtMs    int64                  `json:"at_ms,omitempty"`
			EveryMs int64                  `json:"every_ms,omitempty"`
			Expr    string                 `json:"expr,omitempty"`
			TZ      string                 `json:"tz,omitempty"`
		} `json:"schedule"`
		ID string `json:"id,omitempty"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("cron: invalid params: %w", err)
	}

	switch strings.ToLower(p.Action) {
	case "list":
		jobs := t.Service.ListJobs()
		out, _ := json.MarshalIndent(map[string]any{
			"count": len(jobs),
			"jobs":  jobs,
		}, "", "  ")
		return string(out), nil

	case "add":
		if p.Message == "" {
			return "", fmt.Errorf("cron: 'message' is required for add")
		}
		job := scheduler.Job{
			Name: p.Name,
			Schedule: scheduler.Schedule{
				Kind:    p.Schedule.Kind,
				AtMs:    p.Schedule.AtMs,
				EveryMs: p.Schedule.EveryMs,
				Expr:    p.Schedule.Expr,
				TZ:      p.Schedule.TZ,
			},
			Payload: scheduler.Payload{
				Message:   p.Message,
				Notes:     p.Notes,
				Owner:     p.Owner,
				DeliverTo: p.DeliverTo,
				PermMode:  p.PermMode,
			},
		}
		id, err := t.Service.AddJob(job)
		if err != nil {
			return "", fmt.Errorf("cron: add: %w", err)
		}
		return fmt.Sprintf("scheduled job %s (name=%q)", id, job.Name), nil

	case "remove":
		if p.ID == "" {
			return "", fmt.Errorf("cron: 'id' is required for remove")
		}
		if err := t.Service.RemoveJob(p.ID); err != nil {
			return "", fmt.Errorf("cron: remove: %w", err)
		}
		return fmt.Sprintf("removed job %s", p.ID), nil

	case "describe":
		if p.ID == "" {
			return "", fmt.Errorf("cron: 'id' is required for describe")
		}
		j := t.Service.GetJob(p.ID)
		if j == nil {
			return "", fmt.Errorf("cron: job %s not found", p.ID)
		}
		out, _ := json.MarshalIndent(j, "", "  ")
		return string(out), nil

	default:
		return "", fmt.Errorf("cron: unknown action %q (want add|list|remove|describe)", p.Action)
	}
}
