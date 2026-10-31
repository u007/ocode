package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

// A write into an Office add-in folder granted by Settings (which appends the
// folder to extra_allowed_paths) must be auto-allowed. This is the end-to-end
// effect the "Microsoft Office add-in folders" presets exist for: without the
// grant the same write asks because the folder is outside the workdir.
func TestPermissions_OfficeAddinGrantedPathWriteAutoAllows(t *testing.T) {
	pkgDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// A NON-temp location: a t.TempDir() target would auto-allow via isTempDir
	// regardless of the grant, making the test vacuous.
	wef := filepath.Join(pkgDir, "testdata", "office-addin-wef")
	if isTempDir(wef) {
		t.Fatalf("%q resolves under a temp root; the allow would be vacuous", wef)
	}
	target := filepath.Join(wef, "ocode-excel.xml")
	args := json.RawMessage(`{"file_path":` + jsonStr(target) + `,"content":"<xml/>"}`)

	pm := NewPermissionManager()
	pm.SetWorkDir(t.TempDir())

	if dec := pm.Decide("write", args); dec.Level != PermissionAsk {
		t.Fatalf("without the grant: level = %s, want ask", dec.Level)
	}
	if !tool.AddExtraAllowedPath(wef) {
		t.Fatalf("AddExtraAllowedPath(%q) failed", wef)
	}
	t.Cleanup(func() { tool.RemoveExtraAllowedPath(wef) })

	if dec := pm.Decide("write", args); dec.Level != PermissionAllow {
		t.Fatalf("with the grant: level = %s, want allow", dec.Level)
	}
}
