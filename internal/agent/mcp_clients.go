package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"sync"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/mcp"
)

// The shared MCP client registry keeps one live client per configured server
// across repeated loads. Without it every reload spawns a new subprocess for
// every local server, and the old one keeps running until the process exits.
//
// Only the shared loader reads or writes the registry: LoadMCPToolsForConfigShared
// is expected to be called serially (one warm at boot, one per MCP reload).
// The mutex therefore guards map access only, and is never held across
// cmd.Start or ListTools, which are the blocking parts. Concurrent callers
// would not corrupt the map, but two of them could both start a client for the
// same server and race to store it.
var (
	mcpClientsMu sync.Mutex
	mcpClients   = map[string]mcpClientEntry{}
)

// mcpClientEntry is one live client and the config signature it was built from.
type mcpClientEntry struct {
	signature string
	client    *mcp.MCPClient
}

// mcpClientAction is the reuse/evict decision for one configured server.
type mcpClientAction int

const (
	// mcpActionStartNew starts a client because the registry holds none.
	mcpActionStartNew mcpClientAction = iota
	// mcpActionReuse reuses a live client whose signature is unchanged.
	mcpActionReuse
	// mcpActionReplace starts a new client and retires the old one.
	mcpActionReplace
)

// decideMCPClientAction is the pure reuse/evict decision. alive is the stored
// client's Alive() result, passed in so the rule is testable without a process:
// a live client with an unchanged signature is reused, and anything else starts
// a new client (the caller retires the old one only after the replacement
// enumerates successfully).
func decideMCPClientAction(entry *mcpClientEntry, signature string, alive bool) mcpClientAction {
	if entry == nil {
		return mcpActionStartNew
	}
	if entry.signature == signature && alive {
		return mcpActionReuse
	}
	return mcpActionReplace
}

// mcpClientsToRetire returns the registry names that must be closed after a
// load: servers no longer enabled, or whose signature no longer matches the
// enabled config. Pure, so the removal rule is testable without a process.
func mcpClientsToRetire(entries map[string]mcpClientEntry, enabled map[string]string) []string {
	var names []string
	for name, entry := range entries {
		if signature, ok := enabled[name]; !ok || signature != entry.signature {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// mcpConfigSignature is a stable digest of everything that changes what a
// client would connect to or how it behaves. map fields serialize with sorted
// keys (encoding/json sorts map keys), so two equal configs always agree. The
// struct has no field encoding/json can fail on.
func mcpConfigSignature(cfg config.MCPConfig) string {
	payload, _ := json.Marshal(struct {
		Type        string
		Command     []string
		URL         string
		Environment map[string]string
		Headers     map[string]string
		Timeout     int
		OAuth       *config.MCPOAuthConfig
	}{
		Type:        cfg.Type,
		Command:     cfg.Command,
		URL:         cfg.URL,
		Environment: cfg.Environment,
		Headers:     cfg.Headers,
		Timeout:     cfg.Timeout,
		OAuth:       cfg.OAuth,
	})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// mcpRegistryLookup copies the entry for name under the lock. It returns nil
// when the registry holds no client for the server.
func mcpRegistryLookup(name string) *mcpClientEntry {
	mcpClientsMu.Lock()
	defer mcpClientsMu.Unlock()
	entry, ok := mcpClients[name]
	if !ok {
		return nil
	}
	return &entry
}

// mcpRegistryStore installs client as the entry for name and returns the
// client it displaced (nil when there was none or it is the same one).
func mcpRegistryStore(name string, entry mcpClientEntry) *mcp.MCPClient {
	mcpClientsMu.Lock()
	defer mcpClientsMu.Unlock()
	old := mcpClients[name].client
	mcpClients[name] = entry
	if old == entry.client {
		return nil
	}
	return old
}

// mcpRegistryEvict deletes the entry for name only when it still holds client,
// so a client replaced concurrently is not dropped by a stale eviction.
func mcpRegistryEvict(name string, client *mcp.MCPClient) {
	mcpClientsMu.Lock()
	defer mcpClientsMu.Unlock()
	if mcpClients[name].client == client {
		delete(mcpClients, name)
	}
}

// mcpRegistryDelete removes and returns the entry for name.
func mcpRegistryDelete(name string) *mcp.MCPClient {
	mcpClientsMu.Lock()
	defer mcpClientsMu.Unlock()
	client := mcpClients[name].client
	delete(mcpClients, name)
	return client
}

// mcpRegistryEntries copies the registry map under the lock.
func mcpRegistryEntries() map[string]mcpClientEntry {
	mcpClientsMu.Lock()
	defer mcpClientsMu.Unlock()
	entries := make(map[string]mcpClientEntry, len(mcpClients))
	for name, entry := range mcpClients {
		entries[name] = entry
	}
	return entries
}

// mcpSharedLoadOne returns the tools of one enabled server, reusing the cached
// client when it is alive and unchanged. A reused client that fails to list is
// evicted and replaced once; a client that cannot enumerate is closed rather
// than cached. The registry lock is never held here.
func mcpSharedLoadOne(name string, serverCfg config.MCPConfig, signature string) ([]mcp.MCPTool, error) {
	entry := mcpRegistryLookup(name)
	if entry != nil {
		alive := entry.client.Alive()
		switch decideMCPClientAction(entry, signature, alive) {
		case mcpActionReuse:
			tools, err := entry.client.ListTools()
			if err == nil {
				return tools, nil
			}
			// A live client that cannot list is unusable, so drop it before the
			// single fresh-client retry.
			mcpRegistryEvict(name, entry.client)
			_ = entry.client.Close()
		case mcpActionReplace:
			// A dead client can serve nothing, so it is closed now. A live one is
			// kept until its replacement enumerates successfully.
			if !alive {
				mcpRegistryEvict(name, entry.client)
				_ = entry.client.Close()
			}
		}
	}

	client, err := mcpNewClientForConfig(name, serverCfg)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, nil
	}
	tools, err := client.ListTools()
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	if old := mcpRegistryStore(name, mcpClientEntry{signature: signature, client: client}); old != nil {
		// The replacement enumerated, so the previous client may be retired.
		_ = old.Close()
	}
	return tools, nil
}

// mcpNewClientForConfig builds a client for an enabled server. An unknown type
// yields no client, matching LoadMCPToolsForConfig, which skips it silently.
func mcpNewClientForConfig(name string, serverCfg config.MCPConfig) (*mcp.MCPClient, error) {
	switch serverCfg.Type {
	case "local":
		return mcp.NewLocalClient(name, serverCfg)
	case "remote":
		return mcp.NewRemoteClient(name, serverCfg)
	}
	return nil, nil
}

// CloseAllMCPClients closes every cached client and empties the registry. It is
// idempotent and safe to call on handler shutdown.
func CloseAllMCPClients() {
	mcpClientsMu.Lock()
	entries := mcpClients
	mcpClients = map[string]mcpClientEntry{}
	mcpClientsMu.Unlock()
	for _, entry := range entries {
		_ = entry.client.Close()
	}
}
