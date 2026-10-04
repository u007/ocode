package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Remote event fan-in. A browser on a plain-HTTP origin (a share URL, `ocode
// serve`) is capped at six HTTP/1.1 connections per origin, and every SSE
// stream pins one for its whole life. The SPA therefore opens ONE /api/events
// stream and names the remote hosts it has tabs on as ?hosts=a,b; the server
// subscribes to each host's own /api/events and relays the frames down that
// single response, tagged with the originating host. See
// docs/gotchas/share-url-http1-connection-cap.md.

const (
	// hostStreamEvent is the control envelope the relay emits each time a
	// host's upstream stream opens. The client resets that host's seq
	// watermark on it and, from the second one on, reconciles — the same
	// contract a re-established direct stream had.
	hostStreamEvent = "host_stream"

	remoteEventsBackoffBase = time.Second
	remoteEventsBackoffMax  = 30 * time.Second

	// remoteEventsLiveness is how long an upstream stream may stay silent
	// before the relay treats it as dead and reconnects. The remote writes a
	// `: ping` every sseKeepaliveInterval (20s), so this is two missed pings.
	// The browser cannot detect a dead upstream itself: the local stream's
	// own pings keep its liveness timer armed. Tests may shorten it via the
	// registry's eventsLiveness field.
	remoteEventsLiveness = 45 * time.Second
)

// remoteEventsClient reads the long-lived upstream streams. No Timeout: a
// stream has no total-duration bound; each attempt is bounded by the request
// context and the liveness timer instead.
var remoteEventsClient = &http.Client{}

// relayableEventHosts keeps the requested hosts that may be relayed: the same
// admission the path-less remote proxy applies (the host must be the host of
// a saved project). Anything else is logged and skipped.
func (h *Handler) relayableEventHosts(hosts []string) []string {
	if len(hosts) == 0 {
		return nil
	}
	if h.remoteHosts == nil {
		log.Printf("events relay: remote connections not available; not relaying hosts %q", hosts)
		return nil
	}
	var out []string
	for _, host := range hosts {
		if _, ok := h.firstSavedProjectForHost(host); !ok {
			log.Printf("events relay: denied host %q — no saved projects", host)
			continue
		}
		out = append(out, host)
	}
	return out
}

// relayRemoteEvents keeps one upstream /api/events stream to host alive for
// the life of ctx, pushing every envelope (host-tagged) into out. A lost
// stream is retried with exponential backoff, reset once a stream opens.
func (h *Handler) relayRemoteEvents(ctx context.Context, host string, projects []string, out chan<- []byte) {
	delay := remoteEventsBackoffBase
	for {
		opened, err := h.streamRemoteEvents(ctx, host, projects, out)
		if ctx.Err() != nil {
			return
		}
		log.Printf("events relay: host %s stream lost: %v", host, err)
		if opened {
			delay = remoteEventsBackoffBase
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, remoteEventsBackoffMax)
	}
}

// streamRemoteEvents runs one upstream attempt: connect (or reuse) the host's
// workspace, open its /api/events, and relay frames until the stream ends,
// fails, goes silent past the liveness bound, or ctx is done. opened reports
// whether the stream got as far as a 200, so the caller can reset its backoff.
func (h *Handler) streamRemoteEvents(ctx context.Context, host string, projects []string, out chan<- []byte) (opened bool, err error) {
	first, ok := h.firstSavedProjectForHost(host)
	if !ok {
		return false, fmt.Errorf("host has no saved projects")
	}
	ws, err := h.remoteHosts.workspaceForPort(host, first.Path, first.RemotePort)
	if err != nil {
		return false, fmt.Errorf("remote connect: %w", err)
	}

	apiURL, err := url.Parse(ws.APIURL())
	if err != nil {
		return false, fmt.Errorf("parse API URL: %w", err)
	}
	query := url.Values{}
	if len(projects) > 0 {
		query.Set("projects", strings.Join(projects, ","))
	}
	eventsURL := apiURL.ResolveReference(&url.URL{Path: "/api/events", RawQuery: query.Encode()})

	attemptCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, eventsURL.String(), nil)
	if err != nil {
		return false, fmt.Errorf("build events request: %w", err)
	}
	if tok := ws.Token(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	liveness := h.remoteHosts.eventsLiveness
	if liveness <= 0 {
		liveness = remoteEventsLiveness
	}
	silent := time.AfterFunc(liveness, cancel)
	defer silent.Stop()

	resp, err := remoteEventsClient.Do(req)
	if err != nil {
		// The cached workspace proved unusable: drop it so the next attempt
		// reconnects (the proxy ErrorHandler's self-heal). Not when our own
		// caller went away — that says nothing about the tunnel.
		if ctx.Err() == nil {
			h.remoteHosts.drop(host, first.RemotePort)
		}
		return false, fmt.Errorf("GET /api/events: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("GET /api/events returned %d", resp.StatusCode)
	}

	hostJSON, err := json.Marshal(host)
	if err != nil {
		return false, fmt.Errorf("encode host: %w", err)
	}
	openFrame := fmt.Appendf(nil, `{"event":%q,"host":%s,"seq":0,"data":{"state":"open"}}`, hostStreamEvent, hostJSON)
	select {
	case out <- openFrame:
	case <-ctx.Done():
		return true, ctx.Err()
	}

	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return true, fmt.Errorf("read events stream: %w", err)
		}
		silent.Reset(liveness)
		payload, isData := bytes.CutPrefix(bytes.TrimRight(line, "\r\n"), []byte("data: "))
		if !isData {
			continue // event: line, keepalive comment, or frame separator
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(payload, &fields); err != nil {
			log.Printf("events relay: host %s sent a malformed envelope: %v", host, err)
			continue
		}
		fields["host"] = hostJSON
		frame, err := json.Marshal(fields)
		if err != nil {
			log.Printf("events relay: re-encode envelope from host %s: %v", host, err)
			continue
		}
		select {
		case out <- frame:
		case <-ctx.Done():
			return true, ctx.Err()
		}
	}
}
