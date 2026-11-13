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

var gcsAPIBase = "https://storage.googleapis.com/storage/v1"

// GCSConnector implements Connector for Google Cloud Storage (read-only).
type GCSConnector struct {
	id     string
	bucket string
	sec    SecretBundle

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

func NewGCSConnector(id, rootPath string, sec SecretBundle) (*GCSConnector, error) {
	if rootPath == "" {
		return nil, fmt.Errorf("bucket (root_path) required for gcs")
	}
	return &GCSConnector{id: id, bucket: rootPath, sec: sec}, nil
}

func (c *GCSConnector) Provider() string { return "gcs" }
func (c *GCSConnector) ID() string       { return c.id }

func (c *GCSConnector) accessTokenCached(ctx context.Context) (string, error) {
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

func (c *GCSConnector) refreshAccessToken(ctx context.Context) (string, time.Time, error) {
	if c.sec.RefreshToken == "" {
		if c.sec.AccessKey != "" {
			// Treat AccessKey as a pre-minted bearer token (e.g. for
			// service accounts or tests). Not an HMAC key.
			return c.sec.AccessKey, time.Now().Add(1 * time.Hour), nil
		}
		return "", time.Time{}, fmt.Errorf("no refresh token configured")
	}
	vals := url.Values{}
	vals.Set("client_id", c.sec.ClientID)
	vals.Set("client_secret", c.sec.ClientSecret)
	vals.Set("refresh_token", c.sec.RefreshToken)
	vals.Set("grant_type", "refresh_token")
	vals.Set("scope", "https://www.googleapis.com/auth/devstorage.read_only")
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

type gcsObject struct {
	Name        string `json:"name"`
	Size        string `json:"size"`
	ContentType string `json:"contentType"`
	Updated     string `json:"updated"`
}

func (c *GCSConnector) List(ctx context.Context, path string) ([]Entry, error) {
	if strings.Contains(path, "..") {
		return nil, fmt.Errorf("invalid path")
	}
	tok, err := c.accessTokenCached(ctx)
	if err != nil {
		return nil, err
	}
	prefix := path
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	u := gcsAPIBase + "/b/" + url.PathEscape(c.bucket) + "/o?delimiter=/&maxResults=200"
	if prefix != "" {
		u += "&prefix=" + url.QueryEscape(prefix)
	}
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
		b, _ := readAllBody(resp)
		return nil, fmt.Errorf("gcs list failed: %s %s", resp.Status, string(b))
	}
	var out struct {
		Items    []gcsObject `json:"items"`
		Prefixes []string    `json:"prefixes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		resp.Body.Close()
		return nil, err
	}
	resp.Body.Close()

	entries := []Entry{}
	seen := map[string]bool{}
	// Folders from prefixes
	for _, pfx := range out.Prefixes {
		name := strings.TrimSuffix(strings.TrimPrefix(pfx, prefix), "/")
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		entries = append(entries, Entry{
			ID:       pfx,
			Name:     name,
			Path:     pfx,
			IsDir:    true,
			MimeType: "application/x-directory",
		})
	}
	// Objects
	for _, o := range out.Items {
		// skip the prefix placeholder object itself
		if o.Name == prefix {
			continue
		}
		name := strings.TrimPrefix(o.Name, prefix)
		if strings.Contains(name, "/") {
			// nested; skip (handled by prefixes)
			continue
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		sz := int64(0)
		fmt.Sscan(o.Size, &sz)
		mod, _ := time.Parse(time.RFC3339, o.Updated)
		entries = append(entries, Entry{
			ID:       o.Name,
			Name:     name,
			Path:     o.Name,
			IsDir:    false,
			Size:     sz,
			MimeType: o.ContentType,
			Modified: mod,
		})
	}
	return entries, nil
}

func (c *GCSConnector) Read(ctx context.Context, path string) (io.ReadCloser, error) {
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
	u := gcsAPIBase + "/b/" + url.PathEscape(c.bucket) + "/o/" + url.PathEscape(path) + "?alt=media"
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
		b, _ := readAllBody(resp)
		return nil, fmt.Errorf("gcs read failed: %s %s", resp.Status, string(b))
	}
	return resp.Body, nil
}

func (c *GCSConnector) Metadata(ctx context.Context, path string) (*Entry, error) {
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
	u := gcsAPIBase + "/b/" + url.PathEscape(c.bucket) + "/o/" + url.PathEscape(path)
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
		return nil, fmt.Errorf("gcs metadata failed: %s %s", resp.Status, string(b))
	}
	var o gcsObject
	if err := json.NewDecoder(resp.Body).Decode(&o); err != nil {
		return nil, err
	}
	sz := int64(0)
	fmt.Sscan(o.Size, &sz)
	mod, _ := time.Parse(time.RFC3339, o.Updated)
	name := o.Name
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return &Entry{
		ID:       o.Name,
		Name:     name,
		Path:     o.Name,
		IsDir:    false,
		Size:     sz,
		MimeType: o.ContentType,
		Modified: mod,
	}, nil
}

func (c *GCSConnector) Write(ctx context.Context, path string, r io.Reader, size int64) error {
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

	// Metadata check for direct connector use.
	_, _ = c.Metadata(ctx, path)

	u := gcsAPIBase + "/b/" + url.PathEscape(c.bucket) + "/o/" + url.PathEscape(path) + "?uploadType=media"
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
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
		return fmt.Errorf("gcs write failed: %s %s", resp.Status, string(b))
	}
	return nil
}

func (c *GCSConnector) Delete(ctx context.Context, path string) error {
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
	u := gcsAPIBase + "/b/" + url.PathEscape(c.bucket) + "/o/" + url.PathEscape(path)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := driveHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gcs delete failed: %s %s", resp.Status, string(b))
	}
	return nil
}
