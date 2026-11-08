package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

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
	// Fresh is true when this publish created the hub artifact (no public
	// token was stored before). Only a fresh publish can be undone on the hub.
	Fresh bool
	// client is the hub client that made the publish, kept so a later local
	// failure can undo a fresh publish with the same connection.
	client *artifacts.HubClient
}

// hubUndoTimeout bounds the best-effort hub cleanup after a failed publish.
const hubUndoTimeout = 30 * time.Second

// undoFreshHubPublish removes the hub artifact created by a fresh first publish
// and clears its local token, so the failed publish leaves nothing live or
// stored. It is best-effort and runs even after the request context is gone.
// The local token is cleared only once the hub no longer has the artifact: if
// the hub delete fails, the token stays so local unpublish and delete can still
// reach the hub copy. It reports whether the hub copy is gone. A re-publish must
// never call this: the previous version keeps serving on the hub.
func undoFreshHubPublish(ctx context.Context, client *artifacts.HubClient, st *artifacts.Store, artifactID string) bool {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), hubUndoTimeout)
	defer cancel()
	if err := client.DeleteArtifact(ctx, artifactID); err != nil && !errors.Is(err, artifacts.ErrNotFound) {
		log.Printf("artifact hub: undo first publish of %s: delete hub artifact: %v (local token kept)", artifactID, err)
		return false
	}
	if err := st.SetArtifactHubToken(artifactID, ""); err != nil {
		log.Printf("artifact hub: undo first publish of %s: clear local token: %v", artifactID, err)
		return false
	}
	return true
}

// publishArtifactToHub uploads an already-published local version to the hub
// (Mode B). It has no local fallback: a hub failure is returned to the caller,
// which turns it into a 502. A first publish mints the public token, and stores
// it only after the hub accepted the artifact. If a later hub step fails on a
// first publish, the hub artifact is undone so no version-less artifact or
// orphaned token is left behind.
func (h *Handler) publishArtifactToHub(ctx context.Context, a artifacts.Artifact, entrypoint, buildCmd string, files []artifacts.File, st *artifacts.Store, access artifactHubAccess) (artifactHubOutcome, error) {
	client, err := h.hubClient()
	if err != nil {
		return artifactHubOutcome{}, err
	}

	// The stored public token keeps a re-publish on the same link. The hub
	// stores only its sha256.
	publicToken, err := st.GetArtifactHubToken(a.ID)
	if err != nil && !errors.Is(err, artifacts.ErrNotFound) {
		return artifactHubOutcome{}, fmt.Errorf("hub: read local token: %w", err)
	}
	fresh := publicToken == ""
	if fresh {
		publicToken, err = artifacts.NewHubPublicToken()
		if err != nil {
			return artifactHubOutcome{}, err
		}
	}

	if _, err := client.EnsureArtifact(ctx, a.ID, a.Name, publicToken); err != nil {
		return artifactHubOutcome{}, err
	}
	if fresh {
		if err := st.SetArtifactHubToken(a.ID, publicToken); err != nil {
			// The hub now holds an artifact whose token is not stored locally.
			undoFreshHubPublish(ctx, client, st, a.ID)
			return artifactHubOutcome{}, fmt.Errorf("hub: persist local token: %w", err)
		}
	}
	// failed undoes a fresh publish on any later hub failure. A re-publish
	// returns its error unchanged.
	failed := func(err error) (artifactHubOutcome, error) {
		if fresh {
			undoFreshHubPublish(ctx, client, st, a.ID)
		}
		return artifactHubOutcome{}, err
	}
	// Access is set before any version is published so a failure leaves no live
	// version behind (the artifact row alone serves nothing).
	if access.Mode != "" {
		if err := client.SetArtifactAccess(ctx, a.ID, access.Mode, access.Password); err != nil {
			return failed(err)
		}
	}
	for _, f := range files {
		data, err := st.GetBlob(f.BlobHash)
		if err != nil {
			return failed(fmt.Errorf("hub: read blob %s: %w", f.BlobHash, err))
		}
		if err := client.UploadBlob(ctx, a.ID, f.BlobHash, data); err != nil {
			return failed(err)
		}
	}
	if _, err := client.PublishVersion(ctx, a.ID, entrypoint, buildCmd, files); err != nil {
		return failed(err)
	}
	out := artifactHubOutcome{PublicURL: client.PublicURL(a.ID, publicToken), Fresh: fresh, client: client}
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

// hubClient returns a hub client for the sync connection on this machine. A
// machine with no sync token gets an error wrapping errHubNotConnected.
func (h *Handler) hubClient() (*artifacts.HubClient, error) {
	token, ok, err := ocodesync.LoadToken()
	if err != nil {
		return nil, fmt.Errorf("hub: load sync token: %w", err)
	}
	if !ok || token == "" {
		return nil, fmt.Errorf("%w; run the sync login flow first", errHubNotConnected)
	}
	return artifacts.NewHubClient(h.syncClient().BaseURL, token), nil
}

// removeArtifactFromHub runs op against the hub for a hub-hosted artifact. It
// must run before the local row changes, so a hub failure leaves local state
// alone. A hub artifact with no stored public token has no hub copy this
// machine knows about: the token is stored only after the hub accepted the
// artifact, and a failed first publish clears it once the hub copy is removed.
// So there is nothing to remove there and no connection is needed. A hub
// not_found means the hub already has no such artifact, which counts as success.
func (h *Handler) removeArtifactFromHub(ctx context.Context, a artifacts.Artifact, st *artifacts.Store, op func(*artifacts.HubClient) error) error {
	token, err := st.GetArtifactHubToken(a.ID)
	if err != nil && !errors.Is(err, artifacts.ErrNotFound) {
		return fmt.Errorf("hub: read local token: %w", err)
	}
	if token == "" {
		return nil
	}
	client, err := h.hubClient()
	if err != nil {
		return err
	}
	if err := op(client); err != nil && !errors.Is(err, artifacts.ErrNotFound) {
		return err
	}
	return nil
}

// writeHubRemovalError answers a failed hub unpublish or delete: 409 when this
// machine is not connected to the hub, 502 for any other hub failure.
func writeHubRemovalError(w http.ResponseWriter, action string, err error) {
	if errors.Is(err, errHubNotConnected) {
		writeErrorCode(w, http.StatusConflict, "hub_not_connected",
			"sync is not connected; run /sync to connect the hub before changing this hub artifact")
		return
	}
	writeError(w, http.StatusBadGateway, fmt.Sprintf("hub %s: %v", action, err))
}

// answerLocalCommitFailure answers a failed local commit (the version insert or
// the status update) that ran after the hub had already accepted the publish.
// Local artifacts have no hub step, so hub is nil and the plain 500 applies. A
// fresh first publish is undone on the hub. A re-publish cannot be: the hub has
// no delete-version route, so the hub keeps the new version and only re-running
// publish brings the local store back in sync.
func answerLocalCommitFailure(ctx context.Context, w http.ResponseWriter, id string, hub *artifactHubOutcome, st *artifacts.Store, err error) {
	switch {
	case hub == nil:
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("publish: %v", err))
	case hub.Fresh && undoFreshHubPublish(ctx, hub.client, st, id):
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("publish: %v (the hub publish was rolled back)", err))
	case hub.Fresh:
		log.Printf("ERROR artifact publish: %s: local commit failed after the hub accepted the first version, and the hub copy could not be removed: %v", id, err)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("publish: %v; the hub still holds the first version and removing it failed, see the server log", err))
	default:
		log.Printf("ERROR artifact publish: %s: local commit failed after the hub accepted the new version: %v", id, err)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("publish: %v; the hub already holds the new version, so re-run publish to bring the local store back in sync", err))
	}
}
