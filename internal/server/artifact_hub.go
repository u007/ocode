package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/u007/ocode/internal/artifacts"
	ocodesync "github.com/u007/ocode/internal/sync"
)

// errHubNotConnected marks a publish attempted on a machine with no sync
// token. It is a client-actionable condition (connect the hub, or publish
// locally), not a hub failure, so HandlePublish answers 409 instead of 502.
var errHubNotConnected = errors.New("hub: not connected")

// artifactHubAccess is the visitor access configuration carried by a publish
// request. Mode "" means "leave the hub's current setting alone".
type artifactHubAccess struct {
	Mode     string
	Password string
	Emails   []string
}

// artifactHubOutcome is what a Mode B publish produces: the shareable link,
// the invites that were created, and any per-email failures (a partial invite
// failure must not undo a successful publish).
type artifactHubOutcome struct {
	PublicURL    string
	Invites      []artifacts.HubInvite
	InviteErrors []string
}

// publishArtifactToHub uploads an already-published local version to the hub
// (Mode B). It has no local fallback: a hub failure is returned to the caller,
// which turns it into a 502.
func (h *Handler) publishArtifactToHub(ctx context.Context, a artifacts.Artifact, entrypoint, buildCmd string, files []artifacts.File, st *artifacts.Store, access artifactHubAccess) (artifactHubOutcome, error) {
	token, ok, err := ocodesync.LoadToken()
	if err != nil {
		return artifactHubOutcome{}, fmt.Errorf("hub: load sync token: %w", err)
	}
	if !ok || token == "" {
		return artifactHubOutcome{}, fmt.Errorf("%w; run the sync login flow first, or publish locally", errHubNotConnected)
	}
	client := artifacts.NewHubClient(h.syncClient().BaseURL, token)

	// Persist the public token locally on first publish so a re-publish keeps
	// the same link. The hub stores only its sha256.
	publicToken, err := st.GetArtifactHubToken(a.ID)
	if err != nil && !errors.Is(err, artifacts.ErrNotFound) {
		return artifactHubOutcome{}, fmt.Errorf("hub: read local token: %w", err)
	}
	if publicToken == "" {
		publicToken, err = artifacts.NewHubPublicToken()
		if err != nil {
			return artifactHubOutcome{}, err
		}
		if err := st.SetArtifactHubToken(a.ID, publicToken); err != nil {
			return artifactHubOutcome{}, fmt.Errorf("hub: persist local token: %w", err)
		}
	}

	if _, err := client.EnsureArtifact(ctx, a.ID, a.Name, publicToken); err != nil {
		return artifactHubOutcome{}, err
	}
	// Access is set before any version is published so a failure leaves no live
	// version behind (the artifact row alone serves nothing).
	if access.Mode != "" {
		if err := client.SetArtifactAccess(ctx, a.ID, access.Mode, access.Password); err != nil {
			return artifactHubOutcome{}, err
		}
	}
	for _, f := range files {
		data, err := st.GetBlob(f.BlobHash)
		if err != nil {
			return artifactHubOutcome{}, fmt.Errorf("hub: read blob %s: %w", f.BlobHash, err)
		}
		if err := client.UploadBlob(ctx, a.ID, f.BlobHash, data); err != nil {
			return artifactHubOutcome{}, err
		}
	}
	if _, err := client.PublishVersion(ctx, a.ID, entrypoint, buildCmd, files); err != nil {
		return artifactHubOutcome{}, err
	}
	out := artifactHubOutcome{PublicURL: client.PublicURL(a.ID, publicToken)}
	for _, email := range access.Emails {
		invite, err := client.AddArtifactInvite(ctx, a.ID, email, publicToken)
		if err != nil {
			out.InviteErrors = append(out.InviteErrors, fmt.Sprintf("%s: %v", email, err))
			continue
		}
		out.Invites = append(out.Invites, invite)
	}
	return out, nil
}
