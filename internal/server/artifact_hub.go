package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/u007/ocode/internal/artifacts"
	ocodesync "github.com/u007/ocode/internal/sync"
)

// publishArtifactToHub uploads an already-published local version to the hub
// (Mode B). It has no local fallback: a hub failure is returned to the caller,
// which turns it into a 502.
func (h *Handler) publishArtifactToHub(ctx context.Context, a artifacts.Artifact, entrypoint, buildCmd string, files []artifacts.File, st *artifacts.Store) (string, error) {
	token, ok, err := ocodesync.LoadToken()
	if err != nil {
		return "", fmt.Errorf("hub: load sync token: %w", err)
	}
	if !ok || token == "" {
		return "", errors.New("hub: not connected; run the sync login flow first")
	}
	client := artifacts.NewHubClient(h.syncClient().BaseURL, token)

	// Persist the public token locally on first publish so a re-publish keeps
	// the same link. The hub stores only its sha256.
	publicToken, err := st.GetArtifactHubToken(a.ID)
	if err != nil && !errors.Is(err, artifacts.ErrNotFound) {
		return "", fmt.Errorf("hub: read local token: %w", err)
	}
	if publicToken == "" {
		publicToken, err = artifacts.NewHubPublicToken()
		if err != nil {
			return "", err
		}
		if err := st.SetArtifactHubToken(a.ID, publicToken); err != nil {
			return "", fmt.Errorf("hub: persist local token: %w", err)
		}
	}

	if _, err := client.EnsureArtifact(ctx, a.ID, a.Name, publicToken); err != nil {
		return "", err
	}
	for _, f := range files {
		data, err := st.GetBlob(f.BlobHash)
		if err != nil {
			return "", fmt.Errorf("hub: read blob %s: %w", f.BlobHash, err)
		}
		if err := client.UploadBlob(ctx, a.ID, f.BlobHash, data); err != nil {
			return "", err
		}
	}
	if _, err := client.PublishVersion(ctx, a.ID, entrypoint, buildCmd, files); err != nil {
		return "", err
	}
	return client.PublicURL(a.ID, publicToken), nil
}
