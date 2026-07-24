package scheduler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// OnJobFunc is invoked when a job becomes due. It runs the actual work (e.g. an
// agent turn) and returns an error to be recorded on the job's state. The
// scheduler recovers panics from this callback so one bad job can't kill the
// host loop.
type OnJobFunc func(ctx context.Context, job *Job) error

// Service is the scheduler engine. It is safe for concurrent use: all mutations
// of the in-memory job set and the on-disk store are guarded by mu.
type Service struct {
	storePath string
	onJob     OnJobFunc
	maxJobs   int

	mu       sync.Mutex
	jobs     []Job
	started  bool
	loadedAt time.Time
	now      func() time.Time

	stopCh chan struct{}
	wakeCh chan struct{}
	wg     sync.WaitGroup

	// Drainer is the outbox drainer. Optional; nil = no outbox draining.
	// Set via SetDrainerSink or by callers that wire one manually.
	Drainer *Drainer
}

// NewService creates a scheduler that persists jobs to storePath. The OnJob
// callback must be set (via SetOnJob) before Start.
func NewService(storePath string) *Service {
	return &Service{
		storePath: storePath,
		maxJobs:   50, // Claude Code caps a session at 50 scheduled tasks
		now:       time.Now,
	}
}

// SetOnJob sets the callback fired when a job is due. Must be called before
// Start.
func (s *Service) SetOnJob(fn OnJobFunc) { s.onJob = fn }

// SetMaxJobs overrides the per-project job cap (default 50).
func (s *Service) SetMaxJobs(n int) {
	if n > 0 {
		s.maxJobs = n
	}
}

// SetDrainerSink replaces the Drainer's per-entry callback. Useful for
// hosts that want to forward deliveries to Telegram, RC, etc. If no
// Drainer is attached yet, a default one is created (with a nil Sink that
// logs only) so the new sink takes effect immediately.
func (s *Service) SetDrainerSink(sink func(Delivery)) {
	if s.Drainer == nil {
		storePath := s.storePath
		s.Drainer = NewDrainer(NewOutbox(storePath), nil)
	}
	s.Drainer.Sink = sink
}

// SetClock overrides the time source (used by tests).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// Start loads the store and begins the scheduling loop. It is a no-op if
// already started.
func (s *Service) Start() error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil
	}
	if err := s.load(); err != nil {
		s.mu.Unlock()
		return err
	}
	s.started = true
	s.stopCh = make(chan struct{})
	s.wakeCh = make(chan struct{}, 1)
	s.wg.Add(1)
	s.mu.Unlock()
	go s.run()
	return nil
}

// Stop terminates the scheduling loop and waits for it to exit. It also
// stops the attached Drainer if one is set.
func (s *Service) Stop() {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		s.stopDrainer()
		return
	}
	s.started = false
	close(s.stopCh)
	s.mu.Unlock()
	s.wg.Wait()
	s.stopDrainer()
}

func (s *Service) stopDrainer() {
	if s.Drainer != nil {
		s.Drainer.Stop()
	}
}

func (s *Service) run() {
	defer s.wg.Done()
	safety := time.NewTicker(maxPollLeg)
	defer safety.Stop()
	for {
		s.mu.Lock()
		delay := s.nextFireDelayLocked()
		s.mu.Unlock()

		var timer *time.Timer
		if delay > 0 {
			timer = time.NewTimer(delay)
		}
		select {
		case <-s.stopCh:
			if timer != nil {
				timer.Stop()
			}
			return
		case <-s.wakeCh:
			if timer != nil {
				timer.Stop()
			}
			// Recompute soonest due job.
		case <-timer.C:
			s.tick()
		case <-safety.C:
			// Drift guard: re-check the wall clock at least every maxPollLeg.
			s.tick()
		}
	}
}

// nextFireDelayLocked returns how long until the soonest due job, or a long idle
// duration when there are no enabled jobs. Caller must hold mu.
func (s *Service) nextFireDelayLocked() time.Duration {
	now := s.now().UnixMilli()
	soonest := int64(0)
	for i := range s.jobs {
		j := &s.jobs[i]
		if !j.Enabled || j.State.NextRunAtMs == 0 {
			continue
		}
		if soonest == 0 || j.State.NextRunAtMs < soonest {
			soonest = j.State.NextRunAtMs
		}
	}
	if soonest == 0 {
		return 24 * time.Hour // idle; woken by AddJob/RemoveJob
	}
	d := time.Duration(soonest-now) * time.Millisecond
	if d < 0 {
		return 0
	}
	return d
}

// tick finds due jobs (picking up any external edits first), fires them, and
// reschedules.
func (s *Service) tick() {
	s.syncFromDisk()
	now := s.now().UnixMilli()

	s.mu.Lock()
	var due []int // indices into s.jobs
	for i := range s.jobs {
		j := &s.jobs[i]
		if j.Enabled && j.State.NextRunAtMs > 0 && j.State.NextRunAtMs <= now {
			due = append(due, i)
		}
	}
	s.mu.Unlock()

	for _, idx := range due {
		s.mu.Lock()
		// Re-check: a concurrent removal could have shrunk the slice.
		if idx >= len(s.jobs) {
			s.mu.Unlock()
			continue
		}
		job := &s.jobs[idx] // pointer to the real element so reschedule persists
		s.mu.Unlock()
		s.executeJob(job)
	}
}

// syncFromDisk reloads the job list from disk if another process (e.g. the TUI)
// modified the store since we last loaded it. In-memory runtime state is
// preserved for jobs that still exist; newly added jobs get a computed next
// run; removed jobs are dropped.
func (s *Service) syncFromDisk() {
	s.mu.Lock()
	defer s.mu.Unlock()
	info, err := os.Stat(s.storePath)
	if err != nil || !info.ModTime().After(s.loadedAt) {
		return
	}
	store, err := readStore(s.storePath)
	if err != nil {
		return
	}
	byID := make(map[string]int, len(s.jobs))
	for i := range s.jobs {
		byID[s.jobs[i].ID] = i
	}
	merged := make([]Job, 0, len(store.Jobs))
	for _, j := range store.Jobs {
		if i, ok := byID[j.ID]; ok {
			existing := s.jobs[i]
			// Adopt authored fields; keep runtime state from memory.
			existing.Name = j.Name
			existing.Schedule = j.Schedule
			existing.Payload = j.Payload
			existing.Enabled = j.Enabled
			if existing.State.NextRunAtMs == 0 {
				existing.State.NextRunAtMs = s.mustNextRun(&existing)
			}
			merged = append(merged, existing)
		} else {
			j.State.NextRunAtMs = s.mustNextRun(&j)
			merged = append(merged, j)
		}
	}
	s.jobs = merged
	s.loadedAt = info.ModTime()
}

// executeJob runs the onJob callback for a single due job, records the outcome,
// and reschedules or deletes the job.
func (s *Service) executeJob(j *Job) {
	defer func() {
		if r := recover(); r != nil {
			s.mu.Lock()
			j.State.LastStatus = "error"
			j.State.LastError = fmt.Sprintf("panic: %v", r)
			s.mu.Unlock()
			_ = s.persist()
		}
	}()

	now := s.now()
	s.mu.Lock()
	j.State.LastRunAtMs = now.UnixMilli()
	j.State.Runs++
	cb := s.onJob
	s.mu.Unlock()

	var runErr error
	if cb != nil {
		runErr = cb(context.Background(), j)
	}

	s.mu.Lock()
	if runErr != nil {
		j.State.LastStatus = "error"
		j.State.LastError = runErr.Error()
	} else {
		j.State.LastStatus = "ok"
		j.State.LastError = ""
	}
	switch j.Schedule.Kind {
	case KindAt:
		s.removeJobLocked(j.ID)
	case KindEvery:
		if j.CreatedAtMs > 0 && now.UnixMilli()-j.CreatedAtMs > sevenDaysMs {
			s.removeJobLocked(j.ID)
		} else {
			j.State.NextRunAtMs = s.mustNextRun(j)
		}
	case KindCron:
		j.State.NextRunAtMs = s.mustNextRun(j)
	}
	s.mu.Unlock()
	_ = s.persist()
}

// --- Public mutation API ---

// AddJob validates and persists a new job. The Schedule and Payload are
// required; Name is optional (a default is derived). It returns the assigned
// 8-char id.
func (s *Service) AddJob(j Job) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.jobs) >= s.maxJobs {
		return "", fmt.Errorf("job limit (%d) reached", s.maxJobs)
	}
	j.ID = s.genIDLocked()
	if j.Name == "" {
		j.Name = j.Payload.Message
		if len(j.Name) > 40 {
			j.Name = j.Name[:37] + "..."
		}
	}
	if err := validateSchedule(j.Schedule); err != nil {
		return "", err
	}
	now := s.now()
	j.CreatedAtMs = now.UnixMilli()
	j.Enabled = true
	nr, err := s.computeNextRun(&j, now)
	if err != nil {
		return "", err
	}
	j.State.NextRunAtMs = nr
	s.jobs = append(s.jobs, j)
	if err := s.persistLocked(); err != nil {
		return "", err
	}
	s.wake()
	return j.ID, nil
}

// RemoveJob deletes a job by id.
func (s *Service) RemoveJob(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.removeJobLocked(id) {
		return fmt.Errorf("job %s not found", id)
	}
	if err := s.persistLocked(); err != nil {
		return err
	}
	s.wake()
	return nil
}

// JobPatch is a partial update for UpdateJob. Only non-nil fields are applied.
// Schedule, when provided, replaces the entire Schedule and recomputes
// State.NextRunAtMs using the same validation path as AddJob.
type JobPatch struct {
	Enabled  *bool
	Name     *string
	Schedule *Schedule
	Payload  *Payload
}

// UpdateJob updates the job identified by id and returns the updated job.
// It leaves the job unmodified on error.
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
	if patch.Payload != nil && patch.Payload.Message == "" {
		return Job{}, fmt.Errorf("message is required")
	}

	j := &s.jobs[idx]
	prevEnabled := j.Enabled
	if patch.Enabled != nil {
		j.Enabled = *patch.Enabled
	}
	if patch.Name != nil {
		j.Name = *patch.Name
	}
	if patch.Schedule != nil {
		j.Schedule = *patch.Schedule
	}
	if patch.Payload != nil {
		j.Payload = *patch.Payload
	}
	if j.Name == "" {
		j.Name = j.Payload.Message
		if len(j.Name) > 40 {
			j.Name = j.Name[:37] + "..."
		}
	}
	if patch.Schedule != nil || (patch.Enabled != nil && *patch.Enabled && !prevEnabled) {
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

// removeJobLocked removes a job from the slice. Caller must hold mu.
func (s *Service) removeJobLocked(id string) bool {
	for i := range s.jobs {
		if s.jobs[i].ID == id {
			s.jobs = append(s.jobs[:i], s.jobs[i+1:]...)
			return true
		}
	}
	return false
}

// ListJobs returns a copy of all jobs.
func (s *Service) ListJobs() []Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Job, len(s.jobs))
	copy(out, s.jobs)
	return out
}

// GetJob returns a copy of a single job, or nil if not found.
func (s *Service) GetJob(id string) *Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.jobs {
		if s.jobs[i].ID == id {
			c := s.jobs[i]
			return &c
		}
	}
	return nil
}

// --- Scheduling math ---

// computeNextRun returns the epoch-ms of the next fire for a job, relative to
// `from`. For recurring schedules a small random jitter is added so many jobs
// don't all fire on the same wall second.
func (s *Service) computeNextRun(j *Job, from time.Time) (int64, error) {
	switch j.Schedule.Kind {
	case KindAt:
		return j.Schedule.AtMs, nil
	case KindEvery:
		if j.Schedule.EveryMs <= 0 {
			return 0, fmt.Errorf("every_ms must be > 0")
		}
		return from.UnixMilli() + j.Schedule.EveryMs, nil
	case KindCron:
		loc := time.Local
		if j.Schedule.TZ != "" {
			l, err := time.LoadLocation(j.Schedule.TZ)
			if err != nil {
				return 0, fmt.Errorf("invalid tz %q: %w", j.Schedule.TZ, err)
			}
			loc = l
		}
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
		sched, err := parser.Parse(j.Schedule.Expr)
		if err != nil {
			return 0, fmt.Errorf("invalid cron expr %q: %w", j.Schedule.Expr, err)
		}
		next := sched.Next(from.In(loc))
		n, err := rand.Int(rand.Reader, big.NewInt(maxJitterMs))
		if err != nil {
			return 0, fmt.Errorf("jitter RNG failed: %w", err)
		}
		return next.UnixMilli() + n.Int64(), nil
	default:
		return 0, fmt.Errorf("unknown schedule kind %q", j.Schedule.Kind)
	}
}

// mustNextRun computes the next run, falling back to now+1h on error so a bad
// schedule can't wedge the loop.
func (s *Service) mustNextRun(j *Job) int64 {
	nr, err := s.computeNextRun(j, s.now())
	if err != nil {
		return s.now().Add(time.Hour).UnixMilli()
	}
	return nr
}

func validateSchedule(sc Schedule) error {
	switch sc.Kind {
	case KindAt:
		if sc.AtMs <= 0 {
			return fmt.Errorf("at schedule requires at_ms > 0")
		}
	case KindEvery:
		if sc.EveryMs <= 0 {
			return fmt.Errorf("every schedule requires every_ms > 0")
		}
	case KindCron:
		if sc.Expr == "" {
			return fmt.Errorf("cron schedule requires expr")
		}
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
		if _, err := parser.Parse(sc.Expr); err != nil {
			return fmt.Errorf("invalid cron expr %q: %w", sc.Expr, err)
		}
	default:
		return fmt.Errorf("unknown schedule kind %q", sc.Kind)
	}
	return nil
}

// --- Persistence ---

func (s *Service) load() error {
	store, err := readStore(s.storePath)
	if err != nil {
		return err
	}
	now := s.now()
	for i := range store.Jobs {
		j := &store.Jobs[i]
		// Preserve a still-future next-run (set before a restart); recompute
		// past/stale ones so nothing is silently skipped.
		if j.State.NextRunAtMs == 0 || j.State.NextRunAtMs <= now.UnixMilli() {
			nr, err := s.computeNextRun(j, now)
			if err != nil {
				return fmt.Errorf("job %s: %w", j.ID, err)
			}
			j.State.NextRunAtMs = nr
		}
	}
	s.jobs = store.Jobs
	if info, err := os.Stat(s.storePath); err == nil {
		s.loadedAt = info.ModTime()
	} else {
		s.loadedAt = now
	}
	return nil
}

func readStore(path string) (Store, error) {
	var store Store
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Store{Version: 1, Jobs: []Job{}}, nil
		}
		return store, err
	}
	if err := json.Unmarshal(data, &store); err != nil {
		return store, fmt.Errorf("corrupt store %s: %w", path, err)
	}
	if store.Jobs == nil {
		store.Jobs = []Job{}
	}
	return store, nil
}

// persist writes the in-memory store to disk atomically (temp file + rename)
// and records the new mtime so syncFromDisk won't reload our own write.
func (s *Service) persist() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persistLocked()
}

func (s *Service) persistLocked() error {
	store := Store{Version: 1, Jobs: s.jobs}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.storePath), 0o755); err != nil {
		return err
	}
	tmp := s.storePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.storePath); err != nil {
		return err
	}
	if info, err := os.Stat(s.storePath); err == nil {
		s.loadedAt = info.ModTime()
	}
	return nil
}

// wake non-blockingly signals the run loop to recompute the soonest due job
// (used after AddJob/RemoveJob).
func (s *Service) wake() {
	select {
	case s.wakeCh <- struct{}{}:
	default:
	}
}

// genIDLocked returns a new unique 8-char hex id. Caller must hold mu.
func (s *Service) genIDLocked() string {
	seen := make(map[string]bool, len(s.jobs))
	for i := range s.jobs {
		seen[s.jobs[i].ID] = true
	}
	for {
		b := make([]byte, 4)
		if _, err := rand.Read(b); err != nil {
			// Fallback to time-based id if the system RNG is unavailable.
			return fmt.Sprintf("%08x", time.Now().Nanosecond())
		}
		id := hex.EncodeToString(b)[:idLen]
		if !seen[id] {
			return id
		}
	}
}
