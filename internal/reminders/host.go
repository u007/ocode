package reminders

import (
	"fmt"
	"os"
	"path/filepath"
)

// StartForHost is the standard wiring for a long-lived ocode host
// (serve/web/desktop). It builds a Service rooted at the project's
// reminders.json, loads it, and starts the run loop.
//
// The host supplies the AgentRunner (which the host, not this package,
// implements over the agent/tool/session plumbing) and may supply a Notifier.
// Delivery is wired to the SHARED scheduler outbox and run history by the
// caller, not here: which Outbox instance the process uses is the host's
// decision, and passing the cron service's own instance is exactly what makes
// one drainer fan out both kinds of result.
//
// Errors are returned so the host can log and carry on without reminders; a
// broken reminder list must never stop the server from booting.
func StartForHost(workDir string, runner AgentRunner, notifier Notifier) (*Service, error) {
	if runner == nil {
		return nil, fmt.Errorf("reminders: AgentRunner is required")
	}
	storePath, err := DefaultStorePath(workDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(storePath), 0o755); err != nil {
		return nil, err
	}
	svc := NewService(storePath)
	svc.workDir = workDir
	svc.SetAgentRunner(runner)
	svc.SetNotifier(notifier)
	if err := svc.Start(); err != nil {
		return nil, err
	}
	return svc, nil
}

// WorkDir returns the project root this service was started for ("" when it
// was constructed directly, e.g. in tests). It is the default Owner recorded
// on a delivery, which is the same workdir hint a cron job's Payload.Owner
// carries, so the existing Telegram/RC resolver can route a reminder exactly
// as it routes a job.
func (s *Service) WorkDir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.workDir
}

// defaultOwner returns the item's own Owner, falling back to the service's
// workDir. Centralised so Add, Update and the firing path all agree.
func (s *Service) defaultOwner(owner string) string {
	if owner != "" {
		return owner
	}
	return s.workDir
}
