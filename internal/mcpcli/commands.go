package mcpcli

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/jamesmercstudio/ocode/internal/auth"
	"github.com/jamesmercstudio/ocode/internal/config"
	"github.com/jamesmercstudio/ocode/internal/mcp"
)

func Run(args []string) error {
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
	fmt.Println("Commands:")
	fmt.Println("  add <name>       Add an MCP server (interactive wizard)")
	fmt.Println("  list, ls         List all MCP servers with status")
	fmt.Println("  auth <name>      Trigger OAuth flow for a remote server")
	fmt.Println("  auth list        List OAuth-capable servers")
	fmt.Println("  logout <name>    Clear stored OAuth tokens")
	fmt.Println("  debug <name>     Diagnose connection issues")
}

func runAdd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ocode mcp add <name>")
	}
	name := args[0]

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

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

	if cfg.MCP == nil {
		cfg.MCP = make(map[string]config.MCPConfig)
	}
	cfg.MCP[name] = mcpCfg

	configPath, err := resolveConfigPath()
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}

	if err := config.Save(cfg, configPath); err != nil {
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
		symbol := "ok"
		if r.status == "off" {
			symbol = "off"
		} else if !strings.HasPrefix(r.tools, "fail") && !strings.HasPrefix(r.tools, "connection") {
			symbol = "ok"
		} else {
			symbol = "fail"
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
	token, err := runOAuthFlow(oauth)
	if err != nil {
		return fmt.Errorf("OAuth flow failed: %w", err)
	}

	auth.SetMCPAuth(name, auth.MCPAuthToken{
		AccessToken:  token,
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(time.Hour).Unix(),
	})

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
		if mcpCfg.Headers != nil && mcpCfg.Headers["Authorization"] != "" {
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

	if mcpCfg.Headers == nil || mcpCfg.Headers["Authorization"] == "" {
		fmt.Printf("No stored token for server %q\n", name)
		return nil
	}

	delete(mcpCfg.Headers, "Authorization")
	cfg.MCP[name] = mcpCfg

	configPath, err := resolveConfigPath()
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}

	if err := config.Save(cfg, configPath); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Printf("Cleared OAuth token for server %q\n", name)
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
		fmt.Printf("URL: %s\n", mcpCfg.URL)
		parsed, err := url.Parse(mcpCfg.URL)
		if err != nil {
			fmt.Printf("URL valid: no (%v)\n", err)
		} else {
			fmt.Printf("URL valid: yes (scheme=%s, host=%s)\n", parsed.Scheme, parsed.Host)
		}
	}

	if mcpCfg.OAuth != nil && isOAuthEnabled(mcpCfg.OAuth) {
		fmt.Println("OAuth: configured")
		fmt.Printf("  Authorization URL: %s\n", mcpCfg.OAuth.AuthorizationURL)
		fmt.Printf("  Token URL: %s\n", mcpCfg.OAuth.TokenURL)
		fmt.Printf("  Client ID: %s\n", mcpCfg.OAuth.ClientID)
		if len(mcpCfg.OAuth.Scopes) > 0 {
			fmt.Printf("  Scopes: %s\n", strings.Join(mcpCfg.OAuth.Scopes, ", "))
		}
		if mcpCfg.Headers != nil && mcpCfg.Headers["Authorization"] != "" {
			fmt.Println("  Token: stored")
		} else {
			fmt.Println("  Token: not stored")
		}
	} else {
		fmt.Println("OAuth: not configured")
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

func runOAuthFlow(oauth *config.MCPOAuthConfig) (string, error) {
	verifier, challenge := generatePKCE()
	state, err := randomState()
	if err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}

	redirectURI := "http://localhost:9876/oauth/callback"
	authURL := buildAuthorizeURL(oauth, challenge, state, redirectURI)

	fmt.Printf("Opening browser for authorization...\n")
	openBrowser(authURL)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	listener, err := net.Listen("tcp", "127.0.0.1:9876")
	if err != nil {
		return "", fmt.Errorf("bind localhost:9876: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if errParam := q.Get("error"); errParam != "" {
			errCh <- fmt.Errorf("auth error: %s — %s", errParam, q.Get("error_description"))
			http.Error(w, "auth failed", http.StatusBadRequest)
			return
		}
		if q.Get("state") != state {
			errCh <- fmt.Errorf("state mismatch")
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		code := q.Get("code")
		if code == "" {
			errCh <- fmt.Errorf("missing authorization code")
			http.Error(w, "missing code", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`<!doctype html><html><body style="font-family:system-ui;padding:40px"><h2>Authorized for ocode</h2><p>You can close this tab.</p></body></html>`))
		codeCh <- code
	})

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(listener) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	select {
	case <-time.After(5 * time.Minute):
		return "", fmt.Errorf("OAuth timed out")
	case err := <-errCh:
		return "", err
	case code := <-codeCh:
		return exchangeToken(oauth, code, verifier, redirectURI)
	}
}

func generatePKCE() (verifier, challenge string) {
	buf := make([]byte, 32)
	rand.Read(buf)
	verifier = base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return
}

func randomState() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func buildAuthorizeURL(oauth *config.MCPOAuthConfig, challenge, state, redirectURI string) string {
	u, _ := url.Parse(oauth.AuthorizationURL)
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", oauth.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	if len(oauth.Scopes) > 0 {
		q.Set("scope", strings.Join(oauth.Scopes, " "))
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func exchangeToken(oauth *config.MCPOAuthConfig, code, verifier, redirectURI string) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", oauth.ClientID)
	form.Set("code", code)
	form.Set("code_verifier", verifier)
	form.Set("redirect_uri", redirectURI)

	req, err := http.NewRequest("POST", oauth.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token exchange failed: %d %s", resp.StatusCode, string(body))
	}

	var parsed struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if parsed.AccessToken == "" {
		return "", fmt.Errorf("token response missing access_token")
	}

	return parsed.AccessToken, nil
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return
	}
	cmd.Stdout = nil
	cmd.Stderr = nil
	_ = cmd.Start()
}
