package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"runtime"

	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/sysperm"
)

// officeAddinAccessResult is one Office add-in folder's access report. Status
// is "granted", "denied", "not_required", "not_determined", "missing" or
// "unknown"; Message is
// user-facing prose the Settings form renders under the row.
type officeAddinAccessResult struct {
	App            string `json:"app"`
	Path           string `json:"path"`
	Status         string `json:"status"`
	Message        string `json:"message,omitempty"`
	OpenedSettings bool   `json:"opened_settings,omitempty"`
}

// requestOfficeAddinAccess probes one preset, honoring a test override so the
// suite never raises a real OS consent dialog.
func (h *Handler) requestOfficeAddinAccess(ctx context.Context, preset paths.OfficeAddinPath) officeAddinAccessResult {
	if h.officeAddinRequest != nil {
		return h.officeAddinRequest(ctx, preset)
	}
	return probeOfficeAddinAccess(ctx, preset, sysperm.PlatformSupported(), h.syspermRequest)
}

// probeOfficeAddinAccess performs the cross-platform access check for one
// Office add-in folder.
//
// On macOS the read is what raises the "access data from other apps" consent
// dialog, so the user sees the prompt at save time instead of discovering the
// missing grant when a sideload later fails. Windows and Linux have no
// per-application consent gate, so the probe creates and removes a temporary
// file: the grant is write access, and NTFS ACLs or POSIX permissions can still
// deny it.
//
// osGrants and request are parameters (not package calls) so both branches are
// testable on any host OS without firing a real prompt. request must be non-nil
// whenever osGrants is true; the handler passes its syspermRequest seam.
func probeOfficeAddinAccess(
	ctx context.Context,
	preset paths.OfficeAddinPath,
	osGrants bool,
	request func(context.Context, sysperm.Entry) sysperm.RequestResult,
) officeAddinAccessResult {
	res := officeAddinAccessResult{App: preset.App, Path: preset.Path}
	if !preset.Exists {
		// Office creates the folder on first sideload, so there is nothing to
		// probe yet — and macOS raises no dialog for a path that is absent.
		res.Status = "missing"
		res.Message = preset.Label + " has not created this folder yet. Open " + preset.Label + " once, then save again."
		return res
	}
	if osGrants {
		r := request(ctx, sysperm.Entry{
			ID:        sysperm.PathID(preset.Path),
			Label:     preset.Label,
			Kind:      sysperm.KindPath,
			Platform:  runtime.GOOS,
			Supported: true,
			Path:      preset.Path,
			Requested: true,
		})
		res.Status = string(r.Status)
		res.Message = r.Message
		res.OpenedSettings = r.OpenedSettings
		return res
	}

	// No per-application grant exists here. The grant ocode just made is
	// write access, so create and remove a probe file: a readable folder with
	// a denying ACL must report denied.
	f, err := os.CreateTemp(preset.Path, ".ocode-access-*")
	if err != nil {
		res.Status = "denied"
		res.Message = "Cannot write to this folder: " + err.Error()
		return res
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	res.Status = "not_required"
	res.Message = "No OS permission is required on " + runtime.GOOS + "; the folder is writable."
	return res
}

// HandleRequestOfficeAddinAccess requests OS access for the named Office
// add-in presets. It runs AFTER the paths save, so a denied consent dialog
// never undoes the persisted grant.
func (h *Handler) HandleRequestOfficeAddinAccess(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	var payload struct {
		Apps []string `json:"apps"`
	}
	if err := decoder.Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(payload.Apps) == 0 {
		writeError(w, http.StatusBadRequest, "apps is required")
		return
	}

	byApp := make(map[string]paths.OfficeAddinPath)
	for _, preset := range h.officeAddinPresets() {
		byApp[preset.App] = preset
	}
	// Validate every app before probing any: a probe raises a real OS dialog,
	// so a 400 for a later unknown app must not come after an earlier probe.
	seen := make(map[string]bool, len(payload.Apps))
	presets := make([]paths.OfficeAddinPath, 0, len(payload.Apps))
	for _, app := range payload.Apps {
		if seen[app] {
			continue
		}
		seen[app] = true
		preset, ok := byApp[app]
		if !ok {
			writeError(w, http.StatusBadRequest, "unknown Office add-in folder: "+app)
			return
		}
		presets = append(presets, preset)
	}
	results := make([]officeAddinAccessResult, 0, len(presets))
	for _, preset := range presets {
		results = append(results, h.requestOfficeAddinAccess(r.Context(), preset))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"platform":  runtime.GOOS,
		"supported": sysperm.PlatformSupported(),
		"results":   results,
	})
}
