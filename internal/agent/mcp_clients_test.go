package agent

import (
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/mcp"
)

// Process-level reuse (a real server spawned once and reused across two loads)
// is covered by the server integration path, not here: these tests pin the pure
// decision rules, the signature, and the registry's own bookkeeping so a
// regression is caught without starting a subprocess.

func TestMCPConfigSignatureStable(t *testing.T) {
	base := config.MCPConfig{
		Type:        "local",
		Command:     []string{"npx", "-y", "server"},
		Environment: map[string]string{"A": "1", "B": "2"},
		Headers:     map[string]string{"H": "v"},
		Timeout:     1000,
	}
	// Map insertion order must not matter: encoding/json sorts map keys.
	sameMapReordered := config.MCPConfig{
		Type:        "local",
		Command:     []string{"npx", "-y", "server"},
		Environment: map[string]string{"B": "2", "A": "1"},
		Headers:     map[string]string{"H": "v"},
		Timeout:     1000,
	}
	if mcpConfigSignature(base) != mcpConfigSignature(sameMapReordered) {
		t.Error("signature differs for equal configs; map serialization is not deterministic")
	}
	if mcpConfigSignature(base) != mcpConfigSignature(base) {
		t.Error("signature is not stable across calls")
	}

	changed := []struct {
		name string
		cfg  config.MCPConfig
	}{
		{"command", config.MCPConfig{Type: "local", Command: []string{"npx", "-y", "other"}, Environment: base.Environment, Headers: base.Headers, Timeout: 1000}},
		{"environment", config.MCPConfig{Type: "local", Command: base.Command, Environment: map[string]string{"A": "1", "B": "3"}, Headers: base.Headers, Timeout: 1000}},
		{"headers", config.MCPConfig{Type: "local", Command: base.Command, Environment: base.Environment, Headers: map[string]string{"H": "changed"}, Timeout: 1000}},
		{"timeout", config.MCPConfig{Type: "local", Command: base.Command, Environment: base.Environment, Headers: base.Headers, Timeout: 2000}},
		{"url", config.MCPConfig{Type: "remote", URL: "https://example.invalid/mcp", Timeout: 1000}},
		{"oauth", config.MCPConfig{Type: "remote", URL: "https://example.invalid/mcp", Timeout: 1000, OAuth: &config.MCPOAuthConfig{ClientID: "abc"}}},
	}
	want := mcpConfigSignature(base)
	for _, tc := range changed {
		if got := mcpConfigSignature(tc.cfg); got == want {
			t.Errorf("signature ignored a change to %s", tc.name)
		}
	}
}

func TestDecideMCPClientAction(t *testing.T) {
	entry := &mcpClientEntry{signature: "sig-a"}
	cases := []struct {
		name  string
		entry *mcpClientEntry
		sig   string
		alive bool
		want  mcpClientAction
	}{
		{"no entry starts a client", nil, "sig-a", false, mcpActionStartNew},
		{"same signature and alive reuses", entry, "sig-a", true, mcpActionReuse},
		{"same signature but dead replaces", entry, "sig-a", false, mcpActionReplace},
		{"changed signature replaces", entry, "sig-b", true, mcpActionReplace},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := decideMCPClientAction(tc.entry, tc.sig, tc.alive); got != tc.want {
				t.Errorf("decideMCPClientAction = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMCPClientsToRetire(t *testing.T) {
	entries := map[string]mcpClientEntry{
		"kept":    {signature: "sig-a"},
		"changed": {signature: "sig-old"},
		"removed": {signature: "sig-b"},
	}
	enabled := map[string]string{
		"kept":    "sig-a",
		"changed": "sig-new",
	}
	got := mcpClientsToRetire(entries, enabled)
	if len(got) != 2 || got[0] != "changed" || got[1] != "removed" {
		t.Fatalf("mcpClientsToRetire = %v, want [changed removed]", got)
	}
	unchanged := map[string]mcpClientEntry{"kept": {signature: "sig-a"}}
	if got := mcpClientsToRetire(unchanged, map[string]string{"kept": "sig-a"}); len(got) != 0 {
		t.Errorf("mcpClientsToRetire = %v, want none", got)
	}
}

func TestMCPClientAlive(t *testing.T) {
	remote, err := mcp.NewRemoteClient("shared-remote", config.MCPConfig{
		Type: "remote",
		URL:  "https://example.invalid/mcp",
	})
	if err != nil {
		t.Fatalf("NewRemoteClient: %v", err)
	}
	if !remote.Alive() {
		t.Error("remote client reported dead; it holds no process")
	}

	// A local process that has exited closes its stdout reader, which is what
	// Alive reads.
	dead, err := mcp.NewLocalClient("shared-dead", config.MCPConfig{
		Type:    "local",
		Command: []string{"sh", "-c", "exit 0"},
	})
	if err != nil {
		t.Fatalf("NewLocalClient: %v", err)
	}
	defer dead.Close()
	deadline := time.Now().Add(5 * time.Second)
	for dead.Alive() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if dead.Alive() {
		t.Error("local client with an exited process reported alive")
	}

	// A local process still running is alive.
	live, err := mcp.NewLocalClient("shared-live", config.MCPConfig{
		Type:    "local",
		Command: []string{"cat"},
	})
	if err != nil {
		t.Fatalf("NewLocalClient: %v", err)
	}
	defer live.Close()
	if !live.Alive() {
		t.Error("running local client reported dead")
	}
}

func TestCloseAllMCPClientsIdempotent(t *testing.T) {
	remote, err := mcp.NewRemoteClient("shared-close", config.MCPConfig{
		Type: "remote",
		URL:  "https://example.invalid/mcp",
	})
	if err != nil {
		t.Fatalf("NewRemoteClient: %v", err)
	}
	mcpRegistryStore("shared-close", mcpClientEntry{signature: "sig", client: remote})

	CloseAllMCPClients()
	if entries := mcpRegistryEntries(); len(entries) != 0 {
		t.Fatalf("registry still holds %d entries after CloseAll", len(entries))
	}
	// Closing a closed registry is a no-op, so shutdown can call it twice.
	CloseAllMCPClients()
	if entries := mcpRegistryEntries(); len(entries) != 0 {
		t.Fatalf("registry still holds %d entries after a second CloseAll", len(entries))
	}
}

// The registry must never keep a client for a server that is no longer enabled
// and must close it through Close (a remote Close is a no-op, which is enough
// to prove the path runs without a process).
func TestLoadMCPToolsForConfigSharedRetiresRemovedServer(t *testing.T) {
	remote, err := mcp.NewRemoteClient("shared-removed", config.MCPConfig{
		Type: "remote",
		URL:  "https://example.invalid/mcp",
	})
	if err != nil {
		t.Fatalf("NewRemoteClient: %v", err)
	}
	mcpRegistryStore("shared-removed", mcpClientEntry{
		signature: mcpConfigSignature(config.MCPConfig{Type: "remote", URL: "https://example.invalid/mcp"}),
		client:    remote,
	})
	t.Cleanup(CloseAllMCPClients)

	res := LoadMCPToolsForConfigShared(&config.Config{MCP: map[string]config.MCPConfig{}})
	if len(res.Tools) != 0 || len(res.Errors) != 0 {
		t.Fatalf("load with no enabled servers returned %+v", res)
	}
	if entries := mcpRegistryEntries(); len(entries) != 0 {
		t.Fatalf("registry kept %d disabled entries: %v", len(entries), entries)
	}
}

// A shared load over a nil/empty config must not touch the registry.
func TestLoadMCPToolsForConfigSharedNilConfig(t *testing.T) {
	t.Cleanup(CloseAllMCPClients)
	if res := LoadMCPToolsForConfigShared(nil); res.Tools != nil || res.Errors != nil {
		t.Errorf("nil config returned %+v, want zero result", res)
	}
	if entries := mcpRegistryEntries(); len(entries) != 0 {
		t.Errorf("registry changed on a nil config: %v", entries)
	}
}
