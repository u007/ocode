package artifacts

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HubClient talks to the hub's /api/ocode/artifacts API (Mode B). It never
// logs or serialises Token: the bearer is only placed in the Authorization
// header.
type HubClient struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// NewHubClient builds a client for baseURL (the resolved sync_url) using the
// ocode sync bearer token.
func NewHubClient(baseURL, token string) *HubClient {
	return &HubClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

// NewHubPublicToken returns a 32-byte base64url capability token. The client
// generates it once and persists it locally, so re-publishing keeps the link.
func NewHubPublicToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("artifacts: hub token rand: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// HubArtifact is the subset of the hub artifact row the client needs.
type HubArtifact struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Slug   string `json:"slug"`
	Status string `json:"status"`
}

// HubPublishResult is what finalising a version returns.
type HubPublishResult struct {
	VersionID string `json:"id"`
	Token     string `json:"token"`
}

// PublicURL is the shareable URL for an artifact. The hub serves /p on its
// own origin.
func (c *HubClient) PublicURL(artifactID, token string) string {
	return c.BaseURL + "/p/" + url.PathEscape(artifactID) + "/" + url.PathEscape(token) + "/"
}

func (c *HubClient) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("artifacts: hub encode %s: %w", path, err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, r)
	if err != nil {
		return nil, fmt.Errorf("artifacts: hub request %s: %w", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.HTTP.Do(req)
}

// statusError reads a bounded error body so a hub failure is actionable
// without dumping an arbitrary response into the log.
func statusError(op string, resp *http.Response) error {
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("artifacts: hub %s: status %d: %s", op, resp.StatusCode, strings.TrimSpace(string(body)))
}

// EnsureArtifact creates the hub artifact, or returns the existing one when
// the client-supplied id already belongs to this user.
func (c *HubClient) EnsureArtifact(ctx context.Context, id, name, publicToken string) (HubArtifact, error) {
	resp, err := c.do(ctx, http.MethodPost, "/api/ocode/artifacts", map[string]string{
		"id":    id,
		"name":  name,
		"token": publicToken,
	})
	if err != nil {
		return HubArtifact{}, err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return HubArtifact{}, statusError("ensure artifact", resp)
	}
	defer resp.Body.Close()
	var out struct {
		Artifact HubArtifact `json:"artifact"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return HubArtifact{}, fmt.Errorf("artifacts: hub decode artifact: %w", err)
	}
	return out.Artifact, nil
}

// UploadBlob asks for a presigned PUT when the hub lacks the blob, then
// uploads the bytes directly to S3. An already-present blob is a no-op.
func (c *HubClient) UploadBlob(ctx context.Context, artifactID, hash string, data []byte) error {
	resp, err := c.do(ctx, http.MethodPost, "/api/ocode/artifacts/"+url.PathEscape(artifactID)+"/blobs", map[string]any{
		"hash": hash,
		"size": len(data),
	})
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return statusError("blob ticket", resp)
	}
	var ticket struct {
		Exists    bool   `json:"exists"`
		UploadURL string `json:"uploadUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ticket); err != nil {
		resp.Body.Close()
		return fmt.Errorf("artifacts: hub decode blob ticket: %w", err)
	}
	resp.Body.Close()
	if ticket.Exists {
		return nil
	}
	if ticket.UploadURL == "" {
		return fmt.Errorf("artifacts: hub returned no upload URL for blob %s", hash)
	}
	put, err := http.NewRequestWithContext(ctx, http.MethodPut, ticket.UploadURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("artifacts: hub blob put request: %w", err)
	}
	put.Header.Set("Content-Type", "application/octet-stream")
	putResp, err := c.HTTP.Do(put)
	if err != nil {
		return fmt.Errorf("artifacts: hub blob put: %w", err)
	}
	defer putResp.Body.Close()
	if putResp.StatusCode < 200 || putResp.StatusCode >= 300 {
		return fmt.Errorf("artifacts: hub blob put: status %d", putResp.StatusCode)
	}
	return nil
}

// PublishVersion finalises a version from blobs already uploaded.
func (c *HubClient) PublishVersion(ctx context.Context, artifactID, entrypoint, buildCmd string, files []File) (HubPublishResult, error) {
	type hubFile struct {
		Path     string `json:"path"`
		BlobHash string `json:"blobHash"`
		Size     int64  `json:"size"`
		Mode     string `json:"mode"`
		Mime     string `json:"mime"`
	}
	payload := map[string]any{
		"entrypoint": entrypoint,
		"buildCmd":   buildCmd,
		"files":      make([]hubFile, 0, len(files)),
	}
	hs := make([]hubFile, 0, len(files))
	for _, f := range files {
		hs = append(hs, hubFile{Path: f.Path, BlobHash: f.BlobHash, Size: f.Size, Mode: f.Mode, Mime: f.Mime})
	}
	payload["files"] = hs

	resp, err := c.do(ctx, http.MethodPost, "/api/ocode/artifacts/"+url.PathEscape(artifactID)+"/versions", payload)
	if err != nil {
		return HubPublishResult{}, err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return HubPublishResult{}, statusError("publish version", resp)
	}
	defer resp.Body.Close()
	var out struct {
		Version struct {
			ID string `json:"id"`
		} `json:"version"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return HubPublishResult{}, fmt.Errorf("artifacts: hub decode publish: %w", err)
	}
	return HubPublishResult{VersionID: out.Version.ID, Token: out.Token}, nil
}
