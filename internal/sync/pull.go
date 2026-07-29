package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/u007/ocode/internal/auth"
)

type pullResponse struct {
	Version int    `json:"version"`
	Blob    string `json:"blob"`
}

// Pull fetches the latest blob from the server, merges it with the local
// state using the snapshot as the merge base, and writes the result to the
// local config file. It is intended to run once at startup (or after login)
// to pull any changes made on another machine.
func (c *Client) Pull(ctx context.Context, blob BlobType) error {
	token, ok, err := LoadToken()
	if err != nil {
		return fmt.Errorf("load sync token: %w", err)
	}
	if !ok {
		return nil // not logged in
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/ocode/sync/"+string(blob), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}

	if res.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(res.Body)
		res.Body.Close()
		return fmt.Errorf("pull returned status %d: %s", res.StatusCode, string(respBody))
	}

	var out pullResponse
	if err := c.decode(res, &out); err != nil {
		return err
	}
	if out.Blob == "" {
		return nil // nothing synced yet for this user
	}

	snapshotPath, err := SnapshotPath(blob)
	if err != nil {
		return err
	}
	base := readSnapshot(snapshotPath)
	localPath, err := localConfigPathFor(blob)
	if err != nil {
		return fmt.Errorf("resolve local path for %s: %w", blob, err)
	}
	local, _ := os.ReadFile(localPath)

	merged, err := Merge(blob, base, jsonOrEmptyObject(local), json.RawMessage(out.Blob))
	if err != nil {
		return fmt.Errorf("merge on pull: %w", err)
	}
	os.MkdirAll(filepath.Dir(localPath), 0700)
	if err := writeLocalConfigFileAtomic(localPath, merged); err != nil {
		return err
	}
	// Refresh auth in-memory cache after direct disk write.
	if blob == BlobTypeAuth {
		if err := auth.LoadStore(); err != nil {
			log.Printf("sync: failed to refresh auth cache after pulling %s: %v", blob, err)
		}
	}
	writeSnapshot(snapshotPath, merged, out.Version)
	return nil
}

func jsonOrEmptyObject(raw []byte) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(raw)
}
