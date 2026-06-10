package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// --- Classification ---------------------------------------------------------

func TestClassifyInterpreterExecution(t *testing.T) {
	cases := []struct {
		name       string
		command    string
		wantOK     bool
		wantLang   string
		wantMode   string
		wantEntry  string
		wantRemote string
	}{
		{"heredoc", "python3 - <<'PY'\nopen('/tmp/x','w').write('hi')\nPY", true, "python", "heredoc", "", ""},
		{"script_file", "python file.py", true, "python", "script_file", "file.py", ""},
		{"stdin_pipe", "python - < job.py", true, "python", "stdin_pipe", "job.py", ""},
		{"node_script", "node script.js", true, "javascript", "script_file", "script.js", ""},
		{"bun_run_file", "bun run ./x.ts", true, "javascript", "script_file", "./x.ts", ""},
		{"deno_run", "deno run app.ts", true, "javascript", "script_file", "app.ts", ""},
		{"inline_eval", `node -e "console.log(1)"`, true, "javascript", "inline_eval", "", ""},
		{"python_inline", `python -c "print(1)"`, true, "python", "inline_eval", "", ""},
		{"remote_npx", "npx cowsay hi", true, "javascript", "remote", "", "cowsay"},
		{"remote_bunx", "bunx prettier .", true, "javascript", "remote", "", "prettier"},
		{"remote_pnpm_dlx", "pnpm dlx tsx foo", true, "javascript", "remote", "", "tsx"},
		{"bare_repl", "python", true, "python", "unknown_source", "", ""},
		{"not_interpreter", "ls -la", false, "", "", "", ""},
		{"grep", "grep foo bar.txt", false, "", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ie, ok := classifyInterpreterExecution(tc.command)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v want %v (ie=%+v)", ok, tc.wantOK, ie)
			}
			if !ok {
				return
			}
			if ie.Language != tc.wantLang || ie.SourceMode != tc.wantMode {
				t.Fatalf("lang=%q mode=%q want lang=%q mode=%q", ie.Language, ie.SourceMode, tc.wantLang, tc.wantMode)
			}
			if tc.wantEntry != "" && ie.Entrypoint != tc.wantEntry {
				t.Fatalf("entrypoint=%q want %q", ie.Entrypoint, tc.wantEntry)
			}
			if tc.wantRemote != "" && ie.RemoteSpec != tc.wantRemote {
				t.Fatalf("remoteSpec=%q want %q", ie.RemoteSpec, tc.wantRemote)
			}
		})
	}
}

func TestExtractHeredocsUnterminated(t *testing.T) {
	header, docs := extractHeredocs("python3 - <<'PY'\nimport os\n# missing delimiter")
	if header != "python3 -" {
		t.Fatalf("header=%q", header)
	}
	if len(docs) != 1 {
		t.Fatalf("want 1 doc, got %d", len(docs))
	}
	if docs[0].terminated {
		t.Fatal("expected unterminated heredoc")
	}
	if !strings.Contains(docs[0].body, "import os") {
		t.Fatalf("body=%q", docs[0].body)
	}
}

// TestMultilineInlineEvalBodyPreserved guards a regression where a multi-line
// `python3 -c "<body with newlines>"` was truncated to its first line by
// extractHeredocs, leaving EmbeddedBody empty. The empty body forced the
// interpreter auto-permission path to report auto_interp_no_source and fall
// back to a human prompt instead of analyzing the source.
func TestMultilineInlineEvalBodyPreserved(t *testing.T) {
	cmd := "python3 -c \"\nimport hashlib\n# compute a slug\nslug = hashlib.sha256(b'/x').hexdigest()[:12]\nprint(slug)\n\""
	ie, ok := classifyInterpreterExecution(cmd)
	if !ok {
		t.Fatalf("not classified")
	}
	if ie.SourceMode != "inline_eval" {
		t.Fatalf("mode=%q want inline_eval", ie.SourceMode)
	}
	if !strings.Contains(ie.EmbeddedBody, "import hashlib") || !strings.Contains(ie.EmbeddedBody, "print(slug)") {
		t.Fatalf("EmbeddedBody truncated: %q", ie.EmbeddedBody)
	}
}

// TestExtractHeredocsNoHeredocPreservesFullCommand ensures a command with
// newlines but no heredoc operator returns the full command, not just line one.
func TestExtractHeredocsNoHeredocPreservesFullCommand(t *testing.T) {
	cmd := "python3 -c \"line1\nline2\""
	header, docs := extractHeredocs(cmd)
	if len(docs) != 0 {
		t.Fatalf("want 0 docs, got %d", len(docs))
	}
	if header != cmd {
		t.Fatalf("header=%q want full command %q", header, cmd)
	}
}

// TestTokenizeShellSkipsComments guards a regression where a '#' comment line in
// a multi-line bash command was tokenized as a command word named "#", producing
// a bogus `bash.prefix.#` ASK that escalated otherwise-allowed scripts to the
// permission prompt.
func TestTokenizeShellSkipsComments(t *testing.T) {
	cmd := "# Replace dynamic imports\nsed -i '' 's/a/b/g' file.tsx\n# trailing note\nhead -1 file.tsx"
	cmds, err := parseShellCommandLine(cmd)
	if err != nil {
		t.Fatalf("err %v", err)
	}
	if len(cmds) != 2 {
		t.Fatalf("want 2 commands (sed, head), got %d: %#v", len(cmds), cmds)
	}
	if cmds[0].cmdWords[0] != "sed" || cmds[1].cmdWords[0] != "head" {
		t.Fatalf("unexpected commands: %q, %q", cmds[0].cmdWords[0], cmds[1].cmdWords[0])
	}
	for _, c := range cmds {
		if c.cmdWords[0] == "#" {
			t.Fatalf("comment leaked as command: %#v", c.cmdWords)
		}
	}
}

// TestTokenizeShellMidWordHashLiteral ensures a '#' inside a word (e.g. a URL
// fragment) stays literal and does not start a comment.
func TestTokenizeShellMidWordHashLiteral(t *testing.T) {
	cmds, err := parseShellCommandLine("curl https://example.com/page#section")
	if err != nil {
		t.Fatalf("err %v", err)
	}
	if len(cmds) != 1 || cmds[0].cmdWords[1] != "https://example.com/page#section" {
		t.Fatalf("mid-word '#' not preserved: %#v", cmds)
	}
}

func TestHeredocClassificationCapturesBody(t *testing.T) {
	ie, ok := classifyInterpreterExecution("python3 - <<'PY'\nprint('hi')\nPY")
	if !ok || ie.SourceMode != "heredoc" {
		t.Fatalf("ok=%v ie=%+v", ok, ie)
	}
	if !ie.Terminated {
		t.Fatal("expected terminated heredoc")
	}
	if strings.TrimSpace(ie.EmbeddedBody) != "print('hi')" {
		t.Fatalf("body=%q", ie.EmbeddedBody)
	}
}

// --- bun run guard ----------------------------------------------------------

func TestBunRunFileGuard(t *testing.T) {
	cases := []struct {
		command string
		allow   bool
	}{
		{"bun run build", true},         // manifest script — still auto-allowed
		{"bun run test", true},          // manifest script
		{"bun run ./evil.ts", false},    // path-like — must drop to Ask
		{"bun run scripts/x.js", false}, // path-like
		{"bun run index.ts", false},     // script extension
		{"npm run build", true},         // npm run unaffected
	}
	for _, tc := range cases {
		if got := matchSubcommandAllow(tc.command); got != tc.allow {
			t.Errorf("matchSubcommandAllow(%q)=%v want %v", tc.command, got, tc.allow)
		}
	}
}

// --- Package-runner safe tools ----------------------------------------------

func TestRunnerInvokedSafeTool(t *testing.T) {
	cases := []struct {
		command string
		allow   bool
	}{
		{"npx tsc --noEmit", true}, // the reported case
		{"npx -y tsc", true},       // boolean flag skipped
		{"bunx eslint .", true},    // bunx runner
		{"pnpm dlx prettier --write .", true},
		{"yarn dlx biome check", true},
		{"pnpm exec vitest run", true},
		{"bun x tsgo", true},
		{"npx --package=evil tsc", false},   // value flag → fail closed
		{"npx -p evil tsc", false},          // value flag → fail closed
		{"npx create-react-app foo", false}, // not a safe tool
		{"npx vite", false},                 // executes a dev server, not inert
		{"npx", false},                      // runner with no tool
		{"npx some-random-cli", false},      // unknown tool
	}
	for _, tc := range cases {
		if got := matchSubcommandAllow(tc.command); got != tc.allow {
			t.Errorf("matchSubcommandAllow(%q)=%v want %v", tc.command, got, tc.allow)
		}
	}
}

// --- Allowed roots ----------------------------------------------------------

func TestAllowedRootsAndMembership(t *testing.T) {
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	pm := NewPermissionManager()
	pm.SetWorkDir(resolved)

	inside := filepath.Join(resolved, "f.txt") // not-yet-existing file under workdir
	if !pm.IsPathWithinAllowedRoots(inside) {
		t.Fatalf("expected %s within roots", inside)
	}
	if pm.IsPathWithinAllowedRoots("/definitely/not/a/root/x") {
		t.Fatal("unexpected: out-of-root path reported in scope")
	}
	if !pm.IsPathWithinAllowedRoots("/tmp/scratch.txt") {
		t.Fatal("expected /tmp to be an allowed root")
	}
}

// --- Verifier ---------------------------------------------------------------

func newVerifierAgent(t *testing.T) (*Agent, string) {
	t.Helper()
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	a := NewAgent(nil, nil, &config.Config{}, nil)
	a.Permissions().SetWorkDir(resolved)
	return a, resolved
}

func TestVerifyInterpreterEffects(t *testing.T) {
	a, root := newVerifierAgent(t)
	in := filepath.Join(root, "in.txt")
	out := filepath.Join(root, "out.json")
	ie := &InterpreterExec{Language: "python", SourceMode: "script_file", RawCommand: "python f.py"}

	base := func() *interpreterModelResponse {
		return &interpreterModelResponse{Decision: "allow", Confidence: 0.95, Summary: "s",
			Effects: interpreterEffects{Reads: []string{in}, Writes: []string{out}}}
	}

	t.Run("happy allow (destructive enabled)", func(t *testing.T) {
		ok, reason := a.verifyInterpreterEffects(ie, base(), 0.85, true, false)
		if !ok {
			t.Fatalf("expected allow, got %q", reason)
		}
	})
	t.Run("low confidence asks", func(t *testing.T) {
		r := base()
		r.Confidence = 0.5
		if ok, _ := a.verifyInterpreterEffects(ie, r, 0.85, true, false); ok {
			t.Fatal("expected ask for low confidence")
		}
	})
	t.Run("non-empty unknown asks", func(t *testing.T) {
		r := base()
		r.Effects.Unknown = []string{"dynamic import"}
		if ok, _ := a.verifyInterpreterEffects(ie, r, 0.85, true, false); ok {
			t.Fatal("expected ask for unknown effects")
		}
	})
	t.Run("truncated asks", func(t *testing.T) {
		if ok, _ := a.verifyInterpreterEffects(ie, base(), 0.85, true, true); ok {
			t.Fatal("expected ask for truncated source")
		}
	})
	t.Run("write outside roots asks", func(t *testing.T) {
		r := base()
		r.Effects.Writes = []string{"/etc/passwd"}
		if ok, _ := a.verifyInterpreterEffects(ie, r, 0.85, true, false); ok {
			t.Fatal("expected ask for out-of-root write")
		}
	})
	t.Run("sensitive path asks", func(t *testing.T) {
		r := base()
		r.Effects.Writes = []string{filepath.Join(root, ".env")}
		if ok, _ := a.verifyInterpreterEffects(ie, r, 0.85, true, false); ok {
			t.Fatal("expected ask for sensitive path")
		}
	})
	t.Run("subprocess asks", func(t *testing.T) {
		r := base()
		r.Effects.Subprocesses = []string{"sh -c rm"}
		if ok, _ := a.verifyInterpreterEffects(ie, r, 0.85, true, false); ok {
			t.Fatal("expected ask for subprocess")
		}
	})
	t.Run("network without policy asks", func(t *testing.T) {
		r := base()
		r.Effects.Writes = nil
		r.Effects.Network = []string{"evil.com"}
		if ok, _ := a.verifyInterpreterEffects(ie, r, 0.85, true, false); ok {
			t.Fatal("expected ask for unapproved network host")
		}
	})
	t.Run("destructive requires allow_destructive", func(t *testing.T) {
		if ok, _ := a.verifyInterpreterEffects(ie, base(), 0.85, false, false); ok {
			t.Fatal("expected ask: destructive without allow_destructive")
		}
	})
	t.Run("read-only allowed without destructive flag", func(t *testing.T) {
		r := base()
		r.Effects.Writes = nil
		if ok, reason := a.verifyInterpreterEffects(ie, r, 0.85, false, false); !ok {
			t.Fatalf("expected allow for read-only, got %q", reason)
		}
	})
}

// --- Grant matching ---------------------------------------------------------

func TestMatchInterpreterGrant(t *testing.T) {
	pm := NewPermissionManager()
	root := filepath.Join(t.TempDir(), "project")
	grantPath := filepath.Join(root, "job.py")
	cmd := "python job.py"
	ie := &InterpreterExec{Language: "python", SourceMode: "script_file", Entrypoint: grantPath, RawCommand: cmd}
	if pm.MatchInterpreterGrant(ie, "abc", true) {
		t.Fatal("no grant should match")
	}
	pm.AddAutoGrant(config.AutoGrant{Kind: "interpreter_exact", Language: "python", SourceMode: "script_file", NormalizedCommand: normalizeGrantCommand(cmd), EntrypointPath: grantPath, EntrypointSHA256: "abc", CWD: safeGetwd()})
	if !pm.MatchInterpreterGrant(ie, "abc", true) {
		t.Fatal("expected matching grant")
	}
	if pm.MatchInterpreterGrant(ie, "different", true) {
		t.Fatal("changed source hash must not match")
	}
	if pm.MatchInterpreterGrant(&InterpreterExec{Language: "python", SourceMode: "script_file", Entrypoint: filepath.Join(root, "other.py"), RawCommand: cmd}, "abc", true) {
		t.Fatal("changed path must not match")
	}
	if pm.MatchInterpreterGrant(&InterpreterExec{Language: "python", SourceMode: "script_file", Entrypoint: grantPath, RawCommand: "python -O job.py"}, "abc", true) {
		t.Fatal("changed command must not match")
	}

	// Destructive grant: must NOT match when allowDestructive is false.
	pm.AddAutoGrant(config.AutoGrant{Kind: "interpreter_exact", Language: "python", SourceMode: "script_file", NormalizedCommand: normalizeGrantCommand(cmd), EntrypointPath: grantPath, EntrypointSHA256: "def", CWD: safeGetwd(), Destructive: true})
	destructIE := &InterpreterExec{Language: "python", SourceMode: "script_file", Entrypoint: grantPath, RawCommand: cmd}
	if pm.MatchInterpreterGrant(destructIE, "def", false) {
		t.Fatal("destructive grant must not match when allowDestructive is false")
	}
	if !pm.MatchInterpreterGrant(destructIE, "def", true) {
		t.Fatal("destructive grant must match when allowDestructive is true")
	}
}

// --- End-to-end consultation (mocked model) ---------------------------------

func mockModelJSON(t *testing.T, body string) func() {
	t.Helper()
	prev := newClientFn
	newClientFn = func(_ *config.Config, _ string) LLMClient {
		return &MockClient{Response: &Message{Role: "assistant", Content: body}}
	}
	return func() { newClientFn = prev }
}

func newConsultAgent(t *testing.T) (*Agent, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	orig, _ := os.Getwd()
	if err := os.Chdir(resolved); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		if orig != "" {
			os.Chdir(orig)
		}
	})
	cfg := &config.Config{}
	cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Enabled: true, Model: "anthropic/claude-sonnet-4-6", AllowDestructive: true, MinConfidence: 0.85}
	a := NewAgent(nil, nil, cfg, nil)
	a.Permissions().SetWorkDir(resolved)
	a.Permissions().SetAutoPermissionEnabled(true)
	return a, resolved
}

func TestAskPermissionModelInterpreterAllowsAndPersistsGrant(t *testing.T) {
	a, root := newConsultAgent(t)
	scriptPath := filepath.Join(root, "job.py")
	out := filepath.Join(root, "out.txt")
	if err := os.WriteFile(scriptPath, []byte("open('out.txt','w').write('hi')\n"), 0644); err != nil {
		t.Fatalf("write script: %v", err)
	}
	resp := `{"decision":"allow","confidence":0.95,"summary":"writes out.txt","effects":{"reads":[],"writes":["` + out + `"],"deletes":[],"network":[],"subprocesses":[],"unknown":[]}}`
	defer mockModelJSON(t, resp)()

	ie, ok := classifyInterpreterExecution("python job.py")
	if !ok {
		t.Fatal("classify failed")
	}
	allowed, reason := a.askPermissionModelInterpreter("python job.py", ie)
	if !allowed {
		t.Fatalf("expected allow, got reason=%q", reason)
	}
	// Grant persisted to disk and loadable.
	var cfg config.Config
	if err := config.LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Ocode.Permissions.Auto == nil || len(cfg.Ocode.Permissions.Auto.Grants) != 1 {
		t.Fatalf("expected 1 persisted grant, got %+v", cfg.Ocode.Permissions.Auto)
	}
	g := cfg.Ocode.Permissions.Auto.Grants[0]
	if g.Kind != "interpreter_exact" || g.Language != "python" || g.EntrypointSHA256 == "" {
		t.Fatalf("unexpected grant: %+v", g)
	}
	if g.NormalizedCommand != normalizeGrantCommand("python job.py") {
		t.Fatalf("unexpected normalized command: %+v", g)
	}
	if g.EntrypointPath != filepath.Join(root, "job.py") {
		t.Fatalf("unexpected entrypoint path: %+v", g)
	}

	// Second identical run short-circuits via the in-memory grant (model would
	// otherwise be consulted) — still allowed.
	allowed2, _ := a.askPermissionModelInterpreter("python job.py", ie)
	if !allowed2 {
		t.Fatal("expected grant short-circuit allow on repeat")
	}
}

func TestAskPermissionModelInterpreterStdinPipeAllowsAndPersistsGrant(t *testing.T) {
	a, root := newConsultAgent(t)
	scriptPath := filepath.Join(root, "job.py")
	out := filepath.Join(root, "out.txt")
	if err := os.WriteFile(scriptPath, []byte("open('out.txt','w').write('hi')\n"), 0644); err != nil {
		t.Fatalf("write script: %v", err)
	}
	resp := `{"decision":"allow","confidence":0.95,"summary":"reads stdin source","effects":{"reads":["` + scriptPath + `"],"writes":["` + out + `"],"deletes":[],"network":[],"subprocesses":[],"unknown":[]}}`
	defer mockModelJSON(t, resp)()

	ie, ok := classifyInterpreterExecution("python - < job.py")
	if !ok {
		t.Fatal("classify failed")
	}
	if ie.SourceMode != "stdin_pipe" || ie.Entrypoint != "job.py" {
		t.Fatalf("unexpected classification: %+v", ie)
	}

	allowed, reason := a.askPermissionModelInterpreter("python - < job.py", ie)
	if !allowed {
		t.Fatalf("expected allow, got reason=%q", reason)
	}

	var cfg config.Config
	if err := config.LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("load: %v", err)
	}
	auto := cfg.Ocode.Permissions.Auto
	if auto == nil || len(auto.Grants) != 1 {
		t.Fatalf("expected 1 persisted grant, got %+v", auto)
	}
	g := auto.Grants[0]
	if g.Kind != "interpreter_exact" || g.Language != "python" || g.SourceMode != "stdin_pipe" || g.EntrypointSHA256 == "" {
		t.Fatalf("unexpected grant: %+v", g)
	}
	if g.NormalizedCommand != normalizeGrantCommand("python - < job.py") {
		t.Fatalf("unexpected normalized command: %+v", g)
	}
	if g.EntrypointPath != filepath.Join(root, "job.py") {
		t.Fatalf("unexpected entrypoint path: %+v", g)
	}

	allowed2, _ := a.askPermissionModelInterpreter("python - < job.py", ie)
	if !allowed2 {
		t.Fatal("expected grant short-circuit allow on repeat")
	}
}

func TestAskPermissionModelInterpreterInlineCodeDoesNotPersistGrant(t *testing.T) {
	a, root := newConsultAgent(t)
	resp := `{"decision":"allow","confidence":0.95,"summary":"inline code","effects":{"reads":[],"writes":[],"deletes":[],"network":[],"subprocesses":[],"unknown":[]}}`
	defer mockModelJSON(t, resp)()

	cmd := "python3 - <<'PY'\nprint('hi')\nPY"
	ie, ok := classifyInterpreterExecution(cmd)
	if !ok {
		t.Fatal("classify failed")
	}
	if ie.SourceMode != "heredoc" {
		t.Fatalf("unexpected source mode: %+v", ie)
	}
	allowed, reason := a.askPermissionModelInterpreter(cmd, ie)
	if !allowed {
		t.Fatalf("expected allow, got reason=%q", reason)
	}

	var cfg config.Config
	if err := config.LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Ocode.Permissions.Auto != nil && len(cfg.Ocode.Permissions.Auto.Grants) > 0 {
		t.Fatalf("expected no persisted grant for inline code, got %+v", cfg.Ocode.Permissions.Auto.Grants)
	}
	if a.permissions.MatchInterpreterGrant(ie, hashBytes([]byte(ie.EmbeddedBody)), true) {
		t.Fatal("expected no in-memory durable grant for inline code")
	}
	_ = root
}

func TestAskPermissionModelInterpreterFailClosed(t *testing.T) {
	cases := []struct {
		name string
		resp string
	}{
		{"low_confidence", `{"decision":"allow","confidence":0.4,"summary":"x","effects":{"reads":[],"writes":[],"deletes":[],"network":[],"subprocesses":[],"unknown":[]}}`},
		{"unknown_effects", `{"decision":"allow","confidence":0.99,"summary":"x","effects":{"reads":[],"writes":[],"deletes":[],"network":[],"subprocesses":[],"unknown":["dyn path"]}}`},
		{"decision_ask", `{"decision":"ask","confidence":0.99,"summary":"x","effects":{"reads":[],"writes":[],"deletes":[],"network":[],"subprocesses":[],"unknown":[]}}`},
		{"not_json", `I think this is fine, ALLOW`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, root := newConsultAgent(t)
			scriptPath := filepath.Join(root, "job.py")
			if err := os.WriteFile(scriptPath, []byte("print('hi')\n"), 0644); err != nil {
				t.Fatalf("write script: %v", err)
			}
			defer mockModelJSON(t, tc.resp)()
			ie, _ := classifyInterpreterExecution("python job.py")
			allowed, _ := a.askPermissionModelInterpreter("python job.py", ie)
			if allowed {
				t.Fatal("expected fail-closed (ask), got allow")
			}
		})
	}
}

func TestAskPermissionModelInterpreterScriptOutsideRootsAsks(t *testing.T) {
	a, _ := newConsultAgent(t)
	// Script path outside allowed roots — source cannot be acquired safely.
	resp := `{"decision":"allow","confidence":0.99,"summary":"x","effects":{"reads":[],"writes":[],"deletes":[],"network":[],"subprocesses":[],"unknown":[]}}`
	defer mockModelJSON(t, resp)()
	ie := &InterpreterExec{Language: "python", SourceMode: "script_file", Entrypoint: "/etc/hosts", RawCommand: "python /etc/hosts"}
	if allowed, _ := a.askPermissionModelInterpreter("python /etc/hosts", ie); allowed {
		t.Fatal("expected ask for script outside allowed roots")
	}
}

// guard: classification dispatch leaves non-interpreter bash untouched.
func TestConsultPermissionModelNonInterpreterUsesPlainPath(t *testing.T) {
	a, _ := newConsultAgent(t)
	defer mockModelJSON(t, "ALLOW: looks fine")()
	allowed, _ := a.consultPermissionModel("bash", json.RawMessage(`{"command":"ls -la"}`), &PermissionRequest{ToolName: "bash"})
	if !allowed {
		t.Fatal("expected plain ALLOW path to allow ls")
	}
}
