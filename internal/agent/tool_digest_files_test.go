package agent

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fileToolsBox registers the explainer's file tools for workDir the way the
// runner does, and returns the toolbox they are called through.
func fileToolsBox(t *testing.T, workDir string) *toolDigestToolbox {
	t.Helper()
	box := &toolDigestToolbox{}
	if err := registerToolDigestFileTools(box, toolDigestSnapshot{WorkDir: workDir}); err != nil {
		t.Fatalf("registerToolDigestFileTools: %v", err)
	}
	return box
}

// callFileTool runs one named tool through the toolbox with a JSON object built
// from args.
func callFileTool(t *testing.T, box *toolDigestToolbox, name string, args map[string]any) (string, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return box.Execute(context.Background(), name, raw)
}

func writeDigestFixture(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFileTools_ProjectRootConfinementAndSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink and FIFO fixtures are POSIX-only")
	}
	root := t.TempDir()
	canonRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	writeDigestFixture(t, root, "ok.txt", "project content")
	writeDigestFixture(t, outside, "secret.txt", "OUTSIDE-SECRET")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(root, "passwd")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "outdir")); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("mkfifo", filepath.Join(root, "pipe")).CombinedOutput(); err != nil {
		t.Fatalf("mkfifo: %v: %s", err, out)
	}

	// validateProjectPath: lexical escapes, absolute outside paths, symlink
	// escapes, and a missing nested path that resolves its deepest ancestor.
	for _, p := range []string{"../x", filepath.Join(outside, "secret.txt"), "link.txt", "outdir/new.txt"} {
		if got, err := validateProjectPath(root, p); err == nil {
			t.Fatalf("validateProjectPath(%q) = %q, want rejection", p, got)
		}
	}
	if got, err := validateProjectPath(root, "ok.txt"); err != nil || got != filepath.Join(canonRoot, "ok.txt") {
		t.Fatalf("validateProjectPath(ok.txt) = %q, %v", got, err)
	}
	if got, err := validateProjectPath(root, "a/b/new.txt"); err != nil || got != filepath.Join(canonRoot, "a", "b", "new.txt") {
		t.Fatalf("validateProjectPath(a/b/new.txt) = %q, %v; want the deepest-ancestor join", got, err)
	}

	box := fileToolsBox(t, root)
	for _, p := range []string{"../x", filepath.Join(outside, "secret.txt"), "link.txt", "passwd", "outdir/secret.txt"} {
		out, err := callFileTool(t, box, "read", map[string]any{"path": p})
		if err == nil {
			t.Fatalf("read(%q) succeeded: %q", p, out)
		}
		if strings.Contains(out, "OUTSIDE-SECRET") || strings.Contains(out, "root:") {
			t.Fatalf("read(%q) leaked outside content: %q", p, out)
		}
	}
	// A FIFO is refused before it is opened; opening it would block forever.
	if _, err := callFileTool(t, box, "read", map[string]any{"path": "pipe"}); err == nil {
		t.Fatal("read of a FIFO succeeded")
	}
	if out, err := callFileTool(t, box, "read", map[string]any{"path": "ok.txt"}); err != nil || !strings.Contains(out, "project content") {
		t.Fatalf("read(ok.txt) = %q, %v", out, err)
	}
	for _, name := range []string{"glob", "grep", "list"} {
		args := map[string]any{"path": "outdir", "pattern": "*", "intent": "t"}
		if name == "grep" {
			args["pattern"] = "x"
		}
		if out, err := callFileTool(t, box, name, args); err == nil {
			t.Fatalf("%s over a symlinked outside dir succeeded: %q", name, out)
		}
	}

	// A snapshot whose project root cannot be resolved is a registration error.
	if err := registerToolDigestFileTools(&toolDigestToolbox{}, toolDigestSnapshot{WorkDir: filepath.Join(root, "missing")}); err == nil {
		t.Fatal("registerToolDigestFileTools accepted a missing project root")
	}
}

func TestFileTools_SensitivePathsFiltered(t *testing.T) {
	root := t.TempDir()
	writeDigestFixture(t, root, "ok.txt", "visible")
	writeDigestFixture(t, root, "sub/deep/notes.md", "visible too")
	writeDigestFixture(t, root, ".env", "TOKEN=abc")
	writeDigestFixture(t, root, "id_rsa", "PRIVATE-KEY-BODY")
	writeDigestFixture(t, root, ".git/config", "[core]")
	writeDigestFixture(t, root, "browse/login-states/session.json", "COOKIE-BODY")
	writeDigestFixture(t, root, "sub/app.pem", "CERT-BODY")
	box := fileToolsBox(t, root)

	out, err := callFileTool(t, box, "glob", map[string]any{"pattern": "**/*", "path": "", "intent": "t"})
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if !strings.Contains(out, "ok.txt") || !strings.Contains(out, "sub/deep/notes.md") {
		t.Fatalf("glob dropped visible files: %q", out)
	}
	for _, bad := range []string{".env", "id_rsa", ".git/config", "login-states", "app.pem"} {
		if strings.Contains(out, bad) {
			t.Fatalf("glob leaked %q: %q", bad, out)
		}
	}

	out, err = callFileTool(t, box, "list", map[string]any{"path": "", "intent": "t"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "ok.txt") {
		t.Fatalf("list dropped ok.txt: %q", out)
	}
	for _, bad := range []string{".env", "id_rsa", ".git"} {
		if strings.Contains(out, bad) {
			t.Fatalf("list leaked %q: %q", bad, out)
		}
	}

	for _, p := range []string{".env", "id_rsa", ".git/config", "browse/login-states/session.json", "sub/app.pem"} {
		out, err := callFileTool(t, box, "read", map[string]any{"path": p})
		if err == nil {
			t.Fatalf("read(%q) succeeded: %q", p, out)
		}
		for _, leak := range []string{"TOKEN=abc", "PRIVATE-KEY-BODY", "[core]", "COOKIE-BODY", "CERT-BODY"} {
			if strings.Contains(out, leak) {
				t.Fatalf("read(%q) leaked %q", p, leak)
			}
		}
	}
	if _, err := callFileTool(t, box, "list", map[string]any{"path": "browse/login-states", "intent": "t"}); err == nil {
		t.Fatal("list of the login-state directory succeeded")
	}
}

func TestFileTools_GrepSkipsSensitiveBeforeRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FIFO fixture is POSIX-only")
	}
	root := t.TempDir()
	writeDigestFixture(t, root, "ok.txt", "needle visible\n")
	writeDigestFixture(t, root, "id_rsa", "needle TOKEN-ID-RSA-LEAK\n")
	writeDigestFixture(t, root, "credentials.json", "needle TOKEN-CRED-LEAK\n")
	// A FIFO named like a secret: any open of it blocks forever, so the search
	// returning at all shows it was never opened.
	if out, err := exec.Command("mkfifo", filepath.Join(root, ".env")).CombinedOutput(); err != nil {
		t.Fatalf("mkfifo: %v: %s", err, out)
	}
	box := fileToolsBox(t, root)

	out, err := callFileTool(t, box, "grep", map[string]any{"pattern": "needle", "intent": "t"})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if !strings.Contains(out, "ok.txt") {
		t.Fatalf("grep dropped ok.txt: %q", out)
	}
	for _, leak := range []string{"TOKEN-ID-RSA-LEAK", "TOKEN-CRED-LEAK", "id_rsa", "credentials.json"} {
		if strings.Contains(out, leak) {
			t.Fatalf("grep leaked %q: %q", leak, out)
		}
	}
}
