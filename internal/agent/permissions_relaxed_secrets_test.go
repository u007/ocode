package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// relaxedSecretsManager builds an agent whose auto-permission layer has the
// `secrets` category switched off, with dir as its workdir.
func relaxedSecretsAgent(t *testing.T, dir string) *Agent {
	t.Helper()
	a := newTestAgent(nil, nil, nil, nil)
	a.Permissions().SetWorkDir(dir)
	a.Permissions().SetAutoPermissionConfig(&config.AutoPermissionConfig{
		Enabled:         true,
		RelaxedConcerns: []string{"secrets"},
	})
	return a
}

func writeSecretFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("KEY=value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Unticking `secrets` in Settings → Permissions is the user explicitly accepting
// credential exposure, so the deterministic sensitive-path guard in
// verifyAutoGrant must stop vetoing the judge's allow. Before this, the judge
// allowed at ~0.96 and the guard refused anyway, which made the checkbox a no-op
// for every credential read.
func TestVerifyAutoGrantHonoursRelaxedSecrets(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	writeSecretFile(t, envPath)
	args, err := json.Marshal(map[string]string{"path": envPath})
	if err != nil {
		t.Fatal(err)
	}

	// Enforced (the default): the guard refuses the credential read.
	a := newTestAgent(nil, nil, nil, nil)
	a.Permissions().SetWorkDir(dir)
	if ok, why := a.verifyAutoGrant("read", args, &PermissionRequest{ToolName: "read"}); ok {
		t.Fatalf("expected .env read to be refused while secrets is enforced, got ok (%s)", why)
	}

	// Relaxed: the same read passes the guard.
	relaxed := relaxedSecretsAgent(t, dir)
	if ok, why := relaxed.verifyAutoGrant("read", args, &PermissionRequest{ToolName: "read"}); !ok {
		t.Fatalf("expected .env read to pass with secrets relaxed, got %q", why)
	}
}

// The `secrets` category is about credential material, not about repository
// integrity: a planted hook or workflow executes arbitrary code from inside the
// workdir, so it stays guarded even when the user switched secrets off.
func TestVerifyAutoGrantRelaxedSecretsDoesNotCoverRepoMetadata(t *testing.T) {
	dir := t.TempDir()
	hook := filepath.Join(dir, ".git", "hooks", "pre-commit")
	writeSecretFile(t, hook)
	args, err := json.Marshal(map[string]string{"path": hook})
	if err != nil {
		t.Fatal(err)
	}

	a := relaxedSecretsAgent(t, dir)
	if ok, _ := a.verifyAutoGrant("write", args, &PermissionRequest{ToolName: "write"}); ok {
		t.Fatal("secrets opt-out must not clear the repo-metadata guard")
	}
}

// A path the user granted as an extra allowed dir must clear the guard, and a
// path outside every root must still be refused. This is the regression that
// "I already granted the dir but it keeps asking" reported: the predicate must be
// the extra-path-aware IsPathWithinAllowedRoots, not a workdir-only check.
func TestVerifyAutoGrantHonoursGrantedExtraPath(t *testing.T) {
	workDir := t.TempDir()
	extra := t.TempDir()
	// t.TempDir() lives under the OS temp root, which is itself an allowed root,
	// so an "outside" path has to sit somewhere else entirely. The home dir is
	// not in the root set (only the workdir, registered extras, cache dirs and
	// temp are), and the file need not exist for the guard to judge it.
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home dir to build an out-of-scope path")
	}
	outsideFile := filepath.Join(home, ".ocode-guard-test-outside", "notes.txt")

	granted := filepath.Join(extra, "notes.txt")
	writeSecretFile(t, granted)

	tool.SetPersistentExtraAllowedPaths([]string{extra})
	t.Cleanup(func() { tool.SetPersistentExtraAllowedPaths(nil) })

	a := newTestAgent(nil, nil, nil, nil)
	a.Permissions().SetWorkDir(workDir)

	grantedArgs, err := json.Marshal(map[string]string{"path": granted})
	if err != nil {
		t.Fatal(err)
	}
	if ok, why := a.verifyAutoGrant("write", grantedArgs, &PermissionRequest{ToolName: "write"}); !ok {
		t.Fatalf("granted extra path should clear the guard, got %q", why)
	}

	outsideArgs, err := json.Marshal(map[string]string{"path": outsideFile})
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := a.verifyAutoGrant("write", outsideArgs, &PermissionRequest{ToolName: "write"}); ok {
		t.Fatal("a path outside every allowed root must stay refused")
	}
}
