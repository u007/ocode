package mcpcli

import (
	"bufio"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/mcp"
)

func Run(args []string) error {
	// Check for help flag
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			printUsage()
			return nil
		}
	}

	if len(args) == 0 {
		printUsage()
		return nil
	}

	switch args[0] {
	case "add":
		return runAdd(args[1:])
	case "list", "ls":
		return runList()
	case "auth":
		if len(args) > 1 && args[1] == "list" {
			return runAuthList()
		}
		if len(args) > 0 {
			return runAuth(args[1:])
		}
		return runAuthList()
	case "logout":
		return runLogout(args[1:])
	case "debug":
		return runDebug(args[1:])
	default:
		printUsage()
		return fmt.Errorf("unknown mcp subcommand: %s", args[0])
	}
}

func printUsage() {
	fmt.Println("Usage: ocode mcp <command> [args]")
	fmt.Println()
	fmt.Println("Manage MCP (Model Context Protocol) servers.")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  add <name>       Add an MCP server (interactive wizard)")
	fmt.Println("  list, ls         List all MCP servers with status")
	fmt.Println("  auth <name>      Trigger OAuth flow for a remote server")
	fmt.Println("  auth list        List OAuth-capable servers")
	fmt.Println("  logout <name>    Clear stored OAuth tokens")
	fmt.Println("  debug <name>     Diagnose connection issues")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -h, --help    Show this help message")
}

func runAdd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ocode mcp add <name>")
	}
	name := args[0]

	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Server type (local/remote): ")
	serverType, _ := reader.ReadString('\n')
	serverType = strings.TrimSpace(strings.ToLower(serverType))

	if serverType != "local" && serverType != "remote" {
		return fmt.Errorf("invalid type: %s (must be local or remote)", serverType)
	}

	mcpCfg := config.MCPConfig{
		Type:    serverType,
		Enabled: true,
		Timeout: 5000,
	}

	if serverType == "local" {
		fmt.Print("Command (space-separated): ")
		cmdLine, _ := reader.ReadString('\n')
		cmdLine = strings.TrimSpace(cmdLine)
		if cmdLine == "" {
			return fmt.Errorf("command cannot be empty")
		}
		mcpCfg.Command = strings.Fields(cmdLine)
	} else {
		fmt.Print("URL: ")
		serverURL, _ := reader.ReadString('\n')
		serverURL = strings.TrimSpace(serverURL)
		if serverURL == "" {
			return fmt.Errorf("URL cannot be empty")
		}
		mcpCfg.URL = serverURL
	}

	fmt.Print("Enable on startup? (Y/n): ")
	enableResp, _ := reader.ReadString('\n')
	enableResp = strings.TrimSpace(strings.ToLower(enableResp))
	if enableResp == "n" || enableResp == "no" {
		mcpCfg.Enabled = false
	}

	configPath, err := resolveConfigPath()
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}

	if err := config.SaveMCPServer(name, mcpCfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	status := "enabled"
	if !mcpCfg.Enabled {
		status = "disabled"
	}
	fmt.Printf("Added MCP server %q (%s, %s) to %s\n", name, serverType, status, configPath)
	return nil
}

func runList() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if len(cfg.MCP) == 0 {
		fmt.Println("No MCP servers configured.")
		return nil
	}

	fmt.Printf("%-16s %-8s %-8s %s\n", "NAME", "TYPE", "STATUS", "TOOLS")
	fmt.Println(strings.Repeat("-", 56))

	var wg sync.WaitGroup
	type result struct {
		name   string
		typ    string
		status string
		tools  string
	}
	results := make([]result, 0, len(cfg.MCP))
	mu := sync.Mutex{}

	for name, mcpCfg := range cfg.MCP {
		wg.Add(1)
		go func(n string, c config.MCPConfig) {
			defer wg.Done()
			r := result{name: n}
			if c.Type == "remote" {
				r.typ = "remote"
			} else {
				r.typ = "local"
			}
			if !c.Enabled {
				r.status = "off"
				r.tools = "-"
				mu.Lock()
				results = append(results, r)
				mu.Unlock()
				return
			}
			status, toolCount := probeServer(n, c)
			r.status = status
			if toolCount >= 0 {
				r.tools = fmt.Sprintf("%d tools", toolCount)
			} else {
				r.tools = strings.TrimPrefix(status, "fail: ")
			}
			mu.Lock()
			results = append(results, r)
			mu.Unlock()
		}(name, mcpCfg)
	}

	wg.Wait()

	for _, r := range results {
		symbol := "fail"
		if r.status == "off" {
			symbol = "off"
		} else if r.status == "ok" {
			symbol = "ok"
		}
		fmt.Printf("%-16s %-8s %-8s %s\n", r.name, r.typ, symbol, r.tools)
	}

	return nil
}

func probeServer(name string, cfg config.MCPConfig) (status string, toolCount int) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var client *mcp.MCPClient
	var err error

	if cfg.Type == "remote" {
		client, err = mcp.NewRemoteClient(name, cfg)
	} else {
		client, err = mcp.NewLocalClient(name, cfg)
	}
	if err != nil {
		return "fail: " + err.Error(), -1
	}

	if client == nil {
		return "fail: nil client", -1
	}

	done := make(chan struct{})
	go func() {
		tools, err := client.ListTools()
		if err != nil {
			status = "fail: " + err.Error()
			toolCount = -1
		} else {
			status = "ok"
			toolCount = len(tools)
		}
		close(done)
	}()

	select {
	case <-done:
		return status, toolCount
	case <-ctx.Done():
		return "fail: timeout", -1
	}
}

func runAuth(args []string) error {
	if len(args) == 0 {
		return runAuthList()
	}
	name := args[0]

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	mcpCfg, ok := cfg.MCP[name]
	if !ok {
		return fmt.Errorf("MCP server %q not found", name)
	}

	if mcpCfg.Type != "remote" {
		return fmt.Errorf("OAuth only supported for remote servers (%q is local)", name)
	}

	if mcpCfg.OAuth == nil || !isOAuthEnabled(mcpCfg.OAuth) {
		return fmt.Errorf("OAuth not configured for server %q", name)
	}

	oauth := mcpCfg.OAuth
	if err := auth.MCPAuthFlow(name, oauth.AuthorizationURL, oauth.TokenURL, oauth.ClientID, oauth.Scopes); err != nil {
		return fmt.Errorf("OAuth flow failed: %w", err)
	}

	fmt.Printf("OAuth token saved for server %q\n", name)
	return nil
}

func runAuthList() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if len(cfg.MCP) == 0 {
		fmt.Println("No MCP servers configured.")
		return nil
	}

	fmt.Printf("%-16s %-8s %s\n", "NAME", "TYPE", "OAUTH")
	fmt.Println(strings.Repeat("-", 44))

	for name, mcpCfg := range cfg.MCP {
		typ := "local"
		if mcpCfg.Type == "remote" {
			typ = "remote"
		}
		oauthStatus := "no"
		if mcpCfg.OAuth != nil && isOAuthEnabled(mcpCfg.OAuth) {
			oauthStatus = "yes"
		}
		_, stored := auth.GetMCPAuth(name)
		if stored || (mcpCfg.Headers != nil && mcpCfg.Headers["Authorization"] != "") {
			oauthStatus += " (token stored)"
		}
		fmt.Printf("%-16s %-8s %s\n", name, typ, oauthStatus)
	}

	return nil
}

func runLogout(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ocode mcp logout <name>")
	}
	name := args[0]

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	mcpCfg, ok := cfg.MCP[name]
	if !ok {
		return fmt.Errorf("MCP server %q not found", name)
	}

	_, authStored := auth.GetMCPAuth(name)
	headerStored := mcpCfg.Headers != nil && mcpCfg.Headers["Authorization"] != ""
	if !authStored && !headerStored {
		fmt.Printf("No stored token for server %q\n", name)
		return nil
	}

	cleared := false
	if authStored {
		if err := auth.DeleteMCPAuth(name); err != nil {
			return fmt.Errorf("delete stored token: %w", err)
		}
		cleared = true
	}
	if headerStored {
		headerCleared, err := config.ClearMCPAuthorization(name)
		if err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		cleared = cleared || headerCleared
	}
	if cleared {
		fmt.Printf("Cleared OAuth token for server %q\n", name)
	}
	return nil
}

func runDebug(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ocode mcp debug <name>")
	}
	name := args[0]

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	mcpCfg, ok := cfg.MCP[name]
	if !ok {
		return fmt.Errorf("MCP server %q not found", name)
	}

	fmt.Printf("Server: %s\n", name)
	fmt.Printf("Type: %s\n", mcpCfg.Type)
	fmt.Printf("Enabled: %v\n", mcpCfg.Enabled)

	if mcpCfg.Type == "local" {
		fmt.Printf("Command: %s\n", strings.Join(mcpCfg.Command, " "))
		if len(mcpCfg.Command) > 0 {
			path, err := exec.LookPath(mcpCfg.Command[0])
			if err != nil {
				fmt.Printf("Binary found: no (%v)\n", err)
			} else {
				fmt.Printf("Binary found: yes (%s)\n", path)
			}
		}
	} else {
		parsed, err := url.Parse(mcpCfg.URL)
		if err != nil {
			fmt.Println("URL valid: no")
		} else {
			fmt.Printf("URL valid: yes (scheme=%s, host=%s)\n", parsed.Scheme, parsed.Host)
		}
	}

	_, storedToken := auth.GetMCPAuth(name)
	if mcpCfg.OAuth != nil && isOAuthEnabled(mcpCfg.OAuth) {
		fmt.Println("OAuth: configured")
		if storedToken || (mcpCfg.Headers != nil && mcpCfg.Headers["Authorization"] != "") {
			fmt.Println("  Token: stored")
		} else {
			fmt.Println("  Token: not stored")
		}
	} else {
		if storedToken {
			fmt.Println("OAuth: not configured (stored credential available)")
		} else {
			fmt.Println("OAuth: not configured")
		}
	}

	if !mcpCfg.Enabled {
		fmt.Println("\nServer is disabled. Skipping connection test.")
		return nil
	}

	fmt.Println("\nTesting connection...")
	status, toolCount := probeServer(name, mcpCfg)
	fmt.Printf("Status: %s\n", status)
	if toolCount >= 0 {
		fmt.Printf("Tools available: %d\n", toolCount)
	}

	return nil
}

func isOAuthEnabled(oauth *config.MCPOAuthConfig) bool {
	if oauth == nil {
		return false
	}
	if oauth.Enabled != nil {
		return *oauth.Enabled
	}
	return oauth.AuthorizationURL != "" && oauth.TokenURL != "" && oauth.ClientID != ""
}

func resolveConfigPath() (string, error) {
	projectRoot := config.FindProjectRoot()
	if projectRoot != "" {
		projectPath := filepath.Join(projectRoot, "opencode.json")
		if _, err := os.Stat(projectPath); err == nil {
			return projectPath, nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("APPDATA"), "opencode", "opencode.json"), nil
	}
	return filepath.Join(home, ".config", "opencode", "opencode.json"), nil
}
