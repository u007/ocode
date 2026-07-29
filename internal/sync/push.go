package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/u007/ocode/internal/auth"
)

// snapshot data persisted on disk alongside the sync token.
type snapshot struct {
	Version int             `json:"version"`
	Blob    json.RawMessage `json:"blob"`
}

const maxPushRetries = 5

type pushResponse struct {
	OK             bool   `json:"ok"`
	Version        int    `json:"version,omitempty"`
	CurrentVersion int    `json:"currentVersion,omitempty"`
	CurrentBlob    string `json:"currentBlob,omitempty"`
}

func readSnapshot(path string) json.RawMessage {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var s snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil
	}
	return s.Blob
}

func lastKnownVersion(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var s snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return 0
	}
	return s.Version
}

func writeSnapshot(path string, blob json.RawMessage, version int) error {
	s := snapshot{Version: version, Blob: blob}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// SnapshotInfo reports the last-synced version and sync time for a blob
// type, for status displays (e.g. the web UI's sync widget). ok is false
// when the blob has never been synced on this machine.
func SnapshotInfo(blob BlobType) (version int, syncedAt time.Time, ok bool) {
	path, err := SnapshotPath(blob)
	if err != nil {
		return 0, time.Time{}, false
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, time.Time{}, false
	}
	return lastKnownVersion(path), info.ModTime(), true
}

func retryBackoff(attempt int) time.Duration {
	base := 250 * time.Millisecond
	return time.Duration(float64(base) * math.Pow(2, float64(attempt)))
}

func readLocalConfigFile(path string) (json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return json.RawMessage(`{}`), nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return json.RawMessage(`{}`), nil
	}
	return json.RawMessage(data), nil
}

// Push sends the local blob to the server, auto-merging and retrying on a
// version conflict. It never returns an error to the caller for a
// resolvable conflict — see spec §5. Retries are bounded; after
// exhausting them it logs and gives up for this cycle only.
func (c *Client) Push(ctx context.Context, blob BlobType, local json.RawMessage) error {
	token, ok, err := LoadToken()
	if err != nil {
		return fmt.Errorf("load sync token: %w", err)
	}
	if !ok {
		return nil // not logged in — nothing to do, not an error
	}

	snapshotPath, err := SnapshotPath(blob)
	if err != nil {
		return err
	}
	base := readSnapshot(snapshotPath)
	version := lastKnownVersion(snapshotPath)
	payload := local

	for attempt := 0; attempt < maxPushRetries; attempt++ {
		resp, err := c.pushOnce(ctx, token, blob, version, payload)
		if err != nil {
			return fmt.Errorf("push %s: %w", blob, err)
		}
		if resp.OK {
			writeSnapshot(snapshotPath, payload, resp.Version)
			return nil
		}

		merged, err := Merge(blob, base, payload, json.RawMessage(resp.CurrentBlob))
		if err != nil {
			log.Printf("sync: merge failed for %s, will retry next cycle: %v", blob, err)
			return nil
		}
		localPath, err := localConfigPathFor(blob)
		if err != nil {
			log.Printf("sync: resolve path for %s: %v", blob, err)
			return nil
		}
		os.MkdirAll(filepath.Dir(localPath), 0700)
		if err := writeLocalConfigFileAtomic(localPath, merged); err != nil {
			log.Printf("sync: writing merged %s to disk failed, will retry next cycle: %v", blob, err)
			return nil
		}
		// Refresh auth in-memory cache after direct disk write.
		if blob == BlobTypeAuth {
			if err := auth.LoadStore(); err != nil {
				log.Printf("sync: failed to refresh auth cache after writing %s: %v", blob, err)
			}
		}
		base = json.RawMessage(resp.CurrentBlob)
		version = resp.CurrentVersion
		payload = merged

		time.Sleep(retryBackoff(attempt))
	}

	log.Printf("sync: gave up pushing %s after %d attempts (persistent conflict), will retry next cycle", blob, maxPushRetries)
	return nil
}

func (c *Client) pushOnce(ctx context.Context, token string, blob BlobType, version int, data json.RawMessage) (*pushResponse, error) {
	body, err := c.jsonBody(map[string]interface{}{
		"version": version,
		"blob":    string(data),
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.BaseURL+"/api/ocode/sync/"+string(blob), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}

	if res.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(res.Body)
		res.Body.Close()
		return nil, fmt.Errorf("push returned status %d: %s", res.StatusCode, string(respBody))
	}

	var pr pushResponse
	if err := c.decode(res, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}
