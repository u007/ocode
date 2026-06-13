// Command gen-models-snapshot fetches the models.dev registry and writes a
// trimmed, embedded snapshot to internal/agent/models-snapshot.json.
//
// The snapshot is gitignored (a build-time artifact), so regenerate it with:
//
//	make models-snapshot
//
// It is projected down to the fields the agent.modelEntry/providerEntry structs
// read — keeping the binary lean while retaining cost (used for spend) plus
// props likely useful later. When you add a field to those structs, add it to
// modelKeys below and regenerate.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	source = "https://models.dev/api.json"
	dest   = "internal/agent/models-snapshot.json"
)

// modelKeys are the per-model fields retained in the snapshot. Keep in sync with
// agent.modelEntry's JSON tags.
var modelKeys = []string{
	"id", "name", "family", "attachment", "reasoning", "tool_call",
	"temperature", "knowledge", "release_date", "last_updated",
	"open_weights", "modalities", "limit", "cost",
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gen-models-snapshot:", err)
		os.Exit(1)
	}
}

func run() error {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(source)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", source, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned status %d", source, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32*1024*1024))
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	var live map[string]struct {
		ID     string                     `json:"id"`
		Models map[string]json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(body, &live); err != nil {
		return fmt.Errorf("parse %s: %w", source, err)
	}

	type provider struct {
		ID     string                     `json:"id"`
		Models map[string]json.RawMessage `json:"models"`
	}
	out := make(map[string]provider, len(live))
	for provID, pe := range live {
		id := pe.ID
		if id == "" {
			id = provID
		}
		models := make(map[string]json.RawMessage, len(pe.Models))
		for mid, raw := range pe.Models {
			var full map[string]json.RawMessage
			if err := json.Unmarshal(raw, &full); err != nil {
				return fmt.Errorf("parse model %s/%s: %w", provID, mid, err)
			}
			trimmed := make(map[string]json.RawMessage, len(modelKeys))
			for _, k := range modelKeys {
				if v, ok := full[k]; ok {
					trimmed[k] = v
				}
			}
			projected, err := json.Marshal(trimmed)
			if err != nil {
				return fmt.Errorf("marshal model %s/%s: %w", provID, mid, err)
			}
			models[mid] = projected
		}
		out[provID] = provider{ID: id, Models: models}
	}

	encoded, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	if err := os.WriteFile(dest, append(encoded, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", dest, err)
	}
	fmt.Printf("wrote %s (%d providers, %d bytes)\n", dest, len(out), len(encoded)+1)
	return nil
}
