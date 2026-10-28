package server

import (
	"context"

	"github.com/u007/ocode/internal/browse"
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
	return t, note, nil
}

func (a browseAgentSurfaces) Open(ctx context.Context, url string) (string, error) {
	return a.bs.OpenAgentTab(ctx, url)
}

func (a browseAgentSurfaces) Close(ctx context.Context, stateKey string) error {
	return a.bs.CloseAgentTab(ctx, stateKey)
}
