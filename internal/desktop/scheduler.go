package desktop

import (
	"context"
	"log"
	"os"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/scheduler"
	"github.com/u007/ocode/internal/server"
)

// serverSchedulerRunner is the desktop's concrete implementation of
// scheduler.AgentRunner. It re-uses the server-package runner by going
// through the same exported entry point that cmd/ocode uses, so all the
// agent/tool/session plumbing lives in one place.
type serverSchedulerRunner struct {
	cfg *config.Config
}

// RunScheduledJob satisfies scheduler.AgentRunner.
func (r *serverSchedulerRunner) RunScheduledJob(ctx context.Context, j *scheduler.Job) (string, error) {
	return server.RunScheduledJob(ctx, r.cfg, j)
}

// AttachScheduler wires a scheduler.Service into a desktop-hosted *server.Server
// using the same store layout as `ocode serve` (per-project, under
// GlobalDataDir). Errors are logged but non-fatal: the webview must still
// open even if the scheduler fails to start.
//
// This is the desktop counterpart of the schedulerSetup() hook in
// cmd/ocode/main.go's serve/web paths. The TUI does not host a scheduler;
// only long-lived processes (serve/web/desktop) do.
func AttachScheduler(srv *server.Server, workDir string) {
	if srv == nil {
		return
	}
	wd := workDir
	if wd == "" {
		if cwd, err := os.Getwd(); err == nil {
			wd = cwd
		} else {
			wd = "."
		}
	}
	cfg, err := config.Load()
	if err != nil {
		log.Printf("ocode-desktop: scheduler disabled (config load: %v)", err)
		return
	}
	runner := &serverSchedulerRunner{cfg: cfg}
	svc, err := scheduler.StartForHost(cfg, wd, runner)
	if err != nil {
		log.Printf("ocode-desktop: scheduler disabled (start: %v)", err)
		return
	}
	srv.SetScheduler(svc)
	if store, derr := scheduler.DefaultStorePath(wd); derr == nil {
		log.Printf("ocode-desktop: scheduler attached (store: %s)", store)
	}
}
