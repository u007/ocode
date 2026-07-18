package scheduler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/u007/ocode/internal/paths"
)

// DefaultStorePath returns the per-project on-disk path for the scheduler
// store. Layout:
//
//	<GlobalDataDir>/scheduler/<project-slug>/jobs.json
//
// The slug is a stable identifier derived from the working directory so jobs
// authored in the TUI fire in the matching server/desktop process.
func DefaultStorePath(workDir string) (string, error) {
	root, err := paths.GlobalDataDir()
	if err != nil {
		return "", err
	}
	slug := slugify(workDir)
	if slug == "" {
		slug = "default"
	}
	return filepath.Join(root, "scheduler", slug, "jobs.json"), nil
}

// StartForHost is the standard wiring for a long-lived ocode host
// (server/desktop). It builds a scheduler service pointed at the project
// store, wires the host-supplied AgentRunner as the OnJob callback, and
// starts the loop.
//
// The host constructs the concrete AgentRunner (e.g. one that builds an
// agent, loads/seed the cron:<id> session, runs Step, and persists). This
// keeps the scheduler package free of agent/tool/session imports.
//
// The returned *Service also has a Drainer started on a background context;
// hosts can override the delivery sink via SetDrainerSink to push results
// to Telegram, RC, etc. The default sink is log-only.
func StartForHost(cfg any, workDir string, runner AgentRunner) (*Service, error) {
	if runner == nil {
		return nil, fmt.Errorf("scheduler: AgentRunner is required")
	}
	storePath, err := DefaultStorePath(workDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(storePath), 0o755); err != nil {
		return nil, err
	}
	svc := NewService(storePath)
	ob := NewOutbox(storePath)
	d := &Dispatcher{
		Runner: runner,
		Outbox: ob,
	}
	svc.SetOnJob(d.OnJob)
	// Attach a drainer so the outbox doesn't accumulate indefinitely.
	// Hosts can override the sink via svc.Drainer.Sink.
	svc.Drainer = NewDrainer(ob, nil)
	if err := svc.Start(); err != nil {
		return nil, err
	}
	svc.Drainer.Start(context.Background())
	return svc, nil
}

// slugify is a tiny, stable, filesystem-safe encoding of a directory path
// (shortened via hash to keep paths short). Mirrors the project-slug concept
// in internal/session.
func slugify(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	// Short, stable, file-safe. We deliberately avoid the project session
	// package to keep the scheduler package import graph minimal.
	clean := filepath.Clean(abs)
	// Use base + a 12-char hex of FNV-1a for stability across renames of
	// intermediate directories.
	const prime = 1099511628211
	var hash uint64 = 1469598103934665603
	for i := 0; i < len(clean); i++ {
		hash ^= uint64(clean[i])
		hash *= prime
	}
	return fmt.Sprintf("%s-%012x", filepath.Base(clean), hash&0xFFFFFFFFFFFF)
}
