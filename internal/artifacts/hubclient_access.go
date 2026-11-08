package artifacts

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// SetArtifactAccess sets the visitor access mode on the hub. The password is
// optional and only used by the "password" mode; the hub hashes it and never
// returns it.
func (c *HubClient) SetArtifactAccess(ctx context.Context, artifactID, mode, password string) error {
	payload := map[string]any{"mode": mode}
	if password != "" {
		payload["password"] = password
	}
	resp, err := c.do(ctx, http.MethodPut, "/api/ocode/artifacts/"+url.PathEscape(artifactID)+"/access", payload)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return statusError("set access", resp)
	}
	resp.Body.Close()
	return nil
}

// HubInvite is one allowlist entry created by the hub. inviteUrl is returned
// even when no mail provider is configured, so the owner can share it by hand.
type HubInvite struct {
	Email     string `json:"email"`
	InviteURL string `json:"inviteUrl"`
	EmailSent bool   `json:"emailSent"`
}

// AddArtifactInvite invites an email to an artifact. The hub needs the
// artifact's public token to build the invite link; it stores only its hash.
func (c *HubClient) AddArtifactInvite(ctx context.Context, artifactID, email, publicToken string) (HubInvite, error) {
	resp, err := c.do(ctx, http.MethodPost, "/api/ocode/artifacts/"+url.PathEscape(artifactID)+"/invites", map[string]any{
		"email":       email,
		"publicToken": publicToken,
	})
	if err != nil {
		return HubInvite{}, err
	}
	if resp.StatusCode != http.StatusCreated {
		return HubInvite{}, statusError("add invite", resp)
	}
	defer resp.Body.Close()
	var out HubInvite
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return HubInvite{}, fmt.Errorf("artifacts: hub decode invite: %w", err)
	}
	return out, nil
}
