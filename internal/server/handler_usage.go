package server

import (
	"net/http"

	"github.com/jamesmercstudio/ocode/internal/usage"
)

func (h *Handler) HandleGetUsage(w http.ResponseWriter, r *http.Request) {
	rangeLabel := r.URL.Query().Get("range")
	if rangeLabel == "" {
		rangeLabel = "day"
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
