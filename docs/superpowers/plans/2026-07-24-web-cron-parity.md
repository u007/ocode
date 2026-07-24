# Web Cron/Scheduling Parity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Cron tab to the web UI (and desktop, which embeds the same web app) so users can list, create, edit, enable/disable, and delete scheduled jobs, view delivery history, and manage Telegram routing targets — bringing web/desktop to parity with the TUI's `/cron` command.

**Architecture:** One small backend addition (`UpdateJob` + `PATCH /api/cron/{id}`) on top of the existing `internal/scheduler` + `internal/server` REST surface. On the frontend, a new `Cron` top-level tab wired into `TopTabs.tsx`, `App.tsx`, and `SessionPage.tsx`, backed by four new components under `web/src/components/Cron/` and new `api.*Cron*` client methods.

**Tech Stack:** Go (`net/http`, `internal/scheduler`), React + TypeScript, Tailwind, Radix UI primitives (`Dialog`, `Select`) via the project's `web/src/components/ui/*` wrappers, `lucide-react` icons.

## Global Constraints

- Reference design: `docs/superpowers/specs/2026-07-24-web-cron-parity-design.md`.
- Backend must not change persistence format, the `cron` tool's LLM-facing contract, or existing outbox/targets endpoints.
- No SSE/push for job state — polling only, matching `GitPanel`'s `REFRESH_INTERVAL` pattern.
- **No frontend test runner exists in this project** (`web/package.json` has no `vitest`/`jest`/testing-library dependency, and no `*.test.tsx` files exist anywhere under `web/src`). Do not introduce one as a side effect of this feature — frontend tasks are verified via `tsc`/`vite build` (the project's existing `npm run build` script) and manual exercise, matching how `GitPanel`/`LogPanel`/`PluginsPanel` were built with no tests. Only the Go backend addition gets unit tests, per the project's existing Go test conventions (`internal/scheduler/scheduler_test.go`, `internal/server/scheduler_targets_http_test.go`).
- Follow existing component conventions exactly: functional components, `useState`/`useEffect`/`useCallback`, `api` object from `web/src/api/client.ts` for JSON calls, Tailwind zinc palette (`bg-zinc-900`, `border-zinc-700`, `text-zinc-400`, etc.) matching sibling panels.
- Both `web/src/App.tsx` (the non-session-scoped `HomeApp`) and `web/src/pages/SessionPage.tsx` independently render the tab switch — every tab-wiring change must be made in both files.

---

## File Structure

**Backend (Go):**
- Modify: `internal/scheduler/scheduler.go` — add `JobPatch` type and `UpdateJob` method.
- Modify: `internal/scheduler/scheduler_test.go` — add `TestUpdateJob`.
- Modify: `internal/server/scheduler.go` — add `PATCH /api/cron/{id}` handler + route registration.
- Create: `internal/server/scheduler_update_http_test.go` — handler test mirroring `scheduler_targets_http_test.go`.

**Frontend (TypeScript/React):**
- Modify: `web/src/api/types.ts` — add `CronJob`, `CronSchedule`, `CronPayload`, `CronJobState`, `CronDelivery` types.
- Modify: `web/src/api/client.ts` — add `listCronJobs`, `getCronJob`, `addCronJob`, `updateCronJob`, `deleteCronJob`, `getCronOutbox`, `drainCronOutbox`, `getCronTargets`, `setCronTarget`.
- Create: `web/src/components/Cron/cronFormat.ts` — pure helper: `describeSchedule(schedule)` → human-readable string (TS port of `internal/tui/command_cron.go`'s `cronDescribe`), plus `nextRunLabel`/`lastRunLabel` epoch-ms formatters.
- Create: `web/src/components/Cron/CronJobDialog.tsx` — create/edit form dialog.
- Create: `web/src/components/Cron/CronOutboxPanel.tsx` — collapsible delivery-history list.
- Create: `web/src/components/Cron/CronTargetsPanel.tsx` — collapsible key-value editor for Telegram targets.
- Create: `web/src/components/Cron/CronPanel.tsx` — tab root: table + "Add job" button, mounts the two panels above.
- Modify: `web/src/components/Layout/TopTabs.tsx` — add `cron` tab entry.
- Modify: `web/src/App.tsx` — import `CronPanel`, render on `activeTab === "cron"`.
- Modify: `web/src/pages/SessionPage.tsx` — same.

---

## Task 1: Backend — `UpdateJob` on `scheduler.Service`

**Files:**
- Modify: `internal/scheduler/scheduler.go`
- Test: `internal/scheduler/scheduler_test.go`

**Interfaces:**
- Consumes: existing `Service.mu`, `Service.jobs []Job`, `Service.now()`, `Service.computeNextRun(*Job, time.Time) (int64, error)`, `Service.persistLocked() error`, `Service.wake()`, `validateSchedule(Schedule) error` — all already defined in `internal/scheduler/scheduler.go` and `internal/scheduler/types.go`.
- Produces: `type JobPatch struct { Enabled *bool; Name *string; Schedule *Schedule; Payload *Payload }` and `func (s *Service) UpdateJob(id string, patch JobPatch) (Job, error)` — later tasks (the HTTP handler) call this exact signature.

- [ ] **Step 1: Write the failing test**

Append to `internal/scheduler/scheduler_test.go`:

```go
func TestUpdateJob(t *testing.T) {
	s := newTestService(t)

	id, err := s.AddJob(Job{
		Schedule: Schedule{Kind: KindEvery, EveryMs: int64(time.Minute / time.Millisecond)},
		Payload:  Payload{Message: "original"},
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	// Toggle enabled off.
	disabled := false
	updated, err := s.UpdateJob(id, JobPatch{Enabled: &disabled})
	if err != nil {
		t.Fatalf("update enabled: %v", err)
	}
	if updated.Enabled {
		t.Fatal("want disabled after update")
	}

	// Edit name and payload message.
	newName := "renamed"
	newPayload := Payload{Message: "updated message"}
	updated, err = s.UpdateJob(id, JobPatch{Name: &newName, Payload: &newPayload})
	if err != nil {
		t.Fatalf("update name/payload: %v", err)
	}
	if updated.Name != "renamed" || updated.Payload.Message != "updated message" {
		t.Fatalf("want renamed/updated message, got %+v", updated)
	}
	// Enabled must survive the unrelated update.
	if updated.Enabled {
		t.Fatal("want enabled to remain false after unrelated update")
	}

	// Edit schedule recomputes NextRunAtMs.
	before := s.GetJob(id).State.NextRunAtMs
	newSchedule := Schedule{Kind: KindEvery, EveryMs: int64(2 * time.Hour / time.Millisecond)}
	updated, err = s.UpdateJob(id, JobPatch{Schedule: &newSchedule})
	if err != nil {
		t.Fatalf("update schedule: %v", err)
	}
	if updated.State.NextRunAtMs == before {
		t.Fatal("want NextRunAtMs recomputed after schedule change")
	}
	wantNext := fixedNow.UnixMilli() + newSchedule.EveryMs
	if updated.State.NextRunAtMs != wantNext {
		t.Fatalf("want next run %d, got %d", wantNext, updated.State.NextRunAtMs)
	}

	// Invalid schedule is rejected and leaves the job untouched.
	badSchedule := Schedule{Kind: KindEvery, EveryMs: 0}
	if _, err := s.UpdateJob(id, JobPatch{Schedule: &badSchedule}); err == nil {
		t.Fatal("expected error for invalid schedule")
	}

	// Unknown id.
	if _, err := s.UpdateJob("nonexistent", JobPatch{Enabled: &disabled}); err == nil {
		t.Fatal("expected error for unknown id")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/scheduler/... -run TestUpdateJob -v`
Expected: FAIL — `s.UpdateJob undefined (type *Service has no field or method UpdateJob)`

- [ ] **Step 3: Write minimal implementation**

In `internal/scheduler/scheduler.go`, add directly after `RemoveJob` (after the closing brace at line 350, before `removeJobLocked`):

```go
// JobPatch is a partial update for UpdateJob. Only non-nil fields are
// applied; Schedule, when set, replaces the whole Schedule struct and
// triggers a NextRunAtMs recompute (mirrors AddJob's validation path).
type JobPatch struct {
	Enabled  *bool
	Name     *string
	Schedule *Schedule
	Payload  *Payload
}

// UpdateJob applies patch to the job identified by id and returns the
// updated job. Returns an error (leaving the job unmodified) if id is
// unknown or patch.Schedule is invalid.
func (s *Service) UpdateJob(id string, patch JobPatch) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := -1
	for i := range s.jobs {
		if s.jobs[i].ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		return Job{}, fmt.Errorf("job %s not found", id)
	}

	if patch.Schedule != nil {
		if err := validateSchedule(*patch.Schedule); err != nil {
			return Job{}, err
		}
	}

	j := &s.jobs[idx]
	if patch.Enabled != nil {
		j.Enabled = *patch.Enabled
	}
	if patch.Name != nil {
		j.Name = *patch.Name
	}
	if patch.Payload != nil {
		j.Payload = *patch.Payload
	}
	if patch.Schedule != nil {
		j.Schedule = *patch.Schedule
		nr, err := s.computeNextRun(j, s.now())
		if err != nil {
			return Job{}, err
		}
		j.State.NextRunAtMs = nr
	}

	if err := s.persistLocked(); err != nil {
		return Job{}, err
	}
	s.wake()
	return *j, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/scheduler/... -run TestUpdateJob -v`
Expected: PASS

- [ ] **Step 5: Run the full scheduler package test suite**

Run: `go test ./internal/scheduler/...`
Expected: PASS (no regressions)

- [ ] **Step 6: Commit**

```bash
git add internal/scheduler/scheduler.go internal/scheduler/scheduler_test.go
git commit -m "feat(scheduler): add UpdateJob for enabling/disabling and editing jobs"
```

---

## Task 2: Backend — `PATCH /api/cron/{id}` HTTP handler

**Files:**
- Modify: `internal/server/scheduler.go`
- Create: `internal/server/scheduler_update_http_test.go`

**Interfaces:**
- Consumes: `scheduler.Service.UpdateJob(id string, patch scheduler.JobPatch) (scheduler.Job, error)` from Task 1; existing `writeJSON`, `writeError` helpers already used by `cronHandler` methods in `internal/server/scheduler.go`; existing `cronScheduleReq` type (already defined at `internal/server/scheduler.go:33`).
- Produces: `PATCH /api/cron/{id}` route returning `200 {"...job fields..."}` on success, `404` if unknown id, `400` on invalid JSON or invalid schedule. Later tasks (frontend `updateCronJob`) call this exact route/method/body shape.

- [ ] **Step 1: Write the failing test**

Create `internal/server/scheduler_update_http_test.go`:

```go
package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/scheduler"
)

func TestCronUpdateEndpoint(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	svc := scheduler.NewService(storePath)
	if err := svc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(svc.Stop)

	id, err := svc.AddJob(scheduler.Job{
		Schedule: scheduler.Schedule{Kind: scheduler.KindEvery, EveryMs: 60000},
		Payload:  scheduler.Payload{Message: "hi"},
	})
	if err != nil {
		t.Fatalf("seed job: %v", err)
	}

	h := &cronHandler{svc: svc}
	mux := http.NewServeMux()
	mux.HandleFunc("PATCH /api/cron/{id}", h.update)

	// Disable the job.
	body, _ := json.Marshal(map[string]any{"enabled": false})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("PATCH", "/api/cron/"+id, bytes.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	var got scheduler.Job
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Enabled {
		t.Fatal("want disabled")
	}

	// Unknown id -> 404.
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("PATCH", "/api/cron/nonexistent", bytes.NewReader(body)))
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}

	// Invalid schedule -> 400.
	badBody, _ := json.Marshal(map[string]any{
		"schedule": map[string]any{"kind": "every", "every_ms": 0},
	})
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("PATCH", "/api/cron/"+id, bytes.NewReader(badBody)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/... -run TestCronUpdateEndpoint -v`
Expected: FAIL — `h.update undefined (type *cronHandler has no field or method update)`

- [ ] **Step 3: Write minimal implementation**

In `internal/server/scheduler.go`, add a new handler method after `func (h *cronHandler) get` (currently ending at line 176) and a `cronUpdateRequest` type near `cronAddRequest`:

```go
// cronUpdateRequest is the JSON body for PATCH /api/cron/{id}. Every field
// is optional; only present fields are applied.
type cronUpdateRequest struct {
	Enabled  *bool            `json:"enabled,omitempty"`
	Name     *string          `json:"name,omitempty"`
	Message  *string          `json:"message,omitempty"`
	Notes    *string          `json:"notes,omitempty"`
	Schedule *cronScheduleReq `json:"schedule,omitempty"`
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

	patch := scheduler.JobPatch{
		Enabled: req.Enabled,
		Name:    req.Name,
	}
	if req.Schedule != nil {
		patch.Schedule = &scheduler.Schedule{
			Kind:    req.Schedule.Kind,
			AtMs:    req.Schedule.AtMs,
			EveryMs: req.Schedule.EveryMs,
			Expr:    req.Schedule.Expr,
			TZ:      req.Schedule.TZ,
		}
	}
	if req.Message != nil || req.Notes != nil {
		existing := h.svc.GetJob(id)
		if existing == nil {
			writeError(w, http.StatusNotFound, fmt.Sprintf("job %s not found", id))
			return
		}
		payload := existing.Payload
		if req.Message != nil {
			payload.Message = *req.Message
		}
		if req.Notes != nil {
			payload.Notes = *req.Notes
		}
		patch.Payload = &payload
	}

	updated, err := h.svc.UpdateJob(id, patch)
	if err != nil {
		if err.Error() == fmt.Sprintf("job %s not found", id) {
			writeError(w, http.StatusNotFound, err.Error())
		} else {
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, updated)
}
```

Then register the route in `SetScheduler` (`internal/server/scheduler.go`, in the block that currently registers `GET /api/cron`, `POST /api/cron`, `GET /api/cron/{id}`, `DELETE /api/cron/{id}` around line 379-383):

```go
	s.mux.HandleFunc("PATCH /api/cron/{id}", s.authMiddleware(h.update))
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/... -run TestCronUpdateEndpoint -v`
Expected: PASS

- [ ] **Step 5: Run the full server package test suite**

Run: `go test ./internal/server/...`
Expected: PASS (no regressions)

- [ ] **Step 6: Full repo build check**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 7: Commit**

```bash
git add internal/server/scheduler.go internal/server/scheduler_update_http_test.go
git commit -m "feat(server): add PATCH /api/cron/{id} to enable/disable and edit jobs"
```

---

## Task 3: Frontend — types and API client methods

**Files:**
- Modify: `web/src/api/types.ts`
- Modify: `web/src/api/client.ts`

**Interfaces:**
- Consumes: `fetchJSON<T>`, `apiPath`, `authHeaders` from `web/src/api/client.ts` (already defined, lines 30-65).
- Produces: types `CronScheduleKind`, `CronSchedule`, `CronPermMode`, `CronPayload`, `CronJobState`, `CronJob`, `CronDelivery` in `web/src/api/types.ts`; methods `api.listCronJobs`, `api.getCronJob`, `api.addCronJob`, `api.updateCronJob`, `api.deleteCronJob`, `api.getCronOutbox`, `api.drainCronOutbox`, `api.getCronTargets`, `api.setCronTarget` in `web/src/api/client.ts` — Task 4/5/6/7 (the React components) call these exact names and shapes.

- [ ] **Step 1: Add types**

Append to `web/src/api/types.ts`:

```ts
// ── Cron / scheduled jobs (GET/POST /api/cron, PATCH/GET/DELETE /api/cron/{id}) ──
export type CronScheduleKind = "at" | "every" | "cron";
export type CronPermMode = "normal" | "yolo" | "locked";

export interface CronSchedule {
  kind: CronScheduleKind;
  at_ms?: number;
  every_ms?: number;
  expr?: string;
  tz?: string;
}

export interface CronPayload {
  message: string;
  notes?: string;
  owner?: string;
  deliver_to?: string;
  perm_mode?: CronPermMode;
}

export interface CronJobState {
  next_run_at_ms?: number;
  last_run_at_ms?: number;
  last_status?: string;
  last_error?: string;
  runs?: number;
}

export interface CronJob {
  id: string;
  name: string;
  schedule: CronSchedule;
  payload: CronPayload;
  state: CronJobState;
  created_at_ms: number;
  enabled: boolean;
}

export interface CronDelivery {
  job_id: string;
  job_name: string;
  owner: string;
  delivered_to?: string;
  result: string;
  error?: string;
  at: string;
}
```

- [ ] **Step 2: Add client methods**

In `web/src/api/client.ts`, add `CronJob, CronDelivery` to the type-only import block at the top (the `import type { ... } from "./types"` block starting at line 1), then append to the `api` object, after the `removePlugin` entry (after line 351, before the `// ── Dynamic commands / skills ──` comment):

```ts
  // ── Cron / scheduled jobs ──
  listCronJobs: () => fetchJSON<{ jobs: CronJob[] }>("/api/cron"),
  getCronJob: (id: string) => fetchJSON<CronJob>(`/api/cron/${encodeURIComponent(id)}`),
  addCronJob: (job: {
    name?: string;
    message: string;
    notes?: string;
    owner?: string;
    deliver_to?: string;
    perm_mode?: string;
    schedule: {
      kind: string;
      at_ms?: number;
      every_ms?: number;
      expr?: string;
      tz?: string;
    };
  }) =>
    fetchJSON<{ id: string }>("/api/cron", {
      method: "POST",
      body: JSON.stringify(job),
    }),
  updateCronJob: (
    id: string,
    patch: {
      enabled?: boolean;
      name?: string;
      message?: string;
      notes?: string;
      schedule?: {
        kind: string;
        at_ms?: number;
        every_ms?: number;
        expr?: string;
        tz?: string;
      };
    },
  ) =>
    fetchJSON<CronJob>(`/api/cron/${encodeURIComponent(id)}`, {
      method: "PATCH",
      body: JSON.stringify(patch),
    }),
  deleteCronJob: async (id: string): Promise<void> => {
    const res = await fetch(apiPath(`/api/cron/${encodeURIComponent(id)}`), {
      method: "DELETE",
      headers: authHeaders(),
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: res.statusText }));
      throw new Error(err.error || res.statusText);
    }
  },
  getCronOutbox: (limit?: number) =>
    fetchJSON<{ entries: CronDelivery[] }>(
      `/api/cron/outbox${limit ? `?limit=${limit}` : ""}`,
    ),
  drainCronOutbox: () =>
    fetchJSON<{ entries: CronDelivery[] }>("/api/cron/outbox?drain=true"),
  getCronTargets: () =>
    fetchJSON<{ targets: Record<string, number> }>("/api/cron/targets"),
  setCronTarget: (workdir: string, chatId: number) =>
    fetchJSON<{ ok: boolean }>("/api/cron/targets", {
      method: "POST",
      body: JSON.stringify({ workdir, chat_id: chatId }),
    }),
```

- [ ] **Step 3: Type-check**

Run: `cd web && npx tsc --noEmit`
Expected: no errors (existing project has no `tsc` errors; this task must not introduce any — the `dispatch` build script already runs `tsc && vite build`, so `--noEmit` here is the fast equivalent for iteration)

- [ ] **Step 4: Commit**

```bash
git add web/src/api/types.ts web/src/api/client.ts
git commit -m "feat(web): add cron job types and API client methods"
```

---

## Task 4: Frontend — schedule formatting helper

**Files:**
- Create: `web/src/components/Cron/cronFormat.ts`

**Interfaces:**
- Consumes: `CronSchedule`, `CronJobState` types from Task 3 (`web/src/api/types.ts`).
- Produces: `describeSchedule(schedule: CronSchedule): string`, `formatEpochMs(ms: number | undefined): string` — Task 5 (`CronPanel.tsx`) imports both by name.

- [ ] **Step 1: Write the file**

Create `web/src/components/Cron/cronFormat.ts`:

```ts
import type { CronSchedule } from "@/api/types";

/** Human-readable description of a schedule, mirroring the TUI's
 *  cronDescribe() in internal/tui/command_cron.go. */
export function describeSchedule(schedule: CronSchedule): string {
  switch (schedule.kind) {
    case "at":
      return schedule.at_ms
        ? `at ${new Date(schedule.at_ms).toLocaleString()}`
        : "at (unset)";
    case "every": {
      if (!schedule.every_ms) return "every (unset)";
      const seconds = Math.round(schedule.every_ms / 1000);
      if (seconds % 3600 === 0) return `every ${seconds / 3600}h`;
      if (seconds % 60 === 0) return `every ${seconds / 60}m`;
      return `every ${seconds}s`;
    }
    case "cron":
      return schedule.tz
        ? `${schedule.expr} (tz ${schedule.tz})`
        : schedule.expr || "cron (unset)";
    default:
      return schedule.kind;
  }
}

/** Formats an epoch-ms timestamp for table display, or a dash if unset. */
export function formatEpochMs(ms: number | undefined): string {
  if (!ms) return "—";
  return new Date(ms).toLocaleString();
}
```

- [ ] **Step 2: Type-check**

Run: `cd web && npx tsc --noEmit`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add web/src/components/Cron/cronFormat.ts
git commit -m "feat(web): add cron schedule formatting helpers"
```

---

## Task 5: Frontend — `CronJobDialog.tsx` (create/edit form)

**Files:**
- Create: `web/src/components/Cron/CronJobDialog.tsx`

**Interfaces:**
- Consumes: `api.addCronJob`, `api.updateCronJob` (Task 3); `CronJob`, `CronScheduleKind`, `CronPermMode` types (Task 3); UI primitives `Dialog`, `DialogContent`, `DialogHeader`, `DialogTitle` from `@/components/ui/dialog`, `Button` from `@/components/ui/button`, `Input` from `@/components/ui/input`, `Select`/`SelectTrigger`/`SelectContent`/`SelectItem`/`SelectValue` from `@/components/ui/select` (all pre-existing, used per Task decision to follow "Library Discipline" — no hand-rolled dropdowns/modals).
- Produces: `export default function CronJobDialog(props: { open: boolean; onOpenChange: (open: boolean) => void; editingJob: CronJob | null; onSaved: () => void })` — Task 8 (`CronPanel.tsx`) renders `<CronJobDialog open={...} onOpenChange={...} editingJob={...} onSaved={...} />`.

- [ ] **Step 1: Write the component**

Create `web/src/components/Cron/CronJobDialog.tsx`:

```tsx
import { useEffect, useState } from "react";
import { api } from "@/api/client";
import type { CronJob, CronPermMode, CronScheduleKind } from "@/api/types";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Loader2 } from "lucide-react";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  editingJob: CronJob | null;
  onSaved: () => void;
}

const KIND_LABELS: Record<CronScheduleKind, string> = {
  at: "Once",
  every: "Every",
  cron: "Cron expression",
};

const PERM_LABELS: Record<CronPermMode, string> = {
  normal: "Normal (ask/deny like an interactive session)",
  yolo: "YOLO (auto-allow everything)",
  locked: "Locked (read-only)",
};

/** Converts a Date-local <input type="datetime-local"> value to epoch ms. */
function localDatetimeToMs(value: string): number | undefined {
  if (!value) return undefined;
  const ms = new Date(value).getTime();
  return Number.isNaN(ms) ? undefined : ms;
}

export default function CronJobDialog({ open, onOpenChange, editingJob, onSaved }: Props) {
  const [name, setName] = useState("");
  const [message, setMessage] = useState("");
  const [notes, setNotes] = useState("");
  const [deliverTo, setDeliverTo] = useState("");
  const [permMode, setPermMode] = useState<CronPermMode>("normal");
  const [kind, setKind] = useState<CronScheduleKind>("every");
  const [atLocal, setAtLocal] = useState("");
  const [everyMinutes, setEveryMinutes] = useState("60");
  const [expr, setExpr] = useState("0 * * * *");
  const [tz, setTz] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setError(null);
    if (editingJob) {
      setName(editingJob.name);
      setMessage(editingJob.payload.message);
      setNotes(editingJob.payload.notes ?? "");
      setDeliverTo(editingJob.payload.deliver_to ?? "");
      setPermMode(editingJob.payload.perm_mode ?? "normal");
      setKind(editingJob.schedule.kind);
      setAtLocal(
        editingJob.schedule.at_ms
          ? new Date(editingJob.schedule.at_ms).toISOString().slice(0, 16)
          : "",
      );
      setEveryMinutes(
        editingJob.schedule.every_ms
          ? String(Math.round(editingJob.schedule.every_ms / 60000))
          : "60",
      );
      setExpr(editingJob.schedule.expr ?? "0 * * * *");
      setTz(editingJob.schedule.tz ?? "");
    } else {
      setName("");
      setMessage("");
      setNotes("");
      setDeliverTo("");
      setPermMode("normal");
      setKind("every");
      setAtLocal("");
      setEveryMinutes("60");
      setExpr("0 * * * *");
      setTz("");
    }
  }, [open, editingJob]);

  const buildSchedule = () => {
    if (kind === "at") {
      const atMs = localDatetimeToMs(atLocal);
      if (!atMs) throw new Error("pick a valid date/time");
      return { kind: "at", at_ms: atMs };
    }
    if (kind === "every") {
      const minutes = Number(everyMinutes);
      if (!minutes || minutes <= 0) throw new Error("interval must be > 0 minutes");
      return { kind: "every", every_ms: minutes * 60000 };
    }
    if (!expr.trim()) throw new Error("cron expression is required");
    return { kind: "cron", expr: expr.trim(), tz: tz.trim() || undefined };
  };

  const save = async () => {
    setError(null);
    if (!message.trim()) {
      setError("message is required");
      return;
    }
    setSaving(true);
    try {
      const schedule = buildSchedule();
      if (editingJob) {
        await api.updateCronJob(editingJob.id, {
          name: name.trim() || undefined,
          message: message.trim(),
          notes: notes.trim() || undefined,
          schedule,
        });
      } else {
        await api.addCronJob({
          name: name.trim() || undefined,
          message: message.trim(),
          notes: notes.trim() || undefined,
          deliver_to: deliverTo.trim() || undefined,
          perm_mode: permMode,
          schedule,
        });
      }
      onSaved();
      onOpenChange(false);
    } catch (err) {
      console.error("failed to save cron job", err);
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="text-sm">
            {editingJob ? "Edit scheduled job" : "New scheduled job"}
          </DialogTitle>
        </DialogHeader>

        <div className="space-y-3 py-2">
          <div>
            <label className="text-xs text-zinc-500 uppercase tracking-wider">Name</label>
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="(defaults to the message)" className="h-8 text-xs mt-1" />
          </div>

          <div>
            <label className="text-xs text-zinc-500 uppercase tracking-wider">Message</label>
            <Input value={message} onChange={(e) => setMessage(e.target.value)} placeholder="prompt to run when the job fires" className="h-8 text-xs mt-1" />
          </div>

          <div>
            <label className="text-xs text-zinc-500 uppercase tracking-wider">Notes</label>
            <Input value={notes} onChange={(e) => setNotes(e.target.value)} placeholder="optional purpose/description" className="h-8 text-xs mt-1" />
          </div>

          <div>
            <label className="text-xs text-zinc-500 uppercase tracking-wider">Schedule</label>
            <Select value={kind} onValueChange={(v) => setKind(v as CronScheduleKind)}>
              <SelectTrigger className="h-8 text-xs mt-1">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {(Object.keys(KIND_LABELS) as CronScheduleKind[]).map((k) => (
                  <SelectItem key={k} value={k}>{KIND_LABELS[k]}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {kind === "at" && (
            <Input
              type="datetime-local"
              value={atLocal}
              onChange={(e) => setAtLocal(e.target.value)}
              className="h-8 text-xs"
            />
          )}
          {kind === "every" && (
            <div className="flex items-center gap-2">
              <Input
                type="number"
                min={1}
                value={everyMinutes}
                onChange={(e) => setEveryMinutes(e.target.value)}
                className="h-8 text-xs w-24"
              />
              <span className="text-xs text-zinc-500">minutes</span>
            </div>
          )}
          {kind === "cron" && (
            <div className="space-y-2">
              <Input value={expr} onChange={(e) => setExpr(e.target.value)} placeholder="minute hour dom month dow" className="h-8 text-xs font-mono" />
              <Input value={tz} onChange={(e) => setTz(e.target.value)} placeholder="IANA timezone (optional, defaults to host local)" className="h-8 text-xs" />
            </div>
          )}

          {!editingJob && (
            <>
              <div>
                <label className="text-xs text-zinc-500 uppercase tracking-wider">Deliver to</label>
                <Input value={deliverTo} onChange={(e) => setDeliverTo(e.target.value)} placeholder="optional delivery hint" className="h-8 text-xs mt-1" />
              </div>
              <div>
                <label className="text-xs text-zinc-500 uppercase tracking-wider">Permission mode</label>
                <Select value={permMode} onValueChange={(v) => setPermMode(v as CronPermMode)}>
                  <SelectTrigger className="h-8 text-xs mt-1">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {(Object.keys(PERM_LABELS) as CronPermMode[]).map((p) => (
                      <SelectItem key={p} value={p}>{PERM_LABELS[p]}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                {permMode === "yolo" && (
                  <p className="text-xs text-yellow-500 mt-1">
                    YOLO auto-allows every tool call this job makes. Only use for jobs you fully trust.
                  </p>
                )}
              </div>
            </>
          )}

          {error && <div className="text-xs text-red-400">{error}</div>}
        </div>

        <div className="flex justify-end gap-2 pt-2">
          <Button variant="ghost" size="sm" className="h-8 text-xs" onClick={() => onOpenChange(false)} disabled={saving}>
            Cancel
          </Button>
          <Button size="sm" className="h-8 text-xs gap-1.5" onClick={save} disabled={saving}>
            {saving && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
            {editingJob ? "Save" : "Create"}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
```

- [ ] **Step 2: Type-check**

Run: `cd web && npx tsc --noEmit`
Expected: no errors. If `Button`'s `variant` prop doesn't accept `"ghost"`, check `web/src/components/ui/button.tsx`'s `buttonVariants` and swap to whatever variant name that file defines for a plain/secondary style — match the file, don't guess.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/Cron/CronJobDialog.tsx
git commit -m "feat(web): add CronJobDialog create/edit form"
```

---

## Task 6: Frontend — `CronOutboxPanel.tsx`

**Files:**
- Create: `web/src/components/Cron/CronOutboxPanel.tsx`

**Interfaces:**
- Consumes: `api.getCronOutbox`, `api.drainCronOutbox` (Task 3); `CronDelivery` type (Task 3).
- Produces: `export default function CronOutboxPanel()` — Task 8 (`CronPanel.tsx`) renders `<CronOutboxPanel />` with no props.

- [ ] **Step 1: Write the component**

Create `web/src/components/Cron/CronOutboxPanel.tsx`:

```tsx
import { useCallback, useEffect, useState } from "react";
import { ChevronDown, ChevronRight, Trash2, Loader2 } from "lucide-react";
import { api } from "@/api/client";
import type { CronDelivery } from "@/api/types";
import { Button } from "@/components/ui/button";

export default function CronOutboxPanel() {
  const [expanded, setExpanded] = useState(false);
  const [entries, setEntries] = useState<CronDelivery[]>([]);
  const [loading, setLoading] = useState(false);
  const [clearing, setClearing] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await api.getCronOutbox(50);
      setEntries(res.entries);
    } catch (err) {
      console.error("failed to load cron outbox", err);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (expanded) load();
  }, [expanded, load]);

  const clear = async () => {
    setClearing(true);
    try {
      await api.drainCronOutbox();
      setEntries([]);
    } catch (err) {
      console.error("failed to clear cron outbox", err);
    } finally {
      setClearing(false);
    }
  };

  return (
    <div className="border-t border-zinc-700">
      <button
        className="w-full flex items-center justify-between px-3 py-2 text-xs text-zinc-400 hover:text-zinc-200"
        onClick={() => setExpanded((v) => !v)}
      >
        <span className="flex items-center gap-1.5 uppercase tracking-wider">
          {expanded ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />}
          Delivery history
        </span>
        {loading && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
      </button>
      {expanded && (
        <div className="px-3 pb-3 space-y-1">
          {entries.length > 0 && (
            <div className="flex justify-end">
              <Button variant="ghost" size="sm" className="h-6 text-xs gap-1" onClick={clear} disabled={clearing}>
                <Trash2 className="w-3 h-3" /> Clear
              </Button>
            </div>
          )}
          {entries.length === 0 && !loading && (
            <div className="text-xs text-zinc-500 py-2">No deliveries yet.</div>
          )}
          {entries.map((e, i) => (
            <div key={`${e.job_id}-${e.at}-${i}`} className="text-xs border border-zinc-800 rounded p-2">
              <div className="flex items-center justify-between">
                <span className="font-medium text-zinc-300">{e.job_name}</span>
                <span className="text-zinc-500">{new Date(e.at).toLocaleString()}</span>
              </div>
              {e.error ? (
                <div className="text-red-400 mt-1">{e.error}</div>
              ) : (
                <div className="text-zinc-400 mt-1 line-clamp-3">{e.result}</div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Type-check**

Run: `cd web && npx tsc --noEmit`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add web/src/components/Cron/CronOutboxPanel.tsx
git commit -m "feat(web): add CronOutboxPanel delivery history viewer"
```

---

## Task 7: Frontend — `CronTargetsPanel.tsx`

**Files:**
- Create: `web/src/components/Cron/CronTargetsPanel.tsx`

**Interfaces:**
- Consumes: `api.getCronTargets`, `api.setCronTarget` (Task 3).
- Produces: `export default function CronTargetsPanel()` — Task 8 renders `<CronTargetsPanel />` with no props.

- [ ] **Step 1: Write the component**

Create `web/src/components/Cron/CronTargetsPanel.tsx`:

```tsx
import { useCallback, useEffect, useState } from "react";
import { ChevronDown, ChevronRight, Trash2, Plus, Loader2 } from "lucide-react";
import { api } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

export default function CronTargetsPanel() {
  const [expanded, setExpanded] = useState(false);
  const [targets, setTargets] = useState<Record<string, number>>({});
  const [loading, setLoading] = useState(false);
  const [workdir, setWorkdir] = useState("");
  const [chatId, setChatId] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await api.getCronTargets();
      setTargets(res.targets);
    } catch (err) {
      console.error("failed to load cron targets", err);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (expanded) load();
  }, [expanded, load]);

  const add = async () => {
    setError(null);
    const id = Number(chatId);
    if (!workdir.trim() || !id) {
      setError("workdir and a numeric chat id are required");
      return;
    }
    setBusy(true);
    try {
      await api.setCronTarget(workdir.trim(), id);
      setWorkdir("");
      setChatId("");
      await load();
    } catch (err) {
      console.error("failed to set cron target", err);
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const remove = async (wd: string) => {
    setBusy(true);
    try {
      await api.setCronTarget(wd, 0);
      await load();
    } catch (err) {
      console.error("failed to remove cron target", err);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="border-t border-zinc-700">
      <button
        className="w-full flex items-center justify-between px-3 py-2 text-xs text-zinc-400 hover:text-zinc-200"
        onClick={() => setExpanded((v) => !v)}
      >
        <span className="flex items-center gap-1.5 uppercase tracking-wider">
          {expanded ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />}
          Telegram routing targets
        </span>
        {loading && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
      </button>
      {expanded && (
        <div className="px-3 pb-3 space-y-2">
          {Object.entries(targets).length === 0 && !loading && (
            <div className="text-xs text-zinc-500 py-1">No routing targets set.</div>
          )}
          {Object.entries(targets).map(([wd, cid]) => (
            <div key={wd} className="flex items-center justify-between text-xs border border-zinc-800 rounded px-2 py-1.5">
              <div className="min-w-0">
                <div className="truncate text-zinc-300">{wd}</div>
                <div className="text-zinc-500">chat {cid}</div>
              </div>
              <Button variant="ghost" size="sm" className="h-6 w-6 p-0 shrink-0" onClick={() => remove(wd)} disabled={busy}>
                <Trash2 className="w-3 h-3" />
              </Button>
            </div>
          ))}
          <div className="flex items-center gap-1.5 pt-1">
            <Input value={workdir} onChange={(e) => setWorkdir(e.target.value)} placeholder="/abs/path" className="h-7 text-xs" />
            <Input value={chatId} onChange={(e) => setChatId(e.target.value)} placeholder="chat id" type="number" className="h-7 text-xs w-24" />
            <Button size="sm" className="h-7 w-7 p-0 shrink-0" onClick={add} disabled={busy}>
              <Plus className="w-3.5 h-3.5" />
            </Button>
          </div>
          {error && <div className="text-xs text-red-400">{error}</div>}
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Type-check**

Run: `cd web && npx tsc --noEmit`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add web/src/components/Cron/CronTargetsPanel.tsx
git commit -m "feat(web): add CronTargetsPanel Telegram routing editor"
```

---

## Task 8: Frontend — `CronPanel.tsx` (tab root)

**Files:**
- Create: `web/src/components/Cron/CronPanel.tsx`

**Interfaces:**
- Consumes: `api.listCronJobs`, `api.deleteCronJob`, `api.updateCronJob` (Task 3); `describeSchedule`, `formatEpochMs` (Task 4); `CronJobDialog` (Task 5); `CronOutboxPanel` (Task 6); `CronTargetsPanel` (Task 7); `CronJob` type (Task 3).
- Produces: `export default function CronPanel()` — Task 9 (`TopTabs.tsx`/`App.tsx`/`SessionPage.tsx`) renders `<CronPanel />` with no props, exactly like `<LogPanel />`/`<AssetsPanel />`.

- [ ] **Step 1: Write the component**

Create `web/src/components/Cron/CronPanel.tsx`:

```tsx
import { useCallback, useEffect, useState } from "react";
import { Plus, Trash2, Pencil, Loader2, Clock } from "lucide-react";
import { api } from "@/api/client";
import type { CronJob } from "@/api/types";
import { describeSchedule, formatEpochMs } from "./cronFormat";
import CronJobDialog from "./CronJobDialog";
import CronOutboxPanel from "./CronOutboxPanel";
import CronTargetsPanel from "./CronTargetsPanel";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";

const REFRESH_INTERVAL = 15000;

export default function CronPanel() {
  const [jobs, setJobs] = useState<CronJob[]>([]);
  const [loading, setLoading] = useState(false);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editingJob, setEditingJob] = useState<CronJob | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await api.listCronJobs();
      setJobs(res.jobs.slice().sort((a, b) => a.name.localeCompare(b.name)));
    } catch (err) {
      console.error("failed to load cron jobs", err);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
    const interval = setInterval(load, REFRESH_INTERVAL);
    return () => clearInterval(interval);
  }, [load]);

  const toggleEnabled = async (job: CronJob) => {
    setBusyId(job.id);
    try {
      await api.updateCronJob(job.id, { enabled: !job.enabled });
      await load();
    } catch (err) {
      console.error("failed to toggle cron job", err);
    } finally {
      setBusyId(null);
    }
  };

  const remove = async (job: CronJob) => {
    setBusyId(job.id);
    try {
      await api.deleteCronJob(job.id);
      await load();
    } catch (err) {
      console.error("failed to delete cron job", err);
    } finally {
      setBusyId(null);
    }
  };

  const openCreate = () => {
    setEditingJob(null);
    setDialogOpen(true);
  };

  const openEdit = (job: CronJob) => {
    setEditingJob(job);
    setDialogOpen(true);
  };

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center justify-between p-3 border-b border-zinc-700">
        <label className="text-xs text-zinc-500 uppercase tracking-wider flex items-center gap-1.5">
          <Clock className="w-3.5 h-3.5" /> Scheduled Jobs
        </label>
        <div className="flex items-center gap-2">
          {loading && <Loader2 className="w-3.5 h-3.5 animate-spin text-zinc-500" />}
          <Button size="sm" className="h-7 gap-1.5 text-xs" onClick={openCreate}>
            <Plus className="w-3.5 h-3.5" /> Add job
          </Button>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto">
        {jobs.length === 0 && !loading && (
          <div className="text-xs text-zinc-500 p-4">No scheduled jobs.</div>
        )}
        {jobs.length > 0 && (
          <table className="w-full text-xs">
            <thead>
              <tr className="text-zinc-500 border-b border-zinc-800">
                <th className="text-left font-medium px-3 py-2">Name</th>
                <th className="text-left font-medium px-3 py-2">Schedule</th>
                <th className="text-left font-medium px-3 py-2">Next run</th>
                <th className="text-left font-medium px-3 py-2">Last run</th>
                <th className="text-left font-medium px-3 py-2">Status</th>
                <th className="text-left font-medium px-3 py-2">Runs</th>
                <th className="text-left font-medium px-3 py-2">Enabled</th>
                <th className="px-3 py-2" />
              </tr>
            </thead>
            <tbody>
              {jobs.map((job) => (
                <tr key={job.id} className="border-b border-zinc-800/50 hover:bg-zinc-800/30">
                  <td className="px-3 py-2 text-zinc-200">{job.name}</td>
                  <td className="px-3 py-2 text-zinc-400 font-mono">{describeSchedule(job.schedule)}</td>
                  <td className="px-3 py-2 text-zinc-400">{formatEpochMs(job.state.next_run_at_ms)}</td>
                  <td className="px-3 py-2 text-zinc-400">{formatEpochMs(job.state.last_run_at_ms)}</td>
                  <td className="px-3 py-2">
                    {job.state.last_status === "ok" && (
                      <Badge className="bg-green-500/20 text-green-400">ok</Badge>
                    )}
                    {job.state.last_status === "error" && (
                      <Badge className="bg-red-500/20 text-red-400" title={job.state.last_error}>error</Badge>
                    )}
                    {!job.state.last_status && <span className="text-zinc-600">—</span>}
                  </td>
                  <td className="px-3 py-2 text-zinc-400">{job.state.runs ?? 0}</td>
                  <td className="px-3 py-2">
                    <button
                      onClick={() => toggleEnabled(job)}
                      disabled={busyId === job.id}
                      className={`px-2 py-0.5 rounded text-xs ${
                        job.enabled
                          ? "bg-green-500/20 text-green-400"
                          : "bg-zinc-700 text-zinc-400"
                      }`}
                    >
                      {job.enabled ? "on" : "off"}
                    </button>
                  </td>
                  <td className="px-3 py-2">
                    <div className="flex items-center gap-1 justify-end">
                      <Button variant="ghost" size="sm" className="h-6 w-6 p-0" onClick={() => openEdit(job)}>
                        <Pencil className="w-3 h-3" />
                      </Button>
                      <Button variant="ghost" size="sm" className="h-6 w-6 p-0" onClick={() => remove(job)} disabled={busyId === job.id}>
                        <Trash2 className="w-3 h-3" />
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      <CronOutboxPanel />
      <CronTargetsPanel />

      <CronJobDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        editingJob={editingJob}
        onSaved={load}
      />
    </div>
  );
}
```

- [ ] **Step 2: Type-check**

Run: `cd web && npx tsc --noEmit`
Expected: no errors. If `Badge`'s prop signature in `web/src/components/ui/badge.tsx` doesn't accept a raw `className` override or `title`, check that file and adjust to whatever prop it actually exposes.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/Cron/CronPanel.tsx
git commit -m "feat(web): add CronPanel scheduled-jobs tab"
```

---

## Task 9: Frontend — wire the tab into `TopTabs.tsx`, `App.tsx`, `SessionPage.tsx`

**Files:**
- Modify: `web/src/components/Layout/TopTabs.tsx`
- Modify: `web/src/App.tsx`
- Modify: `web/src/pages/SessionPage.tsx`

**Interfaces:**
- Consumes: `CronPanel` default export (Task 8).
- Produces: n/a — this is the terminal wiring task; nothing later depends on it.

- [ ] **Step 1: Add the tab entry to `TopTabs.tsx`**

In `web/src/components/Layout/TopTabs.tsx`, add `Clock` to the `lucide-react` import at line 1:

```ts
import { MessageSquare, FolderGit2, GitBranch, ScrollText, Paperclip, Activity, FileCode, X, Clock } from "lucide-react";
```

Add a `cron` entry to `mainTabs` (currently lines 14-21), placed after `git` and before `status` to match the design's stated tab order intent (Chat/Files/Git/Status/Logs/Assets → insert Cron after Git):

```ts
const mainTabs = [
  { id: "chat", label: "Chat", icon: MessageSquare },
  { id: "files", label: "Files", icon: FolderGit2 },
  { id: "git", label: "Git", icon: GitBranch },
  { id: "cron", label: "Cron", icon: Clock },
  { id: "status", label: "Status", icon: Activity },
  { id: "logs", label: "Logs", icon: ScrollText },
  { id: "assets", label: "Assets", icon: Paperclip },
];
```

- [ ] **Step 2: Render `CronPanel` in `App.tsx`**

In `web/src/App.tsx`, add the import after the `GitPanel` import (line 14):

```ts
import CronPanel from "./components/Cron/CronPanel";
```

Add the tab render after the `git` tab line (currently `{activeTab === "git" && <GitPanel onOpenFile={handleOpenFile} />}` around line 297):

```tsx
            {activeTab === "cron" && <CronPanel />}
```

- [ ] **Step 3: Render `CronPanel` in `SessionPage.tsx`**

In `web/src/pages/SessionPage.tsx`, add the import after the `GitPanel` import (line 17):

```ts
import CronPanel from "../components/Cron/CronPanel";
```

Add the tab render after the `git` tab line (currently `{activeTab === "git" && <GitPanel onOpenFile={handleOpenFile} />}` around line 423):

```tsx
          {activeTab === "cron" && <CronPanel />}
```

- [ ] **Step 4: Type-check**

Run: `cd web && npx tsc --noEmit`
Expected: no errors

- [ ] **Step 5: Production build**

Run: `cd web && npm run build`
Expected: build succeeds, `web/dist/` regenerated with no errors

- [ ] **Step 6: Manual smoke test**

Run the dev server: `cd web && npm run dev`, then run the ocode web server (`go run . serve` or equivalent per `README.md`/`AGENTS.md`), open the app in a browser, click the new **Cron** tab, and verify:
- Empty state renders ("No scheduled jobs.")
- "Add job" opens the dialog; create an "every 1 minute" test job and confirm it appears in the table with a correctly formatted schedule string
- Toggling "on"/"off" flips the badge and persists across a manual refresh
- Edit opens the dialog pre-filled, saving updates the row
- Delete removes the row
- Delivery history and routing targets sections expand/collapse and load without errors (empty states are fine if no deliveries/targets exist yet)

- [ ] **Step 7: Commit**

```bash
git add web/src/components/Layout/TopTabs.tsx web/src/App.tsx web/src/pages/SessionPage.tsx
git commit -m "feat(web): wire Cron tab into the app shell"
```

---

## Task 10: Final verification

**Files:** none (verification only)

- [ ] **Step 1: Full Go test suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 2: Full Go build**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 3: Full web build**

Run: `cd web && npm run build`
Expected: no errors

- [ ] **Step 4: Confirm desktop build still embeds the updated web assets**

Run: `make desktop` (or whatever target `Makefile` defines for `cmd/ocode-desktop` — check `Makefile` for the exact target name before running) if you want to confirm the Wails shell picks up the new tab; this is optional smoke coverage since desktop embeds the same `web/dist` output verified in Step 3, but flag to the user if the target name differs from `make desktop` so they can run the real one.

No commit for this task — it's a checkpoint, not a change.
