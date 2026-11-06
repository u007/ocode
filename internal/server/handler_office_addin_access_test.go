package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/sysperm"
)

type officeAddinAccessResponse struct {
	Platform  string                    `json:"platform"`
	Supported bool                      `json:"supported"`
	Results   []officeAddinAccessResult `json:"results"`
}

func decodeOfficeAddinResponse(t *testing.T, raw []byte) officeAddinAccessResponse {
	t.Helper()
	var body officeAddinAccessResponse
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body
}

func TestHandleRequestOfficeAddinAccessProbesNamedApps(t *testing.T) {
	h := testConfigHandler(t)
	h.officeAddinPaths = func() []paths.OfficeAddinPath {
		return []paths.OfficeAddinPath{
			{App: "excel", Label: "Excel", Path: "/office/excel/wef", Exists: true, Available: true},
			{App: "word", Label: "Word", Path: "/office/word/wef", Exists: true, Available: true},
		}
	}
	var probed []string
	h.officeAddinRequest = func(_ context.Context, preset paths.OfficeAddinPath) officeAddinAccessResult {
		probed = append(probed, preset.App)
		return officeAddinAccessResult{
			App:     preset.App,
			Path:    preset.Path,
			Status:  "granted",
			Message: "Access granted.",
		}
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/config/ocode/paths/request", strings.NewReader(`{"apps":["word"]}`))
	h.HandleRequestOfficeAddinAccess(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if len(probed) != 1 || probed[0] != "word" {
		t.Fatalf("probed = %v, want [word]", probed)
	}
	body := decodeOfficeAddinResponse(t, w.Body.Bytes())
	if len(body.Results) != 1 || body.Results[0].App != "word" || body.Results[0].Status != "granted" {
		t.Fatalf("results = %+v, want one granted word entry", body.Results)
	}
}

func TestHandleRequestOfficeAddinAccessDedupesApps(t *testing.T) {
	h := testConfigHandler(t)
	h.officeAddinPaths = func() []paths.OfficeAddinPath {
		return []paths.OfficeAddinPath{{App: "excel", Label: "Excel", Path: "/office/excel/wef", Exists: true}}
	}
	calls := 0
	h.officeAddinRequest = func(_ context.Context, preset paths.OfficeAddinPath) officeAddinAccessResult {
		calls++
		return officeAddinAccessResult{App: preset.App, Path: preset.Path, Status: "granted"}
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/config/ocode/paths/request", strings.NewReader(`{"apps":["excel","excel"]}`))
	h.HandleRequestOfficeAddinAccess(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if calls != 1 {
		t.Fatalf("probe calls = %d, want 1", calls)
	}
}

func TestHandleRequestOfficeAddinAccessRejectsBadInput(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"empty list", `{"apps":[]}`},
		{"unknown app", `{"apps":["powerpoint"]}`},
		{"valid app before unknown app", `{"apps":["excel","powerpoint"]}`},
		{"malformed", `{"apps":`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := testConfigHandler(t)
			h.officeAddinPaths = func() []paths.OfficeAddinPath {
				return []paths.OfficeAddinPath{{App: "excel", Label: "Excel", Path: "/office/excel/wef"}}
			}
			h.officeAddinRequest = func(_ context.Context, preset paths.OfficeAddinPath) officeAddinAccessResult {
				t.Fatalf("bad input reached the probe: %+v", preset)
				return officeAddinAccessResult{}
			}

			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/api/config/ocode/paths/request", strings.NewReader(tc.body))
			h.HandleRequestOfficeAddinAccess(w, r)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestProbeOfficeAddinAccessSkipsMissingFolder(t *testing.T) {
	res := probeOfficeAddinAccess(t.Context(),
		paths.OfficeAddinPath{App: "excel", Label: "Excel", Path: "/office/excel/wef", Exists: false},
		true,
		func(context.Context, sysperm.Entry) sysperm.RequestResult {
			t.Fatal("a missing folder must not trigger an OS request")
			return sysperm.RequestResult{}
		},
	)
	if res.Status != "missing" {
		t.Fatalf("status = %q, want missing", res.Status)
	}
	if !strings.Contains(res.Message, "Open Excel once") {
		t.Fatalf("message = %q, want the open-once hint", res.Message)
	}
}

func TestProbeOfficeAddinAccessRequestsOSGrant(t *testing.T) {
	var got sysperm.Entry
	res := probeOfficeAddinAccess(t.Context(),
		paths.OfficeAddinPath{App: "excel", Label: "Excel", Path: "/office/excel/wef", Exists: true},
		true,
		func(_ context.Context, e sysperm.Entry) sysperm.RequestResult {
			got = e
			return sysperm.RequestResult{ID: e.ID, Status: sysperm.StatusGranted, Message: "Access granted."}
		},
	)
	if res.Status != "granted" || res.Message != "Access granted." {
		t.Fatalf("result = %+v, want granted", res)
	}
	if got.Kind != sysperm.KindPath || got.Path != "/office/excel/wef" || !got.Requested {
		t.Fatalf("request entry = %+v, want a requested KindPath entry", got)
	}
}

func TestProbeOfficeAddinAccessChecksFilesystemWithoutOSGrants(t *testing.T) {
	noOSRequest := func(context.Context, sysperm.Entry) sysperm.RequestResult {
		t.Fatal("the filesystem branch must not request an OS grant")
		return sysperm.RequestResult{}
	}
	dir := t.TempDir()
	res := probeOfficeAddinAccess(t.Context(),
		paths.OfficeAddinPath{App: "addins", Label: "Office Add-ins", Path: dir, Exists: true},
		false,
		noOSRequest,
	)
	if res.Status != "not_required" {
		t.Fatalf("status = %q, want not_required for a readable folder", res.Status)
	}

	gone := filepath.Join(dir, "gone")
	res = probeOfficeAddinAccess(t.Context(),
		paths.OfficeAddinPath{App: "addins", Label: "Office Add-ins", Path: gone, Exists: true},
		false,
		noOSRequest,
	)
	if res.Status != "denied" {
		t.Fatalf("status = %q, want denied for an unreadable folder", res.Status)
	}
}
