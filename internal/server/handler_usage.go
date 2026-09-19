package server

import (
	"net/http"
	"time"

	"github.com/u007/ocode/internal/usage"
)

func (h *Handler) HandleGetUsage(w http.ResponseWriter, r *http.Request) {
	rangeLabel := r.URL.Query().Get("range")
	if rangeLabel == "" {
		rangeLabel = "day"
	}

	// ?session_id= scopes the summary to one chat session's attributed ledger
	// rows (the same records the per-session spend gauge sums). Without it the
	// endpoint keeps its process-wide behavior.
	if sessionID := r.URL.Query().Get("session_id"); sessionID != "" {
		recs, err := usage.Query(time.Time{}, time.Now())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		scoped := make([]usage.Record, 0, len(recs))
		for _, rec := range recs {
			if rec.SessionID == sessionID {
				scoped = append(scoped, rec)
			}
		}
		writeJSON(w, http.StatusOK, usage.Summarize(scoped))
		return
	}

	labelMap := map[string]string{
		"hour":         "Last hour",
		"day":          "Today",
		"week":         "This week (last 7 days)",
		"month":        "This month (last 30 days)",
		"last-month":   "Last month",
		"last-3-month": "Last 3 months",
		"all":          "All time",
	}
	fullLabel, ok := labelMap[rangeLabel]
	if !ok {
		writeError(w, http.StatusBadRequest, "range must be one of: hour, day, week, month, last-month, last-3-month, all")
		return
	}

	var records []usage.Record
	for _, dr := range usage.DateRanges {
		if dr.Label == fullLabel {
			f, t := dr.From()
			recs, err := usage.Query(f, t)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			records = recs
			break
		}
	}

	writeJSON(w, http.StatusOK, usage.Summarize(records))
}
