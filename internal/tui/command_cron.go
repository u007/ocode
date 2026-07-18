package tui

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/scheduler"
)

// runCronCmd implements /cron <list|remove|describe|add ...>.
//
// The TUI itself doesn't host the scheduler (only the long-lived serve/web/
// desktop host does), but it can still author/manage jobs by writing
// directly to the shared on-disk store. The host picks up the change on its
// next ≤60s timer leg via mtime-based external reload. This matches the
// design in docs/scheduled-jobs.md.
func runCronCmd(m *model, args []string) tea.Cmd {
	if len(args) == 0 {
		m.messages = append(m.messages, message{
			role: roleAssistant,
			text: cronUsage(),
		})
		m.rerenderTranscriptAndMaybeScroll()
		return nil
	}
	storePath, err := scheduler.DefaultStorePath(m.workDir)
	if err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("cron: %v", err)})
		m.rerenderTranscriptAndMaybeScroll()
		return nil
	}
	svc := scheduler.NewService(storePath)
	// /cron is a status/authoring command — no need to Start() the engine.
	// Touching the store directly works because the host reloads on mtime.
	_ = svc

	sub := strings.ToLower(args[0])
	switch sub {
	case "list":
		jobs := svc.ListJobs()
		if len(jobs) == 0 {
			m.messages = append(m.messages, message{role: roleAssistant, text: "No scheduled jobs."})
		} else {
			var b strings.Builder
			fmt.Fprintf(&b, "%d scheduled job(s):\n", len(jobs))
			for _, j := range jobs {
				fmt.Fprintf(&b, "  %s — %s  %s\n", j.ID, j.Name, cronDescribe(&j))
			}
			m.messages = append(m.messages, message{role: roleAssistant, text: b.String()})
		}

	case "remove":
		if len(args) < 2 {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /cron remove <id>"})
			break
		}
		if err := svc.RemoveJob(args[1]); err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("cron: %v", err)})
		} else {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("removed job %s", args[1])})
		}

	case "describe":
		if len(args) < 2 {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /cron describe <id>"})
			break
		}
		j := svc.GetJob(args[1])
		if j == nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("cron: job %s not found", args[1])})
			break
		}
		data, _ := json.MarshalIndent(j, "", "  ")
		m.messages = append(m.messages, message{role: roleAssistant, text: string(data)})

	case "add":
		// Two forms:
		//   /cron add <every_ms> <message...>
		//   /cron add at <iso_or_unix_ms> <message...>
		//   /cron add cron "<expr>" [tz <iana>] <message...>
		id, err := cronAddFromArgs(svc, args[1:])
		if err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: "cron: " + err.Error()})
		} else {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("scheduled job %s", id)})
		}

	default:
		m.messages = append(m.messages, message{role: roleAssistant, text: cronUsage()})
	}
	m.rerenderTranscriptAndMaybeScroll()
	return nil
}

func cronAddFromArgs(svc *scheduler.Service, args []string) (string, error) {
	if len(args) < 2 {
		return "", fmt.Errorf("usage: /cron add <every_ms|at <ms>|cron <expr> [tz <iana>]> <message...>")
	}
	job := scheduler.Job{Name: "tui-job", Payload: scheduler.Payload{Owner: "tui"}}
	switch strings.ToLower(args[0]) {
	case "at":
		ms, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			return "", fmt.Errorf("invalid at_ms %q: %w", args[1], err)
		}
		job.Schedule = scheduler.Schedule{Kind: scheduler.KindAt, AtMs: ms}
		args = args[2:]
	case "cron":
		expr := args[1]
		args = args[2:]
		job.Schedule = scheduler.Schedule{Kind: scheduler.KindCron, Expr: expr}
		if len(args) >= 2 && strings.ToLower(args[0]) == "tz" {
			job.Schedule.TZ = args[1]
			args = args[2:]
		}
	case "every":
		ms, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			return "", fmt.Errorf("invalid every_ms %q: %w", args[1], err)
		}
		job.Schedule = scheduler.Schedule{Kind: scheduler.KindEvery, EveryMs: ms}
		args = args[2:]
	default:
		// Implicit: leading token is every_ms.
		ms, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return "", fmt.Errorf("expected every_ms or schedule kind, got %q", args[0])
		}
		job.Schedule = scheduler.Schedule{Kind: scheduler.KindEvery, EveryMs: ms}
		args = args[1:]
	}
	job.Payload.Message = strings.TrimSpace(strings.Join(args, " "))
	if job.Payload.Message == "" {
		return "", fmt.Errorf("message is required")
	}
	return svc.AddJob(job)
}

func cronDescribe(j *scheduler.Job) string {
	switch j.Schedule.Kind {
	case scheduler.KindAt:
		return fmt.Sprintf("at %s", time.UnixMilli(j.Schedule.AtMs).Format(time.RFC1123))
	case scheduler.KindEvery:
		return fmt.Sprintf("every %s", time.Duration(j.Schedule.EveryMs)*time.Millisecond)
	case scheduler.KindCron:
		if j.Schedule.TZ != "" {
			return j.Schedule.Expr + " (tz " + j.Schedule.TZ + ")"
		}
		return j.Schedule.Expr
	}
	return string(j.Schedule.Kind)
}

func cronUsage() string {
	return `/cron — manage scheduled jobs (see docs/scheduled-jobs.md)

  /cron list                              List all jobs
  /cron describe <id>                     Show one job in full
  /cron remove <id>                       Cancel a job
  /cron add every <ms> <message...>       Run every <ms> milliseconds
  /cron add at <ms> <message...>          Run once at epoch ms <ms>
  /cron add cron "<expr>" [tz <iana>] <message...>
                                          Run on a 5-field cron expression
                                          (minute hour dom month dow)

Note: jobs fire in the long-lived serve/web/desktop host, not the TUI.
The TUI authors/manages jobs via the shared on-disk store.`
}
