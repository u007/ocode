package lsp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/u007/ocode/internal/lsp/broker"
)

// startTestDaemon wires a real gopls-backed daemonUpstream behind a broker
// listener, mirroring what RunDaemon (cmd_daemon.go) does in production
// minus the StartOnce/metadata-file bootstrap — tests talk to the listener
// directly instead of discovering it on disk.
func startTestDaemon(t *testing.T, root string) (*daemonUpstream, broker.Metadata, func()) {
	t.Helper()
	real, err := NewClient("gopls")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := real.Initialize(root, "go"); err != nil {
		real.Close()
		t.Fatalf("Initialize: %v", err)
	}
	upstream := newDaemonUpstream(real, time.Minute, func() {})
	identity := broker.Identity{Root: root, Executable: "gopls", LanguageID: "go"}
	meta, err := broker.NewMetadata(identity, 1, 1)
	if err != nil {
		real.Close()
		t.Fatalf("NewMetadata: %v", err)
	}
	listener, err := broker.Listen(meta, upstream.handleConn)
	if err != nil {
		real.Close()
		t.Fatalf("Listen: %v", err)
	}
	cleanup := func() {
		listener.Close()
		real.Close()
	}
	return upstream, listener.Metadata(), cleanup
}

func connectTestBrokerClient(t *testing.T, meta broker.Metadata, id string) *Client {
	t.Helper()
	c, err := NewBrokerClient(context.Background(), meta, id)
	if err != nil {
		t.Fatalf("NewBrokerClient(%s): %v", id, err)
	}
	return c
}

// TestDaemonUpstreamReconcilesDocVersionsAcrossClients is the core
// correctness property of the shared daemon: two ocode processes (modelled
// here as two independent broker-backed *Client instances, each with its
// own private didOpen/didChange version counter) editing the same file must
// not corrupt the real server's document state. The daemon must fold the
// second client's own "first open" into a re-versioned change against one
// canonical per-URI counter instead of forwarding two conflicting streams.
func TestDaemonUpstreamReconcilesDocVersionsAcrossClients(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not installed")
	}
	tmp := t.TempDir()
	srcPath := filepath.Join(tmp, "demo.go")
	original := "package demo\n\nfunc Hello() {}\n"
	if err := os.WriteFile(srcPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	upstream, meta, cleanup := startTestDaemon(t, tmp)
	defer cleanup()

	clientA := connectTestBrokerClient(t, meta, "process-a")
	defer clientA.Close()
	clientB := connectTestBrokerClient(t, meta, "process-b")
	defer clientB.Close()

	if err := clientA.EnsureOpen(srcPath); err != nil {
		t.Fatalf("clientA.EnsureOpen: %v", err)
	}

	// Notify is fire-and-forget over the wire, so the daemon may not have
	// processed the frame yet when EnsureOpen returns.
	uri := fileURI(srcPath)
	var doc *docState
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		upstream.mu.Lock()
		doc = upstream.docs[uri]
		upstream.mu.Unlock()
		if doc != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if doc == nil || doc.refs != 1 || doc.version != 1 {
		t.Fatalf("after first open: doc=%+v, want refs=1 version=1", doc)
	}

	// clientB has never opened this URI from its own perspective, so
	// UpdateText sends its own "first" didOpen — with different content
	// than what the daemon already told the real server.
	edited := "package demo\n\nfunc Hello() {}\n\nfunc World() {}\n"
	if err := clientB.UpdateText(srcPath, edited); err != nil {
		t.Fatalf("clientB.UpdateText: %v", err)
	}

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		upstream.mu.Lock()
		doc = upstream.docs[uri]
		upstream.mu.Unlock()
		if doc != nil && doc.version == 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if doc == nil || doc.refs != 2 || doc.version != 2 || doc.text != edited {
		t.Fatalf("after second client's open: doc=%+v, want refs=2 version=2 text=%q", doc, edited)
	}
}

// TestDaemonUpstreamBroadcastsDiagnosticsToAllClients verifies the push
// extension end to end: every connected broker client's onDiagnostics
// handler (installed the same way Manager.ClientForExt installs it on an
// isolated client) must fire when the daemon's real server publishes
// diagnostics, not just the client that triggered them.
func TestDaemonUpstreamBroadcastsDiagnosticsToAllClients(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not installed")
	}
	tmp := t.TempDir()
	srcPath := filepath.Join(tmp, "demo.go")
	if err := os.WriteFile(srcPath, []byte("package demo\n\nfunc Hello() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	upstream, meta, cleanup := startTestDaemon(t, tmp)
	defer cleanup()

	clientA := connectTestBrokerClient(t, meta, "process-a")
	defer clientA.Close()
	clientB := connectTestBrokerClient(t, meta, "process-b")
	defer clientB.Close()

	gotA := make(chan string, 1)
	gotB := make(chan string, 1)
	clientA.SetDiagnosticsHandler(func(uri string, diags []Diagnostic) { gotA <- uri })
	clientB.SetDiagnosticsHandler(func(uri string, diags []Diagnostic) { gotB <- uri })

	// Exercise the fan-out directly rather than waiting on a real
	// diagnostics roundtrip from gopls (slow and content-dependent); the
	// property under test is "every connected PushSender receives the
	// broadcast", not gopls's own diagnostics behavior (covered by the
	// existing TestManagerDidChangeOnFileSave).
	uri := fileURI(srcPath)
	upstream.broadcastDiagnostics(uri, nil)

	wantURI := uri
	select {
	case got := <-gotA:
		if got != wantURI {
			t.Fatalf("clientA got uri %q, want %q", got, wantURI)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("clientA did not receive broadcast diagnostics")
	}
	select {
	case got := <-gotB:
		if got != wantURI {
			t.Fatalf("clientB got uri %q, want %q", got, wantURI)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("clientB did not receive broadcast diagnostics")
	}
}
