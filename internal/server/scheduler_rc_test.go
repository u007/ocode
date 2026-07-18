package server

import (
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/scheduler"
)

func TestRCBridgePusherPushesDelivery(t *testing.T) {
	ch := make(chan CronDelivery, 8)
	bridge := &RCBridge{CronDeliveryCh: ch}
	pusher := RCBridgePusher{Bridge: bridge}
	pusher.PushCronResult(0, "job-1", "ticker", "me", "hi", "")
	select {
	case d := <-ch:
		if d.JobID != "job-1" || d.Result != "hi" {
			t.Fatalf("delivery mismatch: %+v", d)
		}
	default:
		t.Fatal("expected delivery on channel")
	}
}

func TestRCBridgePusherNoOpWhenBridgeNil(t *testing.T) {
	pusher := RCBridgePusher{}
	// Should not panic.
	pusher.PushCronResult(0, "x", "y", "z", "w", "")
}

func TestRCBridgePushCronDeliveryDropsOldestWhenFull(t *testing.T) {
	ch := make(chan CronDelivery, 2)
	bridge := &RCBridge{CronDeliveryCh: ch}
	bridge.PushCronDelivery(CronDelivery{JobID: "first"})
	bridge.PushCronDelivery(CronDelivery{JobID: "second"})
	bridge.PushCronDelivery(CronDelivery{JobID: "third"})
	// Buffer was 2; we expect "first" to have been dropped, and the two
	// most recent (second, third) to remain.
	var got []string
	for i := 0; i < 2; i++ {
		select {
		case d := <-ch:
			got = append(got, d.JobID)
		default:
		}
	}
	if len(got) != 2 || got[0] != "second" || got[1] != "third" {
		t.Fatalf("expected [second third], got %v", got)
	}
}

func TestSetTelegramCronSinkFansOutToRCBridge(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	svc := scheduler.NewService(storePath)
	if err := svc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(svc.Stop)
	// Seed a job so the resolver path doesn't bail.
	id, _ := svc.AddJob(scheduler.Job{
		Name:     "ticker",
		Schedule: scheduler.Schedule{Kind: scheduler.KindEvery, EveryMs: 60000},
		Payload:  scheduler.Payload{Message: "ping", Owner: "/me/proj"},
	})

	// Build a server with an active RC bridge.
	ch := make(chan CronDelivery, 8)
	h := NewHandler()
	h.scheduler = svc
	h.rc = &RCBridge{CronDeliveryCh: ch}
	srv := &Server{scheduler: svc, handler: h}

	pusher := &capturePusher{}
	srv.SetTelegramCronSink(pusher, func(_ *scheduler.Job) (int64, bool) {
		return 0, false // log-only — no Telegram call
	})

	// Drive a delivery through the sink.
	svc.Drainer.Sink(scheduler.Delivery{JobID: id, JobName: "ticker", Owner: "/me/proj", Result: "ok"})

	// TUI bridge should have received the delivery.
	select {
	case d := <-ch:
		if d.JobID != id || d.Result != "ok" {
			t.Fatalf("bridge delivery mismatch: %+v", d)
		}
	default:
		t.Fatal("TUI bridge did not receive the delivery")
	}
	// Telegram pusher should NOT have been called (resolver returned no-deliver).
	if len(pusher.calls) != 0 {
		t.Fatalf("Telegram should not have been called, got %+v", pusher.calls)
	}
}
