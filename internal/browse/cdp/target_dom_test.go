package cdp

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestTargetGetDocumentSendsDepthAndReturnsDecodedMap pins the DOM inspector's
// tree fetch: the whole document in one reply (depth -1), pierce off, and the
// reply's raw wire shape forwarded to the caller.
func TestTargetGetDocumentSendsDepthAndReturnsDecodedMap(t *testing.T) {
	tgt, stub := newAgentTestTarget(t)
	stub.SetResult("DOM.getDocument", map[string]any{
		"root": map[string]any{"nodeId": 1, "nodeName": "HTML", "childNodeCount": 2},
	})

	got, err := tgt.GetDocument(context.Background(), -1)
	if err != nil {
		t.Fatalf("GetDocument: %v", err)
	}
	root, ok := got["root"].(map[string]any)
	if !ok {
		t.Fatalf("result root = %#v, want decoded map", got["root"])
	}
	if root["nodeName"] != "HTML" || root["nodeId"] != float64(1) {
		t.Errorf("root = %#v, want nodeName HTML nodeId 1", root)
	}

	calls := stub.CallsFor("DOM.getDocument")
	if len(calls) != 1 {
		t.Fatalf("DOM.getDocument calls = %d, want 1", len(calls))
	}
	if calls[0].SessionID != "sess-agent" {
		t.Errorf("session = %q, want sess-agent", calls[0].SessionID)
	}
	var params map[string]any
	if err := json.Unmarshal(calls[0].Params, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if params["depth"] != float64(-1) {
		t.Errorf("depth = %#v, want -1", params["depth"])
	}
	if params["pierce"] != false {
		t.Errorf("pierce = %#v, want false", params["pierce"])
	}
}

// TestTargetGetBoxModelSendsNodeID pins the highlight's input: DOM.getBoxModel
// addressed by the nodeId the inspector resolved from the tree.
func TestTargetGetBoxModelSendsNodeID(t *testing.T) {
	tgt, stub := newAgentTestTarget(t)
	stub.SetResult("DOM.getBoxModel", map[string]any{
		"model": map[string]any{
			"content": []any{1, 2, 3, 4, 5, 6, 7, 8},
			"width":   16,
			"height":  8,
		},
	})

	got, err := tgt.GetBoxModel(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetBoxModel: %v", err)
	}
	model, ok := got["model"].(map[string]any)
	if !ok {
		t.Fatalf("result model = %#v, want decoded map", got["model"])
	}
	if model["width"] != float64(16) {
		t.Errorf("model width = %#v, want 16", model["width"])
	}

	calls := stub.CallsFor("DOM.getBoxModel")
	if len(calls) != 1 {
		t.Fatalf("DOM.getBoxModel calls = %d, want 1", len(calls))
	}
	var params map[string]any
	if err := json.Unmarshal(calls[0].Params, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if params["nodeId"] != float64(42) {
		t.Errorf("nodeId = %#v, want 42", params["nodeId"])
	}
}

// TestTargetDOMErrorsMapToSharedVocabulary pins domError: a deadline becomes
// ErrTimeout, a closed conn ErrDisconnected, and a CDP protocol reply
// ErrProtocolError — the same vocabulary GetNodeForLocation already uses, so
// the SPA can branch on one set of strings for every DOM.* call.
func TestTargetDOMErrorsMapToSharedVocabulary(t *testing.T) {
	type call func(context.Context, *Target) error
	calls := map[string]call{
		"GetDocument": func(ctx context.Context, tgt *Target) error {
			_, err := tgt.GetDocument(ctx, -1)
			return err
		},
		"GetBoxModel": func(ctx context.Context, tgt *Target) error {
			_, err := tgt.GetBoxModel(ctx, 7)
			return err
		},
		"PushNodesByBackendIdsToFrontend": func(ctx context.Context, tgt *Target) error {
			_, err := tgt.PushNodesByBackendIdsToFrontend(ctx, []int{7})
			return err
		},
	}

	t.Run("timeout", func(t *testing.T) {
		for name, run := range calls {
			t.Run(name, func(t *testing.T) {
				tgt, stub := newAgentTestTarget(t)
				gate := make(chan struct{})
				t.Cleanup(func() { close(gate) })
				stub.SetReplyGate(func(string, json.RawMessage) <-chan struct{} { return gate })

				ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
				defer cancel()
				err := run(ctx, tgt)
				if err == nil || err.Error() != ErrTimeout {
					t.Fatalf("err = %v, want %q", err, ErrTimeout)
				}
			})
		}
	})

	t.Run("disconnected", func(t *testing.T) {
		for name, run := range calls {
			t.Run(name, func(t *testing.T) {
				tgt, stub := newAgentTestTarget(t)
				stub.Close()
				err := run(context.Background(), tgt)
				if err == nil || err.Error() != ErrDisconnected {
					t.Fatalf("err = %v, want %q", err, ErrDisconnected)
				}
			})
		}
	})

	t.Run("protocol_error", func(t *testing.T) {
		const msg = "Node with given id does not belong to the document"
		for name, run := range calls {
			method := map[string]string{
				"GetDocument":                     "DOM.getDocument",
				"GetBoxModel":                     "DOM.getBoxModel",
				"PushNodesByBackendIdsToFrontend": "DOM.pushNodesByBackendIdsToFrontend",
			}[name]
			t.Run(name, func(t *testing.T) {
				tgt, stub := newAgentTestTarget(t)
				stub.SetFailure(method, msg)
				err := run(context.Background(), tgt)
				if err == nil {
					t.Fatal("call succeeded despite a protocol failure")
				}
				if !strings.HasPrefix(err.Error(), ErrProtocolError+": "+msg) {
					t.Fatalf("err = %q, want prefix %q", err, ErrProtocolError+": "+msg)
				}
			})
		}
	})
}

// TestTargetPushNodesByBackendIDsSendsIDsAndReturnsNodeIDs pins the map from a
// stable backendNodeId to the current document's nodeId: the request carries
// the backend ids and the raw nodeIds reply is forwarded for the SPA to read.
func TestTargetPushNodesByBackendIDsSendsIDsAndReturnsNodeIDs(t *testing.T) {
	tgt, stub := newAgentTestTarget(t)
	stub.SetResult("DOM.pushNodesByBackendIdsToFrontend", map[string]any{
		"nodeIds": []any{42},
	})

	got, err := tgt.PushNodesByBackendIdsToFrontend(context.Background(), []int{7})
	if err != nil {
		t.Fatalf("PushNodesByBackendIdsToFrontend: %v", err)
	}
	ids, ok := got["nodeIds"].([]any)
	if !ok || len(ids) != 1 || ids[0] != float64(42) {
		t.Fatalf("nodeIds = %#v, want [42]", got["nodeIds"])
	}

	calls := stub.CallsFor("DOM.pushNodesByBackendIdsToFrontend")
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	var params map[string]any
	if err := json.Unmarshal(calls[0].Params, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	want := []any{float64(7)}
	if !reflect.DeepEqual(params["backendNodeIds"], want) {
		t.Errorf("backendNodeIds = %#v, want %#v", params["backendNodeIds"], want)
	}
}

// TestTargetGetNodeForLocationKeepsBackendNodeIDWithoutNodeID pins the first
// right-click: Chrome omits nodeId until a document is requested, but the
// location is still valid through backendNodeId.
func TestTargetGetNodeForLocationKeepsBackendNodeIDWithoutNodeID(t *testing.T) {
	tgt, stub := newAgentTestTarget(t)
	stub.SetResult("DOM.getNodeForLocation", map[string]any{"backendNodeId": 9})

	loc, err := tgt.GetNodeForLocation(context.Background(), 3, 4)
	if err != nil {
		t.Fatalf("GetNodeForLocation: %v", err)
	}
	if loc.NodeId != 0 || loc.BackendNodeId != 9 {
		t.Errorf("location = %+v, want nodeId 0 backendNodeId 9", loc)
	}
}

// TestTargetGetNodeForLocationWithoutAnyIDIsNoNode keeps the no-node verdict:
// a location with neither id is still ErrNoNode.
func TestTargetGetNodeForLocationWithoutAnyIDIsNoNode(t *testing.T) {
	tgt, stub := newAgentTestTarget(t)
	stub.SetResult("DOM.getNodeForLocation", map[string]any{})

	_, err := tgt.GetNodeForLocation(context.Background(), 3, 4)
	if err == nil || err.Error() != ErrNoNode {
		t.Fatalf("err = %v, want %q", err, ErrNoNode)
	}
}

// TestTargetDescribeNodeByBackendIDOmitsNodeID pins the describe-by-backend
// path: a zero nodeId is not sent alongside the backendNodeId.
func TestTargetDescribeNodeByBackendIDOmitsNodeID(t *testing.T) {
	tgt, stub := newAgentTestTarget(t)
	stub.SetResult("DOM.describeNode", map[string]any{"node": map[string]any{"nodeName": "A"}})

	if _, err := tgt.DescribeNode(context.Background(), 0, map[string]any{"backendNodeId": 9}); err != nil {
		t.Fatalf("DescribeNode: %v", err)
	}
	calls := stub.CallsFor("DOM.describeNode")
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	var params map[string]any
	if err := json.Unmarshal(calls[0].Params, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if _, has := params["nodeId"]; has {
		t.Errorf("params = %#v, want no nodeId", params)
	}
	if params["backendNodeId"] != float64(9) {
		t.Errorf("backendNodeId = %#v, want 9", params["backendNodeId"])
	}
}
