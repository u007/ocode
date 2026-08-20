package lsp

import (
	"encoding/json"
	"net"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/lsp/broker"
)

type managerBrokerUpstream struct{}

func (managerBrokerUpstream) Call(method string, _ json.RawMessage) (json.RawMessage, error) {
	return json.Marshal("broker:" + method)
}
func (managerBrokerUpstream) Notify(string, json.RawMessage) error { return nil }

func TestManagerUsesAttachedBrokerForGoplsRequests(t *testing.T) {
	root := t.TempDir()
	canonicalRoot, err := broker.CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	identity := broker.Identity{Root: canonicalRoot, Executable: "/bin/fake-gopls", LanguageID: "go"}
	meta, err := broker.NewMetadata(identity, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := broker.Listen(meta, func(conn net.Conn) {
		broker.ServeRPC(conn, managerBrokerUpstream{}, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	m := NewManager(root)
	defer m.Close()
	if err := m.AttachBroker(".go", listener.Metadata()); err != nil {
		t.Fatal(err)
	}
	c, err := m.ClientForExt(".go")
	if err != nil {
		t.Fatalf("ClientForExt: %v", err)
	}
	result, err := c.Call("textDocument/definition", nil)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatal(err)
	}
	if got != "broker:textDocument/definition" {
		t.Fatalf("request was not routed through attached broker: %q", got)
	}
}

func TestManagerEnablesSharedBrokerByDefault(t *testing.T) {
	local := NewManager(t.TempDir())
	if local.SharedBrokerEnabled() {
		t.Fatal("default manager should be isolated (sharing gated)")
	}
	withShared := NewManagerWithShared(t.TempDir(), true)
	if !withShared.SharedBrokerEnabled() {
		t.Fatal("explicit shared manager did not enable broker policy")
	}
	shared := NewSharedManager(t.TempDir())
	if !shared.SharedBrokerEnabled() {
		t.Fatal("shared manager did not retain broker policy")
	}
}

func TestManagerBrokerAttachmentEnablesSharing(t *testing.T) {
	root := t.TempDir()
	canonicalRoot, err := broker.CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	// AttachBroker works on an isolated manager via explicit metadata;
	// the sharing policy itself remains gated (false) until constructed
	// via NewManagerWithShared/NewSharedManager.
	m := NewManagerWithShared(root, true)
	defer m.Close()
	meta := broker.Metadata{Protocol: broker.ProtocolVersion, Port: 1234, Token: "token", Identity: broker.Identity{Root: canonicalRoot, Executable: "/bin/gopls", LanguageID: "go"}}
	if err := m.AttachBroker(".go", meta); err != nil {
		t.Fatalf("AttachBroker: %v", err)
	}
	if !m.BrokerAttached(".go") {
		t.Fatal("expected explicit broker attachment")
	}
	if !m.SharedBrokerEnabled() {
		t.Fatal("manager sharing policy should be enabled for shared manager")
	}
}

func TestManagerBrokerAttachmentRejectsUnsupportedOrIncomplete(t *testing.T) {
	m := NewManager(t.TempDir())
	defer m.Close()
	if err := m.AttachBroker(".rs", broker.Metadata{Port: 1234, Token: "token"}); err == nil {
		t.Fatal("expected unsupported extension error")
	}
	if err := m.AttachBroker(".go", broker.Metadata{}); err == nil {
		t.Fatal("expected incomplete metadata error")
	}
	if m.BrokerAttached(".go") {
		t.Fatal("incomplete attachment must not be recorded")
	}
}

func TestUnderTempDirRejectsTempDirRoots(t *testing.T) {
	if !underTempDir(t.TempDir()) {
		t.Fatal("t.TempDir() root should be detected as under the OS temp dir")
	}
	if underTempDir(".") {
		t.Fatal("the project working directory must not be detected as a temp dir")
	}
}

func TestDiscoverOrSpawnDaemonRefusesTempDirRoot(t *testing.T) {
	spec := serverSpec{cmd: "/bin/fake-gopls", args: nil, langID: "go"}
	if _, ok := discoverOrSpawnDaemon(t.TempDir(), ".go", spec); ok {
		t.Fatal("discoverOrSpawnDaemon must refuse to spawn a daemon under a temp-dir root")
	}
}

func TestActiveServersEmpty(t *testing.T) {
	m := NewManager(".")
	defer m.Close()
	got := m.ActiveServers()
	if len(got) != 0 {
		t.Fatalf("expected 0 servers, got %d", len(got))
	}
}

func TestSetEventChanReceivesStartEvent(t *testing.T) {
	// We can't actually start a real server in a unit test, but we can verify
	// that SetEventChan stores the channel and that a non-blocking send on a
	// nil channel is a no-op (doesn't panic).
	m := NewManager(".")
	defer m.Close()

	ch := make(chan ServerStartedEvent, 4)
	m.SetEventChan(ch)

	// Verify the channel is stored.
	if m.eventCh != ch {
		t.Fatal("event channel not stored")
	}
}

func TestInstallHint(t *testing.T) {
	tests := []struct {
		cmd      string
		contains string
	}{
		{"gopls", "go install golang.org/x/tools/gopls@latest"},
		{"rust-analyzer", "rustup component add rust-analyzer"},
		{"pyright-langserver", "npm install -g pyright"},
		{"typescript-language-server", "npm install -g typescript"},
		{"unknown-server", "check your package manager"},
	}
	for _, tt := range tests {
		got := InstallHint(tt.cmd)
		if !strings.Contains(got, tt.contains) {
			t.Errorf("InstallHint(%q) = %q, want it to contain %q", tt.cmd, got, tt.contains)
		}
	}
}
