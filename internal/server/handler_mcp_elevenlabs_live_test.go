package server

import (
	"os"
	"testing"

	"github.com/u007/ocode/internal/mcp"
)

// Live check of the whole ElevenLabs path: the preset config, the stored OAuth
// token (written by the browser flow) and the Streamable HTTP transport.
//
// Run it against the same HOME/XDG the sign-in used:
//
//	OCODE_LIVE_ELEVENLABS=1 HOME=<tmp> XDG_DATA_HOME=<tmp>/.local/share \
//	  go test ./internal/server -run TestElevenLabsMCPLive -timeout 120s -v
func TestElevenLabsMCPLive(t *testing.T) {
	if os.Getenv("OCODE_LIVE_ELEVENLABS") == "" {
		t.Skip("set OCODE_LIVE_ELEVENLABS=1 after completing the browser sign-in")
	}

	client, err := mcp.NewRemoteClient(elevenLabsMCPName, elevenLabsMCPPreset())
	if err != nil {
		t.Fatalf("NewRemoteClient: %v", err)
	}
	defer client.Close()

	tools, err := client.ListTools()
	if err != nil {
		t.Fatalf("ListTools over Streamable HTTP: %v", err)
	}
	if len(tools) == 0 {
		t.Fatal("server returned no tools")
	}
	for _, tool := range tools {
		t.Logf("tool: %s", tool.Name())
	}
}
