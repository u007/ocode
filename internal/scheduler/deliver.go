package scheduler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Delivery is one record written to the outbox when a job's result is
// emitted. It is intentionally small and JSONL so external sinks (Telegram
// bot, RC client, web poll endpoint) can stream it without holding the
// scheduler package's import graph.
type Delivery struct {
	JobID       string    `json:"job_id"`
	JobName     string    `json:"job_name"`
	Owner       string    `json:"owner"`
	DeliveredTo string    `json:"delivered_to,omitempty"` // optional hint set on the job
	Result      string    `json:"result"`
	Error       string    `json:"error,omitempty"`
	At          time.Time `json:"at"`
}

// Outbox appends deliveries to a JSONL file under the scheduler's store
// directory. Safe to call from multiple goroutines. Missing parent dirs are
// created on first write. Errors are returned to the caller so the
// Dispatcher can log them; the scheduler itself never panics on a write
// failure (the job already ran).
type Outbox struct {
	mu  sync.Mutex
	dir string // directory; OutboxFile is <dir>/deliveries.jsonl
}

// NewOutbox returns an outbox rooted next to the given storePath (so
// outbox lives in the same project dir as the scheduler's jobs.json).
func NewOutbox(storePath string) *Outbox {
	return &Outbox{dir: filepath.Dir(storePath)}
}

// Path returns the absolute path of the JSONL file the outbox writes to.
func (o *Outbox) Path() string { return filepath.Join(o.dir, "deliveries.jsonl") }

// Append writes one delivery as a single JSONL line. Creates the file (and
// parent dir) on first call.
func (o *Outbox) Append(d Delivery) error {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := os.MkdirAll(o.dir, 0o755); err != nil {
		return err
	}
	if d.At.IsZero() {
		d.At = time.Now().UTC()
	}
	line, err := json.Marshal(d)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(o.Path(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return nil
}

// Drain returns the contents of the JSONL file and, on success, truncates it.
// Intended for one-shot delivery (e.g. the Telegram bot fetching pending
// results). Returns an empty slice when the file does not exist.
func (o *Outbox) Drain() ([]Delivery, error) {
	if o == nil {
		return nil, nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	data, err := os.ReadFile(o.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Delivery
	for _, line := range splitJSONL(data) {
		if len(line) == 0 {
			continue
		}
		var d Delivery
		if err := json.Unmarshal(line, &d); err != nil {
			return nil, fmt.Errorf("outbox: corrupt line: %w", err)
		}
		out = append(out, d)
	}
	// Truncate after a successful read.
	if err := os.WriteFile(o.Path(), nil, 0o644); err != nil {
		return out, err
	}
	return out, nil
}

// Peek returns the contents of the JSONL file without truncating.
func (o *Outbox) Peek() ([]Delivery, error) {
	if o == nil {
		return nil, nil
	}
	data, err := os.ReadFile(o.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Delivery
	for _, line := range splitJSONL(data) {
		if len(line) == 0 {
			continue
		}
		var d Delivery
		if err := json.Unmarshal(line, &d); err != nil {
			return nil, fmt.Errorf("outbox: corrupt line: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

// splitJSONL splits on '\n' without allocating (data is small, infrequent).
func splitJSONL(data []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			out = append(out, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		out = append(out, data[start:])
	}
	return out
}

// OutboxFor returns an outbox for the given project workdir, computed the
// same way as DefaultStorePath.
func OutboxFor(workDir string) (*Outbox, error) {
	p, err := DefaultStorePath(workDir)
	if err != nil {
		return nil, err
	}
	return NewOutbox(p), nil
}
