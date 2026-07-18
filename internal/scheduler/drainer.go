package scheduler

import (
	"context"
	"log"
	"sync"
	"time"
)

// Drainer polls an Outbox and hands each entry to a host-supplied callback.
// It is the bridge between the scheduler's delivery log and external sinks
// (Telegram bot, RC client, web push notification, etc.). The host plugs
// the sink in by setting DrainSink; if no sink is set, Drainer falls back
// to logging each entry at info level so operators can still see that
// jobs ran.
type Drainer struct {
	Outbox *Outbox
	Sink   func(Delivery) // optional; nil = log only
	Period time.Duration  // default 10s

	stop  chan struct{}
	done  chan struct{}
	once  sync.Once
	start sync.Once
}

// NewDrainer returns a Drainer wired to the given outbox. Period defaults
// to 10s when zero.
func NewDrainer(o *Outbox, sink func(Delivery)) *Drainer {
	return &Drainer{Outbox: o, Sink: sink}
}

// Start launches the drain goroutine. Safe to call multiple times; the
// second call is a no-op.
func (d *Drainer) Start(ctx context.Context) {
	d.start.Do(func() {
		if d.Period <= 0 {
			d.Period = 10 * time.Second
		}
		d.stop = make(chan struct{})
		d.done = make(chan struct{})
		go d.loop(ctx)
	})
}

// Stop signals the drain loop to exit and waits for it. Safe to call
// multiple times. Callers should pass a context that is cancelled at
// process shutdown so the loop also exits when the host is gone.
func (d *Drainer) Stop() {
	if d == nil {
		return
	}
	d.once.Do(func() {
		if d.stop == nil {
			return
		}
		close(d.stop)
	})
	if d.done != nil {
		<-d.done
	}
}

func (d *Drainer) loop(ctx context.Context) {
	defer close(d.done)
	t := time.NewTicker(d.Period)
	defer t.Stop()
	// One immediate tick so freshly-scheduled jobs don't wait `Period`
	// before their first drain.
	d.tick()
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stop:
			return
		case <-t.C:
			d.tick()
		}
	}
}

func (d *Drainer) tick() {
	if d.Outbox == nil {
		return
	}
	entries, err := d.Outbox.Drain()
	if err != nil {
		log.Printf("scheduler: drain outbox: %v", err)
		return
	}
	for _, e := range entries {
		if d.Sink == nil {
			log.Printf("scheduler: delivered job=%s owner=%s result=%q err=%q",
				e.JobID, e.Owner, truncate(e.Result, 200), e.Error)
			continue
		}
		// Sink errors must not loop back into the outbox; log and drop.
		// (The job itself has already run; we cannot re-run it just
		// because the sink failed. Persistent sinks must use their own
		// retry log.)
		func(del Delivery) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("scheduler: drain sink panic: %v (job=%s)", r, del.JobID)
				}
			}()
			d.Sink(del)
		}(e)
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
