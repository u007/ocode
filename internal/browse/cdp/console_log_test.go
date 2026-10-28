package cdp

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// newHeadlessCaptureTarget returns a sinkless target on a stub Chrome, so the
// capture buffers are tested without any FrameSink attached.
func newHeadlessCaptureTarget(t *testing.T, key string) (*Target, *stubChrome) {
	t.Helper()
	m, stub := newHeadlessTestManager(t)
	tgt, _, err := m.EnsureHeadless(context.Background(), key)
	if err != nil {
		t.Fatalf("EnsureHeadless: %v", err)
	}
	return tgt, stub
}

// waitCaptured polls cond until it holds. Handler goroutines consume injected
// events asynchronously, so tests wait for each event to land before the next.
func waitCaptured(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// consoleTotal is every console entry the target has recorded, kept or dropped.
func consoleTotal(tgt *Target) int {
	entries, dropped := tgt.ConsoleEntries(consoleReadLimit)
	return len(entries) + dropped
}

// injectConsole sends one event and waits until the target has recorded it.
// Pacing keeps the cross-goroutine handler order deterministic.
func injectConsole(t *testing.T, tgt *Target, stub *stubChrome, method string, params map[string]any) {
	t.Helper()
	before := consoleTotal(tgt)
	stub.InjectEvent(tgt.sessionID, method, params)
	waitCaptured(t, method, func() bool { return consoleTotal(tgt) > before })
}

func consoleAPIParams(level string, ts float64, args ...map[string]any) map[string]any {
	list := make([]any, len(args))
	for i, a := range args {
		list[i] = a
	}
	return map[string]any{"type": level, "args": list, "timestamp": ts}
}

func stringArg(s string) map[string]any {
	return map[string]any{"type": "string", "value": s}
}

func TestConsoleEntriesRecordsCallsAndExceptionsWithoutSink(t *testing.T) {
	tgt, stub := newHeadlessCaptureTarget(t, "tab:console")
	if tgt.sink != nil {
		t.Fatal("headless target has a FrameSink")
	}

	injectConsole(t, tgt, stub, "Runtime.consoleAPICalled", consoleAPIParams("log", 1000,
		stringArg("hello"),
		map[string]any{"type": "number", "value": 42, "description": "42"},
	))
	injectConsole(t, tgt, stub, "Runtime.consoleAPICalled", consoleAPIParams("warning", 1001,
		map[string]any{"type": "undefined"},
	))
	injectConsole(t, tgt, stub, "Runtime.exceptionThrown", map[string]any{
		"timestamp": 1002.0,
		"exceptionDetails": map[string]any{
			"text":         "Uncaught",
			"exception":    map[string]any{"type": "object", "subtype": "error", "description": "Error: boom"},
			"url":          "https://example.com/app.js",
			"lineNumber":   7,
			"columnNumber": 3,
		},
	})

	entries, dropped := tgt.ConsoleEntries(10)
	if dropped != 0 {
		t.Fatalf("dropped = %d, want 0", dropped)
	}
	want := []ConsoleEntry{
		{TS: 1000, Level: "log", Kind: "console", Text: "hello 42"},
		{TS: 1001, Level: "warn", Kind: "console", Text: "undefined"},
		{TS: 1002, Level: "error", Kind: "exception", Text: "Error: boom", Location: "https://example.com/app.js:7:3"},
	}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("entries =\n%+v\nwant\n%+v", entries, want)
	}
}

func TestConsoleEntriesReturnsNewestLimitedAndClamped(t *testing.T) {
	tgt, stub := newHeadlessCaptureTarget(t, "tab:ring")
	total := consoleLogCapacity + 5
	for i := 0; i < total; i++ {
		injectConsole(t, tgt, stub, "Runtime.consoleAPICalled", consoleAPIParams("log", float64(i), stringArg(fmt.Sprintf("m%d", i))))
	}

	all, dropped := tgt.ConsoleEntries(consoleReadLimit)
	if dropped != 5 {
		t.Fatalf("dropped = %d, want 5", dropped)
	}
	if len(all) != consoleLogCapacity {
		t.Fatalf("len = %d, want %d", len(all), consoleLogCapacity)
	}
	if all[0].Text != "m5" || all[len(all)-1].Text != "m204" {
		t.Fatalf("ring ends = %q..%q, want m5..m204", all[0].Text, all[len(all)-1].Text)
	}

	last3, dropped := tgt.ConsoleEntries(3)
	if dropped != 5 || len(last3) != 3 {
		t.Fatalf("ConsoleEntries(3) = %d entries, dropped %d", len(last3), dropped)
	}
	for i, want := range []string{"m202", "m203", "m204"} {
		if last3[i].Text != want {
			t.Fatalf("last3[%d] = %q, want %q", i, last3[i].Text, want)
		}
	}

	if none, _ := tgt.ConsoleEntries(0); len(none) != 0 {
		t.Fatalf("ConsoleEntries(0) returned %d entries", len(none))
	}
	if none, _ := tgt.ConsoleEntries(-4); len(none) != 0 {
		t.Fatalf("ConsoleEntries(-4) returned %d entries", len(none))
	}
	if clamped, _ := tgt.ConsoleEntries(consoleReadLimit + 1000); len(clamped) != consoleLogCapacity {
		t.Fatalf("ConsoleEntries(over limit) returned %d entries, want %d", len(clamped), consoleLogCapacity)
	}
}

func TestConsoleLongMessageIsTruncatedWithFlag(t *testing.T) {
	tgt, stub := newHeadlessCaptureTarget(t, "tab:long")
	// Multi-byte runes make a byte cut and a rune cut differ; the cap is in runes.
	long := strings.Repeat("é", consoleTextRunes+50)
	injectConsole(t, tgt, stub, "Runtime.consoleAPICalled", consoleAPIParams("log", 1, stringArg(long)))
	injectConsole(t, tgt, stub, "Runtime.consoleAPICalled", consoleAPIParams("log", 2, stringArg("short")))

	entries, _ := tgt.ConsoleEntries(10)
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if got := utf8.RuneCountInString(entries[0].Text); got != consoleTextRunes {
		t.Fatalf("truncated text runes = %d, want %d", got, consoleTextRunes)
	}
	if !utf8.ValidString(entries[0].Text) {
		t.Fatal("truncation split a rune")
	}
	if !entries[0].TextTruncated {
		t.Fatal("TextTruncated = false for a capped message")
	}
	if entries[1].Text != "short" || entries[1].TextTruncated {
		t.Fatalf("short entry = %+v, want untruncated", entries[1])
	}
}

func TestConsoleCaptureKeepsSinkForwarding(t *testing.T) {
	m, stub := newHeadlessTestManager(t)
	sink := &testSink{}
	tgt, err := m.Attach(context.Background(), "tab:sinked", sink)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	stub.InjectEvent(tgt.sessionID, "Runtime.consoleAPICalled", consoleAPIParams("error", 5, stringArg("to sink")))
	waitCaptured(t, "sink console", func() bool { return len(sink.getConsoles()) == 1 })

	if got := sink.getConsoles()[0]; got.Level != "error" || len(got.Args) != 1 || got.Args[0] != "to sink" {
		t.Fatalf("sink event = %+v", got)
	}
	entries, _ := tgt.ConsoleEntries(10)
	if len(entries) != 1 || entries[0].Text != "to sink" {
		t.Fatalf("recorded entries = %+v", entries)
	}
}

func TestConsoleTruncateRunesAndBytes(t *testing.T) {
	if got, cut := truncateRunes("aéb", 2); got != "aé" || !cut {
		t.Fatalf("truncateRunes = (%q, %v), want (aé, true)", got, cut)
	}
	if got, cut := truncateRunes("ab", 2); got != "ab" || cut {
		t.Fatalf("truncateRunes at exact length = (%q, %v)", got, cut)
	}
	// "é" is two bytes; a 5-byte cap must back up to the boundary at 4.
	if got, cut := truncateBytes(strings.Repeat("é", 5), 5); got != "éé" || !cut {
		t.Fatalf("truncateBytes = (%q, %v), want (éé, true)", got, cut)
	}
}

func TestBoundedRingWrapsCountsDropsAndClamps(t *testing.T) {
	var r boundedRing[int]
	for i := 0; i < 2; i++ {
		if _, evicted := r.push(i, 3); evicted {
			t.Fatalf("push %d evicted before the ring was full", i)
		}
	}
	if evicted, did := r.push(2, 3); did || evicted != 0 {
		t.Fatalf("push filling the ring = (%d, %v)", evicted, did)
	}
	if evicted, did := r.push(3, 3); !did || evicted != 0 {
		t.Fatalf("first overwrite = (%d, %v), want (0, true)", evicted, did)
	}
	r.push(4, 3)

	got, dropped := r.newest(10, 10)
	if !reflect.DeepEqual(got, []int{2, 3, 4}) || dropped != 2 {
		t.Fatalf("newest(10) = %v dropped %d, want [2 3 4] dropped 2", got, dropped)
	}
	if got, _ := r.newest(2, 10); !reflect.DeepEqual(got, []int{3, 4}) {
		t.Fatalf("newest(2) = %v, want [3 4]", got)
	}
	if got, _ := r.newest(10, 1); !reflect.DeepEqual(got, []int{4}) {
		t.Fatalf("newest with limit 1 = %v, want [4]", got)
	}
	if got, _ := r.newest(0, 10); len(got) != 0 {
		t.Fatalf("newest(0) = %v, want empty", got)
	}
	var empty boundedRing[int]
	if got, dropped := empty.newest(5, 5); len(got) != 0 || dropped != 0 {
		t.Fatalf("empty ring newest = %v dropped %d", got, dropped)
	}
}
