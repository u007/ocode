package cloudconnect

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var driveAPIBase = "https://www.googleapis.com/drive/v3"

var driveHTTPClient = &http.Client{Timeout: 30 * time.Second}

// GoogleDriveConnector implements Connector for Google Drive (read-only).
type GoogleDriveConnector struct {
	id     string
	rootID string // Drive folder id to treat as root ("" = "root")
	sec    SecretBundle

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

func NewGoogleDriveConnector(id, rootPath string, sec SecretBundle) (*GoogleDriveConnector, error) {
	c := &GoogleDriveConnector{id: id, sec: sec}
	if rootPath != "" {
		c.rootID = rootPath
	} else {
		c.rootID = "root"
	}
	return c, nil
}

func (c *GoogleDriveConnector) Provider() string { return "google-drive" }
func (c *GoogleDriveConnector) ID() string       { return c.id }

func (c *GoogleDriveConnector) accessTokenCached(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.accessToken != "" && time.Now().Before(c.tokenExpiry) {
		return c.accessToken, nil
	}
	tok, exp, err := c.refreshAccessToken(ctx)
	if err != nil {
		return "", err
	}
	c.accessToken = tok
	c.tokenExpiry = exp
	return tok, nil
}

func (c *GoogleDriveConnector) refreshAccessToken(ctx context.Context) (string, time.Time, error) {
	if c.sec.RefreshToken == "" {
		// Allow a statically provided access token in ClientSecret field? No.
		// Fall back: if ClientSecret looks like an access token? Keep simple:
		if c.sec.AccessKey != "" {
			return c.sec.AccessKey, time.Now().Add(1 * time.Hour), nil
		}
		return "", time.Time{}, fmt.Errorf("no refresh token configured")
	}
	vals := url.Values{}
	vals.Set("client_id", c.sec.ClientID)
	vals.Set("client_secret", c.sec.ClientSecret)
	vals.Set("refresh_token", c.sec.RefreshToken)
	vals.Set("grant_type", "refresh_token")
	req, err := http.NewRequestWithContext(ctx, "POST", "https://oauth2.googleapis.com/token", strings.NewReader(vals.Encode()))
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := driveHTTPClient.Do(req)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return "", time.Time{}, fmt.Errorf("token refresh failed: %s %s", resp.Status, string(b))
	}
	var tr struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", time.Time{}, err
	}
	exp := time.Now().Add(time.Duration(tr.ExpiresIn-60) * time.Second)
	return tr.AccessToken, exp, nil
}

type driveFile struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	MimeType     string `json:"mimeType"`
	Size         string `json:"size"`
	ModifiedTime string `json:"modifiedTime"`
}

func (c *GoogleDriveConnector) List(ctx context.Context, path string) ([]Entry, error) {
	if strings.Contains(path, "..") {
		return nil, fmt.Errorf("invalid path")
	}
	parent := path
	if parent == "" {
		parent = c.rootID
	}
	tok, err := c.accessTokenCached(ctx)
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf("'%s' in parents and trashed=false", parent)
	u := driveAPIBase + "/files?q=" + url.QueryEscape(q) + "&fields=files(id,name,mimeType,size,modifiedTime)&pageSize=200"
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := doWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("drive list failed: %s %s", resp.Status, string(b))
	}
	var out struct {
		Files []driveFile `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(out.Files))
	for _, f := range out.Files {
		sz := int64(0)
		fmt.Sscan(f.Size, &sz)
		mod, _ := time.Parse(time.RFC3339, f.ModifiedTime)
		entries = append(entries, Entry{
			ID:       f.ID,
			Name:     f.Name,
			Path:     f.ID,
			IsDir:    f.MimeType == "application/vnd.google-apps.folder",
			Size:     sz,
			MimeType: f.MimeType,
			Modified: mod,
		})
	}
	return entries, nil
}

func (c *GoogleDriveConnector) Read(ctx context.Context, path string) (io.ReadCloser, error) {
	if path == "" {
		return nil, fmt.Errorf("empty path")
	}
	if strings.Contains(path, "..") {
		return nil, fmt.Errorf("invalid path")
	}
	tok, err := c.accessTokenCached(ctx)
	if err != nil {
		return nil, err
	}
	// First get metadata to know mime type.
	meta, err := c.Metadata(ctx, path)
	_ = meta
	u := driveAPIBase + "/files/" + path + "?alt=media"
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := doWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("drive read failed: %s %s", resp.Status, string(b))
	}
	return resp.Body, nil
}

func (c *GoogleDriveConnector) Metadata(ctx context.Context, path string) (*Entry, error) {
	if path == "" {
		return nil, fmt.Errorf("empty path")
	}
	if strings.Contains(path, "..") {
		return nil, fmt.Errorf("invalid path")
	}
	tok, err := c.accessTokenCached(ctx)
	if err != nil {
		return nil, err
	}
	u := driveAPIBase + "/files/" + path + "?fields=id,name,mimeType,size,modifiedTime"
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := doWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("drive metadata failed: %s %s", resp.Status, string(b))
	}
	var f driveFile
	if err := json.NewDecoder(resp.Body).Decode(&f); err != nil {
		return nil, err
	}
	sz := int64(0)
	fmt.Sscan(f.Size, &sz)
	mod, _ := time.Parse(time.RFC3339, f.ModifiedTime)
	return &Entry{
		ID:       f.ID,
		Name:     f.Name,
		Path:     f.ID,
		IsDir:    f.MimeType == "application/vnd.google-apps.folder",
		Size:     sz,
		MimeType: f.MimeType,
		Modified: mod,
	}, nil
}

func (c *GoogleDriveConnector) Write(ctx context.Context, path string, r io.Reader, size int64) error {
	if path == "" {
		return fmt.Errorf("empty path")
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("invalid path")
	}
	path = ResolveConflictPath(c, path)

	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	tok, err := c.accessTokenCached(ctx)
	if err != nil {
		return err
	}

	existing := false
	if _, err := c.Metadata(ctx, path); err == nil {
		existing = true
	}

	var method, u string
	if existing {
		method = http.MethodPatch
		u = driveAPIBase + "/files/" + path + "?uploadType=media"
	} else {
		method = http.MethodPost
		u = driveAPIBase + "/files?uploadType=media"
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/octet-stream")
	if size >= 0 {
		req.ContentLength = int64(len(data))
	}
	resp, err := doWithRetry(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("drive write failed: %s %s", resp.Status, string(b))
	}
	return nil
}

func (c *GoogleDriveConnector) Delete(ctx context.Context, path string) error {
	if path == "" {
		return fmt.Errorf("empty path")
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("invalid path")
	}
	tok, err := c.accessTokenCached(ctx)
	if err != nil {
		return err
	}
	u := driveAPIBase + "/files/" + path
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := doWithRetry(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("drive delete failed: %s %s", resp.Status, string(b))
	}
	return nil
}
