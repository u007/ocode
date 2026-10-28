package cdp

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// injectNetwork sends one Network.* event and waits until done holds over the
// target's records, so the handler order is deterministic.
func injectNetwork(t *testing.T, tgt *Target, stub *stubChrome, method string, params map[string]any, done func([]NetworkEntry) bool) {
	t.Helper()
	stub.InjectEvent(tgt.sessionID, method, params)
	waitCaptured(t, method, func() bool {
		entries, _ := tgt.NetworkEntries(networkReadLimit)
		return done(entries)
	})
}

// netTotal is every request record the target has created, kept or dropped.
func netTotal(tgt *Target) int {
	entries, dropped := tgt.NetworkEntries(networkReadLimit)
	return len(entries) + dropped
}

func TestNetworkRequestResponseFinishedIsOneRecord(t *testing.T) {
	tgt, stub := newHeadlessCaptureTarget(t, "tab:net")
	const url = "https://example.com/api?page=2"
	injectNetwork(t, tgt, stub, "Network.requestWillBeSent", map[string]any{
		"requestId": "r1",
		"type":      "Fetch",
		"request": map[string]any{
			"url":      url,
			"method":   "POST",
			"headers":  map[string]any{"Authorization": "Bearer secret-token"},
			"postData": "secret-body",
		},
	}, func(e []NetworkEntry) bool { return len(e) == 1 })
	injectNetwork(t, tgt, stub, "Network.responseReceived", map[string]any{
		"requestId": "r1",
		"type":      "Fetch",
		"response": map[string]any{
			"url":     url,
			"status":  200,
			"headers": map[string]any{"Set-Cookie": "session=secret-cookie"},
		},
	}, func(e []NetworkEntry) bool { return len(e) == 1 && e[0].Status == 200 })
	injectNetwork(t, tgt, stub, "Network.loadingFinished", map[string]any{
		"requestId": "r1", "encodedDataLength": 12,
	}, func(e []NetworkEntry) bool { return len(e) == 1 && e[0].Finished })

	entries, dropped := tgt.NetworkEntries(10)
	if dropped != 0 {
		t.Fatalf("dropped = %d, want 0", dropped)
	}
	want := []NetworkEntry{{
		RequestID:    "r1",
		Method:       "POST",
		URL:          url,
		ResourceType: "Fetch",
		Status:       200,
		Finished:     true,
	}}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("entries =\n%+v\nwant\n%+v", entries, want)
	}
	// Headers and bodies must never reach the record, in any field.
	v := reflect.ValueOf(entries[0])
	for i := 0; i < v.NumField(); i++ {
		if s, ok := v.Field(i).Interface().(string); ok && strings.Contains(s, "secret") {
			t.Fatalf("field %s carries header or body data: %q", v.Type().Field(i).Name, s)
		}
	}
}

func TestNetworkFailedRequestRecordsFailure(t *testing.T) {
	tgt, stub := newHeadlessCaptureTarget(t, "tab:netfail")
	injectNetwork(t, tgt, stub, "Network.requestWillBeSent", map[string]any{
		"requestId": "r2", "type": "Script",
		"request": map[string]any{"url": "https://cdn.example.com/lib.js", "method": "GET"},
	}, func(e []NetworkEntry) bool { return len(e) == 1 })
	injectNetwork(t, tgt, stub, "Network.loadingFailed", map[string]any{
		"requestId": "r2", "type": "Script", "errorText": "net::ERR_CONNECTION_REFUSED",
	}, func(e []NetworkEntry) bool { return len(e) == 1 && e[0].Failed })

	entries, _ := tgt.NetworkEntries(10)
	want := NetworkEntry{
		RequestID:    "r2",
		Method:       "GET",
		URL:          "https://cdn.example.com/lib.js",
		ResourceType: "Script",
		Finished:     true,
		Failed:       true,
		Error:        "net::ERR_CONNECTION_REFUSED",
	}
	if len(entries) != 1 || !reflect.DeepEqual(entries[0], want) {
		t.Fatalf("entries = %+v, want [%+v]", entries, want)
	}
}

func TestNetworkLongURLIsCappedWithFlag(t *testing.T) {
	tgt, stub := newHeadlessCaptureTarget(t, "tab:netlong")
	long := "https://example.com/" + strings.Repeat("a", 600)
	injectNetwork(t, tgt, stub, "Network.requestWillBeSent", map[string]any{
		"requestId": "r3", "request": map[string]any{"url": long, "method": "GET"},
	}, func(e []NetworkEntry) bool { return len(e) == 1 })

	entries, _ := tgt.NetworkEntries(10)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if len(entries[0].URL) != networkURLBytes || !entries[0].URLTruncated {
		t.Fatalf("URL len %d truncated %v, want %d and true", len(entries[0].URL), entries[0].URLTruncated, networkURLBytes)
	}
}

func TestNetworkRingKeepsNewestAndReportsDropped(t *testing.T) {
	tgt, stub := newHeadlessCaptureTarget(t, "tab:netring")
	total := networkLogCapacity + 5
	for i := 0; i < total; i++ {
		id := fmt.Sprintf("r%d", i)
		injectNetwork(t, tgt, stub, "Network.requestWillBeSent", map[string]any{
			"requestId": id, "request": map[string]any{"url": "https://example.com/" + id, "method": "GET"},
		}, func([]NetworkEntry) bool { return netTotal(tgt) == i+1 })
	}

	entries, dropped := tgt.NetworkEntries(networkReadLimit)
	if dropped != 5 || len(entries) != networkLogCapacity {
		t.Fatalf("got %d entries, dropped %d; want %d and 5", len(entries), dropped, networkLogCapacity)
	}
	if entries[0].RequestID != "r5" || entries[len(entries)-1].RequestID != "r204" {
		t.Fatalf("ring ends = %s..%s, want r5..r204", entries[0].RequestID, entries[len(entries)-1].RequestID)
	}

	// The evicted record's requestId must leave the index, or a late event
	// would update a record nothing holds any more.
	tgt.network.mu.Lock()
	_, stillIndexed := tgt.network.byID["r0"]
	tgt.network.mu.Unlock()
	if stillIndexed {
		t.Fatal("evicted requestId r0 is still indexed")
	}
}

func TestNetworkEntryHasNoHeaderOrBodyFields(t *testing.T) {
	typ := reflect.TypeOf(NetworkEntry{})
	for i := 0; i < typ.NumField(); i++ {
		name := strings.ToLower(typ.Field(i).Name)
		for _, banned := range []string{"header", "cookie", "body", "postdata", "payload"} {
			if strings.Contains(name, banned) {
				t.Errorf("NetworkEntry has field %s, which must not hold %s", typ.Field(i).Name, banned)
			}
		}
	}
}

func TestNetworkCaptureKeepsSinkForwarding(t *testing.T) {
	m, stub := newHeadlessTestManager(t)
	sink := &testSink{}
	tgt, err := m.Attach(context.Background(), "tab:netsinked", sink)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	stub.InjectEvent(tgt.sessionID, "Network.requestWillBeSent", map[string]any{
		"requestId": "r9", "request": map[string]any{"url": "https://example.com/s", "method": "GET"},
	})
	waitCaptured(t, "request record", func() bool { return netTotal(tgt) == 1 })
	stub.InjectEvent(tgt.sessionID, "Network.loadingFinished", map[string]any{"requestId": "r9", "encodedDataLength": 3})
	waitCaptured(t, "sink network event", func() bool { return len(sink.getNetworks()) == 1 })

	entries, _ := tgt.NetworkEntries(10)
	if len(entries) != 1 || !entries[0].Finished || entries[0].URL != "https://example.com/s" {
		t.Fatalf("recorded entries = %+v", entries)
	}
}
