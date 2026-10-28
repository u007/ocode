package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"
	"testing"
)

// TestRefTableCapsGrowthAndNeverReusesRefs: the table stops growing at maxRefs.
// Minting one more node clears it, so every held ref is unknown while the new
// ref works and no ref string repeats.
func TestRefTableCapsGrowthAndNeverReusesRefs(t *testing.T) {
	var refs RefTable
	held := make([]string, 0, maxRefs)
	for i := 0; i < maxRefs; i++ {
		held = append(held, refs.Mint(int64(i+1)))
	}
	if id, err := refs.Lookup(held[0]); err != nil || id != 1 {
		t.Fatalf("Lookup(%s) = %d, %v; want 1", held[0], id, err)
	}
	// Re-minting a node already in the table at the cap must not clear it.
	if again := refs.Mint(1); again != held[0] {
		t.Fatalf("re-mint at the cap = %q, want the existing %q", again, held[0])
	}
	if _, err := refs.Lookup(held[len(held)-1]); err != nil {
		t.Fatalf("re-mint at the cap dropped an existing ref: %v", err)
	}

	fresh := refs.Mint(int64(maxRefs + 5))
	if _, err := refs.Lookup(held[len(held)-1]); err == nil || !strings.Contains(err.Error(), "unknown ref") {
		t.Fatalf("Lookup(old ref after the cap clear) err = %v, want unknown ref", err)
	}
	if id, err := refs.Lookup(fresh); err != nil || id != int64(maxRefs+5) {
		t.Fatalf("Lookup(%s) = %d, %v; want the new node", fresh, id, err)
	}
	want := "@e" + fmt.Sprint(maxRefs+1)
	if fresh != want {
		t.Fatalf("fresh ref = %q, want %q (the counter must not reset)", fresh, want)
	}
	for _, old := range held {
		if old == fresh {
			t.Fatalf("ref %q was reused after the clear", fresh)
		}
	}
}

// recordTestRequest records one requestWillBeSent through the real handler code.
func recordTestRequest(tgt *Target, id string) {
	tgt.recordRequestWillBeSent(json.RawMessage(fmt.Sprintf(
		`{"requestId":%q,"type":"Document","request":{"url":"https://example.com/%s","method":"GET"}}`, id, id)))
}

// TestNetworkTerminalEventsDoNotCreateEntries: loadingFinished/loadingFailed
// arrive on their own goroutines and can be processed for a requestId that was
// never recorded; they must be a no-op, not a phantom empty-URL record. A
// response for an unknown id MAY create the record (it can be the first event
// processed for that id), but only with identifying data.
func TestNetworkTerminalEventsDoNotCreateEntries(t *testing.T) {
	tgt := &Target{}
	recordTestRequest(tgt, "real")

	tgt.recordLoadingFinished(json.RawMessage(`{"requestId":"ghost2"}`))
	tgt.recordLoadingFailed(json.RawMessage(`{"requestId":"ghost3","type":"Fetch","errorText":"boom"}`))

	entries, dropped := tgt.NetworkEntries(networkReadLimit)
	if dropped != 0 {
		t.Fatalf("dropped = %d, want 0", dropped)
	}
	if len(entries) != 1 || entries[0].RequestID != "real" {
		t.Fatalf("entries = %+v, want only the recorded request", entries)
	}
	if entries[0].URL != "https://example.com/real" || entries[0].Status != 0 || entries[0].Finished {
		t.Fatalf("a ghost event mutated the real record: %+v", entries[0])
	}

	// A response can be the first event for an id; it must still carry a URL.
	tgt.recordResponseReceived(json.RawMessage(`{"requestId":"ghost","type":"Fetch","response":{"url":"https://example.com/ghost","status":200}}`))
	entries, _ = tgt.NetworkEntries(networkReadLimit)
	var ghost *NetworkEntry
	for i := range entries {
		if entries[i].RequestID == "ghost" {
			ghost = &entries[i]
		}
	}
	if ghost == nil {
		t.Fatal("a response for an unknown id created no record")
	}
	if ghost.URL != "https://example.com/ghost" || ghost.Status != 200 {
		t.Fatalf("response-created record = %+v, want the URL and status from the response", *ghost)
	}
}

// TestNetworkTerminalEventsDoNotEvictRealEntries: at the ring's capacity a
// phantom record would evict a real request; a terminal event for an unknown id
// must leave the ring untouched.
func TestNetworkTerminalEventsDoNotEvictRealEntries(t *testing.T) {
	tgt := &Target{}
	for i := 0; i < networkLogCapacity; i++ {
		recordTestRequest(tgt, fmt.Sprintf("r%d", i))
	}
	for i := 0; i < 5; i++ {
		tgt.recordLoadingFinished(json.RawMessage(fmt.Sprintf(`{"requestId":"ghost-fin%d"}`, i)))
		tgt.recordLoadingFailed(json.RawMessage(fmt.Sprintf(`{"requestId":"ghost-fail%d","type":"Fetch","errorText":"boom"}`, i)))
	}

	entries, dropped := tgt.NetworkEntries(networkReadLimit)
	if dropped != 0 {
		t.Fatalf("dropped = %d, want 0: unknown-id terminal events evicted real records", dropped)
	}
	if len(entries) != networkLogCapacity {
		t.Fatalf("entries = %d, want %d", len(entries), networkLogCapacity)
	}
	for _, e := range entries {
		if e.URL == "" {
			t.Fatalf("a phantom empty-URL record reached the log: %+v", e)
		}
	}
}

// TestSnapshotRefusesATreeOverTheNodeBudget: the element and byte caps only run
// after the whole tree is buffered, so a tree wider than axNodeBudget is refused
// with a signalled error rather than half-read.
func TestSnapshotRefusesATreeOverTheNodeBudget(t *testing.T) {
	stub := newStubChrome()
	t.Cleanup(stub.Close)
	m := newTestManager(t, stub, func(NavEvent) {}, log.New(io.Discard, "", 0))
	t.Cleanup(func() { _ = m.Close(context.Background()) })

	tgt, _, err := m.EnsureHeadless(context.Background(), "tab:too-wide")
	if err != nil {
		t.Fatalf("EnsureHeadless: %v", err)
	}
	nodes := axRootWithChildren(axNodeBudget+1, "button", func(i int) string { return fmt.Sprintf("b%d", i) })
	stub.SetResult("Accessibility.getFullAXTree", map[string]any{"nodes": nodes})

	var refs RefTable
	text, minted, err := tgt.Snapshot(context.Background(), "", &refs)
	if err == nil {
		t.Fatalf("Snapshot over the budget succeeded with %d bytes; want an error", len(text))
	}
	for _, want := range []string{fmt.Sprint(len(nodes)), fmt.Sprint(axNodeBudget), "find or text"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
	if text != "" || len(minted) != 0 {
		t.Fatalf("a refused snapshot still returned text (%d bytes) and %d refs", len(text), len(minted))
	}
	if _, lerr := refs.Lookup("@e1"); lerr == nil {
		t.Fatal("a refused snapshot minted refs")
	}
}

// TestSnapshotPassesTheDepthBound: Chrome is asked to depth-limit the tree, so
// the response is bounded where it honours the parameter.
func TestSnapshotPassesTheDepthBound(t *testing.T) {
	stub := newStubChrome()
	t.Cleanup(stub.Close)
	m := newTestManager(t, stub, func(NavEvent) {}, log.New(io.Discard, "", 0))
	t.Cleanup(func() { _ = m.Close(context.Background()) })

	tgt, _, err := m.EnsureHeadless(context.Background(), "tab:depth")
	if err != nil {
		t.Fatalf("EnsureHeadless: %v", err)
	}
	stub.SetResult("Accessibility.getFullAXTree", map[string]any{
		"nodes": axRootWithChildren(2, "button", func(i int) string { return fmt.Sprintf("b%d", i) }),
	})
	var refs RefTable
	if _, _, err := tgt.Snapshot(context.Background(), "", &refs); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	calls := stub.CallsFor("Accessibility.getFullAXTree")
	if len(calls) != 1 {
		t.Fatalf("getFullAXTree calls = %d, want 1", len(calls))
	}
	if !strings.Contains(string(calls[0].Params), fmt.Sprintf(`"depth":%d`, axMaxDepth)) {
		t.Fatalf("getFullAXTree params = %s, want depth=%d", calls[0].Params, axMaxDepth)
	}
}
