package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// Auto-permission is ocode's LLM auto-approval switch (the Jev judge). It is a SERVER-WIDE setting: turning it on
// changes what every ocode session on this machine auto-allows, not only the Excel workbooks. The pane reaches it
// through these bridge-owned routes. They are not on the /api/ allowlist, so that list does not widen.

// ocodeAutoPermissionPath is ocode's auto-permission block. GET returns it; PUT replaces the whole block.
const ocodeAutoPermissionPath = "/api/config/ocode/permissions-auto"

// ocodeCallTimeout bounds one request to ocode serve made by these routes.
const ocodeCallTimeout = 15 * time.Second

// ocodeReadLimit bounds one response body read from ocode serve.
const ocodeReadLimit = 1 << 20

// ocodeSessionReadLimit bounds one session transcript window read (localtools.go). A window of messages can hold
// large tool output, so it is far above ocodeReadLimit.
const ocodeSessionReadLimit = 16 << 20

// getAutoPermission answers GET /bridge/auto-permission with {"enabled": bool}.
func (s *srv) getAutoPermission(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.ocodeAutoPermissionConfig(r.Context())
	if err != nil {
		bridgeRouteError(w, http.StatusBadGateway, err)
		return
	}
	enabled, err := autoPermissionEnabled(cfg)
	if err != nil {
		bridgeRouteError(w, http.StatusBadGateway, err)
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]bool{"enabled": enabled})
}

// putAutoPermission answers PUT /bridge/auto-permission with {"enabled": bool}. Only "enabled" changes: the rest of
// ocode's block is sent back exactly as ocode returned it. ocode has no partial update, so the GET and PUT below are
// a read-modify-write. autoPermMu serializes the bridge's own writers; an edit made in ocode's UI in that window can
// still be overwritten.
func (s *srv) putAutoPermission(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<10))
	if err != nil {
		bridgeRouteError(w, http.StatusBadRequest, fmt.Errorf("reading auto-permission body: %w", err))
		return
	}
	// A map, not a struct with DisallowUnknownFields: that would match "ENABLED" to "enabled" case-insensitively.
	var req map[string]any
	if json.Unmarshal(body, &req) != nil || len(req) != 1 {
		bridgeRouteError(w, http.StatusBadRequest, errAutoPermissionBody)
		return
	}
	enabled, ok := req["enabled"].(bool)
	if !ok {
		bridgeRouteError(w, http.StatusBadRequest, errAutoPermissionBody)
		return
	}

	s.autoPermMu.Lock()
	defer s.autoPermMu.Unlock()
	cfg, err := s.ocodeAutoPermissionConfig(r.Context())
	if err != nil {
		bridgeRouteError(w, http.StatusBadGateway, err)
		return
	}
	cfg["enabled"] = enabled
	out, err := json.Marshal(cfg)
	if err != nil {
		bridgeRouteError(w, http.StatusBadGateway, fmt.Errorf("encoding auto-permission config: %w", err))
		return
	}
	if _, err := s.ocodeCall(r.Context(), http.MethodPut, ocodeAutoPermissionPath, out); err != nil {
		bridgeRouteError(w, http.StatusBadGateway, err)
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]bool{"enabled": enabled})
}

var errAutoPermissionBody = errors.New(`body must be {"enabled": true} or {"enabled": false}`)

// ocodeAutoPermissionConfig reads ocode's auto-permission block as a generic object, so a PUT can send back every
// field it does not change. Numbers stay json.Number so large integers keep their exact digits.
func (s *srv) ocodeAutoPermissionConfig(ctx context.Context) (map[string]any, error) {
	raw, err := s.ocodeCall(ctx, http.MethodGet, ocodeAutoPermissionPath, nil)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var cfg map[string]any
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("ocode returned an auto-permission config that is not a JSON object: %w", err)
	}
	if cfg == nil {
		return nil, errors.New("ocode returned a null auto-permission config")
	}
	return cfg, nil
}

// autoPermissionEnabled reads "enabled". ocode marks the field omitempty, so an absent key means false.
func autoPermissionEnabled(cfg map[string]any) (bool, error) {
	v, present := cfg["enabled"]
	if !present {
		return false, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("ocode returned auto-permission enabled as %T, not a bool", v)
	}
	return b, nil
}

// ocodeCall sends one request to ocode serve at path and returns the response body. A transport error or a non-2xx
// status is returned as an error that says what ocode answered.
func (s *srv) ocodeCall(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	return s.ocodeRequest(ctx, method, path, "", body, ocodeReadLimit)
}

// ocodeRequest is ocodeCall with a raw query string (no leading "?") and its own response size limit. A response
// over readLimit is an error, never a silently cut body.
func (s *srv) ocodeRequest(ctx context.Context, method, path, query string, body []byte, readLimit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, ocodeCallTimeout)
	defer cancel()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	u := s.target.JoinPath(path)
	u.RawQuery = query
	req, err := http.NewRequestWithContext(ctx, method, u.String(), rd)
	if err != nil {
		return nil, fmt.Errorf("building %s request to ocode: %w", method, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// The pane key never goes upstream; ocode gets the bridge's own token, as on the /api/ proxy.
	if s.cfg.OcodeToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.OcodeToken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ocode serve is not reachable for %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, readLimit+1))
	if err != nil {
		return nil, fmt.Errorf("reading ocode %s %s response: %w", method, path, err)
	}
	if int64(len(out)) > readLimit {
		return nil, fmt.Errorf("ocode serve answered %s %s with more than %d bytes", method, path, readLimit)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &ocodeHTTPError{method: method, path: path, code: resp.StatusCode, status: resp.Status, body: bytes.TrimSpace(out)}
	}
	return out, nil
}

// ocodeHTTPError is a non-2xx answer from ocode serve. Error keeps the text the bridge has always reported; code lets a
// route answer with the matching status (the session-mode route maps 404).
type ocodeHTTPError struct {
	method, path, status string
	code                 int
	body                 []byte
}

func (e *ocodeHTTPError) Error() string {
	return fmt.Sprintf("ocode serve answered %s %s with %s: %s", e.method, e.path, e.status, e.body)
}

// bridgeRouteError logs the failure and answers the pane with {"error": ...} at the given status.
func bridgeRouteError(w http.ResponseWriter, status int, err error) {
	log.Printf("bridge route (HTTP %d): %v", status, err)
	writeJSONStatus(w, status, map[string]string{"error": err.Error()})
}

func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
