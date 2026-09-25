package mcpcli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/auth"
)

func captureMCPListStdout(t *testing.T, fn func() error) string {
	t.Helper()

	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	t.Cleanup(func() { os.Stdout = oldStdout })

	runErr := fn()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = oldStdout
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if runErr != nil {
		t.Fatal(runErr)
	}
	return string(data)
}

func TestRunListClassifiesFailedProbeAsFail(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("OPENCODE_CONFIG_DIR", filepath.Join(home, "custom"))
	t.Setenv("OPENCODE_CONFIG_CONTENT", `{"mcp":{"broken":{"type":"local","command":["/bin/sh","-c","printf '%s\\n' '{\"jsonrpc\":\"2.0\",\"id\":1,\"error\":{\"code\":-32000,\"message\":\"probe failed\"}}'"],"enabled":true,"timeout":1000}}}`)

	output := captureMCPListStdout(t, runList)
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "broken" {
			if len(fields) < 3 {
				t.Fatalf("broken row = %q, want type and status", line)
			}
			if fields[2] != "fail" {
				t.Fatalf("broken status = %q, want fail; row = %q", fields[2], line)
			}
			return
		}
	}
	t.Fatalf("list output = %q, want broken server row", output)
}

func TestRunDebugRedactsRemoteCredentialDetails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("OPENCODE_CONFIG_CONTENT", `{"mcp":{"server":{"type":"remote","url":"https://secret.example/mcp/private-token","enabled":false,"oauth":{"enabled":true,"authorization_url":"https://auth.example/authorize","token_url":"https://auth.example/token","client_id":"private-client","scopes":["private.scope"]}}}}`)

	output := captureMCPListStdout(t, func() error { return runDebug([]string{"server"}) })
	for _, secret := range []string{"private-token", "private-client", "private.scope", "https://auth.example/token"} {
		if strings.Contains(output, secret) {
			t.Fatalf("debug output leaked %q: %s", secret, output)
		}
	}
}

func TestRunLogoutDeletesStoredAuthCredential(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("OPENCODE_CONFIG_CONTENT", `{"mcp":{"server":{"type":"remote","url":"https://mcp.example.test/mcp","enabled":true}}}`)

	const serverURL = "https://mcp.example.test/mcp"
	if err := auth.SetMCPAuthForServer("server", serverURL, auth.MCPAuthToken{
		AccessToken: "stored-access",
		TokenType:   "Bearer",
		Expiry:      time.Now().Add(time.Hour).Unix(),
		ClientID:    "stored-client",
	}); err != nil {
		t.Fatal(err)
	}

	output := captureMCPListStdout(t, func() error { return runLogout([]string{"server"}) })
	if _, ok := auth.GetMCPAuthForServer("server", serverURL); ok {
		t.Fatal("logout left the stored MCP credential active")
	}
	if !strings.Contains(output, "Cleared OAuth token") {
		t.Fatalf("logout output = %q, want cleared confirmation", output)
	}
}
