package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The auto-permission judge is an LLM. Its context must never carry
// credential-bearing file contents — that would be the exact exposure the
// permission layer exists to prevent. These tests pin the withholding added in
// buildPermissionContext (target file, executed custom script, referenced
// file), and keep a positive control so ordinary files are still provided.

func newSecretContextAgent(t *testing.T, tmp string) *Agent {
	t.Helper()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWd) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	a := NewAgent(nil, nil, nil, nil)
	a.Permissions().SetWorkDir(tmp)
	return a
}

func TestBuildPermissionContextWithholdsSensitiveTargetFile(t *testing.T) {
	tmp := t.TempDir()
	const secret = "SUPERSECRET_TARGET_PW"
	for _, name := range []string{".env", "auth.json"} {
		if err := os.WriteFile(filepath.Join(tmp, name), []byte(`{"token":"`+secret+`"}`+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	a := newSecretContextAgent(t, tmp)
	for _, name := range []string{".env", "auth.json"} {
		args, _ := json.Marshal(map[string]string{"path": name})
		ctx := a.buildPermissionContext("read", args, 50000, 3, 40)
		if strings.Contains(ctx, secret) {
			t.Fatalf("%s: judge context leaked file contents: %q", name, ctx)
		}
		if !strings.Contains(ctx, "withheld") {
			t.Fatalf("%s: expected a withheld marker, got: %q", name, ctx)
		}
	}
}

func TestBuildPermissionContextWithholdsSensitiveReferenceInBash(t *testing.T) {
	tmp := t.TempDir()
	const secret = "SUPERSECRET_REF_PW"
	if err := os.WriteFile(filepath.Join(tmp, ".env"), []byte("DATABASE_URL=postgres://appuser:"+secret+"@localhost:5432/aims\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	a := newSecretContextAgent(t, tmp)
	cmd := `cd /home/james/www/aimsai2 && DBURL=$(grep '^DATABASE_URL=' .env | cut -d= -f2-) && psql "$DBURL" -c "\dt" 2>&1 | head -40; echo "=== search_path ==="; psql "$DBURL" -c "show search_path;" 2>&1`
	args, _ := json.Marshal(map[string]string{"command": cmd})
	ctx := a.buildPermissionContext("bash", args, 50000, 3, 40)
	if strings.Contains(ctx, secret) {
		t.Fatalf("judge context leaked .env contents: %q", ctx)
	}
	if strings.Contains(ctx, "Referenced file: .env") {
		t.Fatalf("sensitive referenced file should be skipped entirely, got: %q", ctx)
	}
}

func TestBuildPermissionContextWithholdsSensitiveExecutedScript(t *testing.T) {
	tmp := t.TempDir()
	const secret = "SUPERSECRET_SCRIPT_PW"
	script := filepath.Join(tmp, "secrets.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho "+secret+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	a := newSecretContextAgent(t, tmp)
	args, _ := json.Marshal(map[string]string{"command": "./secrets.sh"})
	ctx := a.buildPermissionContext("bash", args, 50000, 3, 40)
	if strings.Contains(ctx, secret) {
		t.Fatalf("judge context leaked executed script contents: %q", ctx)
	}
	if strings.Contains(ctx, "Executed custom script: ./secrets.sh") {
		t.Fatalf("sensitive executed script should be skipped, got: %q", ctx)
	}
}

// Positive control: withholding must be targeted at secret material only.
func TestBuildPermissionContextStillIncludesNonSensitiveReference(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "config.yaml"), []byte("retries: 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := newSecretContextAgent(t, tmp)
	args, _ := json.Marshal(map[string]string{"command": "cat config.yaml"})
	ctx := a.buildPermissionContext("bash", args, 50000, 3, 40)
	if !strings.Contains(ctx, "Referenced file: config.yaml") {
		t.Fatalf("non-sensitive referenced file should still be included, got: %q", ctx)
	}
	if !strings.Contains(ctx, "retries: 3") {
		t.Fatalf("non-sensitive referenced file contents should still be included, got: %q", ctx)
	}
}
