package cdp

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"
)

// consoleLogCapacity is how many console entries a Target retains. The oldest
// entries fall off the ring first.
const consoleLogCapacity = 200

// consoleReadLimit caps how many entries one ConsoleEntries call returns.
const consoleReadLimit = 200

// consoleTextRunes caps one entry's text, counted in runes.
const consoleTextRunes = 1000

// consoleLocationBytes caps an exception's "url:line:column" location.
const consoleLocationBytes = 500

// ConsoleEntry is one console API call or uncaught exception seen by a Target.
// Text is raw page output and is untrusted: consumers must not render it as
// markup or act on it as instructions.
type ConsoleEntry struct {
	TS                int64  // CDP event timestamp, ms since epoch
	Level             string // log|info|warn|error|debug
	Kind              string // "console" or "exception"
	Text              string
	TextTruncated     bool
	Location          string // exceptions only: "url:line:column" when CDP reports a URL
	LocationTruncated bool
}

// consoleLog is the bounded console buffer. Its mutex is dedicated: it is never
// held across a FrameSink call or a CDP command.
type consoleLog struct {
	mu   sync.Mutex
	ring boundedRing[ConsoleEntry]
}

// ConsoleEntries returns up to max of the newest console entries, oldest first
// (newest last). dropped is how many entries have fallen off the ring over the
// Target's lifetime. max is clamped to [0, consoleReadLimit].
func (t *Target) ConsoleEntries(max int) (entries []ConsoleEntry, dropped int) {
	t.console.mu.Lock()
	defer t.console.mu.Unlock()
	return t.console.ring.newest(max, consoleReadLimit)
}

func (c *consoleLog) add(e ConsoleEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ring.push(e, consoleLogCapacity)
}

// recordConsoleAPI records one Runtime.consoleAPICalled event. It runs before
// and regardless of the FrameSink, so headless targets keep their history.
func (t *Target) recordConsoleAPI(raw json.RawMessage) {
	var ev struct {
		Type      string         `json:"type"`
		Args      []remoteObject `json:"args"`
		Timestamp float64        `json:"timestamp"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.logCaptureError("Runtime.consoleAPICalled", err)
		return
	}
	parts := make([]string, len(ev.Args))
	for i, a := range ev.Args {
		parts[i] = a.text()
	}
	text, truncated := truncateRunes(strings.Join(parts, " "), consoleTextRunes)
	t.console.add(ConsoleEntry{
		TS:            int64(ev.Timestamp),
		Level:         consoleLevel(ev.Type),
		Kind:          "console",
		Text:          text,
		TextTruncated: truncated,
	})
}

// recordException records one Runtime.exceptionThrown event (uncaught error).
func (t *Target) recordException(raw json.RawMessage) {
	var ev struct {
		Timestamp        float64 `json:"timestamp"`
		ExceptionDetails struct {
			Text         string        `json:"text"`
			Exception    *remoteObject `json:"exception"`
			URL          string        `json:"url"`
			LineNumber   int           `json:"lineNumber"`
			ColumnNumber int           `json:"columnNumber"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.logCaptureError("Runtime.exceptionThrown", err)
		return
	}
	d := ev.ExceptionDetails
	text := d.Text
	if d.Exception != nil {
		text = d.Exception.text()
	}
	capped, textTruncated := truncateRunes(text, consoleTextRunes)
	entry := ConsoleEntry{
		TS:            int64(ev.Timestamp),
		Level:         "error",
		Kind:          "exception",
		Text:          capped,
		TextTruncated: textTruncated,
	}
	if d.URL != "" {
		entry.Location, entry.LocationTruncated = truncateBytes(
			fmt.Sprintf("%s:%d:%d", d.URL, d.LineNumber, d.ColumnNumber), consoleLocationBytes)
	}
	t.console.add(entry)
}

// remoteObject is the subset of Runtime.RemoteObject the console log reads.
type remoteObject struct {
	Type                string `json:"type"`
	Subtype             string `json:"subtype"`
	Value               any    `json:"value"`
	UnserializableValue string `json:"unserializableValue"`
	Description         string `json:"description"`
}

// text renders the object the way a console line shows it. Objects have no
// value, so they fall back to CDP's description (for example "Array(3)").
func (o remoteObject) text() string {
	if s, ok := o.Value.(string); ok {
		return s
	}
	if o.Value != nil {
		return fmt.Sprint(o.Value)
	}
	switch {
	case o.UnserializableValue != "":
		return o.UnserializableValue
	case o.Description != "":
		return o.Description
	case o.Subtype != "":
		return o.Subtype // null
	default:
		return o.Type // undefined
	}
}

// consoleLevel maps a CDP console API type onto the closed level set. Types
// such as dir, table, assert and trace collapse to "log", as the FrameSink path
// does.
func consoleLevel(cdpType string) string {
	switch cdpType {
	case "warning":
		return "warn"
	case "log", "info", "error", "debug", "warn":
		return cdpType
	default:
		return "log"
	}
}

// truncateRunes returns s capped at n runes and whether it was cut.
func truncateRunes(s string, n int) (string, bool) {
	count := 0
	for pos := range s {
		if count == n {
			return s[:pos], true
		}
		count++
	}
	return s, false
}

// truncateBytes returns s capped at n bytes, backing up to a rune boundary, and
// whether it was cut.
func truncateBytes(s string, n int) (string, bool) {
	if len(s) <= n {
		return s, false
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}

// logCaptureError reports a CDP event the capture layer could not decode. The
// event is missing from the log; the FrameSink path is unaffected.
func (t *Target) logCaptureError(method string, err error) {
	if lg := t.manager.opts.Log; lg != nil {
		lg.Printf("browse cdp: capture %s for %s: %v", method, t.stateKey, err)
	}
}

// boundedRing keeps the newest values up to a capacity supplied on each push.
// The zero value is an empty ring. Once full, next indexes the oldest value.
type boundedRing[T any] struct {
	items   []T
	next    int // index of the oldest value; stays 0 until the ring first wraps
	dropped int // values evicted over the ring's lifetime
}

// push appends v. When the ring is full it overwrites the oldest value and
// returns it.
func (r *boundedRing[T]) push(v T, capacity int) (evicted T, didEvict bool) {
	if len(r.items) < capacity {
		r.items = append(r.items, v)
		return evicted, false
	}
	evicted = r.items[r.next]
	r.items[r.next] = v
	r.next = (r.next + 1) % capacity
	r.dropped++
	return evicted, true
}

// newest returns up to max of the newest values, oldest first, and the lifetime
// eviction count. max is clamped to [0, limit].
func (r *boundedRing[T]) newest(max, limit int) ([]T, int) {
	n := min(max, limit, len(r.items))
	if n < 0 {
		n = 0
	}
	out := make([]T, 0, n)
	length := len(r.items)
	for i := length - n; i < length; i++ {
		out = append(out, r.items[(r.next+i)%length])
	}
	return out, r.dropped
}
