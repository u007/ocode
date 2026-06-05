package ide

import (
	"encoding/json"
	"testing"
)

func TestRPCOutOmitsNilParams(t *testing.T) {
	got, err := json.Marshal(rpcOut{JSONRPC: "2.0", Method: "notifications/initialized"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(got, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := payload["params"]; ok {
		t.Fatalf("params field present in %s", string(got))
	}
	if payload["jsonrpc"] != "2.0" {
		t.Fatalf("jsonrpc = %v, want 2.0", payload["jsonrpc"])
	}
	if payload["method"] != "notifications/initialized" {
		t.Fatalf("method = %v, want notifications/initialized", payload["method"])
	}
}

func TestRPCOutKeepsEmptyParamsObject(t *testing.T) {
	got, err := json.Marshal(rpcOut{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: map[string]any{}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(got, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	params, ok := payload["params"].(map[string]any)
	if !ok {
		t.Fatalf("params field missing or wrong type in %s", string(got))
	}
	if len(params) != 0 {
		t.Fatalf("params = %#v, want empty object", params)
	}
}
