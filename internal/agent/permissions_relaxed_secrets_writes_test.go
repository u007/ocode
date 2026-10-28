package agent

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// The opt-out is about EXPOSURE, so it clears the guard for a READ of a
// credential file only. Mutating one (write/edit/delete) can destroy or plant
// credentials — an integrity risk the user never accepted by switching the
// category off — so those stay refused even with `secrets` relaxed.
func TestVerifyAutoGrantRelaxedSecretsDoesNotCoverCredentialWrites(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	writeSecretFile(t, envPath)
	args, err := json.Marshal(map[string]string{"path": envPath})
	if err != nil {
		t.Fatal(err)
	}

	a := relaxedSecretsAgent(t, dir)
	for _, toolName := range []string{"write", "edit", "delete", "multiedit", "multi_file_edit", "replace_lines", "apply_patch"} {
		if ok, _ := a.verifyAutoGrant(toolName, args, &PermissionRequest{ToolName: toolName}); ok {
			t.Fatalf("%s of a credential file must stay gated with secrets relaxed", toolName)
		}
	}

	// Read is the one that clears, so the test above cannot pass by the
	// relaxation being inert.
	if ok, why := a.verifyAutoGrant("read", args, &PermissionRequest{ToolName: "read"}); !ok {
		t.Fatalf("read of a credential file should pass with secrets relaxed, got %q", why)
	}
}
