package server

import (
	"context"

	"github.com/u007/ocode/internal/browse"
	"github.com/u007/ocode/internal/browse/cdp"
)

// browseAgentSurfaces adapts the browse server's agent accessors to the tool's
// browserSurfaces. It is the only place the browser tool reaches the Chrome
// manager.
type browseAgentSurfaces struct {
	bs *browse.Server
}

func (a browseAgentSurfaces) Surface(ctx context.Context, stateKey, url string) (browserSurface, string, error) {
	t, note, err := a.bs.AgentTarget(ctx, stateKey, url)
	if err != nil {
		return nil, "", err
	}
	return cdpAgentSurface{Target: t}, note, nil
}

// cdpAgentSurface is a Chrome target as the tool reads it. The target's console and
// network logs are in memory and cannot fail to read, so both reads return a nil
// error. Every other method is the target's own.
type cdpAgentSurface struct {
	*cdp.Target
}

func (c cdpAgentSurface) ConsoleEntries(max int) ([]cdp.ConsoleEntry, int, error) {
	entries, dropped := c.Target.ConsoleEntries(max)
	return entries, dropped, nil
}

func (c cdpAgentSurface) NetworkEntries(max int) ([]cdp.NetworkEntry, int, error) {
	entries, dropped := c.Target.NetworkEntries(max)
	return entries, dropped, nil
}

func (a browseAgentSurfaces) Open(ctx context.Context, url string) (string, error) {
	return a.bs.OpenAgentTab(ctx, url)
}

func (a browseAgentSurfaces) Close(ctx context.Context, stateKey string) error {
	return a.bs.CloseAgentTab(ctx, stateKey)
}
