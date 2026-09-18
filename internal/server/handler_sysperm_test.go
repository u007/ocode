package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/sysperm"
)

// syspermTestHandler builds a Handler whose catalog/requester are stubbed so
// no OS probe or consent dialog runs. The catalog mirrors the persisted
// enable/request flags so the reconcile path can be exercised end-to-end.
func syspermTestHandler(t *testing.T) *Handler {
	t.Helper()
	h := testConfigHandler(t)
	h.systemPermissionCatalog = func(context.Context) []sysperm.Entry {
		cfg := h.systemPermissionsConfig()
		entries := []sysperm.Entry{
			{
				ID: "macos.files.documents", Label: "Documents", Kind: sysperm.KindPath,
				Platform: "darwin", Supported: true, Status: sysperm.StatusDenied, Path: "/tmp/docs",
				Enabled: cfg.Get("macos.files.documents").Enabled,
			},
			{
				ID: "macos.accessibility", Label: "Accessibility", Kind: sysperm.KindCategory,
				Platform: "darwin", Supported: true, Status: sysperm.StatusDenied,
				Enabled: cfg.Get("macos.accessibility").Enabled,
			},
		}
		for id, ec := range cfg.Entries {
			if ec.Path == "" {
				continue
			}
			entries = append(entries, sysperm.Entry{
				ID: id, Label: ec.Label, Kind: sysperm.KindPath, Platform: "darwin",
				Supported: true, Status: sysperm.StatusDenied, Enabled: ec.Enabled, Path: ec.Path,
			})
		}
		return entries
	}
	h.requestSystemPermission = func(_ context.Context, e sysperm.Entry) sysperm.RequestResult {
		return sysperm.RequestResult{ID: e.ID, Status: sysperm.StatusGranted, Message: "requested " + e.ID}
	}
	return h
}

type syspermResponse struct {
	Platform  string                  `json:"platform"`
	Supported bool                    `json:"supported"`
	Entries   []sysperm.Entry         `json:"entries"`
	Result    *sysperm.RequestResult  `json:"result"`
	Results   []sysperm.RequestResult `json:"results"`
}

func decodeSysperm(t *testing.T, body []byte) syspermResponse {
	t.Helper()
	var resp syspermResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode response: %v (body: %s)", err, body)
	}
	return resp
}

func TestGetSystemPermissions(t *testing.T) {
	h := syspermTestHandler(t)
	w := httptest.NewRecorder()
	h.HandleGetSystemPermissions(w, httptest.NewRequest(http.MethodGet, "/api/config/system-permissions", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET status = %d: %s", w.Code, w.Body.String())
	}
	resp := decodeSysperm(t, w.Body.Bytes())
	if len(resp.Entries) != 2 {
		t.Fatalf("entries = %+v, want 2", resp.Entries)
	}
	if resp.Entries[0].ID != "macos.files.documents" || resp.Entries[1].ID != "macos.accessibility" {
		t.Fatalf("unexpected entries: %+v", resp.Entries)
	}
}

func TestPutSystemPermissionEnablesAndRequests(t *testing.T) {
	h := syspermTestHandler(t)
	w := httptest.NewRecorder()
	h.HandleSetSystemPermission(w, httptest.NewRequest(http.MethodPut, "/api/config/system-permissions",
		strings.NewReader(`{"id":"macos.files.documents","enabled":true}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d: %s", w.Code, w.Body.String())
	}
	resp := decodeSysperm(t, w.Body.Bytes())
	if resp.Result == nil || resp.Result.Status != sysperm.StatusGranted {
		t.Fatalf("result = %+v, want granted request", resp.Result)
	}
	ec := h.systemPermissionsConfig().Get("macos.files.documents")
	if !ec.Enabled || !ec.Requested {
		t.Fatalf("persisted entry = %+v, want enabled+requested", ec)
	}
}

func TestPutSystemPermissionDisableDoesNotRequest(t *testing.T) {
	h := syspermTestHandler(t)
	put := func(body string) syspermResponse {
		w := httptest.NewRecorder()
		h.HandleSetSystemPermission(w, httptest.NewRequest(http.MethodPut, "/api/config/system-permissions", strings.NewReader(body)))
		if w.Code != http.StatusOK {
			t.Fatalf("PUT status = %d: %s", w.Code, w.Body.String())
		}
		return decodeSysperm(t, w.Body.Bytes())
	}
	put(`{"id":"macos.accessibility","enabled":true}`)
	resp := put(`{"id":"macos.accessibility","enabled":false}`)
	if resp.Result != nil {
		t.Fatalf("disabling produced a request result: %+v", resp.Result)
	}
	if ec := h.systemPermissionsConfig().Get("macos.accessibility"); ec.Enabled {
		t.Fatalf("persisted entry = %+v, want disabled", ec)
	}
}

func TestPutSystemPermissionAddsCustomPath(t *testing.T) {
	h := syspermTestHandler(t)
	w := httptest.NewRecorder()
	h.HandleSetSystemPermission(w, httptest.NewRequest(http.MethodPut, "/api/config/system-permissions",
		strings.NewReader(`{"path":"/tmp/proj","label":"proj","enabled":true}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d: %s", w.Code, w.Body.String())
	}
	id := sysperm.PathID("/tmp/proj")
	ec := h.systemPermissionsConfig().Get(id)
	if !ec.Enabled || ec.Path != "/tmp/proj" || ec.Label != "proj" {
		t.Fatalf("persisted custom entry = %+v", ec)
	}
	resp := decodeSysperm(t, w.Body.Bytes())
	if resp.Result == nil || resp.Result.ID != id {
		t.Fatalf("custom path enable result = %+v, want request for %s", resp.Result, id)
	}
}

func TestDeleteSystemPermissionRemovesEntry(t *testing.T) {
	h := syspermTestHandler(t)
	id := sysperm.PathID("/tmp/gone")
	w := httptest.NewRecorder()
	h.HandleSetSystemPermission(w, httptest.NewRequest(http.MethodPut, "/api/config/system-permissions",
		strings.NewReader(`{"path":"/tmp/gone","enabled":false}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("add status = %d: %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	h.HandleDeleteSystemPermission(w, httptest.NewRequest(http.MethodDelete,
		"/api/config/system-permissions?id="+url.QueryEscape(id), nil))
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d: %s", w.Code, w.Body.String())
	}
	if _, ok := h.systemPermissionsConfig().Entries[id]; ok {
		t.Fatal("DELETE left the entry in place")
	}
}

func TestRequestSystemPermissionsReconcilesEnabled(t *testing.T) {
	h := syspermTestHandler(t)
	for _, id := range []string{"macos.files.documents", "macos.accessibility"} {
		w := httptest.NewRecorder()
		h.HandleSetSystemPermission(w, httptest.NewRequest(http.MethodPut, "/api/config/system-permissions",
			strings.NewReader(`{"id":"`+id+`","enabled":true}`)))
		if w.Code != http.StatusOK {
			t.Fatalf("enable %s status = %d: %s", id, w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	h.HandleRequestSystemPermissions(w, httptest.NewRequest(http.MethodPost, "/api/config/system-permissions/request",
		strings.NewReader(`{}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("POST status = %d: %s", w.Code, w.Body.String())
	}
	resp := decodeSysperm(t, w.Body.Bytes())
	if len(resp.Results) != 2 {
		t.Fatalf("results = %+v, want 2 reconciled requests", resp.Results)
	}
}

func TestRequestSystemPermissionUnknownID(t *testing.T) {
	h := syspermTestHandler(t)
	w := httptest.NewRecorder()
	h.HandleRequestSystemPermissions(w, httptest.NewRequest(http.MethodPost, "/api/config/system-permissions/request",
		strings.NewReader(`{"id":"macos.does-not-exist"}`)))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
	}
}
