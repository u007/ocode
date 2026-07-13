package telegram

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// SSEEvent is a parsed Server-Sent Event.
type SSEEvent struct {
	Event string
	Data  string
}

// StreamEvents connects to the given SSE URL and invokes onEvent for each
// event until the server closes the stream or ctx is cancelled. The caller is
// expected to pass an Authorization/query-authenticated URL (the ocode RC
// server accepts ?token=).
// token, when non-empty, is sent as an "Authorization: Bearer" header so the
// secret is never placed in the URL query string (which leaks into logs and
// proxies). The ocode RC server also still accepts ?token= for browser
// EventSource clients that cannot set headers.
func StreamEvents(ctx context.Context, url, token string, onEvent func(SSEEvent)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sse: unexpected status %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var event, data string
	flush := func() {
		if event != "" {
			onEvent(SSEEvent{Event: event, Data: data})
		}
		event, data = "", ""
	}
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := scanner.Text()
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			// Per the SSE spec, a single leading space after the colon is part of
			// the framing and must be discarded; multiple `data:` lines for one
			// event are concatenated with LF. Assigning (the old behaviour)
			// silently dropped every line but the last of a multi-line payload.
			val := strings.TrimPrefix(line, "data:")
			val = strings.TrimPrefix(val, " ")
			if data == "" {
				data = val
			} else {
				data += "\n" + val
			}
		}
	}
	flush()
	return scanner.Err()
}

// decodeData is a tiny helper to unmarshal an SSE data payload.
func decodeData(data string, v interface{}) error {
	return json.Unmarshal([]byte(data), v)
}
