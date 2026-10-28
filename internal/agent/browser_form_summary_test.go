package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestPermissions_BrowserFormStringPathOutsideRootsIsFlagged pins that a plain
// string fill_form value naming an existing file outside the allowed roots is
// listed and flagged. A string that names no file, such as typed text, is not
// listed, so a typed password never reaches the dialog.
func TestPermissions_BrowserFormStringPathOutsideRootsIsFlagged(t *testing.T) {
	pm := NewPermissionManager()
	args := json.RawMessage(`{"action":"fill_form","fields":[` +
		`{"selector":"#upload","value":"/etc/hosts"},` +
		`{"selector":"#pw","value":"s3cret-pass"},` +
		`{"selector":"#other","value":"/no/such/file-anywhere"}]}`)
	dec := pm.Decide("browser", args)
	if dec.Level != PermissionAsk || dec.Request == nil {
		t.Fatalf("fill_form = %s, want Ask", dec.Level)
	}
	cmd := dec.Request.Command
	if !strings.Contains(cmd, "upload 1 file(s): /etc/hosts (1 outside allowed roots)") {
		t.Fatalf("summary does not list and flag the string path: %q", cmd)
	}
	if strings.Contains(cmd, "s3cret") || strings.Contains(cmd, "file-anywhere") {
		t.Fatalf("summary echoed text that is not a file: %q", cmd)
	}
}

// TestPermissions_BrowserFormArrayPathOutsideRootsIsFlagged pins that every
// string in an array value is listed and flagged, as a file list always is.
func TestPermissions_BrowserFormArrayPathOutsideRootsIsFlagged(t *testing.T) {
	pm := NewPermissionManager()
	args := json.RawMessage(`{"action":"fill_form","fields":[{"selector":"#f","value":["/etc/hosts","/work/b.pdf"]}]}`)
	dec := pm.Decide("browser", args)
	if dec.Request == nil {
		t.Fatalf("fill_form = %s, want Ask with a request", dec.Level)
	}
	if cmd := dec.Request.Command; !strings.Contains(cmd, "2 file(s): /etc/hosts, /work/b.pdf") || !strings.Contains(cmd, "2 outside allowed roots") {
		t.Fatalf("array paths not all listed and flagged: %q", cmd)
	}
}
