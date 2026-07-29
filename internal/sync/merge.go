package sync

import (
	"encoding/json"
	"fmt"
)

// Merge performs a top-level-key 3-way merge: keys changed only on one
// side since base are taken as-is; keys changed on both sides resolve per
// blobType's conflict policy (local-wins for config, remote-wins for
// secrets — see spec §5).
func Merge(blobType BlobType, base, local, remote json.RawMessage) (json.RawMessage, error) {
	baseMap, err := toMap(base)
	if err != nil {
		return nil, fmt.Errorf("decode base: %w", err)
	}
	localMap, err := toMap(local)
	if err != nil {
		return nil, fmt.Errorf("decode local: %w", err)
	}
	remoteMap, err := toMap(remote)
	if err != nil {
		return nil, fmt.Errorf("decode remote: %w", err)
	}

	merged := make(map[string]json.RawMessage, len(remoteMap))
	for k, v := range remoteMap {
		merged[k] = v
	}

	// Apply local-only changes (keys present in local but not in base, or
	// changed in local relative to base when remote hasn't changed them).
	for k, v := range localMap {
		_, inBase := baseMap[k]
		_, inRemote := remoteMap[k]

		if !inBase && !inRemote {
			// New key absent from both base and remote — keep local.
			merged[k] = v
			continue
		}

		if !inBase && inRemote {
			// Key added remotely but not locally — keep remote (already in merged).
			continue
		}

		if inBase && !inRemote {
			// Key deleted remotely — respect deletion (do not add to merged).
			delete(merged, k)
			continue
		}

		// Key exists in base and both sides.
		localChanged := !jsonEqual(baseMap[k], v)
		remoteChanged := !jsonEqual(baseMap[k], remoteMap[k])

		if localChanged && !remoteChanged {
			merged[k] = v // local-only change
		} else if remoteChanged && !localChanged {
			// remote-only change — keep remote (already in merged)
		} else if localChanged && remoteChanged {
			// Both sides changed — conflict policy
			switch blobType {
			case BlobTypeConfig:
				merged[k] = v // local wins
			case BlobTypeAuth:
				// remote wins — keep what's already in merged
			}
		}
		// Neither changed — keep remote (identical to base, already in merged)
	}

	return json.Marshal(merged)
}

func toMap(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return make(map[string]json.RawMessage), nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if m == nil {
		return make(map[string]json.RawMessage), nil
	}
	return m, nil
}

func jsonEqual(a, b json.RawMessage) bool {
	var av, bv interface{}
	_ = json.Unmarshal(a, &av)
	_ = json.Unmarshal(b, &bv)
	aj, _ := json.Marshal(av)
	bj, _ := json.Marshal(bv)
	return string(aj) == string(bj)
}
