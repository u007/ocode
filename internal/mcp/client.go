package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/r3labs/sse/v2"
	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
)

type MCPTool struct {
	server *MCPClient
	name   string
	desc   string
	schema map[string]interface{}
}

func (t MCPTool) Name() string        { return t.name }
func (t MCPTool) Description() string { return t.desc }
func (t MCPTool) Definition() map[string]any {
	return map[string]any{
		"name":        t.name,
		"description": t.desc,
		"parameters":  t.schema,
	}
}

func (t MCPTool) Execute(args json.RawMessage) (string, error) {
	return t.server.CallTool(t.name, args)
}

func (t MCPTool) Parallel() bool { return false }

type MCPClient struct {
	name    string
	isLocal bool
	timeout time.Duration
	// Local
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	reader *bufio.Scanner
	// Remote
	url      string
	headers  map[string]string
	sse      *sse.Client
	httpCli  *http.Client
	oauthCfg *config.MCPOAuthConfig
	token    *auth.MCPAuthToken
	tokenMu  sync.RWMutex

	mu sync.Mutex
	id int
}

// RemoteHTTPError reports an HTTP failure before attempting to decode an MCP
// response. It intentionally excludes the response body and endpoint URL,
// which may contain credentials or secret-bearing path segments.
type RemoteHTTPError struct {
	ServerName             string
	StatusCode             int
	Status                 string
	AuthenticationRequired bool
}

func (e *RemoteHTTPError) Error() string {
	if e.AuthenticationRequired {
		return fmt.Sprintf("remote MCP server %q requires authorization (HTTP %d)", e.ServerName, e.StatusCode)
	}
	return fmt.Sprintf("remote MCP server %q returned HTTP %d", e.ServerName, e.StatusCode)
}

// ReauthorizationRequiredError tells callers that the stored credential can
// no longer be used and that a new authorization is required. It never
// includes OAuth client IDs, tokens, or endpoint URLs.
type ReauthorizationRequiredError struct {
	ServerName string
	Reason     string
}

func (e *ReauthorizationRequiredError) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("remote MCP server %q requires reauthorization", e.ServerName)
	}
	return fmt.Sprintf("remote MCP server %q requires reauthorization: %s", e.ServerName, e.Reason)
}

type protectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
}

type authorizationServerMetadata struct {
	Issuer        string `json:"issuer"`
	TokenEndpoint string `json:"token_endpoint"`
}

var resourceMetadataChallengePattern = regexp.MustCompile(`(?i)\bresource_metadata\s*=\s*(?:"([^"]*)"|([^,\s]+))`)

func NewRemoteClient(name string, cfg config.MCPConfig) (*MCPClient, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("no URL specified for remote MCP server %s", name)
	}
	if err := validateRemoteServerURL(cfg.URL); err != nil {
		return nil, fmt.Errorf("remote MCP server %q has an invalid URL", name)
	}

	timeout := time.Duration(cfg.Timeout) * time.Millisecond
	if timeout == 0 {
		timeout = 20 * time.Second
	}

	client := sse.NewClient(cfg.URL)
	for k, v := range cfg.Headers {
		client.Headers[k] = v
	}

	httpCli := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return fmt.Errorf("redirects are not allowed for remote MCP requests")
		},
	}

	mc := &MCPClient{
		name:     name,
		isLocal:  false,
		url:      cfg.URL,
		headers:  cfg.Headers,
		sse:      client,
		timeout:  timeout,
		httpCli:  httpCli,
		oauthCfg: cfg.OAuth,
	}

	token, err := mc.loadOrRefreshToken()
	if err != nil {
		return nil, err
	}
	if token != nil {
		mc.token = token
	}

	return mc, nil
}

func NewLocalClient(name string, cfg config.MCPConfig) (*MCPClient, error) {
	if len(cfg.Command) == 0 {
		return nil, fmt.Errorf("no command specified for MCP server %s", name)
	}

	timeout := time.Duration(cfg.Timeout) * time.Millisecond
	if timeout == 0 {
		timeout = 20 * time.Second
	}

	cmd := exec.Command(cfg.Command[0], cfg.Command[1:]...)
	if len(cfg.Environment) > 0 {
		cmd.Env = os.Environ()
		for k, v := range cfg.Environment {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return &MCPClient{
		name:    name,
		isLocal: true,
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		reader:  bufio.NewScanner(stdout),
		timeout: timeout,
	}, nil
}

func (c *MCPClient) request(method string, params any) (json.RawMessage, error) {
	if c.isLocal {
		return c.requestLocal(method, params)
	}
	return c.requestRemote(method, params)
}

func (c *MCPClient) needsOAuth() bool {
	if c.oauthCfg == nil {
		return false
	}
	if c.oauthCfg.Enabled != nil {
		return *c.oauthCfg.Enabled
	}
	return c.oauthCfg.AuthorizationURL != "" && c.oauthCfg.TokenURL != "" && c.oauthCfg.ClientID != ""
}

func (c *MCPClient) loadStoredToken() (*auth.MCPAuthToken, bool) {
	if token, ok := auth.GetMCPAuthForServer(c.name, c.url); ok {
		return &token, true
	}
	if c.needsOAuth() {
		if token, ok := auth.GetNativeMCPAuth(c.name); ok {
			return &token, true
		}
	}
	return nil, false
}

func (c *MCPClient) loadOrRefreshToken() (*auth.MCPAuthToken, error) {
	token, ok := c.loadStoredToken()
	if !ok {
		return nil, nil
	}
	if !token.IsExpired() || token.ServerURL != "" || !c.needsOAuth() {
		return token, nil
	}
	if c.oauthCfg.TokenURL == "" || c.oauthCfg.ClientID == "" {
		return nil, &ReauthorizationRequiredError{
			ServerName: c.name,
			Reason:     "stored credential expired and static refresh configuration is incomplete",
		}
	}
	refreshed, err := c.refreshToken(*token)
	if err != nil {
		return nil, err
	}
	return &refreshed, nil
}

func (c *MCPClient) ensureValidToken() error {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if c.token == nil {
		token, ok := c.loadStoredToken()
		if ok {
			c.token = token
		}
	}
	if c.needsOAuth() && c.token == nil {
		return &ReauthorizationRequiredError{
			ServerName: c.name,
			Reason:     "no stored credential is available",
		}
	}
	if c.needsOAuth() && c.token != nil && c.token.ServerURL == "" && c.token.IsExpired() {
		refreshed, err := c.refreshToken(*c.token)
		if err != nil {
			return err
		}
		c.token = &refreshed
	}
	return nil
}

func (c *MCPClient) AuthHeader() string {
	c.tokenMu.RLock()
	defer c.tokenMu.RUnlock()
	if c.token == nil {
		return ""
	}
	return c.token.AuthorizationHeader()
}

func (c *MCPClient) refreshToken(token auth.MCPAuthToken) (auth.MCPAuthToken, error) {
	if token.ServerURL != "" {
		return auth.MCPAuthToken{}, &ReauthorizationRequiredError{
			ServerName: c.name,
			Reason:     "URL-bound credentials require discovered OAuth metadata",
		}
	}
	if c.oauthCfg != nil && c.oauthCfg.TokenURL != "" && c.oauthCfg.ClientID != "" {
		refreshed, err := auth.RefreshMCPAuthToken(c.name, c.oauthCfg.TokenURL, c.oauthCfg.ClientID, token)
		if err != nil {
			return auth.MCPAuthToken{}, c.safeRefreshError(err)
		}
		return refreshed, nil
	}
	return auth.MCPAuthToken{}, &ReauthorizationRequiredError{
		ServerName: c.name,
		Reason:     "OAuth refresh configuration is unavailable",
	}
}

func (c *MCPClient) safeRefreshError(err error) error {
	// intentionally not logged: auth refresh errors can contain endpoint details.
	if _, ok := errors.AsType[*auth.MCPRefreshError](err); ok {
		return &ReauthorizationRequiredError{
			ServerName: c.name,
			Reason:     "stored credentials were rejected or could not be refreshed",
		}
	}
	return &ReauthorizationRequiredError{
		ServerName: c.name,
		Reason:     "stored credentials could not be refreshed",
	}
}

func (c *MCPClient) requestRemote(method string, params any) (json.RawMessage, error) {
	if err := c.ensureValidToken(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.id++

	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      c.id,
		"method":  method,
		"params":  params,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal MCP request: %w", err)
	}

	resp, err := c.sendRemoteRequest(data)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return decodeRemoteResponse(c.name, resp)
	}
	challenge := strings.Join(resp.Header.Values("WWW-Authenticate"), ", ")
	_ = resp.Body.Close()

	if err := c.refreshAfterUnauthorized(challenge); err != nil {
		return nil, err
	}
	retry, err := c.sendRemoteRequest(data)
	if err != nil {
		return nil, err
	}
	if retry.StatusCode == http.StatusUnauthorized {
		_ = retry.Body.Close()
		return nil, &ReauthorizationRequiredError{
			ServerName: c.name,
			Reason:     "the refreshed credential was rejected",
		}
	}
	return decodeRemoteResponse(c.name, retry)
}

func (c *MCPClient) sendRemoteRequest(data []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, c.url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("remote MCP server %q could not create a request", c.name)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	if authHdr := c.AuthHeader(); authHdr != "" {
		req.Header.Set("Authorization", authHdr)
	}
	resp, err := c.httpCli.Do(req)
	if err != nil {
		// intentionally not logged: transport errors include the full endpoint URL.
		return nil, fmt.Errorf("remote MCP server %q request failed", c.name)
	}
	return resp, nil
}

func decodeRemoteResponse(serverName string, resp *http.Response) (json.RawMessage, error) {
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, &RemoteHTTPError{
			ServerName:             serverName,
			StatusCode:             resp.StatusCode,
			Status:                 http.StatusText(resp.StatusCode),
			AuthenticationRequired: resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden,
		}
	}

	var response struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("remote MCP server %q returned invalid JSON", serverName)
	}
	if response.Error != nil {
		return nil, fmt.Errorf("remote MCP server %q returned protocol error %d", serverName, response.Error.Code)
	}
	return response.Result, nil
}

func (c *MCPClient) refreshAfterUnauthorized(challenge string) error {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	if c.token == nil {
		if token, ok := c.loadStoredToken(); ok {
			c.token = token
		}
	}
	if c.token == nil || c.token.RefreshToken == "" {
		return &RemoteHTTPError{
			ServerName:             c.name,
			StatusCode:             http.StatusUnauthorized,
			Status:                 http.StatusText(http.StatusUnauthorized),
			AuthenticationRequired: true,
		}
	}

	token := *c.token
	tokenURL := ""
	clientID := token.ClientID
	if token.ServerURL == "" && c.oauthCfg != nil && c.needsOAuth() {
		tokenURL = c.oauthCfg.TokenURL
		if c.oauthCfg.ClientID != "" {
			clientID = c.oauthCfg.ClientID
		}
	}
	if tokenURL == "" {
		discovered, err := c.discoverTokenEndpoint(challenge)
		if err != nil {
			return &ReauthorizationRequiredError{
				ServerName: c.name,
				Reason:     "OAuth metadata could not be discovered",
			}
		}
		tokenURL = discovered
	}
	if clientID == "" {
		return &ReauthorizationRequiredError{
			ServerName: c.name,
			Reason:     "stored client identity is unavailable",
		}
	}

	var refreshed auth.MCPAuthToken
	var err error
	if token.ServerURL != "" {
		refreshed, err = auth.RefreshMCPAuthTokenForServer(c.name, c.url, tokenURL, clientID, token)
	} else {
		refreshed, err = auth.RefreshMCPAuthToken(c.name, tokenURL, clientID, token)
	}
	if err != nil {
		return c.safeRefreshError(err)
	}
	c.token = &refreshed
	return nil
}

func (c *MCPClient) discoverTokenEndpoint(challenge string) (string, error) {
	resourceURLs, err := c.resourceMetadataURLs(challenge)
	if err != nil {
		return "", err
	}
	var resource protectedResourceMetadata
	var metadataErr error
	resourceFound := false
	for _, resourceURL := range resourceURLs {
		var candidate protectedResourceMetadata
		if err := c.fetchJSONMetadata(resourceURL, &candidate); err != nil {
			metadataErr = err
			continue
		}
		resource = candidate
		resourceFound = true
		break
	}
	if !resourceFound {
		if metadataErr != nil {
			return "", metadataErr
		}
		return "", fmt.Errorf("OAuth protected-resource metadata is unavailable")
	}
	if !sameMCPURL(c.url, resource.Resource) {
		return "", fmt.Errorf("OAuth protected-resource identity does not match the MCP server")
	}
	if len(resource.AuthorizationServers) == 0 {
		return "", fmt.Errorf("OAuth protected-resource metadata did not advertise an authorization server")
	}
	authorizationURL, err := url.Parse(resource.AuthorizationServers[0])
	if err != nil {
		return "", fmt.Errorf("OAuth authorization-server metadata URL is invalid")
	}
	if err := validateOAuthMetadataURL(authorizationURL); err != nil {
		return "", err
	}
	metadataURL, err := authorizationServerMetadataURL(authorizationURL)
	if err != nil {
		return "", err
	}
	var authorization authorizationServerMetadata
	if err := c.fetchJSONMetadata(metadataURL.String(), &authorization); err != nil {
		return "", err
	}
	if !sameMCPURL(authorizationURL.String(), authorization.Issuer) {
		return "", fmt.Errorf("OAuth authorization-server issuer does not match metadata")
	}
	if strings.TrimSpace(authorization.TokenEndpoint) == "" {
		return "", fmt.Errorf("OAuth authorization-server metadata did not advertise a token endpoint")
	}
	tokenURL, err := url.Parse(authorization.TokenEndpoint)
	if err != nil {
		return "", fmt.Errorf("OAuth token endpoint metadata is invalid")
	}
	if !tokenURL.IsAbs() {
		tokenURL = authorizationURL.ResolveReference(tokenURL)
	}
	if err := validateOAuthMetadataURL(tokenURL); err != nil {
		return "", err
	}
	return tokenURL.String(), nil
}

func (c *MCPClient) resourceMetadataURL(challenge string) (string, error) {
	urls, err := c.resourceMetadataURLs(challenge)
	if err != nil {
		return "", err
	}
	return urls[0], nil
}

func (c *MCPClient) resourceMetadataURLs(challenge string) ([]string, error) {
	if match := resourceMetadataChallengePattern.FindStringSubmatch(challenge); len(match) > 0 {
		value := match[1]
		if value == "" {
			value = match[2]
		}
		metadataURL, err := url.Parse(value)
		if err != nil {
			return nil, fmt.Errorf("OAuth protected-resource metadata URL is invalid")
		}
		if err := validateOAuthMetadataURL(metadataURL); err != nil {
			return nil, err
		}
		return []string{metadataURL.String()}, nil
	}

	base, err := url.Parse(c.url)
	if err != nil {
		return nil, fmt.Errorf("remote MCP server URL is invalid")
	}
	root := *base
	root.Path = "/.well-known/oauth-protected-resource"
	root.RawQuery = ""
	root.RawPath = ""
	root.Fragment = ""
	resourcePath := strings.TrimSuffix(base.Path, "/")
	pathAware := *base
	pathAware.Path = "/.well-known/oauth-protected-resource" + resourcePath
	pathAware.RawQuery = ""
	pathAware.RawPath = ""
	pathAware.Fragment = ""
	if err := validateOAuthMetadataURL(&root); err != nil {
		return nil, err
	}
	if err := validateOAuthMetadataURL(&pathAware); err != nil {
		return nil, err
	}
	if pathAware.String() == root.String() {
		return []string{root.String()}, nil
	}
	return []string{pathAware.String(), root.String()}, nil
}

func sameMCPURL(left, right string) bool {
	leftURL, leftErr := url.Parse(left)
	rightURL, rightErr := url.Parse(right)
	if leftErr != nil || rightErr != nil || !leftURL.IsAbs() || !rightURL.IsAbs() {
		return false
	}
	leftURL.Fragment = ""
	rightURL.Fragment = ""
	return leftURL.String() == rightURL.String()
}

func (c *MCPClient) fetchJSONMetadata(endpoint string, destination any) error {
	if c.httpCli == nil {
		return fmt.Errorf("OAuth metadata client is unavailable")
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("OAuth metadata request could not be created")
	}
	resp, err := c.httpCli.Do(req)
	if err != nil {
		// intentionally not logged: transport errors include the full metadata URL.
		return fmt.Errorf("OAuth metadata request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("OAuth metadata endpoint returned HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(destination); err != nil {
		return fmt.Errorf("OAuth metadata response was not valid JSON")
	}
	return nil
}

func authorizationServerMetadataURL(issuer *url.URL) (*url.URL, error) {
	if issuer == nil {
		return nil, fmt.Errorf("OAuth authorization-server metadata URL is invalid")
	}
	if err := validateOAuthMetadataURL(issuer); err != nil {
		return nil, err
	}

	metadataURL := *issuer
	issuerPath := strings.TrimSuffix(metadataURL.Path, "/")
	metadataURL.Path = "/.well-known/oauth-authorization-server" + issuerPath
	metadataURL.RawPath = ""
	metadataURL.RawQuery = ""
	metadataURL.Fragment = ""
	return &metadataURL, nil
}

func validateOAuthMetadataURL(metadataURL *url.URL) error {
	if metadataURL == nil || !metadataURL.IsAbs() || metadataURL.Hostname() == "" {
		return fmt.Errorf("OAuth metadata URL is invalid")
	}
	if metadataURL.Scheme == "https" {
		return nil
	}
	if metadataURL.Scheme == "http" && isLoopbackHost(metadataURL.Hostname()) {
		return nil
	}
	return fmt.Errorf("OAuth metadata URL must use HTTPS")
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	parsed := net.ParseIP(host)
	return parsed != nil && parsed.IsLoopback()
}

func validateRemoteServerURL(serverURL string) error {
	parsed, err := url.Parse(serverURL)
	if err != nil || !parsed.IsAbs() || parsed.Hostname() == "" {
		return fmt.Errorf("invalid remote MCP URL")
	}
	if parsed.Scheme == "https" {
		return nil
	}
	if parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname()) {
		return nil
	}
	return fmt.Errorf("remote MCP URL must use HTTPS unless it targets loopback")
}

func (c *MCPClient) requestLocal(method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.id++

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      c.id,
		"method":  method,
		"params":  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	fmt.Fprintln(c.stdin, string(data))

	type scanResult struct {
		ok  bool
		err error
	}
	done := make(chan scanResult, 1)
	go func() {
		done <- scanResult{ok: c.reader.Scan(), err: c.reader.Err()}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("MCP server %s timed out after %s", c.name, c.timeout)
	case res := <-done:
		if !res.ok {
			if res.err != nil {
				return nil, fmt.Errorf("failed to read response from MCP server %s: %w", c.name, res.err)
			}
			return nil, fmt.Errorf("failed to read response from MCP server %s", c.name)
		}
	}

	var resp struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int             `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(c.reader.Bytes(), &resp); err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("MCP error (%d): %s", resp.Error.Code, resp.Error.Message)
	}

	return resp.Result, nil
}

func (c *MCPClient) ListTools() ([]MCPTool, error) {
	result, err := c.request("tools/list", map[string]interface{}{})
	if err != nil {
		return nil, err
	}

	var toolsResp struct {
		Tools []struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			InputSchema map[string]interface{} `json:"inputSchema"`
		} `json:"tools"`
	}

	if err := json.Unmarshal(result, &toolsResp); err != nil {
		return nil, err
	}

	var tools []MCPTool
	for _, t := range toolsResp.Tools {
		tools = append(tools, MCPTool{
			server: c,
			name:   c.name + "_" + t.Name,
			desc:   t.Description,
			schema: t.InputSchema,
		})
	}
	return tools, nil
}

func (c *MCPClient) CallTool(name string, args json.RawMessage) (string, error) {
	// Strip prefix
	shortName := strings.TrimPrefix(name, c.name+"_")

	params := map[string]interface{}{
		"name":      shortName,
		"arguments": args,
	}

	result, err := c.request("tools/call", params)
	if err != nil {
		return "", err
	}

	var callResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}

	if err := json.Unmarshal(result, &callResp); err != nil {
		return "", err
	}

	var b strings.Builder
	for _, c := range callResp.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}

	if callResp.IsError {
		return b.String(), fmt.Errorf("MCP tool call returned error")
	}

	return b.String(), nil
}
