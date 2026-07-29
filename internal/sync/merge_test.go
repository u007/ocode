package sync

import (
	"encoding/json"
	"testing"
)

func mustJSON(t *testing.T, s string) json.RawMessage {
	t.Helper()
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("invalid test fixture JSON %q: %v", s, err)
	}
	return json.RawMessage(s)
}

func TestMergeNonConflictingKeysUnion(t *testing.T) {
	base := mustJSON(t, `{"a":1,"b":2}`)
	local := mustJSON(t, `{"a":1,"b":2,"c":3}`)
	remote := mustJSON(t, `{"a":1,"b":9}`)

	merged, err := Merge(BlobTypeConfig, base, local, remote)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	var out map[string]float64
	json.Unmarshal(merged, &out)
	if out["a"] != 1 {
		t.Errorf("expected a=1, got %v", out["a"])
	}
	if out["b"] != 9 {
		t.Errorf("expected b=9 (remote wins unchanged locally), got %v", out["b"])
	}
	if out["c"] != 3 {
		t.Errorf("expected c=3 (local addition), got %v", out["c"])
	}
}

func TestMergeLocalWinsOnConflictForConfig(t *testing.T) {
	base := mustJSON(t, `{"theme":"light","font":"sans"}`)
	local := mustJSON(t, `{"theme":"dark","font":"sans"}`)
	remote := mustJSON(t, `{"theme":"system","font":"serif"}`)

	merged, err := Merge(BlobTypeConfig, base, local, remote)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	var out map[string]string
	json.Unmarshal(merged, &out)
	if out["theme"] != "dark" {
		t.Errorf("expected theme=dark (local wins for config), got %v", out["theme"])
	}
}

func TestMergeRemoteWinsOnConflictForAuth(t *testing.T) {
	base := mustJSON(t, `{"key":"old","region":"us"}`)
	local := mustJSON(t, `{"key":"local-key","region":"us"}`)
	remote := mustJSON(t, `{"key":"remote-key","region":"eu"}`)

	merged, err := Merge(BlobTypeAuth, base, local, remote)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	var out map[string]string
	json.Unmarshal(merged, &out)
	if out["key"] != "remote-key" {
		t.Errorf("expected key=remote-key (remote wins for auth), got %v", out["key"])
	}
	if out["region"] != "eu" {
		t.Errorf("expected region=eu (remote change), got %v", out["region"])
	}
}

func TestMergeNewKeysInBothFollowPolicy(t *testing.T) {
	base := mustJSON(t, `{"a":1}`)
	local := mustJSON(t, `{"a":1,"localOnly":"local-val"}`)
	remote := mustJSON(t, `{"a":1,"remoteOnly":"remote-val"}`)

	// Config: local wins
	merged, err := Merge(BlobTypeConfig, base, local, remote)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	var out map[string]string
	json.Unmarshal(merged, &out)
	if out["localOnly"] != "local-val" {
		t.Errorf("expected localOnly='local-val' (local wins for config), got %v", out["localOnly"])
	}
	if out["remoteOnly"] != "remote-val" {
		t.Errorf("expected remoteOnly='remote-val', got %v", out["remoteOnly"])
	}

	// Auth: remote wins on conflict, but new local-only key is kept.
	merged2, err := Merge(BlobTypeAuth, base, local, remote)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	var out2 map[string]string
	json.Unmarshal(merged2, &out2)
	if out2["localOnly"] != "local-val" {
		t.Errorf("expected localOnly='local-val' for auth (new local key is kept), got %v", out2["localOnly"])
	}
	if out2["remoteOnly"] != "remote-val" {
		t.Errorf("expected remoteOnly='remote-val' for auth, got %v", out2["remoteOnly"])
	}
}

func TestMergeLocalOnlyAuthKeySurvivesWhenRemoteAbsent(t *testing.T) {
	// A brand new key in local that exists in neither base nor remote
	// must be preserved for BlobTypeAuth, not silently dropped.
	base := mustJSON(t, `{"a":1}`)
	local := mustJSON(t, `{"a":1,"newKey":"secret"}`)
	remote := mustJSON(t, `{"a":1}`)

	merged, err := Merge(BlobTypeAuth, base, local, remote)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	var out map[string]string
	json.Unmarshal(merged, &out)
	if out["newKey"] != "secret" {
		t.Errorf("expected newKey='secret' (local-only auth key must be kept), got %v", out["newKey"])
	}
}

func TestMergeHandlesEmptyBase(t *testing.T) {
	base := mustJSON(t, `{}`)
	local := mustJSON(t, `{"theme":"dark"}`)
	remote := mustJSON(t, `{}`)

	merged, err := Merge(BlobTypeConfig, base, local, remote)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	var out map[string]string
	json.Unmarshal(merged, &out)
	if out["theme"] != "dark" {
		t.Errorf("expected local-only key to survive an empty base, got %v", out)
	}
}

func TestMergeNilInputs(t *testing.T) {
	merged, err := Merge(BlobTypeConfig, nil, mustJSON(t, `{"a":1}`), nil)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	var out map[string]float64
	json.Unmarshal(merged, &out)
	if out["a"] != 1 {
		t.Errorf("expected a=1, got %v", out["a"])
	}
}
