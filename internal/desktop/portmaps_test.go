package desktop

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/remote"
	"github.com/u007/ocode/internal/tool"
)

func newTestPortMapsHandler(t *testing.T) (*portMapsHandler, *http.ServeMux) {
	t.Helper()
	store, err := projects.NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	ref := projects.ProjectRef{Host: "user@host", Path: "/proj"}
	if err := store.AddRemote(ref.Host, ref.Path); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	fm := remote.NewForwardManager(sup, remote.Target{Kind: remote.KindSSH, Host: "host"})
	h := &portMapsHandler{fm: fm, store: store, ref: ref, localToken: "desktop-tok"}
	mux := http.NewServeMux()
	h.register(mux)
	return h, mux
}

func doPortMapsReq(mux *http.ServeMux, method, path, token string, body []byte) *httptest.ResponseRecorder {
	url := path
	if token != "" {
		if bytes.ContainsRune([]byte(path), '?') {
			url += "&token=" + token
		} else {
			url += "?token=" + token
		}
	}
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, url, bytes.NewReader(body))
	} else {
		req = httptest.NewRequest(method, url, nil)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestPortMapsRequiresLocalToken(t *testing.T) {
	_, mux := newTestPortMapsHandler(t)
	rec := doPortMapsReq(mux, "GET", "/api/desktop/portmaps", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET without token = %d, want 401", rec.Code)
	}
	rec = doPortMapsReq(mux, "GET", "/api/desktop/portmaps", "wrong-token", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET with wrong token = %d, want 401", rec.Code)
	}
}

func TestPortMapsListEmptyInitially(t *testing.T) {
	_, mux := newTestPortMapsHandler(t)
	rec := doPortMapsReq(mux, "GET", "/api/desktop/portmaps", "desktop-tok", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got []portMapView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("initial list = %+v, want empty", got)
	}
}

func TestPortMapsAddPersistsButLiveOpenFailsWithoutRealSSH(t *testing.T) {
	// fm.Start shells out to a real `ssh` process against an unreachable
	// host, so it's expected to fail here — this test only verifies the add
	// request persists the entry before attempting to open it live.
	h, mux := newTestPortMapsHandler(t)
	body, _ := json.Marshal(map[string]int{"remote_port": 3000, "local_port": 3000})
	rec := doPortMapsReq(mux, "POST", "/api/desktop/portmaps", "desktop-tok", body)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("POST add unexpectedly unauthorized")
	}

	maps, err := h.store.PortMaps(h.ref)
	if err != nil {
		t.Fatalf("PortMaps: %v", err)
	}
	if len(maps) != 1 || maps[0].RemotePort != 3000 {
		t.Fatalf("PortMaps = %+v, want one entry for remote port 3000", maps)
	}
}

func TestPortMapsRemoveDeletesEntry(t *testing.T) {
	h, mux := newTestPortMapsHandler(t)
	if err := h.store.AddPortMap(h.ref, 4000, 4000); err != nil {
		t.Fatalf("AddPortMap: %v", err)
	}
	rec := doPortMapsReq(mux, "DELETE", "/api/desktop/portmaps/4000", "desktop-tok", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	maps, _ := h.store.PortMaps(h.ref)
	if len(maps) != 0 {
		t.Fatalf("PortMaps after DELETE = %+v, want empty", maps)
	}
}

func TestPortMapsDisableStopsWithoutRemoving(t *testing.T) {
	h, mux := newTestPortMapsHandler(t)
	if err := h.store.AddPortMap(h.ref, 5000, 5000); err != nil {
		t.Fatalf("AddPortMap: %v", err)
	}
	rec := doPortMapsReq(mux, "POST", "/api/desktop/portmaps/5000/disable", "desktop-tok", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	maps, _ := h.store.PortMaps(h.ref)
	if len(maps) != 1 || maps[0].Enabled {
		t.Fatalf("PortMaps after disable = %+v, want one disabled entry", maps)
	}
}

func TestPortMapsRemoveUnknownPortNotFound(t *testing.T) {
	_, mux := newTestPortMapsHandler(t)
	rec := doPortMapsReq(mux, "DELETE", "/api/desktop/portmaps/9999", "desktop-tok", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE unknown port = %d, want 404", rec.Code)
	}
}
