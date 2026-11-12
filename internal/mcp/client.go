package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/r3labs/sse/v2"
	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

type MCPTool struct {
	server *MCPClient
	name   string
	desc   string
	schema map[string]interface{}
	risk   string
	// uiResource is the MCP Apps resource (tools/list `_meta.ui.resourceUri`)
	// this tool declares, or "" when it has none. It is display metadata only
	// and is deliberately absent from Definition().
	uiResource string
}

func (t MCPTool) Name() string        { return t.name }
func (t MCPTool) Description() string { return t.desc }

// MCPServer returns the configured name of the server this tool came from.
func (t MCPTool) MCPServer() string { return t.server.name }

// Risk returns the server's per-tool risk class, or "" when it reported none.
func (t MCPTool) Risk() string { return t.risk }

// Client returns the client this tool was listed by.
func (t MCPTool) Client() *MCPClient { return t.server }

// MCPAppResource returns the tool's MCP Apps UI resource URI, or "" when the
// tool declares none.
func (t MCPTool) MCPAppResource() string { return t.uiResource }

// TakeMCPAppResult returns and removes the raw result of the call that the tool
// call ID names. ran is true only when that call executed on the server and
// returned a non-error result. Only MCP Apps tools keep results, so a tool with
// no UI resource always reports ran false. raw is nil when the result was over
// the size cap.
func (t MCPTool) TakeMCPAppResult(toolCallID string) (json.RawMessage, bool) {
	return t.server.takeAppResult(toolCallID)
}

// TakeCallImage returns and removes the first image that the call the tool call
// ID names returned.
func (t MCPTool) TakeCallImage(toolCallID string) ([]byte, string, bool) {
	img, ok := t.server.takeCallImage(toolCallID)
	return img.data, img.mime, ok
}

func (t MCPTool) Definition() map[string]any {
	return map[string]any{
		"name":        t.name,
		"description": t.desc,
		"parameters":  t.schema,
	}
}

// Execute calls the tool without a tool call ID, so nothing about the call (its
// image or its MCP Apps result) is kept. The model's calls go through ExecuteCtx.
func (t MCPTool) Execute(args json.RawMessage) (string, error) {
	return t.server.CallTool(t.name, args)
}

// ExecuteCtx calls the tool for the tool call ID carried by ctx and keeps that
// call's image and MCP Apps result under the ID, for the agent to take.
func (t MCPTool) ExecuteCtx(ctx context.Context, args json.RawMessage) (string, error) {
	callID, _ := tool.ToolCallIDFrom(ctx)
	return t.server.callTool(t.name, callID, args)
}

func (t MCPTool) Parallel() bool { return false }

// mcpProtocolVersion is the MCP revision ocode announces in the initialize
// handshake of a local (stdio) server.
const mcpProtocolVersion = "2025-06-18"

// mcpMethodNotFound is the JSON-RPC error code for an unknown method.
const mcpMethodNotFound = -32601

// maxMCPAppResultBytes bounds the raw result kept for an MCP Apps widget.
const maxMCPAppResultBytes = 512 * 1024

// maxLocalLineBytes bounds one line of a local server's stdout.
const maxLocalLineBytes = 32 << 20

// RPCError is a JSON-RPC error object returned by an MCP server. Callers that
// need the code (for example to tell bad arguments from a server fault) use
// errors.As.
type RPCError struct {
	Code    int
	Message string
	// remote is set for an error from a remote server. Its text keeps the
	// historical wording, which names the server and omits the message.
	remote     bool
	remoteName string
}

func (e *RPCError) Error() string {
	if e.remote {
		return fmt.Sprintf("remote MCP server %q returned protocol error %d", e.remoteName, e.Code)
	}
	return fmt.Sprintf("MCP error (%d): %s", e.Code, e.Message)
}

// toolUIMeta is what tools/list told us about one tool, keyed by short name.
type toolUIMeta struct {
	uiResource   string
	modelVisible bool
	appCallable  bool
}

// mcpImage is the first decodable image of a tool's most recent call.
type mcpImage struct {
	data []byte
	mime string
}

type MCPClient struct {
	name    string
	isLocal bool
	timeout time.Duration
	// Local
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	// handshaken is set once the initialize handshake has completed.
	handshaken bool
	// pendingMu guards pending (the requests waiting for a response, by JSON-RPC
	// id), readerErr and readerClosed. The stdout reader routes through it.
	pendingMu    sync.Mutex
	pending      map[int]chan localReply
	readerErr    error
	readerClosed bool
	// readerDone is closed when the stdout reader has exited.
	readerDone chan struct{}
	// closeOnce guards Close; closeErr is its result.
	closeOnce sync.Once
	closeErr  error
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

	// uiMu guards toolMeta, which ListTools replaces wholesale.
	uiMu     sync.Mutex
	toolMeta map[string]toolUIMeta
	// images and appResults keep each call's side-channel outcome until the
	// call's taker reads it, keyed by tool call ID and never by tool name: the
	// client is shared by every session that uses its server.
	images     boundedCallMap[mcpImage]
	appResults boundedCallMap[json.RawMessage]
}

// Name returns the configured server name.
func (c *MCPClient) Name() string { return c.name }

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

	client := &MCPClient{
		name:       name,
		isLocal:    true,
		cmd:        cmd,
		stdin:      stdin,
		stdout:     stdout,
		readerDone: make(chan struct{}),
		timeout:    timeout,
	}
	go client.readStdout(stdout)
	return client, nil
}

// readStdout is the only reader of the local server's stdout. It runs for the
// life of the process and never waits for a consumer: each line is routed to
// the request it answers, or dropped, so an idle or chatty server cannot park
// this goroutine.
func (c *MCPClient) readStdout(stdout io.Reader) {
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), maxLocalLineBytes)
	for sc.Scan() {
		// Scanner.Bytes is reused on the next Scan. routeLocalLine only reads
		// it, and the reply it hands over is decoded into fresh memory.
		c.routeLocalLine(sc.Bytes())
	}
	c.finishReader(sc.Err())
	close(c.readerDone)
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
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("remote MCP server %q returned invalid JSON", serverName)
	}
	if response.Error != nil {
		return nil, &RPCError{
			Code:       response.Error.Code,
			Message:    response.Error.Message,
			remote:     true,
			remoteName: serverName,
		}
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

// requestLocal runs one JSON-RPC round trip on a local server. The first call
// performs the initialize handshake; c.mu serializes everything, so the
// handshake and the request share one lock and do not re-enter the public path.
func (c *MCPClient) requestLocal(method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.handshakeLocked(); err != nil {
		return nil, err
	}
	return c.roundTripLocked(method, params)
}

// handshakeLocked sends initialize and notifications/initialized once. A
// server that answers initialize with -32601 is a legacy server with no
// handshake, so it is treated as done. Any other failure is returned, and the
// handshake is retried on the next request. Caller must hold c.mu.
func (c *MCPClient) handshakeLocked() error {
	if c.handshaken {
		return nil
	}
	_, err := c.roundTripLocked("initialize", map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities": map[string]any{
			"extensions": map[string]any{
				"io.modelcontextprotocol/ui": map[string]any{
					"mimeTypes": []string{"text/html;profile=mcp-app"},
				},
			},
		},
		"clientInfo": map[string]any{"name": "ocode", "version": "1"},
	})
	if err != nil {
		var rpcErr *RPCError
		if errors.As(err, &rpcErr) && rpcErr.Code == mcpMethodNotFound {
			c.handshaken = true
			return nil
		}
		return fmt.Errorf("MCP handshake with server %s failed: %w", c.name, err)
	}
	if err := c.writeLocked(map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	}); err != nil {
		return err
	}
	c.handshaken = true
	return nil
}

// writeLocked writes one newline-delimited JSON-RPC message to the server's
// stdin. Caller must hold c.mu.
func (c *MCPClient) writeLocked(msg map[string]any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write request to MCP server %s: %w", c.name, err)
	}
	return nil
}

// roundTripLocked writes one request and returns the result of the response
// whose id matches it. The reader routes responses by id, so no other line can
// be taken for this one. A request that times out is unregistered, and a late
// response to it is dropped by the reader. Caller must hold c.mu.
func (c *MCPClient) roundTripLocked(method string, params any) (json.RawMessage, error) {
	c.id++
	id := c.id
	reply := make(chan localReply, 1)
	if err := c.register(id, reply); err != nil {
		return nil, err
	}
	if err := c.writeLocked(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}); err != nil {
		c.unregister(id)
		return nil, err
	}

	timer := time.NewTimer(c.timeout)
	defer timer.Stop()
	select {
	case <-timer.C:
		c.unregister(id)
		return nil, fmt.Errorf("MCP server %s timed out after %s", c.name, c.timeout)
	case r := <-reply:
		return r.result, r.err
	}
}

// localReply is the outcome routed to the request waiting for a response.
type localReply struct {
	result json.RawMessage
	err    error
}

// routeLocalLine hands a line of the server's stdout to the request it answers.
// Notifications, server-initiated requests (anything with a method), stale or
// unsolicited responses and lines that are not JSON have no waiter and are
// dropped at once.
func (c *MCPClient) routeLocalLine(line []byte) {
	var head struct {
		ID     json.RawMessage `json:"id"`
		Method *string         `json:"method"`
	}
	if err := json.Unmarshal(line, &head); err != nil || head.Method != nil {
		return
	}
	var id *int
	if err := json.Unmarshal(head.ID, &id); err != nil || id == nil {
		return
	}
	c.pendingMu.Lock()
	reply, waiting := c.pending[*id]
	if waiting {
		delete(c.pending, *id)
	}
	c.pendingMu.Unlock()
	if !waiting {
		return
	}
	// reply has room for its one value and is removed from pending, so this
	// send never blocks.
	reply <- decodeLocalReply(line)
}

// decodeLocalReply turns the response line of a waiting request into its outcome.
func decodeLocalReply(line []byte) localReply {
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *RPCError       `json:"error"`
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		return localReply{err: fmt.Errorf("invalid response from MCP server: %w", err)}
	}
	if resp.Error != nil {
		return localReply{err: resp.Error}
	}
	return localReply{result: resp.Result}
}

// register records the waiter for the response to request id. It fails at once
// when the server's stdout has already ended.
func (c *MCPClient) register(id int, reply chan localReply) error {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	if c.readerClosed {
		return c.readerFailureLocked()
	}
	if c.pending == nil {
		c.pending = make(map[int]chan localReply)
	}
	c.pending[id] = reply
	return nil
}

// unregister removes the waiter for request id. A reply the reader already
// delivered is simply dropped with its channel.
func (c *MCPClient) unregister(id int) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	delete(c.pending, id)
}

// readerFailureLocked is the error for a request once the server's stdout has
// ended. Caller holds c.pendingMu.
func (c *MCPClient) readerFailureLocked() error {
	if c.readerErr != nil {
		return fmt.Errorf("failed to read response from MCP server %s: %w", c.name, c.readerErr)
	}
	return fmt.Errorf("MCP server %s closed its stdout", c.name)
}

// finishReader runs when the server's stdout ends. It records why, fails every
// waiting request, and makes later requests fail at once.
func (c *MCPClient) finishReader(scanErr error) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	c.readerErr = scanErr
	c.readerClosed = true
	failure := c.readerFailureLocked()
	for id, reply := range c.pending {
		reply <- localReply{err: failure}
		delete(c.pending, id)
	}
}

// Close stops a local server. It closes the server's stdin, kills the process
// and reaps it, so the stdout reader ends and every waiting request fails. It
// is idempotent and does nothing for a remote client. Nothing calls it yet: the
// process-wide MCP cache and the per-session clients are not wired to it.
func (c *MCPClient) Close() error {
	if !c.isLocal {
		return nil
	}
	c.closeOnce.Do(func() { c.closeErr = c.stopLocal() })
	return c.closeErr
}

// stopLocal does the work of Close.
func (c *MCPClient) stopLocal() error {
	var errs []error
	if c.stdin != nil {
		if err := c.stdin.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close stdin of MCP server %s: %w", c.name, err))
		}
	}
	if c.cmd != nil && c.cmd.Process != nil {
		if err := c.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			errs = append(errs, fmt.Errorf("kill MCP server %s: %w", c.name, err))
		}
		// Wait reports the kill signal after a kill, which is the expected outcome.
		_ = c.cmd.Wait()
	}
	return errors.Join(errs...)
}

// modelVisibleFor reports whether tools/list `_meta.ui.visibility` makes a tool
// visible to the model. An absent or empty list means the model sees the tool.
// App callability is explicit only (see AppToolAllowed).
func modelVisibleFor(visibility []string) bool {
	if len(visibility) == 0 {
		return true
	}
	return slices.Contains(visibility, "model")
}

// toolMetaFields is the part of a tools/list entry's _meta that MCP Apps reads.
type toolMetaFields struct {
	UI *struct {
		ResourceURI string   `json:"resourceUri"`
		Visibility  []string `json:"visibility"`
	} `json:"ui"`
	// FlatResourceURI is the flat spelling some servers use instead of the
	// nested _meta.ui.resourceUri.
	FlatResourceURI string `json:"ui/resourceUri"`
}

// parseToolUIMeta returns a tool's UI resource and visibility list. A _meta
// that does not parse means the tool has no UI metadata: it stays model-visible
// and app-callable, and it declares no resource. The parse failure is logged,
// not returned, so one bad tool does not hide the server's other tools.
func (c *MCPClient) parseToolUIMeta(toolName string, raw json.RawMessage) (string, []string) {
	if len(raw) == 0 {
		return "", nil
	}
	var fields toolMetaFields
	if err := json.Unmarshal(raw, &fields); err != nil {
		log.Printf("mcp: server %s tool %s has malformed _meta, ignoring its UI metadata: %v", c.name, toolName, err)
		return "", nil
	}
	uiResource := fields.FlatResourceURI
	var visibility []string
	if fields.UI != nil {
		if fields.UI.ResourceURI != "" {
			uiResource = fields.UI.ResourceURI
		}
		visibility = fields.UI.Visibility
	}
	return uiResource, visibility
}

// ListTools returns the model-visible tools and records, for every listed
// tool, its UI resource and audiences (see AppToolAllowed and HasUIResource).
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
			// Risk is an optional per-tool class (read | write | structural) that
			// some servers report; standard MCP servers omit it.
			Risk string `json:"risk"`
			// Meta is decoded per tool by parseToolUIMeta, so one malformed
			// _meta cannot fail the whole listing.
			Meta json.RawMessage `json:"_meta"`
		} `json:"tools"`
	}

	if err := json.Unmarshal(result, &toolsResp); err != nil {
		return nil, err
	}

	meta := make(map[string]toolUIMeta, len(toolsResp.Tools))
	var tools []MCPTool
	for _, t := range toolsResp.Tools {
		uiResource, visibility := c.parseToolUIMeta(t.Name, t.Meta)
		modelVisible := modelVisibleFor(visibility)
		appCallable := slices.Contains(visibility, "app")
		meta[t.Name] = toolUIMeta{
			uiResource:   uiResource,
			modelVisible: modelVisible,
			appCallable:  appCallable,
		}
		if !modelVisible {
			continue
		}
		tools = append(tools, MCPTool{
			server:     c,
			name:       c.name + "_" + t.Name,
			desc:       t.Description,
			schema:     t.InputSchema,
			risk:       t.Risk,
			uiResource: uiResource,
		})
	}

	c.uiMu.Lock()
	c.toolMeta = meta
	c.uiMu.Unlock()
	return tools, nil
}

// AppToolAllowed reports whether a widget may call the tool with this short
// name. Only a tool the server listed with a _meta.ui.visibility array that
// contains "app" is callable by a view. An absent visibility, a model-only
// visibility and a name the server did not list are all refused.
func (c *MCPClient) AppToolAllowed(shortName string) bool {
	c.uiMu.Lock()
	defer c.uiMu.Unlock()
	meta, listed := c.toolMeta[shortName]
	return listed && meta.appCallable
}

// HasUIResource reports whether any listed tool declares uri as its UI resource.
func (c *MCPClient) HasUIResource(uri string) bool {
	if uri == "" {
		return false
	}
	c.uiMu.Lock()
	defer c.uiMu.Unlock()
	for _, meta := range c.toolMeta {
		if meta.uiResource == uri {
			return true
		}
	}
	return false
}

// HasAnyUIResource reports whether the server lists any tool that declares an
// MCP Apps UI resource. An app-only tool counts: it has no model-visible
// entry, but a widget may still call it.
func (c *MCPClient) HasAnyUIResource() bool {
	c.uiMu.Lock()
	defer c.uiMu.Unlock()
	for _, meta := range c.toolMeta {
		if meta.uiResource != "" {
			return true
		}
	}
	return false
}

// uiResourceOf returns the UI resource listed for a short tool name, or "".
func (c *MCPClient) uiResourceOf(shortName string) string {
	c.uiMu.Lock()
	defer c.uiMu.Unlock()
	return c.toolMeta[shortName].uiResource
}

// CallToolRaw calls a tool by its short name (no server prefix) and returns
// the JSON-RPC result object unmodified. Empty args are sent as {}.
func (c *MCPClient) CallToolRaw(shortName string, args json.RawMessage) (json.RawMessage, error) {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	return c.request("tools/call", map[string]interface{}{
		"name":      shortName,
		"arguments": args,
	})
}

// ReadResource issues resources/read for uri and returns the result object
// unmodified.
func (c *MCPClient) ReadResource(uri string) (json.RawMessage, error) {
	return c.request("resources/read", map[string]interface{}{"uri": uri})
}

// rawToolResult is the subset of a tools/call result that CallTool reads.
type rawToolResult struct {
	Content []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		Data     string `json:"data"`
		MimeType string `json:"mimeType"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

// summarizeToolResult joins the text blocks, falling back to "[image]" when a
// result has images but no text. It also returns the first image that decodes
// to non-empty bytes and carries a mime type, or nil.
func summarizeToolResult(resp rawToolResult) (string, *mcpImage) {
	var text strings.Builder
	textBlocks, imageBlocks := 0, 0
	var img *mcpImage
	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			textBlocks++
			text.WriteString(block.Text)
		case "image":
			imageBlocks++
			if img != nil || block.MimeType == "" {
				continue
			}
			data, err := base64.StdEncoding.DecodeString(block.Data)
			if err == nil && len(data) > 0 {
				img = &mcpImage{data: data, mime: block.MimeType}
			}
		}
	}
	if textBlocks == 0 && imageBlocks > 0 {
		return "[image]", img
	}
	return text.String(), img
}

// recordCallOutcome keeps the side-channel outcome of one successful call under
// its tool call ID. The first image is kept when the call returned one. The
// MCP Apps result is kept for a tool with a UI resource, and only under the size
// cap; a tool without a UI resource records no app entry, so such calls cannot
// evict an app call's entry.
func (c *MCPClient) recordCallOutcome(callID, shortName string, raw json.RawMessage, img *mcpImage) {
	if img != nil {
		c.images.put(callID, *img)
	}
	if c.uiResourceOf(shortName) == "" {
		return
	}
	var app json.RawMessage
	if len(raw) <= maxMCPAppResultBytes {
		app = raw
	}
	c.appResults.put(callID, app)
}

// takeAppResult returns and removes the MCP Apps outcome of the call that callID
// names. ok is true when that call ran successfully.
func (c *MCPClient) takeAppResult(callID string) (json.RawMessage, bool) {
	return c.appResults.take(callID)
}

// takeCallImage returns and removes the image of the call that callID names.
func (c *MCPClient) takeCallImage(callID string) (mcpImage, bool) {
	return c.images.take(callID)
}

// CallTool calls a tool by its prefixed name and returns its text content. The
// call has no tool call ID, so nothing about it is kept.
func (c *MCPClient) CallTool(name string, args json.RawMessage) (string, error) {
	return c.callTool(name, "", args)
}

// callTool calls a tool by its prefixed name and returns its text content. A
// successful call with a tool call ID keeps its outcome under that ID; a call
// without one keeps nothing.
func (c *MCPClient) callTool(name, callID string, args json.RawMessage) (string, error) {
	shortName := strings.TrimPrefix(name, c.name+"_")

	raw, err := c.CallToolRaw(shortName, args)
	if err != nil {
		return "", err
	}

	var callResp rawToolResult
	if err := json.Unmarshal(raw, &callResp); err != nil {
		return "", err
	}

	text, img := summarizeToolResult(callResp)
	if callResp.IsError {
		return text, fmt.Errorf("MCP tool call returned error")
	}
	if callID != "" {
		c.recordCallOutcome(callID, shortName, raw, img)
	}
	return text, nil
}
