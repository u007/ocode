package server

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/u007/ocode/internal/scheduler"
)

type capturePusher struct {
	mu    sync.Mutex
	calls []captureCall
}

type captureCall struct {
	ChatID    int64
	JobID     string
	JobName   string
	Owner     string
	Result    string
	ErrString string
}

func (c *capturePusher) PushCronResult(chatID int64, jobID, jobName, owner, result, errStr string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, captureCall{chatID, jobID, jobName, owner, result, errStr})
}

func TestSetTelegramCronSinkForwardsDeliveries(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	svc := scheduler.NewService(storePath)
	// Seed a job so SetTelegramCronSink's resolver can find it.
	_, _ = svc.AddJob(scheduler.Job{
		Name:     "ticker",
		Schedule: scheduler.Schedule{Kind: scheduler.KindEvery, EveryMs: 60000},
		Payload:  scheduler.Payload{Message: "ping", Owner: "me"},
	})
	if err := svc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(svc.Stop)

	srv := &Server{scheduler: svc, handler: NewHandler()}
	srv.handler.scheduler = svc
	pusher := &capturePusher{}
	var resolveCalls int
	srv.SetTelegramCronSink(pusher, func(_ *scheduler.Job) (int64, bool) {
		resolveCalls++
		return 12345, true
	})

	// After SetTelegramCronSink, the service's drainer is non-nil and its
	// Sink is wired. Drive a single delivery through the sink to verify the
	// end-to-end shape (resolver consulted, pusher invoked).
	if svc.Drainer == nil {
		t.Fatal("Drainer should be non-nil after SetTelegramCronSink")
	}
	if svc.Drainer.Sink == nil {
		t.Fatal("Drainer.Sink should be set")
	}
	jobID := svc.ListJobs()[0].ID
	svc.Drainer.Sink(scheduler.Delivery{JobID: jobID, JobName: "ticker", Owner: "me", Result: "hi"})
	if resolveCalls == 0 {
		t.Fatal("resolver not consulted")
	}
	if len(pusher.calls) == 0 {
		t.Fatal("pusher not called")
	}
	if pusher.calls[0].ChatID != 12345 || pusher.calls[0].Result != "hi" || pusher.calls[0].JobID != jobID {
		t.Fatalf("pusher call mismatch: %+v", pusher.calls[0])
	}
}
