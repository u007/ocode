package cdp

import (
	"encoding/json"
	"sync"
)

// networkLogCapacity is how many requests a Target retains. The oldest fall off
// the ring first.
const networkLogCapacity = 200

// networkReadLimit caps how many entries one NetworkEntries call returns.
const networkReadLimit = 200

// networkURLBytes caps a request URL in bytes.
const networkURLBytes = 500

// networkTokenBytes caps the method and resource type. Both are short CDP
// tokens; the cap only stops a hostile page from growing a record.
const networkTokenBytes = 32

// networkErrorBytes caps the failure text of a failed request.
const networkErrorBytes = 200

// NetworkEntry is one request a Target saw, deduplicated per CDP requestId. It
// carries no headers, cookies, or request/response bodies by design.
type NetworkEntry struct {
	RequestID      string
	Method         string
	URL            string
	URLTruncated   bool
	ResourceType   string // CDP resource type: Document, Script, Fetch, XHR, ...
	Status         int    // HTTP status; 0 until a response arrives
	Finished       bool   // a terminal event (loadingFinished or loadingFailed) arrived
	Failed         bool   // loadingFailed arrived
	Error          string // failure text when Failed
	ErrorTruncated bool
}

// networkLog is the bounded request buffer. byID maps a live requestId to its
// record so later events update that record instead of adding another one.
type networkLog struct {
	mu   sync.Mutex
	ring boundedRing[*NetworkEntry]
	byID map[string]*NetworkEntry
}

// NetworkEntries returns up to max of the newest request records, oldest first
// (newest last). dropped is how many records have fallen off the ring over the
// Target's lifetime. max is clamped to [0, networkReadLimit].
func (t *Target) NetworkEntries(max int) (entries []NetworkEntry, dropped int) {
	t.network.mu.Lock()
	defer t.network.mu.Unlock()
	var recs []*NetworkEntry
	recs, dropped = t.network.ring.newest(max, networkReadLimit)
	entries = make([]NetworkEntry, len(recs))
	for i, e := range recs {
		entries[i] = *e
	}
	return entries, dropped
}

// createEntry returns the record for requestID, creating it on first sight.
// Events can reach their handlers out of order (each Network.* event has its own
// goroutine), so a redirect reuses the record. Caller holds the log's mutex.
func (n *networkLog) createEntry(requestID string) *NetworkEntry {
	if e, ok := n.byID[requestID]; ok {
		return e
	}
	if n.byID == nil {
		n.byID = make(map[string]*NetworkEntry)
	}
	e := &NetworkEntry{RequestID: requestID}
	if evicted, didEvict := n.ring.push(e, networkLogCapacity); didEvict {
		delete(n.byID, evicted.RequestID)
	}
	n.byID[requestID] = e
	return e
}

// lookupEntry returns the existing record for requestID, or nil. Caller holds
// the log's mutex.
func (n *networkLog) lookupEntry(requestID string) *NetworkEntry {
	return n.byID[requestID]
}

// updateEntry applies update to the record for requestID under the log mutex.
// It never creates one: the terminal loading events can arrive for a request
// whose requestWillBeSent was never recorded (or has already fallen off the
// ring), and creating a record for them would push a phantom empty-URL entry
// that evicts a real one. Only requestWillBeSent and responseReceived create
// (see createOrUpdateEntry).
func (t *Target) updateEntry(requestID string, update func(e *NetworkEntry)) {
	t.network.mu.Lock()
	defer t.network.mu.Unlock()
	if e := t.network.lookupEntry(requestID); e != nil {
		update(e)
	}
}

// createOrUpdateEntry applies update to requestID's record, creating it on
// first sight.
//
// Two callers may legitimately be the FIRST event for an id, because each
// Network event is handled on its own goroutine and they can be processed out
// of order: requestWillBeSent (URL/method) and responseReceived (status, and a
// URL when the request event is late or was evicted). Both carry enough to
// identify a real request, so neither can produce an empty phantom row. The
// terminal events (loadingFinished/loadingFailed) carry no identifying data and
// must stay no-ops for an unknown id.
func (t *Target) createOrUpdateEntry(requestID string, update func(e *NetworkEntry)) {
	t.network.mu.Lock()
	defer t.network.mu.Unlock()
	update(t.network.createEntry(requestID))
}

// setURL stores u, capped at networkURLBytes.
//
// Redaction decision: the URL is kept whole apart from the cap, including its
// query string. The existing FrameSink path already sends full URLs to the UI,
// and redactHeaders covers headers only. A query can still carry a token, so
// the server tool layer is the place to decide whether to scrub it.
func setURL(e *NetworkEntry, u string) {
	e.URL, e.URLTruncated = truncateBytes(u, networkURLBytes)
}

// recordRequestWillBeSent records Network.requestWillBeSent, the primary
// creator of a record (a response for a request whose request event is late may
// create it first; see createOrUpdateEntry). A redirect reuses the requestId
// and sends a new hop, so the newest URL and method win.
func (t *Target) recordRequestWillBeSent(raw json.RawMessage) {
	var ev struct {
		RequestID string `json:"requestId"`
		Type      string `json:"type"`
		Request   struct {
			URL    string `json:"url"`
			Method string `json:"method"`
		} `json:"request"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.logCaptureError("Network.requestWillBeSent", err)
		return
	}
	t.createOrUpdateEntry(ev.RequestID, func(e *NetworkEntry) {
		if ev.Request.Method != "" {
			e.Method, _ = truncateBytes(ev.Request.Method, networkTokenBytes)
		}
		if ev.Type != "" {
			e.ResourceType, _ = truncateBytes(ev.Type, networkTokenBytes)
		}
		if ev.Request.URL != "" {
			setURL(e, ev.Request.URL)
		}
	})
}

// recordResponseReceived records Network.responseReceived. The response URL is
// used only when the request URL never arrived, so the request URL wins. It may
// create the record: a response can be the first event processed for its id.
func (t *Target) recordResponseReceived(raw json.RawMessage) {
	var ev struct {
		RequestID string `json:"requestId"`
		Type      string `json:"type"`
		Response  struct {
			URL    string `json:"url"`
			Status int    `json:"status"`
		} `json:"response"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.logCaptureError("Network.responseReceived", err)
		return
	}
	t.createOrUpdateEntry(ev.RequestID, func(e *NetworkEntry) {
		e.Status = ev.Response.Status
		if ev.Type != "" {
			e.ResourceType, _ = truncateBytes(ev.Type, networkTokenBytes)
		}
		if e.URL == "" && ev.Response.URL != "" {
			setURL(e, ev.Response.URL)
		}
	})
}

// recordLoadingFinished records Network.loadingFinished, the success terminal event.
func (t *Target) recordLoadingFinished(raw json.RawMessage) {
	var ev struct {
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.logCaptureError("Network.loadingFinished", err)
		return
	}
	t.updateEntry(ev.RequestID, func(e *NetworkEntry) {
		e.Finished = true
	})
}

// recordLoadingFailed records Network.loadingFailed, the failure terminal event.
// A failed request counts as Finished too: nothing more will arrive for it.
func (t *Target) recordLoadingFailed(raw json.RawMessage) {
	var ev struct {
		RequestID string `json:"requestId"`
		Type      string `json:"type"`
		ErrorText string `json:"errorText"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.logCaptureError("Network.loadingFailed", err)
		return
	}
	t.updateEntry(ev.RequestID, func(e *NetworkEntry) {
		e.Finished = true
		e.Failed = true
		e.Error, e.ErrorTruncated = truncateBytes(ev.ErrorText, networkErrorBytes)
		if ev.Type != "" {
			e.ResourceType, _ = truncateBytes(ev.Type, networkTokenBytes)
		}
	})
}
