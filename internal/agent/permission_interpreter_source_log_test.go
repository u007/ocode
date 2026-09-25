package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/debuglog"
)

// The Log tab must show whether the auto-permission judge actually read an
// interpreter script (and which bytes), or why it could not — otherwise a
// human-ask on `python3 script.py` is indistinguishable from "never looked".
func TestAcquireInterpreterSourceLogsReadOutcome(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	script := filepath.Join(dir, "check.py")
	if err := os.WriteFile(script, []byte("print('hi')\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	pm := NewPermissionManager()
	pm.SetWorkDir(dir)
	a := &Agent{permissions: pm, workDir: dir, sessionID: "ses_srclog"}

	findLog := func(substr string) string {
		for _, e := range debuglog.Log.Snapshot() {
			if e.SessionID == "ses_srclog" && strings.Contains(e.Message, substr) {
				return e.Message
			}
		}
		return ""
	}

	debuglog.Log.Clear()
	if _, _, _, ok := a.acquireInterpreterSource(&InterpreterExec{Language: "python", SourceMode: "script_file", Entrypoint: script}); !ok {
		t.Fatal("expected in-root script to be read")
	}
	msg := findLog("tier=auto_interp_source_read")
	if msg == "" || !strings.Contains(msg, "path="+script) || !strings.Contains(msg, "bytes=12") {
		t.Fatalf("missing/incomplete source_read log, got %q", msg)
	}

	debuglog.Log.Clear()
	if _, _, _, ok := a.acquireInterpreterSource(&InterpreterExec{Language: "python", SourceMode: "script_file", Entrypoint: "/definitely/not/a/root/x.py"}); ok {
		t.Fatal("expected out-of-root script to be unavailable")
	}
	if msg := findLog("tier=auto_interp_source_unavailable"); !strings.Contains(msg, "reason=outside_allowed_roots") {
		t.Fatalf("missing outside-roots log, got %q", msg)
	}

	debuglog.Log.Clear()
	if _, _, _, ok := a.acquireInterpreterSource(&InterpreterExec{Language: "python", SourceMode: "inline_eval", EmbeddedBody: "print(1)", Terminated: true}); !ok {
		t.Fatal("expected inline source")
	}
	if msg := findLog("tier=auto_interp_source_read"); !strings.Contains(msg, "mode=inline_eval") {
		t.Fatalf("missing inline source_read log, got %q", msg)
	}
}
