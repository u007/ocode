package scheduler

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRunner struct {
	result string
	err    error
}

func (f *fakeRunner) RunScheduledJob(_ context.Context, _ *Job) (string, error) {
	return f.result, f.err
}

func TestDispatcherWritesOutboxOnSuccess(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	ob := NewOutbox(storePath)
	d := &Dispatcher{
		Runner: &fakeRunner{result: "hello"},
		Outbox: ob,
	}
	j := &Job{ID: "abc12", Name: "test", Payload: Payload{Owner: "me"}}
	if err := d.OnJob(context.Background(), j); err != nil {
		t.Fatalf("OnJob: %v", err)
	}
	data, err := os.ReadFile(ob.Path())
	if err != nil {
		t.Fatalf("read outbox: %v", err)
	}
	if !strings.Contains(string(data), `"hello"`) {
		t.Fatalf("expected result in outbox, got: %s", data)
	}
	var rec Delivery
	if err := json.Unmarshal(data[:strings.IndexByte(string(data), '\n')], &rec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rec.JobID != "abc12" || rec.Result != "hello" || rec.Owner != "me" {
		t.Fatalf("rec mismatch: %+v", rec)
	}
}

func TestDispatcherWritesOutboxOnError(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	ob := NewOutbox(storePath)
	d := &Dispatcher{
		Runner: &fakeRunner{err: errBoom},
		Outbox: ob,
	}
	j := &Job{ID: "errid", Name: "x"}
	if err := d.OnJob(context.Background(), j); err == nil {
		t.Fatal("expected error to bubble up")
	}
	entries, _ := ob.Peek()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Error == "" {
		t.Fatalf("expected error recorded, got %+v", entries[0])
	}
}

var errBoom = boomErr("boom")

type boomErr string

func (b boomErr) Error() string { return string(b) }
