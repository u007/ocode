package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// FileSearchStreamDone is the terminal frame of the search stream. Total is
// the number of matches streamed; Capped reports that the scan stopped early
// at the result cap (more matches exist but were not scanned).
type FileSearchStreamDone struct {
	Total     int  `json:"total"`
	Capped    bool `json:"capped"`
	Truncated bool `json:"truncated"`
}

// HandleFileSearchStream is the streaming variant of HandleFileSearch. Query params are identical
// (q/query, path, exts, ignore/exclude, regex/match, caseSensitive/case, wholeWord, includeIgnored — see HandleFileSearch),
// but `limit` semantic differs: here it is a total-result cap (default maxSearchResults) rather than a page size.
// The handler emits one `result` SSE frame per batch of matches as they are found, followed by a single `done` frame
// with the run metadata. Unlike the paginated endpoint it never waits for the whole tree walk: the first batch lands
// as soon as searchStreamBatchSize matches accumulate (or searchStreamBatchInterval elapses), then progress frames keep
// flowing until the scan finishes.
//
// Frames are `event: result\ndata: {"results":[...]}` / `event: done\ndata:
// {"total":N,"capped":bool}`. A client that disconnects mid-walk (request
// context cancelled, or a frame write failing once the connection closed)
// aborts the scan quietly — no trailing done is emitted.
func (h *Handler) HandleFileSearchStream(w http.ResponseWriter, r *http.Request) {
	p, err := h.parseSearchParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	// Tell buffering reverse proxies (nginx etc.) to pass chunks through
	// unmodified instead of buffering until the stream ends.
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Emit a comment frame and flush immediately so the client's fetch()
	// handshake resolves right away and it can render "searching…" before the
	// first result batch lands (comment frames are ignored by SSE parsers).
	if _, err := fmt.Fprintf(w, ": search started\n\n"); err != nil {
		return
	}
	flusher.Flush()

	// writeFrame marshals and writes one SSE frame, flushing after it. http
	// Flusher.Flush() has no error return, so a dead client is detected via
	// the Fprintf write error instead (writes fail once the connection is
	// closed).
	writeFrame := func(event string, data interface{}) error {
		jsonData, err := json.Marshal(data)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, jsonData); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	if len(p.keywords) == 0 {
		_ = writeFrame("done", FileSearchStreamDone{Total: 0})
		return
	}

	// The limit param caps how many matches are streamed; default is the shared
	// maxSearchResults cap. offset/pagination are meaningless for a stream.
	maxTotal := maxSearchResults
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 && v <= maxSearchResults {
			maxTotal = v
		}
	}

	ctx := r.Context()
	batch := make([]FileSearchResult, 0, searchStreamBatchSize)
	lastFlush := time.Now()
	total, capped, abortErr := searchFiles(ctx, p, maxTotal, func(res FileSearchResult) error {
		batch = append(batch, res)
		if len(batch) >= searchStreamBatchSize || time.Since(lastFlush) >= searchStreamBatchInterval {
			if err := writeFrame("result", map[string]interface{}{"results": batch}); err != nil {
				return err
			}
			batch = batch[:0]
			lastFlush = time.Now()
		}
		return nil
	})

	// Client disconnected mid-walk (flush failure or cancellation): stop
	// quietly — a premature done frame would look like a complete search.
	if abortErr != nil || ctx.Err() != nil {
		return
	}
	// Final partial batch, then the terminal frame.
	if len(batch) > 0 {
		if err := writeFrame("result", map[string]interface{}{"results": batch}); err != nil {
			return
		}
	}
	_ = writeFrame("done", FileSearchStreamDone{Total: total, Capped: capped, Truncated: capped})
}
