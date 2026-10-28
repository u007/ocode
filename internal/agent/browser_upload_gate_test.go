package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// writeProjectFile creates path (relative to root) with content.
func writeProjectFile(t *testing.T, root, name, content string) {
	t.Helper()
	full := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

func uploadArgs(files ...string) json.RawMessage {
	payload := map[string]any{"action": "upload", "tab": "agent1", "target": "input[name=doc]"}
	payload["files"] = files
	raw, _ := json.Marshal(payload)
	return raw
}

// TestBrowserUploadSensitiveFileAsksDespiteAlwaysAllow pins the gate that keeps
// a persisted "always allow browser" grant from silently handing a credential
// file to a page. read/write and bash redirects already ask on these paths.
func TestBrowserUploadSensitiveFileAsksDespiteAlwaysAllow(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, ".env", "TOKEN=secret")
	bt := &browserStubTool{ok: true, page: ownTab}
	ag := newTestAgent(nil, []tool.Tool{bt}, &config.Config{}, nil)
	ag.SetWorkDir(root)
	ag.permissions.SetRule("browser", PermissionAllow)

	out, err := ag.handleToolCall("browser", uploadArgs(".env"), nil, "")
	if err != nil {
		t.Fatalf("upload of .env: %v", err)
	}
	if !strings.HasPrefix(out, tool.SentinelPermissionAsk) {
		t.Fatalf("upload of .env did not ask: %q", out)
	}
	if !strings.Contains(out, "tool.browser.sensitive_path") {
		t.Fatalf("ask does not name the sensitive-path rule: %s", out)
	}
	if bt.executed != 0 {
		t.Fatalf("the upload ran before an answer: %d executions", bt.executed)
	}
}

// TestBrowserUploadSensitiveFileAsksForEverySecretVariant covers a key file and
// an SSH private key as well as .env, since isSensitivePath is shared.
func TestBrowserUploadSensitiveFileAsksForEverySecretVariant(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, "certs/server.pem", "PRIVATE")
	writeProjectFile(t, root, "id_ed25519", "PRIVATE")
	for _, name := range []string{"certs/server.pem", "id_ed25519"} {
		bt := &browserStubTool{ok: true, page: ownTab}
		ag := newTestAgent(nil, []tool.Tool{bt}, &config.Config{}, nil)
		ag.SetWorkDir(root)
		ag.permissions.SetRule("browser", PermissionAllow)
		out, err := ag.handleToolCall("browser", uploadArgs(name), nil, "")
		if err != nil {
			t.Fatalf("upload %s: %v", name, err)
		}
		if !strings.HasPrefix(out, tool.SentinelPermissionAsk) {
			t.Fatalf("upload %s did not ask: %q", name, out)
		}
	}
}

// TestBrowserUploadOrdinaryFileStaysAllowed pins that the gate does not
// over-block: a normal project file under an always-allow rule still uploads,
// and the template variants isSecretMaterialPath deliberately exempts stay
// allowed.
func TestBrowserUploadOrdinaryFileStaysAllowed(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, "notes.txt", "hello")
	writeProjectFile(t, root, ".env.example", "TOKEN=placeholder")
	for _, name := range []string{"notes.txt", ".env.example"} {
		bt := &browserStubTool{ok: true, page: ownTab}
		ag := newTestAgent(nil, []tool.Tool{bt}, &config.Config{}, nil)
		ag.SetWorkDir(root)
		ag.permissions.SetRule("browser", PermissionAllow)
		out, err := ag.handleToolCall("browser", uploadArgs(name), nil, "")
		if err != nil {
			t.Fatalf("upload %s: %v", name, err)
		}
		if strings.HasPrefix(out, tool.SentinelPermissionAsk) {
			t.Fatalf("upload %s asked; want it allowed: %q", name, out)
		}
		if bt.executed != 1 {
			t.Fatalf("upload %s executed %d times, want 1", name, bt.executed)
		}
	}
}

// TestBrowserUploadSensitiveFileAllowedInYolo pins the documented parity with
// the read sensitive gate: YOLO returns before that gate in Decide, so this one
// does not second-guess YOLO either.
func TestBrowserUploadSensitiveFileAllowedInYolo(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, ".env", "TOKEN=secret")
	bt := &browserStubTool{ok: true, page: ownTab}
	ag := newTestAgent(nil, []tool.Tool{bt}, &config.Config{}, nil)
	ag.SetWorkDir(root)
	ag.permissions.SetMode(PermissionModeYOLO)

	out, err := ag.handleToolCall("browser", uploadArgs(".env"), nil, "")
	if err != nil {
		t.Fatalf("yolo upload: %v", err)
	}
	if strings.HasPrefix(out, tool.SentinelPermissionAsk) {
		t.Fatalf("yolo upload asked: %q", out)
	}
}

// TestBrowserUploadMissingFileNotEscalated pins that a path the tool will
// refuse anyway is not turned into a sensitive-path ask: the tool's own error
// is the answer, not a dialog about a file that cannot be read.
func TestBrowserUploadMissingFileNotEscalated(t *testing.T) {
	root := t.TempDir()
	bt := &browserStubTool{ok: true, page: ownTab}
	ag := newTestAgent(nil, []tool.Tool{bt}, &config.Config{}, nil)
	ag.SetWorkDir(root)
	ag.permissions.SetRule("browser", PermissionAllow)

	out, err := ag.handleToolCall("browser", uploadArgs(".env"), nil, "")
	if err != nil {
		t.Fatalf("missing upload: %v", err)
	}
	if strings.HasPrefix(out, tool.SentinelPermissionAsk) {
		t.Fatalf("a missing file was escalated to a dialog: %q", out)
	}
}
