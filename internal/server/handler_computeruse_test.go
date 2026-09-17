package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/u007/ocode/internal/computer"
	"github.com/u007/ocode/internal/config"
)

func TestComputerUseConfigEndpoints(t *testing.T) {
	h := testConfigHandler(t)

	get := func() struct {
		Enabled     bool     `json:"enabled"`
		StatusLines []string `json:"status_lines"`
	} {
		w := httptest.NewRecorder()
		h.HandleGetComputerUseConfig(w, httptest.NewRequest(http.MethodGet, "/api/config/computer-use", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("GET status = %d, want 200: %s", w.Code, w.Body.String())
		}
		var response struct {
			Enabled     bool     `json:"enabled"`
			StatusLines []string `json:"status_lines"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode GET response: %v", err)
		}
		if len(response.StatusLines) == 0 {
			t.Fatal("GET response has no status lines")
		}
		if want := computer.StatusLines(config.ComputerUseConfig{Enabled: response.Enabled}); !slices.Equal(response.StatusLines, want) {
			t.Fatalf("status_lines = %v, want %v", response.StatusLines, want)
		}
		return response
	}

	if response := get(); response.Enabled {
		t.Fatal("default computer-use config is enabled")
	}

	w := httptest.NewRecorder()
	h.HandleSetComputerUseConfig(w, httptest.NewRequest(http.MethodPut, "/api/config/computer-use", strings.NewReader(`{"enabled":true}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var putResponse struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &putResponse); err != nil {
		t.Fatalf("decode PUT response: %v", err)
	}
	if !putResponse.Enabled {
		t.Fatal("PUT response is not enabled")
	}
	if response := get(); !response.Enabled {
		t.Fatal("GET did not observe enabled computer-use config")
	}

	path, err := config.ActiveOcodeConfigPath()
	if err != nil {
		t.Fatalf("config path: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "computer_use") {
		t.Fatalf("persisted config does not contain computer_use: %s", data)
	}
}

func TestComputerUseConfigRejectsInvalidJSON(t *testing.T) {
	h := testConfigHandler(t)
	w := httptest.NewRecorder()
	h.HandleSetComputerUseConfig(w, httptest.NewRequest(http.MethodPut, "/api/config/computer-use", strings.NewReader("{")))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}

	w = httptest.NewRecorder()
	h.HandleSetComputerUseConfig(w, httptest.NewRequest(http.MethodPut, "/api/config/computer-use", strings.NewReader(`{}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing enabled status = %d, want 400", w.Code)
	}

	w = httptest.NewRecorder()
	h.HandleSetComputerUseConfig(w, httptest.NewRequest(http.MethodPut, "/api/config/computer-use", strings.NewReader(`{"enabled":true} trailing`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("trailing data status = %d, want 400", w.Code)
	}

	w = httptest.NewRecorder()
	h.HandleSetComputerUseConfig(w, httptest.NewRequest(http.MethodPut, "/api/config/computer-use", strings.NewReader(`{"enabled":true} null`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("trailing null status = %d, want 400", w.Code)
	}
}

func TestComputerUseConfigConcurrentPutsKeepMemoryAndDiskInSync(t *testing.T) {
	h := testConfigHandler(t)
	var wg sync.WaitGroup
	for i := range 16 {
		enabled := i%2 == 0
		wg.Go(func() {
			body := `{"enabled":false}`
			if enabled {
				body = `{"enabled":true}`
			}
			w := httptest.NewRecorder()
			h.HandleSetComputerUseConfig(w, httptest.NewRequest(http.MethodPut, "/api/config/computer-use", strings.NewReader(body)))
			if w.Code != http.StatusOK {
				t.Errorf("PUT status = %d, want 200: %s", w.Code, w.Body.String())
			}
		})
	}
	wg.Wait()

	h.mu.Lock()
	inMemory := h.cfg.Ocode.ComputerUse.Enabled
	h.mu.Unlock()
	path, err := config.ActiveOcodeConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	diskData, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var disk config.OcodeConfig
	if err := json.Unmarshal(diskData, &disk); err != nil {
		t.Fatal(err)
	}
	if disk.ComputerUse.Enabled != inMemory {
		t.Fatalf("memory enabled=%v, disk enabled=%v", inMemory, disk.ComputerUse.Enabled)
	}
}

func TestHandleRequestComputerUsePermissions(t *testing.T) {
	h := testConfigHandler(t)
	h.requestComputerPermissions = func(context.Context) computer.PermissionReport {
		return computer.PermissionReport{
			Platform: "test-os",
			Granted:  false,
			Lines:    []string{"Accessibility: not granted.", "Opened System Settings → Privacy & Security."},
		}
	}

	w := httptest.NewRecorder()
	h.HandleRequestComputerUsePermissions(w, httptest.NewRequest(http.MethodPost, "/api/config/computer-use/permissions", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var report computer.PermissionReport
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if report.Platform != "test-os" || report.Granted {
		t.Fatalf("report = %+v, want platform test-os and granted false", report)
	}
	if len(report.Lines) != 2 {
		t.Fatalf("lines = %v, want 2", report.Lines)
	}
}

// TestHandleRequestComputerUsePermissionsDoesNotPersistConfig pins the
// invariant that requesting permissions never changes the enabled flag.
func TestHandleRequestComputerUsePermissionsDoesNotPersistConfig(t *testing.T) {
	h := testConfigHandler(t)
	h.requestComputerPermissions = func(context.Context) computer.PermissionReport {
		return computer.PermissionReport{Platform: "test-os", Granted: true}
	}

	w := httptest.NewRecorder()
	h.HandleRequestComputerUsePermissions(w, httptest.NewRequest(http.MethodPost, "/api/config/computer-use/permissions", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	h.mu.Lock()
	enabled := h.cfg.Ocode.ComputerUse.Enabled
	h.mu.Unlock()
	if enabled {
		t.Fatal("requesting permissions enabled computer use")
	}
}
