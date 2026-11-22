package agent

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/shell/sandbox"
)

// loginStateFixture lays out the data dir the way production does: an auth.json
// (the reference for the "same class" outcomes), the login-state tree with one
// state file, a sibling directory that only shares the prefix, and an unrelated
// file in the data dir root. It runs under an isolated HOME.
type loginStateFixture struct {
	dataDir  string
	authPath string
	lsDir    string
	lsFile   string
	sibling  string
	other    string
}

func newLoginStateFixture(t *testing.T) loginStateFixture {
	t.Helper()
	isolateConfigHome(t)
	dataDir, err := paths.GlobalDataDir()
	if err != nil {
		t.Fatalf("GlobalDataDir: %v", err)
	}
	f := loginStateFixture{
		dataDir:  dataDir,
		authPath: filepath.Join(dataDir, "auth.json"),
		lsDir:    filepath.Join(dataDir, "browse", "login-states"),
		other:    filepath.Join(dataDir, "other.json"),
	}
	f.lsFile = filepath.Join(f.lsDir, "example.json")
	f.sibling = filepath.Join(dataDir, "browse", "login-states-old", "x.json")
	for _, d := range []string{f.lsDir, filepath.Dir(f.sibling)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{f.authPath, f.lsFile, f.sibling, f.other} {
		if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func newLoginStatePM(t *testing.T, mode PermissionMode) *PermissionManager {
	t.Helper()
	pm := NewPermissionManager()
	pm.SetWorkDir(t.TempDir())
	pm.SetMode(mode)
	return pm
}

// isLoginStateAsk reports whether d is the login-state gate's Ask, for either
// the file-tool rule or the bash synthetic prefix.
func isLoginStateAsk(d PermissionDecision) bool {
	if d.Level != PermissionAsk || d.Request == nil {
		return false
	}
	return strings.HasSuffix(d.Request.Rule, ".login_state") || d.Request.Prefix == "permission.login_state"
}

// fileToolCall is one path-scoped tool call that names target, in the shape
// the tool actually takes.
type fileToolCall struct {
	tool string
	args string
}

func fileToolCalls(target string) []fileToolCall {
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	return []fileToolCall{
		{"read", `{"path":` + q(target) + `}`},
		{"grep", `{"pattern":"x","path":` + q(target) + `}`},
		{"glob", `{"pattern":` + q(target) + `}`},
		{"list", `{"path":` + q(filepath.Dir(target)) + `}`},
		{"write", `{"path":` + q(target) + `,"content":"x"}`},
		{"edit", `{"file_path":` + q(target) + `,"old_string":"a","new_string":"b"}`},
		{"delete", `{"path":` + q(target) + `}`},
		{"multiedit", `{"file_path":` + q(target) + `,"edits":[{"oldString":"a","newString":"b"}]}`},
		{"multi_file_edit", `{"edits":[{"path":` + q(target) + `,"search":"a","replace":"b"}]}`},
		{"apply_patch", `{"patchText":"*** Begin Patch\n*** Update File: ` + target + `\n@@\n-a\n+b\n*** End Patch"}`},
		{"apply_patch", `{"patchText":"*** Begin Patch\n*** Update File: ` + filepath.Join(filepath.Dir(target), "..", "other.json") + `\n*** Move to: ` + target + `\n@@\n-a\n+b\n*** End Patch"}`},
		{"apply_patch", `{"patchText":"*** Begin Patch\n*** Delete File: ` + target + `\n*** End Patch"}`},
	}
}

// TestLoginStateFileToolsAsk: every path-scoped tool that reads or writes a
// login-state file is Ask in normal and sandbox, for reads and writes alike,
// and a user "always allow" for the tool does not reopen it.
func TestLoginStateFileToolsAsk(t *testing.T) {
	f := newLoginStateFixture(t)
	for _, mode := range []PermissionMode{PermissionModeNormal, PermissionModeSandbox} {
		for _, c := range fileToolCalls(f.lsFile) {
			pm := newLoginStatePM(t, mode)
			d := pm.Decide(c.tool, json.RawMessage(c.args))
			if !isLoginStateAsk(d) {
				t.Errorf("mode=%s %s(%s) = %s (rule %v), want the login-state Ask", mode, c.tool, c.args, d.Level, ruleOf(d))
			}
		}
	}

	t.Run("user always-allow for the tool does not reopen it", func(t *testing.T) {
		for _, tool := range []string{"read", "write", "edit", "grep", "list"} {
			pm := newLoginStatePM(t, PermissionModeNormal)
			pm.SetRule(tool, PermissionAllow)
			pm.SetUserConfirmedRule(tool, PermissionAllow)
			args := `{"path":` + strconv.Quote(f.lsFile) + `}`
			if tool == "write" || tool == "edit" {
				args = `{"path":` + strconv.Quote(f.lsFile) + `,"content":"x"}`
			}
			if d := pm.Decide(tool, json.RawMessage(args)); !isLoginStateAsk(d) {
				t.Errorf("%s with a user allow rule = %s, want the login-state Ask", tool, d.Level)
			}
		}
	})
}

// TestLoginStateFileToolsLockedDeny: locked mode refuses read-only tools on
// login-state data, and its existing write refusal stands.
func TestLoginStateFileToolsLockedDeny(t *testing.T) {
	f := newLoginStateFixture(t)
	for _, c := range fileToolCalls(f.lsFile) {
		pm := newLoginStatePM(t, PermissionModeLocked)
		d := pm.Decide(c.tool, json.RawMessage(c.args))
		if d.Level != PermissionDeny {
			t.Errorf("locked %s(%s) = %s, want Deny", c.tool, c.args, d.Level)
		}
	}
}

// TestLoginStateYoloMirrorsAuthJson: yolo has no static checks, and login-state
// files get exactly the auth.json outcome for every file tool. Both are Allow
// today. The decision is recorded in the report, not hidden here.
func TestLoginStateYoloMirrorsAuthJson(t *testing.T) {
	f := newLoginStateFixture(t)
	for _, c := range fileToolCalls(f.lsFile) {
		authCall := strings.ReplaceAll(c.args, f.lsFile, f.authPath)
		pm := newLoginStatePM(t, PermissionModeYOLO)
		got := pm.Decide(c.tool, json.RawMessage(c.args)).Level
		want := newLoginStatePM(t, PermissionModeYOLO).Decide(c.tool, json.RawMessage(authCall)).Level
		if got != want {
			t.Errorf("yolo %s: login-state = %s, auth.json = %s; want parity", c.tool, got, want)
		}
		if got != PermissionAllow {
			t.Errorf("yolo %s: login-state = %s, want Allow (matches auth.json)", c.tool, got)
		}
	}
}

// TestLoginStateBashAsk: a shell line that names the login-state directory is
// Ask in normal and sandbox, whatever the command, and a persisted allow rule
// for the command cannot satisfy it.
func TestLoginStateBashAsk(t *testing.T) {
	f := newLoginStateFixture(t)
	dst := filepath.Join(t.TempDir(), "x")
	cmds := []string{
		"cat " + f.lsFile,
		"cat < " + f.lsFile,
		"cp " + f.lsFile + " " + dst,
		"cp " + f.other + " " + filepath.Join(f.lsDir, "assigned.json"),
		"cd " + f.lsDir + " && cat example.json",
		"cd " + f.dataDir + "/browse && cat login-states/example.json",
		"cat \"$HOME/.local/share/opencode/browse/login-states/example.json\"",
		"tee " + filepath.Join(f.lsDir, "y.json") + " </dev/null",
		"echo '{}' > " + filepath.Join(f.lsDir, "z.json"),
		"python3 -c \"print(open('" + f.lsFile + "').read())\"",
		"ls " + f.lsDir,
	}
	for _, mode := range []PermissionMode{PermissionModeNormal, PermissionModeSandbox} {
		for _, cmd := range cmds {
			pm := newLoginStatePM(t, mode)
			args := json.RawMessage(`{"command":` + strconv.Quote(cmd) + `}`)
			d := pm.Decide("bash", args)
			if !isLoginStateAsk(d) {
				t.Errorf("mode=%s command %q = %s (rule %v), want the login-state Ask", mode, cmd, d.Level, ruleOf(d))
			}
		}
	}

	t.Run("persisted cat prefix allow cannot satisfy it", func(t *testing.T) {
		pm := newLoginStatePM(t, PermissionModeNormal)
		pm.SetBashPrefixRule("cat", PermissionAllow)
		args := json.RawMessage(`{"command":"cat ` + f.lsFile + `"}`)
		if d := pm.Decide("bash", args); !isLoginStateAsk(d) {
			t.Errorf("cat with a persisted allow = %s, want the login-state Ask", d.Level)
		}
	})
}

// TestLoginStateBashVetoesJudgeGrant: verifyAutoGrant (agent.go) refuses a
// judge approval of a bash command when redirectionVetoReason names a target.
// A login-state target must veto, an unrelated one must not.
func TestLoginStateBashVetoesJudgeGrant(t *testing.T) {
	f := newLoginStateFixture(t)
	pm := newLoginStatePM(t, PermissionModeNormal)
	for _, cmd := range []string{
		"cat " + f.lsFile,
		"cp " + f.other + " " + filepath.Join(f.lsDir, "assigned.json"),
		"cd " + f.lsDir + " && cat example.json",
	} {
		if reason := pm.redirectionVetoReason(cmd); !strings.Contains(reason, "login-state") {
			t.Errorf("redirectionVetoReason(%q) = %q, want a login-state veto", cmd, reason)
		}
	}
	if reason := pm.redirectionVetoReason("cat " + f.other); strings.Contains(reason, "login-state") {
		t.Errorf("unrelated data-dir file vetoed as login-state: %q", reason)
	}
}

// TestLoginStatePrefixSiblingNotCaught: browse/login-states-old is a different
// directory and keeps today's behaviour (read and cat allow, no login-state Ask).
func TestLoginStatePrefixSiblingNotCaught(t *testing.T) {
	f := newLoginStateFixture(t)
	for _, mode := range []PermissionMode{PermissionModeNormal, PermissionModeSandbox} {
		pm := newLoginStatePM(t, mode)
		if d := pm.Decide("read", json.RawMessage(`{"path":`+strconv.Quote(f.sibling)+`}`)); isLoginStateAsk(d) || d.Level != PermissionAllow {
			t.Errorf("mode=%s read of sibling = %s, want Allow (not login-state)", mode, d.Level)
		}
		if d := pm.Decide("bash", json.RawMessage(`{"command":"cat `+f.sibling+`"}`)); isLoginStateAsk(d) {
			t.Errorf("mode=%s cat of sibling got the login-state Ask", mode)
		}
	}
	if isLoginStatePath(f.sibling, "") {
		t.Errorf("isLoginStatePath(%q) = true, want false for the sibling", f.sibling)
	}
}

// TestLoginStateUnrelatedDataFileUnchanged: an ordinary file in the data dir
// root keeps today's file-tool outcomes in normal and sandbox.
func TestLoginStateUnrelatedDataFileUnchanged(t *testing.T) {
	f := newLoginStateFixture(t)
	for _, mode := range []PermissionMode{PermissionModeNormal, PermissionModeSandbox} {
		for _, tool := range []string{"read", "write", "edit"} {
			pm := newLoginStatePM(t, mode)
			args := `{"path":` + strconv.Quote(f.other) + `}`
			if tool != "read" {
				args = `{"path":` + strconv.Quote(f.other) + `,"content":"x"}`
			}
			if d := pm.Decide(tool, json.RawMessage(args)); d.Level != PermissionAllow || isLoginStateAsk(d) {
				t.Errorf("mode=%s %s(other.json) = %s, want Allow as before", mode, tool, d.Level)
			}
		}
	}
}

// TestLoginStateApplyPatchAndMultiEditPaths: apply_patch and multi_file_edit
// have no path in the generic extraction, so the login-state check must read
// their headers and edits itself. An Update, Move-to target or Delete is caught.
func TestLoginStateApplyPatchAndMultiEditPaths(t *testing.T) {
	f := newLoginStateFixture(t)
	pm := newLoginStatePM(t, PermissionModeNormal)
	if got, ok := loginStateToolPaths("apply_patch", json.RawMessage(`{"patchText":"*** Begin Patch\n*** Add File: a.txt\n+x\n*** Move to: `+f.lsFile+`\n*** End Patch"}`)); !ok || len(got) != 2 {
		t.Fatalf("apply_patch paths = %v (ok=%v), want both the Add File and Move to headers", got, ok)
	}
	if hit := pm.loginStateToolHit("multi_file_edit", json.RawMessage(`{"edits":[{"path":"a.txt"},{"path":`+strconv.Quote(f.lsFile)+`}]}`)); hit == "" {
		t.Errorf("multi_file_edit second edit into login-state not caught")
	}
	if hit := pm.loginStateToolHit("apply_patch", json.RawMessage(`{"patchText":"*** Begin Patch\n*** Add File: notes.txt\n+x\n*** End Patch"}`)); hit != "" {
		t.Errorf("apply_patch of an ordinary file flagged as login-state: %q", hit)
	}
}

// TestLoginStateSymlinkedAlias: a workdir symlink that points into the
// login-state directory is caught by the resolved match, for file tools and bash.
func TestLoginStateSymlinkedAlias(t *testing.T) {
	f := newLoginStateFixture(t)
	pm := newLoginStatePM(t, PermissionModeNormal)
	alias := filepath.Join(pm.workDir, "alias")
	if err := os.Symlink(f.lsDir, alias); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if d := pm.Decide("read", json.RawMessage(`{"path":"alias/example.json"}`)); !isLoginStateAsk(d) {
		t.Errorf("read through a symlinked alias = %s, want the login-state Ask", d.Level)
	}
	if d := pm.Decide("bash", json.RawMessage(`{"command":"cat alias/example.json"}`)); !isLoginStateAsk(d) {
		t.Errorf("cat through a symlinked alias = %s, want the login-state Ask", d.Level)
	}
}

// TestLoginStatePathPredicate pins the path predicate on the cases that decide
// the rule: the directory itself, a file beneath it, the $HOME spelling, a
// relative spelling, and the sibling that must not match.
func TestLoginStatePathPredicate(t *testing.T) {
	f := newLoginStateFixture(t)
	cases := []struct {
		name    string
		path    string
		workDir string
		want    bool
	}{
		{"dir itself", f.lsDir, "", true},
		{"file beneath", f.lsFile, "", true},
		{"tmp file", filepath.Join(f.lsDir, "x.tmp"), "", true},
		{"lock file", filepath.Join(f.lsDir, "x.lock.json"), "", true},
		{"relative spelling", "browse/login-states/x.json", "", true},
		{"home spelling", "$HOME/.local/share/opencode/browse/login-states/x.json", "", true},
		{"sibling prefix", f.sibling, "", false},
		{"data dir root", f.other, "", false},
		{"auth.json", f.authPath, "", false},
		{"empty", "", "", false},
	}
	for _, c := range cases {
		if got := isLoginStatePath(c.path, c.workDir); got != c.want {
			t.Errorf("%s: isLoginStatePath(%q) = %v, want %v", c.name, c.path, got, c.want)
		}
	}
}

// TestLoginStateMentionBoundary pins the literal-mention boundary: a name byte
// after the segment (login-states-old, login-states.bak) is not the directory.
func TestLoginStateMentionBoundary(t *testing.T) {
	for text, want := range map[string]bool{
		"browse/login-states":            true,
		"browse/login-states/x.json":     true,
		"cat $D/browse/login-states":     true,
		"browse/login-states-old/x.json": false,
		"browse/login-states.bak":        false,
		"browse/login-statesX":           false,
		"unrelated text":                 false,
	} {
		if got := mentionsLoginStates(text); got != want {
			t.Errorf("mentionsLoginStates(%q) = %v, want %v", text, got, want)
		}
	}
}

// TestLoginStateDirFollowsDataDirResolution: the login-state directory is
// computed from paths.GlobalDataDir, so an overridden HOME (through the
// setHomeTree helper, which keeps the XDG variables consistent on every
// platform) moves it with the data dir.
func TestLoginStateDirFollowsDataDirResolution(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	dataDir, err := paths.GlobalDataDir()
	if err != nil {
		t.Fatalf("GlobalDataDir: %v", err)
	}
	got, err := loginStateDir()
	if err != nil {
		t.Fatalf("loginStateDir: %v", err)
	}
	want := filepath.Join(dataDir, "browse", "login-states")
	if got != want {
		t.Fatalf("loginStateDir() = %q, want %q", got, want)
	}
	if !strings.HasPrefix(got, home+string(filepath.Separator)) {
		t.Fatalf("loginStateDir() = %q, want it under the overridden home %q", got, home)
	}
}

// TestLoginStateDirMatchesBoxsession ties this package's definition to
// boxsession's. The agent package cannot import boxsession (a cycle risk, and
// it must build without the boxsession and server packages), so the test parses
// boxsession's source: the loginStateDirName constant and the "browse" segment
// in DefaultLoginStateDir must match loginStateSegment.
func TestLoginStateDirMatchesBoxsession(t *testing.T) {
	src := filepath.Join("..", "boxsession", "loginstate.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, src, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", src, err)
	}
	var dirName string
	var defaultBody *ast.BlockStmt
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if name.Name != "loginStateDirName" || i >= len(vs.Values) {
						continue
					}
					if lit, ok := vs.Values[i].(*ast.BasicLit); ok {
						dirName, _ = strconv.Unquote(lit.Value)
					}
				}
			}
		case *ast.FuncDecl:
			if d.Name.Name == "DefaultLoginStateDir" {
				defaultBody = d.Body
			}
		}
	}
	if dirName == "" {
		t.Fatalf("loginStateDirName constant not found in %s", src)
	}
	if defaultBody == nil {
		t.Fatalf("DefaultLoginStateDir not found in %s", src)
	}
	hasBrowse := false
	ast.Inspect(defaultBody, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if v, _ := strconv.Unquote(lit.Value); v == "browse" {
				hasBrowse = true
			}
		}
		return true
	})
	if !hasBrowse {
		t.Fatalf("DefaultLoginStateDir in %s does not join the \"browse\" segment", src)
	}
	if want := "browse/" + dirName; want != loginStateSegment {
		t.Fatalf("login-state segment drift: agent has %q, boxsession has %q", loginStateSegment, want)
	}
}

// TestLoginStateSandboxRootsNonWritable: the login-state directory is a
// protected carve-out in the OS write-wall (NewRootSet). It is never a writable
// root, nor under one, while a sibling under browse/ stays writable.
func TestLoginStateSandboxRootsNonWritable(t *testing.T) {
	f := newLoginStateFixture(t)
	sibWritable := filepath.Join(f.dataDir, "browse", "cdp-profile")
	if err := os.MkdirAll(sibWritable, 0o755); err != nil {
		t.Fatal(err)
	}
	pm := newLoginStatePM(t, PermissionModeSandbox)
	roots := sandbox.NewRootSet(pm.AllowedRootsClassified())

	lsResolved, err := filepath.EvalSymlinks(f.lsDir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", f.lsDir, err)
	}
	for _, w := range roots.WritableRoots {
		if pathUnderRoot(lsResolved, w) {
			t.Fatalf("login-state dir %q is writable under root %q; roots=%v", lsResolved, w, roots.WritableRoots)
		}
	}
	sibResolved, _ := filepath.EvalSymlinks(sibWritable)
	found := false
	for _, w := range roots.WritableRoots {
		if pathUnderRoot(sibResolved, w) {
			found = true
		}
	}
	if !found {
		t.Fatalf("sibling %q under browse/ lost its writable grant; roots=%v", sibResolved, roots.WritableRoots)
	}
}

func ruleOf(d PermissionDecision) string {
	if d.Request == nil {
		return "-"
	}
	return d.Request.Rule
}

// TestLoginStateMediaAndOCRToolsAsk covers the tools that take a path but are
// not pathScopedTools. imagegen and ocr read a file and send its bytes to a
// provider, and the media tools write output_path, so a login-state target must
// ask for them too. Before this gate every one of them was Allow by default.
func TestLoginStateMediaAndOCRToolsAsk(t *testing.T) {
	f := newLoginStateFixture(t)
	pm := newLoginStatePM(t, PermissionModeNormal)
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	cases := []fileToolCall{
		{"imagegen", `{"prompt":"x","image_path":` + q(f.lsFile) + `}`},
		{"imagegen", `{"prompt":"x","output_path":` + q(f.lsDir) + `}`},
		{"videogen", `{"prompt":"x","output_path":` + q(f.lsFile) + `}`},
		{"audiogen", `{"text":"x","output_path":` + q(f.lsFile) + `}`},
		{"ocr", `{"image_path":` + q(f.lsFile) + `}`},
	}
	for _, c := range cases {
		d := pm.Decide(c.tool, json.RawMessage(c.args))
		if !isLoginStateAsk(d) {
			t.Errorf("%s %s: decision = %+v, want a login-state ask", c.tool, c.args, d)
			continue
		}
		if !IsLoginStateRequest(*d.Request) {
			t.Errorf("%s: request is not recognised as a login-state ask", c.tool)
		}
	}

	// Positive control: a media tool on an ordinary path keeps its default
	// allow, so the gate did not simply start asking for every call.
	ordinary := filepath.Join(t.TempDir(), "picture.png")
	if d := pm.Decide("imagegen", json.RawMessage(`{"prompt":"x","image_path":`+q(ordinary)+`}`)); d.Level != PermissionAllow {
		t.Errorf("imagegen on an ordinary path = %+v, want allow", d)
	}
}
