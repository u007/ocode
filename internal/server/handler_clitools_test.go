package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/plugins/clitools"
)

// stubCLIToolSeams swaps the clitools seams for the duration of the test. The
// real DetectAll probes the host PATH and the real Install shells out to the
// platform package manager, neither of which belongs in a unit test.
func stubCLIToolSeams(t *testing.T, detect func() []clitools.ToolStatus, manager clitools.PackageManager, install func(string) clitools.InstallResult) {
	t.Helper()
	prevDetect, prevManager, prevInstall := cliToolDetectAllFn, cliToolManagerFn, cliToolInstallFn
	cliToolDetectAllFn, cliToolManagerFn, cliToolInstallFn = detect, func() clitools.PackageManager { return manager }, install
	t.Cleanup(func() {
		cliToolDetectAllFn, cliToolManagerFn, cliToolInstallFn = prevDetect, prevManager, prevInstall
	})
}

func testCLIToolHandler(t *testing.T) *Handler {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	return NewHandler()
}

// getCLIToolJobStatus calls the status handler the way the mux does. A bare
// httptest request has no path values, so {id} must be set explicitly or the
// handler sees an empty id and 404s.
func getCLIToolJobStatus(h *Handler, jobID string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/cli-tools/install/"+jobID, nil)
	r.SetPathValue("id", jobID)
	h.HandleGetCliToolsInstallStatus(w, r)
	return w
}

func TestHandleListCliToolsReportsStatusAndManager(t *testing.T) {
	stubCLIToolSeams(t,
		func() []clitools.ToolStatus {
			return []clitools.ToolStatus{
				{Tool: clitools.Tool{Name: "rg", Aliases: []string{"ripgrep"}, Description: "ripgrep", Project: "https://example.test/rg"}, Found: true, Command: "rg"},
				{Tool: clitools.Tool{Name: "fzf", Description: "fuzzy finder"}, Found: false},
			}
		},
		clitools.PMBrew,
		nil,
	)
	h := testCLIToolHandler(t)

	w := httptest.NewRecorder()
	h.HandleListCliTools(w, httptest.NewRequest("GET", "/api/cli-tools", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp cliToolsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.PackageManager != "brew" {
		t.Errorf("package_manager = %q, want brew", resp.PackageManager)
	}
	if resp.Platform == "" {
		t.Error("platform should be set")
	}
	if resp.ManagerHint != "" {
		t.Errorf("manager_hint should be empty when a manager is present, got %q", resp.ManagerHint)
	}
	if len(resp.Tools) != 2 {
		t.Fatalf("len(tools) = %d, want 2", len(resp.Tools))
	}
	if resp.Tools[0].Name != "rg" || !resp.Tools[0].Found || resp.Tools[0].Command != "rg" {
		t.Errorf("tool[0] = %+v, want installed rg", resp.Tools[0])
	}
	if len(resp.Tools[0].Aliases) != 1 || resp.Tools[0].Aliases[0] != "ripgrep" {
		t.Errorf("tool[0] aliases = %v, want [ripgrep]", resp.Tools[0].Aliases)
	}
	if resp.Tools[1].Found || resp.Tools[1].Command != "" {
		t.Errorf("tool[1] = %+v, want not-installed fzf", resp.Tools[1])
	}
	// The unexported install recipe map must never reach the wire.
	if strings.Contains(w.Body.String(), "install") {
		t.Errorf("response leaked install recipes: %s", w.Body.String())
	}
}

func TestHandleListCliToolsIncludesHintWhenNoManager(t *testing.T) {
	stubCLIToolSeams(t, func() []clitools.ToolStatus { return nil }, "", nil)
	h := testCLIToolHandler(t)

	w := httptest.NewRecorder()
	h.HandleListCliTools(w, httptest.NewRequest("GET", "/api/cli-tools", nil))

	var resp cliToolsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.PackageManager != "" {
		t.Errorf("package_manager = %q, want empty", resp.PackageManager)
	}
	if resp.ManagerHint == "" {
		t.Error("manager_hint should explain how to install a package manager")
	}
}

func TestHandleStartCliToolsInstallRejectsUnknownTool(t *testing.T) {
	stubCLIToolSeams(t, nil, clitools.PMBrew, nil)
	h := testCLIToolHandler(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/cli-tools/install", strings.NewReader(`{"tool":"nope-not-a-tool"}`))
	h.HandleStartCliToolsInstall(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestHandleStartCliToolsInstallRejectsMissingTool(t *testing.T) {
	stubCLIToolSeams(t, nil, clitools.PMBrew, nil)
	h := testCLIToolHandler(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/cli-tools/install", strings.NewReader(`{}`))
	h.HandleStartCliToolsInstall(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// A real catalog name (rg) must be accepted without touching the installer;
// the stub blocks until told to finish so we can observe the running state.
func TestHandleStartCliToolsInstallRunsAsyncAndReportsDone(t *testing.T) {
	release := make(chan struct{})
	stubCLIToolSeams(t, nil, clitools.PMBrew, func(name string) clitools.InstallResult {
		if name != "rg" {
			t.Errorf("install called with %q, want rg", name)
		}
		<-release
		return clitools.InstallResult{
			Tool: "rg", Manager: clitools.PMBrew, Ok: true,
			Output: "$ brew install ripgrep\ninstalled",
		}
	})
	h := testCLIToolHandler(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/cli-tools/install", strings.NewReader(`{"tool":"rg"}`))
	h.HandleStartCliToolsInstall(w, r)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", w.Code, w.Body.String())
	}
	var started cliToolInstallStartResponse
	if err := json.Unmarshal(w.Body.Bytes(), &started); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if started.JobID == "" {
		t.Fatal("job_id should be set")
	}
	if started.Tool != "rg" || started.Status != "running" {
		t.Errorf("start response = %+v, want rg/running", started)
	}

	// While the install is blocked the job must report "running" — the whole
	// point of the async contract; a synchronous install would have returned
	// only after the release below.
	w = getCLIToolJobStatus(h, started.JobID)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var mid cliToolInstallStatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &mid); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if mid.Status != "running" {
		t.Errorf("status = %q, want running", mid.Status)
	}

	close(release)
	// Poll until terminal; the goroutine only publishes under the lock.
	deadline := time.Now().Add(5 * time.Second)
	for {
		w = getCLIToolJobStatus(h, started.JobID)
		var done cliToolInstallStatusResponse
		if err := json.Unmarshal(w.Body.Bytes(), &done); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if done.Status != "running" {
			if done.Status != "done" {
				t.Fatalf("status = %q, want done; error=%q", done.Status, done.Error)
			}
			if done.Manager != "brew" {
				t.Errorf("manager = %q, want brew", done.Manager)
			}
			if !strings.Contains(done.Output, "installed") {
				t.Errorf("output = %q, want captured installer output", done.Output)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("install job never reached a terminal state")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestHandleGetCliToolsInstallStatusUnknownJob(t *testing.T) {
	stubCLIToolSeams(t, nil, clitools.PMBrew, nil)
	h := testCLIToolHandler(t)

	w := getCLIToolJobStatus(h, "deadbeef")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestCliToolsInstallErrorSurfacesNoManagerHint(t *testing.T) {
	stubCLIToolSeams(t, nil, "", func(string) clitools.InstallResult {
		return clitools.InstallResult{
			Tool: "rg", NoManager: true,
			Err: errors.New("no supported package manager found"),
		}
	})
	h := testCLIToolHandler(t)

	w := httptest.NewRecorder()
	h.HandleStartCliToolsInstall(w, httptest.NewRequest("POST", "/api/cli-tools/install", strings.NewReader(`{"tool":"rg"}`)))
	var started cliToolInstallStartResponse
	if err := json.Unmarshal(w.Body.Bytes(), &started); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		w = httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/cli-tools/install/"+started.JobID, nil)
		r.SetPathValue("id", started.JobID)
		h.HandleGetCliToolsInstallStatus(w, r)
		var got cliToolInstallStatusResponse
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.Status != "running" {
			if got.Status != "error" {
				t.Fatalf("status = %q, want error", got.Status)
			}
			if !got.NoManager {
				t.Error("no_manager should be true")
			}
			if got.Hint == "" {
				t.Error("hint should carry the package-manager remediation")
			}
			if !strings.Contains(got.Error, "no supported package manager") {
				t.Errorf("error = %q, want the installer error", got.Error)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("install job never reached a terminal state")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
