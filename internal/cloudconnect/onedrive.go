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

var graphAPIBase = "https://graph.microsoft.com/v1.0"

// OneDriveConnector implements Connector for OneDrive (read-only) via Microsoft Graph.
type OneDriveConnector struct {
	id     string
	rootID string
	sec    SecretBundle

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

func NewOneDriveConnector(id, rootPath string, sec SecretBundle) (*OneDriveConnector, error) {
	c := &OneDriveConnector{id: id, sec: sec}
	if rootPath != "" {
		c.rootID = rootPath
	} else {
		c.rootID = "root"
	}
	return c, nil
}

func (c *OneDriveConnector) Provider() string { return "onedrive" }
func (c *OneDriveConnector) ID() string       { return c.id }

func (c *OneDriveConnector) accessTokenCached(ctx context.Context) (string, error) {
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

func (c *OneDriveConnector) refreshAccessToken(ctx context.Context) (string, time.Time, error) {
	if c.sec.RefreshToken == "" {
		if c.sec.AccessKey != "" {
			return c.sec.AccessKey, time.Now().Add(1 * time.Hour), nil
		}
		return "", time.Time{}, fmt.Errorf("no refresh token configured")
	}
	tenant := "common"
	vals := url.Values{}
	vals.Set("client_id", c.sec.ClientID)
	vals.Set("client_secret", c.sec.ClientSecret)
	vals.Set("refresh_token", c.sec.RefreshToken)
	vals.Set("grant_type", "refresh_token")
	vals.Set("scope", "Files.Read offline_access")
	u := "https://login.microsoftonline.com/" + tenant + "/oauth2/v2.0/token"
	req, err := http.NewRequestWithContext(ctx, "POST", u, strings.NewReader(vals.Encode()))
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

type graphItem struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Size         int64  `json:"size"`
	LastModified string `json:"lastModifiedDateTime"`
	File         *struct {
		MimeType string `json:"mimeType"`
	} `json:"file"`
	Folder *struct{} `json:"folder"`
}

func (c *OneDriveConnector) List(ctx context.Context, path string) ([]Entry, error) {
	if strings.Contains(path, "..") {
		return nil, fmt.Errorf("invalid path")
	}
	parent := path
	if parent == "" {
		parent = "root"
	}
	tok, err := c.accessTokenCached(ctx)
	if err != nil {
		return nil, err
	}
	var u string
	if parent == "root" {
		u = graphAPIBase + "/me/drive/root/children"
	} else {
		u = graphAPIBase + "/me/drive/items/" + parent + "/children"
	}
	u += "?$select=id,name,size,lastModifiedDateTime,file,folder&$top=200"

	entries := []Entry{}
	next := u
	for page := 0; page < 10 && next != ""; page++ {
		req, err := http.NewRequestWithContext(ctx, "GET", next, nil)
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
			return nil, fmt.Errorf("onedrive list failed: %s %s", resp.Status, string(b))
		}
		var out struct {
			Value    []graphItem `json:"value"`
			NextLink string      `json:"@odata.nextLink"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			resp.Body.Close()
			return nil, err
		}
		resp.Body.Close()
		for _, it := range out.Value {
			mod, _ := time.Parse(time.RFC3339, it.LastModified)
			mime := ""
			isDir := it.Folder != nil
			if it.File != nil {
				mime = it.File.MimeType
			}
			entries = append(entries, Entry{
				ID:       it.ID,
				Name:     it.Name,
				Path:     it.ID,
				IsDir:    isDir,
				Size:     it.Size,
				MimeType: mime,
				Modified: mod,
			})
		}
		// Only follow nextLink on the Graph host to avoid SSRF/token leakage.
		if out.NextLink != "" && !strings.HasPrefix(out.NextLink, graphAPIBase) {
			next = ""
		} else {
			next = out.NextLink
		}
	}
	return entries, nil
}

func (c *OneDriveConnector) Read(ctx context.Context, path string) (io.ReadCloser, error) {
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
	u := graphAPIBase + "/me/drive/items/" + path + "/content"
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := driveHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 && resp.StatusCode != 302 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("onedrive read failed: %s %s", resp.Status, string(b))
	}
	// Follow redirect if present.
	if resp.StatusCode == 302 {
		loc := resp.Header.Get("Location")
		resp.Body.Close()
		r2, err := http.NewRequestWithContext(ctx, "GET", loc, nil)
		if err != nil {
			return nil, err
		}
		resp2, err := driveHTTPClient.Do(r2)
		if err != nil {
			return nil, err
		}
		if resp2.StatusCode != 200 {
			b, _ := io.ReadAll(resp2.Body)
			resp2.Body.Close()
			return nil, fmt.Errorf("onedrive read failed: %s %s", resp2.Status, string(b))
		}
		return resp2.Body, nil
	}
	return resp.Body, nil
}

func (c *OneDriveConnector) Metadata(ctx context.Context, path string) (*Entry, error) {
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
	u := graphAPIBase + "/me/drive/items/" + path + "?$select=id,name,size,lastModifiedDateTime,file,folder"
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := driveHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("onedrive metadata failed: %s %s", resp.Status, string(b))
	}
	var it graphItem
	if err := json.NewDecoder(resp.Body).Decode(&it); err != nil {
		return nil, err
	}
	mod, _ := time.Parse(time.RFC3339, it.LastModified)
	mime := ""
	isDir := it.Folder != nil
	if it.File != nil {
		mime = it.File.MimeType
	}
	return &Entry{
		ID:       it.ID,
		Name:     it.Name,
		Path:     it.ID,
		IsDir:    isDir,
		Size:     it.Size,
		MimeType: mime,
		Modified: mod,
	}, nil
}

var _ = url.Values{}
var _ = strings.NewReader
var _ = io.Copy
var _ = time.Second

func (c *OneDriveConnector) Write(ctx context.Context, path string, r io.Reader, size int64) error {
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
	u := graphAPIBase + "/me/drive/items/" + path + "/content"
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(data))
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
		return fmt.Errorf("onedrive write failed: %s %s", resp.Status, string(b))
	}
	return nil
}

func (c *OneDriveConnector) Delete(ctx context.Context, path string) error {
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
	u := graphAPIBase + "/me/drive/items/" + path
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
		return fmt.Errorf("onedrive delete failed: %s %s", resp.Status, string(b))
	}
	return nil
}
