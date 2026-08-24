package scheduler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// RunLogEntry is a single datetime-stamped log line for a run.
type RunLogEntry struct {
	At      time.Time `json:"at"`
	Level   string    `json:"level,omitempty"` // info | error | warn
	Message string    `json:"message"`
}

// RunRecord is one execution of a scheduled job. Persisted as JSONL next to
// jobs.json and deliveries.jsonl.
type RunRecord struct {
	ID         string        `json:"id"` // 8-char run id
	JobID      string        `json:"job_id"`
	JobName    string        `json:"job_name"`
	StartedAt  time.Time     `json:"started_at"`
	FinishedAt time.Time     `json:"finished_at"`
	DurationMs int64         `json:"duration_ms"`
	Status     string        `json:"status"` // ok | error
	Input      string        `json:"input"`  // job payload message at time of run
	Output     string        `json:"output,omitempty"`
	Error      string        `json:"error,omitempty"`
	Logs       []RunLogEntry `json:"logs"`
}

// RunHistory appends run records to a JSONL file under the scheduler's store
// directory. Safe for concurrent appends and reads.
type RunHistory struct {
	mu  sync.Mutex
	dir string
}

// NewRunHistory returns a history rooted next to the given storePath (so
// runs live in the same project dir as jobs.json).
func NewRunHistory(storePath string) *RunHistory {
	return &RunHistory{dir: filepath.Dir(storePath)}
}

// Path returns the absolute path of the JSONL file.
func (rh *RunHistory) Path() string { return filepath.Join(rh.dir, "runs.jsonl") }

// Append writes one run as a JSONL line. Creates parent dirs on first call.
func (rh *RunHistory) Append(rec RunRecord) error {
	if rh == nil {
		return nil
	}
	if rec.ID == "" {
		rec.ID = genRunID()
	}
	if rec.StartedAt.IsZero() {
		rec.StartedAt = time.Now().UTC()
	}
	if rec.FinishedAt.IsZero() {
		rec.FinishedAt = time.Now().UTC()
	}
	if rec.DurationMs == 0 && !rec.StartedAt.IsZero() && !rec.FinishedAt.IsZero() {
		rec.DurationMs = rec.FinishedAt.Sub(rec.StartedAt).Milliseconds()
	}
	rh.mu.Lock()
	defer rh.mu.Unlock()
	if err := os.MkdirAll(rh.dir, 0o755); err != nil {
		return err
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(rh.Path(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return nil
}

// List returns runs for the given jobID, sorted newest-first. If jobID is
// empty all runs are returned. Pagination is applied after sorting. Returns
// total count (pre-pagination) and the paged slice.
func (rh *RunHistory) List(jobID string, limit, offset int) ([]RunRecord, int, error) {
	if rh == nil {
		return []RunRecord{}, 0, nil
	}
	rh.mu.Lock()
	defer rh.mu.Unlock()
	data, err := os.ReadFile(rh.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return []RunRecord{}, 0, nil
		}
		return nil, 0, err
	}
	var all []RunRecord
	for _, line := range splitJSONL(data) {
		if len(line) == 0 {
			continue
		}
		var r RunRecord
		if err := json.Unmarshal(line, &r); err != nil {
			return nil, 0, fmt.Errorf("runhistory: corrupt line: %w", err)
		}
		if jobID != "" && r.JobID != jobID {
			continue
		}
		all = append(all, r)
	}
	// Newest first (descending started_at).
	sort.Slice(all, func(i, j int) bool {
		return all[i].StartedAt.After(all[j].StartedAt)
	})
	total := len(all)
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		return []RunRecord{}, total, nil
	}
	all = all[offset:]
	if limit > 0 && limit < len(all) {
		all = all[:limit]
	}
	if all == nil {
		all = []RunRecord{}
	}
	return all, total, nil
}

// Get returns a single run by its ID (and jobID for scoping). Returns
// nil, nil when not found.
func (rh *RunHistory) Get(jobID, runID string) (*RunRecord, error) {
	if rh == nil {
		return nil, nil
	}
	rh.mu.Lock()
	defer rh.mu.Unlock()
	data, err := os.ReadFile(rh.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	for _, line := range splitJSONL(data) {
		if len(line) == 0 {
			continue
		}
		var r RunRecord
		if err := json.Unmarshal(line, &r); err != nil {
			return nil, fmt.Errorf("runhistory: corrupt line: %w", err)
		}
		if r.ID == runID && (jobID == "" || r.JobID == jobID) {
			cpy := r
			return &cpy, nil
		}
	}
	return nil, nil
}

// RunHistoryFor returns a RunHistory for the given project workDir.
func RunHistoryFor(workDir string) (*RunHistory, error) {
	p, err := DefaultStorePath(workDir)
	if err != nil {
		return nil, err
	}
	return NewRunHistory(p), nil
}

func genRunID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%08x", time.Now().UnixNano()&0xFFFFFFFF)
	}
	return hex.EncodeToString(b[:])
}
