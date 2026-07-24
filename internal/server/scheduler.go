package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/u007/ocode/internal/scheduler"
)

// cronHandler serves the REST surface for listing/adding/updating/removing
// scheduled jobs. It is attached to the Server only when a *scheduler.Service
// is set via attachScheduler below.
type cronHandler struct {
	svc *scheduler.Service
}

// cronAddRequest is the JSON body for POST /api/cron. Exactly one of AtMs /
// EveryMs / Expr must be set; the others are derived from the schedule kind.
type cronAddRequest struct {
	Name      string                   `json:"name"`
	Message   string                   `json:"message"`
	Notes     string                   `json:"notes,omitempty"`
	Owner     string                   `json:"owner,omitempty"`
	DeliverTo string                   `json:"deliver_to,omitempty"`
	PermMode  scheduler.PermissionMode `json:"perm_mode,omitempty"`
	Schedule  cronScheduleReq          `json:"schedule"`
}

type cronUpdateRequest struct {
	Enabled   *bool                     `json:"enabled,omitempty"`
	Name      *string                   `json:"name,omitempty"`
	Message   *string                   `json:"message,omitempty"`
	Notes     *string                   `json:"notes,omitempty"`
	Owner     *string                   `json:"owner,omitempty"`
	DeliverTo *string                   `json:"deliver_to,omitempty"`
	PermMode  *scheduler.PermissionMode `json:"perm_mode,omitempty"`
	Schedule  *cronScheduleReq          `json:"schedule,omitempty"`
}

type cronScheduleReq struct {
	Kind    scheduler.ScheduleKind `json:"kind"`
	AtMs    int64                  `json:"at_ms,omitempty"`
	EveryMs int64                  `json:"every_ms,omitempty"`
	Expr    string                 `json:"expr,omitempty"`
	TZ      string                 `json:"tz,omitempty"`
}

func (req cronScheduleReq) toSchedule() scheduler.Schedule {
	return scheduler.Schedule{
		Kind:    req.Kind,
		AtMs:    req.AtMs,
		EveryMs: req.EveryMs,
		Expr:    req.Expr,
		TZ:      req.TZ,
	}
}

func (h *cronHandler) list(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"jobs": h.svc.ListJobs()})
}

func (h *cronHandler) add(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req cronAddRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.Message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}
	job := scheduler.Job{
		Name: req.Name,
		Schedule: scheduler.Schedule{
			Kind:    req.Schedule.Kind,
			AtMs:    req.Schedule.AtMs,
			EveryMs: req.Schedule.EveryMs,
			Expr:    req.Schedule.Expr,
			TZ:      req.Schedule.TZ,
		},
		Payload: scheduler.Payload{
			Message:   req.Message,
			Notes:     req.Notes,
			Owner:     req.Owner,
			DeliverTo: req.DeliverTo,
			PermMode:  req.PermMode,
		},
	}
	id, err := h.svc.AddJob(job)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (h *cronHandler) remove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	if err := h.svc.RemoveJob(id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *cronHandler) update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req cronUpdateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	cur := h.svc.GetJob(id)
	if cur == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("job %s not found", id))
		return
	}
	patch := scheduler.JobPatch{}
	if req.Enabled != nil {
		patch.Enabled = req.Enabled
	}
	if req.Name != nil {
		patch.Name = req.Name
	}
	if req.Schedule != nil {
		sched := req.Schedule.toSchedule()
		patch.Schedule = &sched
	}
	if req.Message != nil || req.Notes != nil || req.Owner != nil || req.DeliverTo != nil || req.PermMode != nil {
		payload := cur.Payload
		if req.Message != nil {
			payload.Message = *req.Message
		}
		if req.Notes != nil {
			payload.Notes = *req.Notes
		}
		if req.Owner != nil {
			payload.Owner = *req.Owner
		}
		if req.DeliverTo != nil {
			payload.DeliverTo = *req.DeliverTo
		}
		if req.PermMode != nil {
			payload.PermMode = *req.PermMode
		}
		patch.Payload = &payload
	}
	updated, err := h.svc.UpdateJob(id, patch)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// handleCronTargetsList returns the current (workdir → chatID) mapping.
func (s *Server) handleCronTargetsList(w http.ResponseWriter, r *http.Request) {
	if s.schedulerTargets == nil {
		writeJSON(w, http.StatusOK, map[string]any{"targets": map[string]int64{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"targets": s.schedulerTargets.All()})
}

// handleCronTargetsSet writes/clears a single (workdir → chatID) mapping.
// Body: {"workdir": "/abs/path", "chat_id": 12345}; chat_id=0 removes.
func (s *Server) handleCronTargetsSet(w http.ResponseWriter, r *http.Request) {
	if s.schedulerTargets == nil {
		writeError(w, http.StatusServiceUnavailable, "no scheduler attached")
		return
	}
	var body struct {
		Workdir string `json:"workdir"`
		ChatID  int64  `json:"chat_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if body.Workdir == "" {
		writeError(w, http.StatusBadRequest, "workdir is required")
		return
	}
	if err := s.schedulerTargets.Set(body.Workdir, body.ChatID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleCronOutbox returns the contents of the scheduler's outbox JSONL file.
// Query params:
//
//	drain=true  — drain (truncate) the file after returning
//	limit=N     — return only the most recent N entries
//
// The response is {"entries": [...]} where each entry is a scheduler.Delivery.
func (s *Server) handleCronOutbox(w http.ResponseWriter, r *http.Request) {
	if s.schedulerOutbox == nil {
		writeJSON(w, http.StatusOK, map[string]any{"entries": []any{}})
		return
	}
	entries, err := s.schedulerOutbox.Peek()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		// Parse and trim (most recent N).
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n < len(entries) {
			entries = entries[len(entries)-n:]
		}
	}
	if r.URL.Query().Get("drain") == "true" {
		// Drain truncates; we already peeked. Re-Drain to clear.
		// (Peek does not mutate, so call Drain after — it returns the
		// current contents and truncates.)
		if _, derr := s.schedulerOutbox.Drain(); derr != nil {
			writeError(w, http.StatusInternalServerError, derr.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func (h *cronHandler) get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	j := h.svc.GetJob(id)
	if j == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("job %s not found", id))
		return
	}
	writeJSON(w, http.StatusOK, j)
}

// SetScheduler attaches a scheduler.Service to the server: it adds
// /api/cron/* routes, exposes the service, wires it into the handler so the
// `cron` tool is included in agent sessions created on this server, and adds
// /api/cron/outbox to read the JSONL delivery log. Safe to call before or
// after Listen; routes are live as soon as the mux serves.
func (s *Server) SetScheduler(svc *scheduler.Service) {
	s.attachScheduler(svc)
	if s.handler != nil {
		s.handler.scheduler = svc
	}
	if svc != nil {
		storePath, _ := scheduler.DefaultStorePath(s.workDir)
		if storePath != "" {
			s.schedulerOutbox = scheduler.NewOutbox(storePath)
			s.schedulerTargets = scheduler.NewTargets(storePath)
		}
		s.mux.HandleFunc("GET /api/cron/outbox", s.authMiddleware(s.handleCronOutbox))
		s.mux.HandleFunc("GET /api/cron/targets", s.authMiddleware(s.handleCronTargetsList))
		s.mux.HandleFunc("POST /api/cron/targets", s.authMiddleware(s.handleCronTargetsSet))
	}
}

// SchedulerOutbox returns the outbox the server is writing to (nil if no
// scheduler is attached).
func (s *Server) SchedulerOutbox() *scheduler.Outbox { return s.schedulerOutbox }

// SchedulerTargets returns the per-project cron-targets registry (nil if no
// scheduler is attached). Useful for tests and for hosts that want to
// surface the mapping in their own UI.
func (s *Server) SchedulerTargets() *scheduler.Targets { return s.schedulerTargets }

// cronPusher is the minimal contract a delivery sink needs to implement to
// forward scheduled-job results to a remote user. The Telegram bot
// satisfies it; other integrations (RC, web push) can satisfy it too.
type cronPusher interface {
	PushCronResult(chatID int64, jobID, jobName, owner, result, errStr string)
}

// CronChatResolver maps a job to a chat ID on the pusher, or returns
// deliver=false to skip forwarding for this job. The server provides a
// default resolver (NewCronChatResolver) that consults the per-project
// Targets registry; hosts may also implement their own (e.g. "always
// send to a global chat").
type CronChatResolver func(job *scheduler.Job) (chatID int64, deliver bool)

// NewCronChatResolver returns a CronChatResolver that looks the job's
// workdir up in the given Targets registry. The workdir is resolved from
// the job's Payload.Owner (set by the host at scheduling time) or, as a
// fallback, the server's current workdir. When no entry is registered,
// the resolver returns deliver=false so the delivery falls through to the
// log-only default sink.
//
// This is the canonical wiring for the "Telegram bot receives cron
// results for the project the user is running" pattern: a single
// SetTelegramCronSink call gives you per-project forwarding with zero
// per-job ceremony.
func NewCronChatResolver(targets *scheduler.Targets, defaultWorkdir string) CronChatResolver {
	return func(j *scheduler.Job) (int64, bool) {
		wd := defaultWorkdir
		if j != nil && j.Payload.Owner != "" {
			wd = j.Payload.Owner
		}
		if wd == "" {
			return 0, false
		}
		id, err := targets.Get(wd)
		if err != nil {
			return 0, false
		}
		return id, true
	}
}

// RCBridgePusher adapts an *RCBridge to the cronPusher interface so the
// scheduler's drainer can forward cron deliveries to the live TUI session
// (not just to Telegram). The TUI consumes CronDeliveryCh via its own
// listener and renders the delivery as a system message in the chat —
// no agent turn, no permission prompts, just a notification.
//
// Construct one per (potentially) active TUI session and pass it to
// Server.SetTelegramCronSink alongside the Telegram bot. The bridge's
// CronDeliveryCh is buffered; if the TUI is absent, Push is a no-op.
type RCBridgePusher struct{ Bridge *RCBridge }

// PushCronResult satisfies cronPusher.
func (p RCBridgePusher) PushCronResult(_ int64, jobID, jobName, owner, result, errStr string) {
	if p.Bridge == nil {
		return
	}
	p.Bridge.PushCronDelivery(CronDelivery{
		JobID:   jobID,
		JobName: jobName,
		Owner:   owner,
		Result:  result,
		Error:   errStr,
	})
}

// SetTelegramCronSink wires a pusher (e.g. the Telegram bot) into the
// scheduler's drainer so cron results are forwarded to a remote chat
// when the job's DeliveredTo is set. The resolver is consulted for each
// delivery; if it returns deliver=false the entry is logged-only. When
// the handler has an active RC bridge (i.e. a TUI session is connected),
// every delivery is also pushed to the TUI's chat as a system message
// — this is the "drainer-as-RC-sink" path that makes the TUI a real-time
// recipient without needing the Telegram bot.
func (s *Server) SetTelegramCronSink(pusher cronPusher, resolve CronChatResolver) {
	if s == nil || s.scheduler == nil || pusher == nil || resolve == nil {
		return
	}
	svc, ok := s.scheduler.(*scheduler.Service)
	if !ok {
		return
	}
	bridge := s.handler.rc // may be nil — TUI just isn't attached
	svc.SetDrainerSink(func(d scheduler.Delivery) {
		// Always fan out to the TUI bridge (passive notification, no resolver
		// gate) so the user sees the result in the chat where the cron job
		// is running.
		if bridge != nil {
			bridge.PushCronDelivery(CronDelivery{
				JobID:   d.JobID,
				JobName: d.JobName,
				Owner:   d.Owner,
				Result:  d.Result,
				Error:   d.Error,
			})
		}
		// We only know enough to forward via Delivery — re-resolve against
		// the project store to get the originating *Job. Use GetJob by
		// looking the job up; jobs.json is the source of truth.
		jobs := svc.ListJobs()
		var job *scheduler.Job
		for i := range jobs {
			if jobs[i].ID == d.JobID {
				job = &jobs[i]
				break
			}
		}
		if job == nil {
			log.Printf("server: cron drain: job %s not found in store; dropping delivery", d.JobID)
			return
		}
		chatID, deliver := resolve(job)
		if !deliver {
			log.Printf("server: cron: delivered job=%s owner=%s result=%q err=%q (no chat registered)",
				d.JobID, d.Owner, truncateForLog(d.Result), d.Error)
			return
		}
		pusher.PushCronResult(chatID, d.JobID, d.JobName, d.Owner, d.Result, d.Error)
	})
}

// AttachTelegramBot is the canonical one-line wiring for cron
// forwarding. It constructs a per-project Targets registry (lazily, next
// to the scheduler's store), wires the given pusher into the drainer
// using NewCronChatResolver, and returns the registry so the caller can
// also expose it via the /api/cron/targets endpoint or hand-edit the
// file. The workdir argument is the host's project root (same value
// passed to StartForHost).
func (s *Server) AttachTelegramBot(workdir string, pusher cronPusher) *scheduler.Targets {
	if s == nil || pusher == nil {
		return nil
	}
	storePath, err := scheduler.DefaultStorePath(workdir)
	if err != nil {
		log.Printf("server: AttachTelegramBot: %v", err)
		return nil
	}
	tg := scheduler.NewTargets(storePath)
	s.SetTelegramCronSink(pusher, NewCronChatResolver(tg, workdir))
	return tg
}

// truncateForLog mirrors the scheduler package's truncate helper without
// the import cycle.
func truncateForLog(s string) string {
	const max = 200
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// Scheduler returns the attached scheduler service (nil if none).
func (s *Server) Scheduler() *scheduler.Service {
	if s.scheduler == nil {
		return nil
	}
	if svc, ok := s.scheduler.(*scheduler.Service); ok {
		return svc
	}
	return nil
}

// attachScheduler wires a scheduler.Service into the server.
func (s *Server) attachScheduler(svc *scheduler.Service) {
	if s == nil || svc == nil {
		return
	}
	s.scheduler = svc
	h := &cronHandler{svc: svc}
	s.mux.HandleFunc("GET /api/cron", s.authMiddleware(h.list))
	s.mux.HandleFunc("POST /api/cron", s.authMiddleware(h.add))
	s.mux.HandleFunc("GET /api/cron/{id}", s.authMiddleware(h.get))
	s.mux.HandleFunc("PATCH /api/cron/{id}", s.authMiddleware(h.update))
	s.mux.HandleFunc("DELETE /api/cron/{id}", s.authMiddleware(h.remove))
}
