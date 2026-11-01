package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
)

// ocodeConnectPath is ocode's provider credential list. It is deliberately NOT on the /api/ allowlist: its response
// carries a masked tail of each stored key and the OAuth account name (internal/server/handler_connect.go). The bridge
// reads it and answers with provider ids only.
const ocodeConnectPath = "/api/auth/connect"

// ocodeProvider is the part of one ocode provider entry the bridge reads. Every other field is dropped.
type ocodeProvider struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// getConnectedProviders answers GET /bridge/connected-providers with {"providers": [ids]}, sorted. A provider counts as
// connected when ocode's status symbol is "✓" (from the environment, a stored key or OAuth, or the config file), the
// same test the TUI /connect list uses. A provider with no stored credential but an env var is still connected.
func (s *srv) getConnectedProviders(w http.ResponseWriter, r *http.Request) {
	raw, err := s.ocodeCall(r.Context(), http.MethodGet, ocodeConnectPath, nil)
	if err != nil {
		bridgeRouteError(w, http.StatusBadGateway, err)
		return
	}
	var resp struct {
		Providers []ocodeProvider `json:"providers"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		bridgeRouteError(w, http.StatusBadGateway, fmt.Errorf("ocode returned a provider list that is not the expected JSON: %w", err))
		return
	}
	if resp.Providers == nil {
		bridgeRouteError(w, http.StatusBadGateway, errors.New(`ocode returned no "providers" list`))
		return
	}
	ids := []string{}
	for _, p := range resp.Providers {
		if p.Status == "✓" {
			ids = append(ids, p.ID)
		}
	}
	sort.Strings(ids)
	writeJSONStatus(w, http.StatusOK, map[string][]string{"providers": ids})
}
