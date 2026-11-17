package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/agent"
)

// A leading sudo/su ask carries NeedsPassword (Unix) and must render a masked
// field. Ordinary permission asks must keep their existing rendering byte for
// byte: no field, no bullets, no elevation-only hint.
func TestElevationDialogMaskedFieldOnlyForNeedsPassword(t *testing.T) {
	m := newPermQueueTestModel(t)

	ordinary := agent.PermissionRequest{ToolName: "bash", Rule: "bash.prefix.ls", Command: "ls -la"}
	showPermAskForTest(t, &m, ordinary, "call_plain")
	plain := stripANSI(m.renderPermissionDialog(80))
	if strings.Contains(plain, "Password:") || strings.Contains(plain, "[Enter] approve") {
		t.Fatalf("ordinary ask rendered an elevation password field:\n%s", plain)
	}

	elevated := agent.PermissionRequest{
		ToolName:         "bash",
		Rule:             "bash.prefix.sudo",
		Command:          "sudo whoami",
		NeedsPassword:    true,
		ElevationCommand: "sudo whoami",
	}
	showPermAskForTest(t, &m, elevated, "call_elev")
	withField := stripANSI(m.renderPermissionDialog(80))
	if !strings.Contains(withField, "Password:") {
		t.Fatalf("elevation ask did not render a password field:\n%s", withField)
	}
	if !strings.Contains(withField, "[Enter] approve") {
		t.Fatalf("elevation ask did not render its approval hint:\n%s", withField)
	}

	// The always-allow bypass must not be offered for an elevation ask.
	if permAlwaysRuleAvailable(elevated) || permAlwaysToolAvailable(elevated) {
		t.Fatal("elevation ask must not offer always-allow choices")
	}
	for _, def := range m.permDialogBtnDefs() {
		if def.choice == "a" || def.choice == "t" {
			t.Errorf("elevation dialog offered always-allow button %q", def.choice)
		}
	}
}

// Typed password characters are echoed as bullets. The raw bytes must never
// appear in a rendered frame, a transcript message, or a log line.
func TestElevationDialogTypedInputRendersMasked(t *testing.T) {
	const password = "s3cr3t-pw"
	m := newPermQueueTestModel(t)
	showPermAskForTest(t, &m, elevationReqForTest(), "call_elev")

	m = typeIntoElevationField(t, m, password)

	rendered := stripANSI(m.renderPermissionDialog(80))
	if strings.Contains(rendered, password) {
		t.Fatalf("typed password leaked into rendered dialog:\n%s", rendered)
	}
	if !strings.Contains(rendered, "•••••••••") {
		t.Fatalf("expected 9 masked bullets, got:\n%s", rendered)
	}

	// Nothing typed during the dialog lands in the transcript/message model.
	for i, msg := range m.messages {
		if strings.Contains(msg.text, password) {
			t.Fatalf("typed password leaked into message %d", i)
		}
	}
}

// Approval (Enter, and a mouse-style "y" answer) stores the exact typed bytes
// under the ask's request id, before the approved call re-executes.
func TestElevationDialogApprovalStoresSecret(t *testing.T) {
	const password = "s3cr3t"
	m := newPermQueueTestModel(t)
	showPermAskForTest(t, &m, elevationReqForTest(), "call_elev")

	var gotID string
	var gotSecret []byte
	calls := 0
	orig := storeElevationSecret
	storeElevationSecret = func(a *agent.Agent, requestID string, secret []byte) {
		calls++
		gotID = requestID
		gotSecret = append([]byte(nil), secret...)
	}
	t.Cleanup(func() { storeElevationSecret = orig })

	m = typeIntoElevationField(t, m, password)
	upd, _ := m.handleChatKeys(tea.KeyPressMsg{Code: tea.KeyEnter}, nil, nil)
	m = upd.(model)

	if calls != 1 {
		t.Fatalf("store called %d times, want exactly 1 (on approval)", calls)
	}
	if gotID != "call_elev" {
		t.Errorf("store requestID = %q, want %q", gotID, "call_elev")
	}
	if string(gotSecret) != password {
		t.Errorf("stored secret = %q, want %q", gotSecret, password)
	}
	// The local widget is cleared after the dialog closes.
	if got := m.permPassword.Value(); got != "" {
		t.Errorf("password field still holds %q after approval", got)
	}
}

// The mouse button path calls handlePermissionChoice("y") directly; it must
// store the typed secret too, so keyboard and click approvals stay in lockstep.
func TestElevationDialogButtonClickApprovalStoresSecret(t *testing.T) {
	const password = "click-secret"
	m := newPermQueueTestModel(t)
	showPermAskForTest(t, &m, elevationReqForTest(), "call_elev")

	var gotID string
	var gotSecret []byte
	orig := storeElevationSecret
	storeElevationSecret = func(a *agent.Agent, requestID string, secret []byte) {
		gotID = requestID
		gotSecret = append([]byte(nil), secret...)
	}
	t.Cleanup(func() { storeElevationSecret = orig })

	m = typeIntoElevationField(t, m, password)
	_ = m.handlePermissionChoice("y")

	if gotID != "call_elev" || string(gotSecret) != password {
		t.Errorf("store(requestID=%q, secret=%q), want (%q, %q)", gotID, gotSecret, "call_elev", password)
	}
}

// Deny (Escape) never stores, and the typed value is dropped with the widget.
func TestElevationDialogDenyNeverStores(t *testing.T) {
	m := newPermQueueTestModel(t)
	showPermAskForTest(t, &m, elevationReqForTest(), "call_elev")

	calls := 0
	orig := storeElevationSecret
	storeElevationSecret = func(a *agent.Agent, requestID string, secret []byte) { calls++ }
	t.Cleanup(func() { storeElevationSecret = orig })

	m = typeIntoElevationField(t, m, "secret")
	upd, _ := m.handleChatKeys(tea.KeyPressMsg{Code: tea.KeyEscape}, nil, nil)
	m = upd.(model)

	if calls != 0 {
		t.Fatalf("store called %d times on Escape, want 0", calls)
	}
	if got := m.permPassword.Value(); got != "" {
		t.Errorf("password field still holds %q after deny", got)
	}
}

// Letters that are decision shortcuts on ordinary asks are password characters
// while the masked field is active — otherwise a password containing 'y' would
// approve the ask mid-typing.
func TestElevationDialogLettersArePasswordCharacters(t *testing.T) {
	m := newPermQueueTestModel(t)
	showPermAskForTest(t, &m, elevationReqForTest(), "call_elev")

	calls := 0
	orig := storeElevationSecret
	storeElevationSecret = func(a *agent.Agent, requestID string, secret []byte) { calls++ }
	t.Cleanup(func() { storeElevationSecret = orig })

	m = typeIntoElevationField(t, m, "nyat")
	if got := m.permPassword.Value(); got != "nyat" {
		t.Fatalf("password field = %q, want %q (shortcuts must be captured as input)", got, "nyat")
	}
	if calls != 0 {
		t.Fatalf("store called %d times while typing, want 0", calls)
	}

	// PermissionModal follows the same rule: letters are password input, not
	// decisions, so a password containing 'n' cannot dismiss the dialog.
	var pmChoice string
	pm := NewPermissionModal(PermissionModalConfig{
		Title: "Permission required", Body: "Allow sudo?", TermWidth: 80, TermHeight: 24,
		NeedsPassword: true,
		OnChoice:      func(c string) { pmChoice = c },
	})
	for _, r := range "nyat" {
		pm.Handle(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if pmChoice != "" {
		t.Fatalf("PermissionModal chose %q while typing a password", pmChoice)
	}
	if got := pm.password.Value(); got != "nyat" {
		t.Errorf("PermissionModal password = %q, want %q", got, "nyat")
	}
}

// Windows authenticates through UAC: NeedsPassword is false and no field is
// shown regardless of the flag carried by a hand-built request.
func TestElevationDialogWindowsShowsNoField(t *testing.T) {
	req := agent.PermissionRequest{ToolName: "bash", NeedsPassword: true, ElevationCommand: "sudo whoami"}
	if elevationNeedsPasswordFieldOnOS(req, "windows") {
		t.Fatal("Windows must never show a masked password field")
	}
	if !elevationNeedsPasswordFieldOnOS(req, "darwin") {
		t.Fatal("Unix should show a masked password field for NeedsPassword")
	}
	if elevationNeedsPasswordFieldOnOS(agent.PermissionRequest{ToolName: "bash"}, "darwin") {
		t.Fatal("ordinary ask must not show a masked password field")
	}
}

// PermissionModal (the reusable modal abstraction) must mask its field and
// hand the exact bytes to OnPassword only on approval.
func TestPermissionModalElevationMaskedInput(t *testing.T) {
	var stored []byte
	var choice string
	pm := NewPermissionModal(PermissionModalConfig{
		Title:         "Permission required",
		Body:          "Allow sudo whoami?",
		Buttons:       FormatPermissionButtons([]permBtnDef{{"Y", "y", "allow"}, {"N", "n", "deny"}}),
		TermWidth:     80,
		TermHeight:    24,
		NeedsPassword: true,
		OnPassword:    func(secret []byte) { stored = append([]byte(nil), secret...) },
		OnChoice:      func(c string) { choice = c },
	})

	if !strings.Contains(stripANSI(pm.Render()), "Password:") {
		t.Fatal("PermissionModal did not render a password field for NeedsPassword")
	}
	for _, r := range "pw123" {
		pm.Handle(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if strings.Contains(stripANSI(pm.Render()), "pw123") {
		t.Fatal("PermissionModal echoed the typed password")
	}
	pm.Handle(tea.KeyPressMsg{Code: tea.KeyEnter})
	if choice != "allow" {
		t.Errorf("choice = %q, want allow", choice)
	}
	if string(stored) != "pw123" {
		t.Errorf("OnPassword got %q, want %q", stored, "pw123")
	}
}

func TestPermissionModalElevationDenyDoesNotStore(t *testing.T) {
	stored := false
	var choice string
	pm := NewPermissionModal(PermissionModalConfig{
		Title:         "Permission required",
		Body:          "Allow sudo whoami?",
		TermWidth:     80,
		TermHeight:    24,
		NeedsPassword: true,
		OnPassword:    func(secret []byte) { stored = true },
		OnChoice:      func(c string) { choice = c },
	})
	pm.Handle(tea.KeyPressMsg{Code: 'p', Text: "p"})
	pm.Handle(tea.KeyPressMsg{Code: tea.KeyEscape})
	if choice != "deny" {
		t.Errorf("choice = %q, want deny", choice)
	}
	if stored {
		t.Fatal("OnPassword must not be called on deny")
	}
}

// Non-elevation dialogs keep their traditional y/n/a/t shortcuts unchanged.
func TestPermissionModalNonElevationShortcutsUnchanged(t *testing.T) {
	var choice string
	pm := NewPermissionModal(PermissionModalConfig{
		Title:      "Permission required",
		Body:       "Allow ls?",
		TermWidth:  80,
		TermHeight: 24,
		OnChoice:   func(c string) { choice = c },
	})
	if strings.Contains(stripANSI(pm.Render()), "Password:") {
		t.Fatal("ordinary dialog showed a password field")
	}
	pm.Handle(tea.KeyPressMsg{Code: 'a'})
	if choice != "always-allow" {
		t.Errorf("choice = %q, want always-allow", choice)
	}
}

// TestElevationDialogSubAgentApprovalCarriesPassword closes the TUI half of the
// sub-agent elevation gap: the masked password typed for a sub-agent ask must
// travel in the PermissionResponse to the child, and must NOT be stored on the
// parent agent's ElevationManager.
func TestElevationDialogSubAgentApprovalCarriesPassword(t *testing.T) {
	const password = "sub-dialog-pw"
	m := newPermQueueTestModel(t)
	respCh := make(chan agent.PermissionResponse, 1)
	upd, _ := m.Update(subAgentPermAskMsg{req: elevationReqForTest(), respCh: respCh})
	m = upd.(model)
	if m.pendingSubAgentResp == nil {
		t.Fatal("precondition: sub-agent dialog not open")
	}
	m = typeIntoElevationField(t, m, password)

	storeCalls := 0
	orig := storeElevationSecret
	storeElevationSecret = func(*agent.Agent, string, []byte) { storeCalls++ }
	t.Cleanup(func() { storeElevationSecret = orig })

	_, _ = m.answerPermAsk("y")
	select {
	case resp := <-respCh:
		if resp.Level != agent.PermissionAllow {
			t.Fatalf("delivered level = %v, want allow", resp.Level)
		}
		if resp.ElevationPassword != password {
			t.Fatalf("delivered elevation password = %q, want %q", resp.ElevationPassword, password)
		}
	default:
		t.Fatal("no response delivered to the parked sub-agent")
	}
	if storeCalls != 0 {
		t.Fatalf("parent store called %d times for a sub-agent ask; must be 0", storeCalls)
	}
	for _, msg := range m.messages {
		if strings.Contains(msg.text, password) {
			t.Fatalf("password leaked into a transcript message: %q", msg.text)
		}
	}
}

func elevationReqForTest() agent.PermissionRequest {
	return agent.PermissionRequest{
		ToolName:         "bash",
		Scope:            agent.PermissionScopeBashPrefix,
		Rule:             "bash.prefix.sudo",
		Prefix:           "sudo",
		Command:          "sudo whoami",
		NeedsPassword:    true,
		ElevationCommand: "sudo whoami",
	}
}

func showPermAskForTest(t *testing.T, m *model, req agent.PermissionRequest, callID string) {
	t.Helper()
	m.showPermAsk(queuedPermAsk{req: req, toolCallID: callID})
}

func typeIntoElevationField(t *testing.T, m model, password string) model {
	t.Helper()
	for _, r := range password {
		upd, _ := m.handleChatKeys(tea.KeyPressMsg{Code: r, Text: string(r)}, nil, nil)
		m = upd.(model)
	}
	return m
}
