package server

import (
	"github.com/u007/ocode/internal/browsertabs"
)

// stripPublisher is the browserStrip the agent's browser tool writes through. A
// write that lands publishes browser_tabs_changed, the same event a client PUT
// publishes, so the Browser panel drops a tab the agent closed.
type stripPublisher struct {
	store *browsertabs.Store
	bus   *EventBus
}

func (p stripPublisher) All() map[string]browsertabs.ProjectBrowsers {
	return p.store.All()
}

func (p stripPublisher) ApplyBulk(patch map[string]browsertabs.ProjectBrowsers) error {
	if err := p.store.ApplyBulk(patch); err != nil {
		return err
	}
	p.bus.Publish("browser_tabs_changed", "", "", nil)
	return nil
}
