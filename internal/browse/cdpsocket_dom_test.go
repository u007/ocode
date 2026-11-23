package browse

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// dialInspectorCDP attaches a browser-panel socket over a fake manager and
// drains the mandatory perfState frame, leaving the connection ready for
// cdp_request round-trips.
func dialInspectorCDP(t *testing.T, stateKey string) (*fakeManager, *websocket.Conn) {
	t.Helper()
	s, fake, ts := newBrowseWithFake(t, "http://example.com")
	grant := s.MintGrant(stateKey, "http://example.com")
	conn, _, err := (&websocket.Dialer{}).Dial(wsURL(ts, stateKey, grant), http.Header{"Origin": []string{"http://example.com"}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	drainAttachPerfState(t, conn)
	return fake, conn
}

func readCDPResponse(t *testing.T, conn *websocket.Conn, requestID string) map[string]any {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read %s response: %v", requestID, err)
	}
	var msg map[string]any
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("decode %s response %q: %v", requestID, data, err)
	}
	if msg["t"] != "cdp_response" || msg["requestId"] != requestID {
		t.Fatalf("response = %v, want cdp_response requestId %q", msg, requestID)
	}
	return msg
}

// TestCDP_GetDocumentRequestsWholeTree pins the inspector's tree fetch over the
// socket: DOM.getDocument reaches the target with depth -1 (the whole document
// in one reply) and its result is forwarded to the browser panel.
func TestCDP_GetDocumentRequestsWholeTree(t *testing.T) {
	fake, conn := dialInspectorCDP(t, "tab:inspector")

	if err := conn.WriteJSON(map[string]any{
		"t": "cdp_request", "method": "DOM.getDocument", "requestId": "doc:1",
	}); err != nil {
		t.Fatalf("write getDocument: %v", err)
	}
	msg := readCDPResponse(t, conn, "doc:1")
	if msg["error"] != nil {
		t.Fatalf("getDocument error = %v", msg["error"])
	}
	root, ok := msg["result"].(map[string]any)["root"].(map[string]any)
	if !ok || root["nodeName"] != "HTML" {
		t.Fatalf("getDocument result = %v, want root nodeName HTML", msg["result"])
	}

	depths := fake.targetFor("tab:inspector").docDepthCalls()
	if len(depths) != 1 || depths[0] != -1 {
		t.Fatalf("GetDocument depths = %v, want [-1]", depths)
	}
}

// TestCDP_GetBoxModelParsesNodeID pins both nodeId wire forms a caller may
// send: a JSON number (float64 after decoding) and a quoted string. Both must
// reach GetBoxModel as the integer the string encodes.
func TestCDP_GetBoxModelParsesNodeID(t *testing.T) {
	cases := []struct {
		name   string
		nodeID any
		want   int
	}{
		{"json number", float64(42), 42},
		{"string", "43", 43},
	}
	fake, conn := dialInspectorCDP(t, "tab:boxmodel")

	var want []int
	for _, tc := range cases {
		requestID := "box:" + strings.ReplaceAll(tc.name, " ", "-")
		if err := conn.WriteJSON(map[string]any{
			"t": "cdp_request", "method": "DOM.getBoxModel",
			"params": map[string]any{"nodeId": tc.nodeID}, "requestId": requestID,
		}); err != nil {
			t.Fatalf("write getBoxModel (%s): %v", tc.name, err)
		}
		msg := readCDPResponse(t, conn, requestID)
		if msg["error"] != nil {
			t.Fatalf("getBoxModel (%s) error = %v", tc.name, msg["error"])
		}
		if model, ok := msg["result"].(map[string]any)["model"].(map[string]any); !ok || model["width"] != float64(16) {
			t.Fatalf("getBoxModel (%s) result = %v, want model width 16", tc.name, msg["result"])
		}
		want = append(want, tc.want)
	}

	got := fake.targetFor("tab:boxmodel").boxModelNodeIDCalls()
	if len(got) != len(want) {
		t.Fatalf("GetBoxModel nodeIds = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("GetBoxModel nodeIds = %v, want %v", got, want)
		}
	}
}

// TestCDP_UnknownMethodStillRejected pins the fallback: a method the socket
// does not dispatch is reported as unknown_method, never silently dropped (a
// dropped reply would leave the inspector waiting forever).
func TestCDP_UnknownMethodStillRejected(t *testing.T) {
	_, conn := dialInspectorCDP(t, "tab:unknown")

	if err := conn.WriteJSON(map[string]any{
		"t": "cdp_request", "method": "DOM.nope", "requestId": "bad:1",
	}); err != nil {
		t.Fatalf("write unknown method: %v", err)
	}
	msg := readCDPResponse(t, conn, "bad:1")
	errStr, _ := msg["error"].(string)
	if !strings.HasPrefix(errStr, "unknown_method: ") {
		t.Fatalf("error = %q, want unknown_method prefix", errStr)
	}
	if !strings.HasSuffix(errStr, "DOM.nope") {
		t.Fatalf("error = %q, want it to name DOM.nope", errStr)
	}
}

// TestCDP_PushNodesByBackendIDsMapsToNodeIDs pins the backendNodeId → nodeId
// mapping over the socket: the ids reach the target as integers and the
// target's nodeIds reply is forwarded for the SPA to read.
func TestCDP_PushNodesByBackendIDsMapsToNodeIDs(t *testing.T) {
	fake, conn := dialInspectorCDP(t, "tab:push")

	if err := conn.WriteJSON(map[string]any{
		"t": "cdp_request", "method": "DOM.pushNodesByBackendIdsToFrontend",
		"params": map[string]any{"backendNodeIds": []any{float64(8)}}, "requestId": "push:1",
	}); err != nil {
		t.Fatalf("write push: %v", err)
	}
	msg := readCDPResponse(t, conn, "push:1")
	if msg["error"] != nil {
		t.Fatalf("push error = %v", msg["error"])
	}
	nodeIDs, ok := msg["result"].(map[string]any)["nodeIds"].([]any)
	if !ok || len(nodeIDs) != 1 || nodeIDs[0] != float64(108) {
		t.Fatalf("push result = %v, want nodeIds [108]", msg["result"])
	}

	calls := fake.targetFor("tab:push").pushCallsSnapshot()
	if len(calls) != 1 || len(calls[0]) != 1 || calls[0][0] != 8 {
		t.Fatalf("push calls = %v, want [[8]]", calls)
	}
}

// TestCDP_PushNodesRejectsMalformedIDs pins the validation: anything other
// than a non-empty array of positive integers is an error reply, and the
// target is never called with a 0, an empty list or a fractional id.
func TestCDP_PushNodesRejectsMalformedIDs(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]any
	}{
		{"missing", map[string]any{}},
		{"empty array", map[string]any{"backendNodeIds": []any{}}},
		{"zero", map[string]any{"backendNodeIds": []any{float64(0)}}},
		{"negative", map[string]any{"backendNodeIds": []any{float64(-3)}}},
		{"fractional", map[string]any{"backendNodeIds": []any{1.5}}},
		{"string", map[string]any{"backendNodeIds": []any{"8"}}},
		{"not an array", map[string]any{"backendNodeIds": float64(8)}},
	}
	fake, conn := dialInspectorCDP(t, "tab:push-bad")
	target := fake.targetFor("tab:push-bad")

	for _, tc := range cases {
		requestID := "push-bad:" + strings.ReplaceAll(tc.name, " ", "-")
		if err := conn.WriteJSON(map[string]any{
			"t": "cdp_request", "method": "DOM.pushNodesByBackendIdsToFrontend",
			"params": tc.params, "requestId": requestID,
		}); err != nil {
			t.Fatalf("write push (%s): %v", tc.name, err)
		}
		msg := readCDPResponse(t, conn, requestID)
		if msg["error"] == nil {
			t.Fatalf("push (%s) result = %v, want an error", tc.name, msg["result"])
		}
	}
	if calls := target.pushCallsSnapshot(); len(calls) != 0 {
		t.Fatalf("target reached for malformed input: %v", calls)
	}
}

// TestCDP_DescribeNodeForwardsBackendNodeID pins the context menu's describe by
// backendNodeId: the id reaches DescribeNode in opts, and a malformed one is
// an error rather than a silent describe of nothing.
func TestCDP_DescribeNodeForwardsBackendNodeID(t *testing.T) {
	_, conn := dialInspectorCDP(t, "tab:describe-backend")

	if err := conn.WriteJSON(map[string]any{
		"t": "cdp_request", "method": "DOM.describeNode",
		"params": map[string]any{"backendNodeId": float64(8)}, "requestId": "desc:1",
	}); err != nil {
		t.Fatalf("write describe: %v", err)
	}
	msg := readCDPResponse(t, conn, "desc:1")
	if msg["error"] != nil {
		t.Fatalf("describe error = %v", msg["error"])
	}
	node, _ := msg["result"].(map[string]any)["node"].(map[string]any)
	opts, _ := node["opts"].(map[string]any)
	if opts["backendNodeId"] != float64(8) {
		t.Fatalf("describe opts = %v, want backendNodeId 8", node["opts"])
	}

	if err := conn.WriteJSON(map[string]any{
		"t": "cdp_request", "method": "DOM.describeNode",
		"params": map[string]any{"backendNodeId": float64(-1)}, "requestId": "desc:2",
	}); err != nil {
		t.Fatalf("write bad describe: %v", err)
	}
	if msg := readCDPResponse(t, conn, "desc:2"); msg["error"] == nil {
		t.Fatalf("describe with backendNodeId -1 = %v, want an error", msg["result"])
	}
}
