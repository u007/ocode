package cloudconnect

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

var dropboxAPIBase = "https://api.dropboxapi.com/2"
var dropboxContentBase = "https://content.dropboxapi.com/2"

// DropboxConnector implements Connector for Dropbox (read-only).
type DropboxConnector struct {
	id   string
	root string
	sec  SecretBundle

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

func NewDropboxConnector(id, rootPath string, sec SecretBundle) (*DropboxConnector, error) {
	c := &DropboxConnector{id: id, sec: sec}
	if rootPath != "" {
		c.root = rootPath
	} else {
		c.root = ""
	}
	return c, nil
}

func (c *DropboxConnector) Provider() string { return "dropbox" }
func (c *DropboxConnector) ID() string       { return c.id }

func (c *DropboxConnector) accessTokenCached(ctx context.Context) (string, error) {
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

func (c *DropboxConnector) refreshAccessToken(ctx context.Context) (string, time.Time, error) {
	if c.sec.RefreshToken == "" {
		if c.sec.AccessKey != "" {
			return c.sec.AccessKey, time.Now().Add(1 * time.Hour), nil
		}
		return "", time.Time{}, fmt.Errorf("no refresh token configured")
	}
	vals := "client_id=" + c.sec.ClientID + "&client_secret=" + c.sec.ClientSecret +
		"&refresh_token=" + c.sec.RefreshToken + "&grant_type=refresh_token"
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.dropboxapi.com/oauth2/token", strings.NewReader(vals))
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

type dropboxEntry struct {
	Tag         string `json:".tag"`
	Name        string `json:"name"`
	PathLower   string `json:"path_lower"`
	PathDisplay string `json:"path_display"`
	Size        int64  `json:"size"`
	ServerMod   string `json:"server_modified"`
}

func (c *DropboxConnector) toPath(p string) string {
	if p == "" {
		if c.root != "" {
			return c.root
		}
		return ""
	}
	if c.root != "" {
		return c.root + "/" + p
	}
	return "/" + p
}

func (c *DropboxConnector) List(ctx context.Context, path string) ([]Entry, error) {
	if strings.Contains(path, "..") {
		return nil, fmt.Errorf("invalid path")
	}
	tok, err := c.accessTokenCached(ctx)
	if err != nil {
		return nil, err
	}
	dbPath := c.toPath(path)
	if dbPath == "" {
		dbPath = ""
	}

	// Initial list_folder call.
	body, _ := json.Marshal(map[string]interface{}{"path": dbPath, "limit": 200})
	req, err := http.NewRequestWithContext(ctx, "POST", dropboxAPIBase+"/files/list_folder", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := doWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		b, _ := readAllBody(resp)
		return nil, fmt.Errorf("dropbox list failed: %s %s", resp.Status, string(b))
	}
	var out struct {
		Entries []dropboxEntry `json:"entries"`
		Cursor  string         `json:"cursor"`
		HasMore bool           `json:"has_more"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		resp.Body.Close()
		return nil, err
	}
	resp.Body.Close()

	entries := []Entry{}
	for _, e := range out.Entries {
		mod, _ := time.Parse(time.RFC3339, e.ServerMod)
		entries = append(entries, Entry{
			ID:       e.PathLower,
			Name:     e.Name,
			Path:     strings.TrimPrefix(e.PathLower, "/"),
			IsDir:    e.Tag == "folder",
			Size:     e.Size,
			Modified: mod,
		})
	}
	// Paginate.
	cursor := out.Cursor
	for out.HasMore {
		body2, _ := json.Marshal(map[string]string{"cursor": cursor})
		req2, err := http.NewRequestWithContext(ctx, "POST", dropboxAPIBase+"/files/list_folder/continue", bytes.NewReader(body2))
		if err != nil {
			return entries, err
		}
		req2.Header.Set("Authorization", "Bearer "+tok)
		req2.Header.Set("Content-Type", "application/json")
		resp2, err := doWithRetry(ctx, req2)
		if err != nil {
			return entries, err
		}
		if resp2.StatusCode != 200 {
			b, _ := readAllBody(resp2)
			return entries, fmt.Errorf("dropbox list continue failed: %s %s", resp2.Status, string(b))
		}
		var out2 struct {
			Entries []dropboxEntry `json:"entries"`
			Cursor  string         `json:"cursor"`
			HasMore bool           `json:"has_more"`
		}
		if err := json.NewDecoder(resp2.Body).Decode(&out2); err != nil {
			resp2.Body.Close()
			return entries, err
		}
		resp2.Body.Close()
		for _, e := range out2.Entries {
			mod, _ := time.Parse(time.RFC3339, e.ServerMod)
			entries = append(entries, Entry{
				ID:       e.PathLower,
				Name:     e.Name,
				Path:     strings.TrimPrefix(e.PathLower, "/"),
				IsDir:    e.Tag == "folder",
				Size:     e.Size,
				Modified: mod,
			})
		}
		cursor = out2.Cursor
		out.HasMore = out2.HasMore
	}
	return entries, nil
}

func (c *DropboxConnector) Read(ctx context.Context, path string) (io.ReadCloser, error) {
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
	dbPath := c.toPath(path)
	if dbPath == "" {
		dbPath = ""
	}
	arg, _ := json.Marshal(map[string]string{"path": dbPath})
	req, err := http.NewRequestWithContext(ctx, "POST", dropboxContentBase+"/files/download", bytes.NewReader(nil))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Dropbox-API-Arg", string(arg))
	resp, err := doWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		b, _ := readAllBody(resp)
		return nil, fmt.Errorf("dropbox read failed: %s %s", resp.Status, string(b))
	}
	return resp.Body, nil
}

func (c *DropboxConnector) Metadata(ctx context.Context, path string) (*Entry, error) {
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
	dbPath := c.toPath(path)
	arg, _ := json.Marshal(map[string]string{"path": dbPath})
	req, err := http.NewRequestWithContext(ctx, "POST", dropboxAPIBase+"/files/get_metadata", bytes.NewReader(arg))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := doWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("dropbox metadata failed: %s %s", resp.Status, string(b))
	}
	var e dropboxEntry
	if err := json.NewDecoder(resp.Body).Decode(&e); err != nil {
		return nil, err
	}
	mod, _ := time.Parse(time.RFC3339, e.ServerMod)
	return &Entry{
		ID:       e.PathLower,
		Name:     e.Name,
		Path:     strings.TrimPrefix(e.PathLower, "/"),
		IsDir:    e.Tag == "folder",
		Size:     e.Size,
		Modified: mod,
	}, nil
}

func (c *DropboxConnector) Write(ctx context.Context, path string, r io.Reader, size int64) error {
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
	dbPath := c.toPath(path)
	arg, _ := json.Marshal(map[string]interface{}{
		"path": dbPath,
		"mode": "overwrite",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dropboxContentBase+"/files/upload", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Dropbox-API-Arg", string(arg))
	req.Header.Set("Content-Type", "application/octet-stream")
	if size >= 0 {
		req.ContentLength = int64(len(data))
	}
	resp, err := driveHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("dropbox write failed: %s %s", resp.Status, string(b))
	}
	return nil
}

func (c *DropboxConnector) Delete(ctx context.Context, path string) error {
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
	dbPath := c.toPath(path)
	arg, _ := json.Marshal(map[string]string{"path": dbPath})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dropboxAPIBase+"/files/delete_v2", bytes.NewReader(arg))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := driveHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("dropbox delete failed: %s %s", resp.Status, string(b))
	}
	return nil
}
