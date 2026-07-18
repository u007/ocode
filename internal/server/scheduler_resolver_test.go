package server

import (
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/scheduler"
)

func TestNewCronChatResolverHonorsRegisteredWorkdir(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	tg := scheduler.NewTargets(storePath)
	_ = tg.Set("/home/me/proj", 12345)
	_ = tg.Set("/home/me/other", 67890)

	resolve := NewCronChatResolver(tg, "/home/me/proj")

	cases := []struct {
		name   string
		owner  string
		wantID int64
		wantOK bool
	}{
		{"uses job owner when set", "/home/me/other", 67890, true},
		{"falls back to default workdir", "", 12345, true},
		{"unknown workdir returns no-deliver", "/somewhere/else", 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			j := &scheduler.Job{Payload: scheduler.Payload{Owner: c.owner}}
			id, ok := resolve(j)
			if id != c.wantID || ok != c.wantOK {
				t.Fatalf("got (%d,%v), want (%d,%v)", id, ok, c.wantID, c.wantOK)
			}
		})
	}
}

func TestSetTelegramCronSinkEndToEnd(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	svc := scheduler.NewService(storePath)
	// Seed a job whose Payload.Owner matches the registered workdir.
	id, _ := svc.AddJob(scheduler.Job{
		Name:     "ticker",
		Schedule: scheduler.Schedule{Kind: scheduler.KindEvery, EveryMs: 60000},
		Payload:  scheduler.Payload{Message: "ping", Owner: "/home/me/proj"},
	})
	if err := svc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(svc.Stop)

	tg := scheduler.NewTargets(storePath)
	_ = tg.Set("/home/me/proj", 99999)
	pusher := &capturePusher{}
	srv := &Server{scheduler: svc, handler: NewHandler()}
	srv.handler.scheduler = svc
	srv.SetTelegramCronSink(pusher, NewCronChatResolver(tg, ""))

	svc.Drainer.Sink(scheduler.Delivery{JobID: id, JobName: "ticker", Owner: "/home/me/proj", Result: "ok"})

	if len(pusher.calls) != 1 {
		t.Fatalf("want 1 pusher call, got %d", len(pusher.calls))
	}
	if pusher.calls[0].ChatID != 99999 {
		t.Fatalf("chat id: got %d want 99999", pusher.calls[0].ChatID)
	}
	if pusher.calls[0].Result != "ok" {
		t.Fatalf("result: got %q want %q", pusher.calls[0].Result, "ok")
	}
}
