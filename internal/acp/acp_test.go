package acp

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// testServer creates a server with in-memory pipes and returns a writer for
// injecting client frames and a reader for consuming server output frames.
func testServer() (clientWriter io.WriteCloser, serverReader *bufio.Scanner, done chan error) {
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()

	done = make(chan error, 1)
	go func() {
		s := &server{
			sessions: make(map[string]*sessionBridge),
			writer:   bufio.NewWriter(stdoutW),
			pending:  make(map[int]chan json.RawMessage),
			// cfg intentionally nil — tests that only exercise JSON-RPC framing
			// do not need a real config.
		}

		scanner := bufio.NewScanner(stdinR)
		scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			s.dispatch(line)
		}
		stdoutW.Close()
		done <- scanner.Err()
	}()

	return stdinW, bufio.NewScanner(stdoutR), done
}

// readFrame reads the next JSON-RPC frame from the server output.
func readFrame(t *testing.T, sc *bufio.Scanner) inFrame {
	t.Helper()
	if !sc.Scan() {
		t.Fatal("scanner ended before a frame was received")
	}
	var f inFrame
	if err := json.Unmarshal(sc.Bytes(), &f); err != nil {
		t.Fatalf("malformed server frame: %v — raw: %s", err, sc.Text())
	}
	return f
}

// sendLine writes a single newline-terminated line to the server.
func sendLine(t *testing.T, w io.Writer, line string) {
	t.Helper()
	if _, err := io.WriteString(w, line+"\n"); err != nil {
		t.Fatalf("write to server: %v", err)
	}
}

// mustParseResult unmarshals f.Result into dst.
func mustParseResult(t *testing.T, f inFrame, dst interface{}) {
	t.Helper()
	if f.Error != nil {
		t.Fatalf("expected result, got error %d: %s", f.Error.Code, f.Error.Message)
	}
	if err := json.Unmarshal(f.Result, dst); err != nil {
		t.Fatalf("parse result: %v", err)
	}
}

// ---------------------------------------------------------------------------

func TestHandshake(t *testing.T) {
	w, sc, _ := testServer()
	sendLine(t, w, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}`)

	f := readFrame(t, sc)

	if f.Error != nil {
		t.Fatalf("initialize returned error: %v", f.Error)
	}

	var result initializeResult
	mustParseResult(t, f, &result)

	if result.ProtocolVersion != 1 {
		t.Errorf("protocolVersion = %d, want 1", result.ProtocolVersion)
	}
	if result.AgentInfo.Name != "ocode" {
		t.Errorf("agentInfo.name = %q, want \"ocode\"", result.AgentInfo.Name)
	}
	if result.AgentCapabilities.LoadSession {
		t.Error("loadSession should be false")
	}
	if !result.AgentCapabilities.PromptCapabilities.EmbeddedContext {
		t.Error("embeddedContext should be true")
	}
	if result.AgentCapabilities.PromptCapabilities.Image {
		t.Error("image should be false")
	}
	if result.AgentCapabilities.PromptCapabilities.Audio {
		t.Error("audio should be false")
	}
	if result.AuthMethods == nil {
		t.Error("authMethods must be non-nil (empty slice)")
	}

	w.Close()
}

func TestMalformedJSON(t *testing.T) {
	w, sc, _ := testServer()
	sendLine(t, w, `{not valid json}`)

	f := readFrame(t, sc)
	if f.Error == nil || f.Error.Code != errParse {
		t.Errorf("expected parse error -32700, got %+v", f.Error)
	}
	w.Close()
}

func TestUnknownMethod(t *testing.T) {
	w, sc, _ := testServer()
	sendLine(t, w, `{"jsonrpc":"2.0","id":42,"method":"no/such/method"}`)

	f := readFrame(t, sc)
	if f.Error == nil || f.Error.Code != errMethodNotFound {
		t.Errorf("expected method-not-found -32601, got %+v", f.Error)
	}
	w.Close()
}

func TestUnknownNotificationSilentlyIgnored(t *testing.T) {
	w, sc, done := testServer()
	// Unknown notification (no id) should produce no output.
	sendLine(t, w, `{"jsonrpc":"2.0","method":"some/unknown/notification"}`)
	// Now send a known request to verify the server is still alive and responding.
	sendLine(t, w, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}`)

	f := readFrame(t, sc)
	// The response must be for the initialize, not the unknown notification.
	if f.Error != nil {
		t.Fatalf("unexpected error: %+v", f.Error)
	}
	var result initializeResult
	mustParseResult(t, f, &result)
	if result.ProtocolVersion != 1 {
		t.Errorf("expected initialize response, got unexpected frame")
	}

	w.Close()
	<-done
}

func TestSessionLoadNotSupported(t *testing.T) {
	w, sc, _ := testServer()
	sendLine(t, w, `{"jsonrpc":"2.0","id":1,"method":"session/load"}`)

	f := readFrame(t, sc)
	if f.Error == nil || f.Error.Code != errMethodNotFound {
		t.Errorf("expected method-not-found, got %+v", f.Error)
	}
	w.Close()
}

func TestAuthenticateNotSupported(t *testing.T) {
	w, sc, _ := testServer()
	sendLine(t, w, `{"jsonrpc":"2.0","id":1,"method":"authenticate"}`)

	f := readFrame(t, sc)
	if f.Error == nil || f.Error.Code != errMethodNotFound {
		t.Errorf("expected method-not-found, got %+v", f.Error)
	}
	w.Close()
}

func TestSessionNewUnknownSession(t *testing.T) {
	w, sc, _ := testServer()
	// Prompt against a session ID that was never created.
	sendLine(t, w, `{"jsonrpc":"2.0","id":1,"method":"session/prompt","params":{"sessionId":"nonexistent","content":[{"type":"text","text":"hi"}]}}`)

	f := readFrame(t, sc)
	if f.Error == nil || f.Error.Code != errInvalidParams {
		t.Errorf("expected invalid-params -32602, got %+v", f.Error)
	}
	w.Close()
}

// ---------------------------------------------------------------------------
// flattenContent tests

func TestFlattenContentText(t *testing.T) {
	blocks := []contentBlock{
		{Type: "text", Text: "hello"},
		{Type: "text", Text: "world"},
	}
	got := flattenContent(blocks)
	if !strings.Contains(got, "hello") || !strings.Contains(got, "world") {
		t.Errorf("flattenContent missing text: %q", got)
	}
}

func TestFlattenContentResource(t *testing.T) {
	blocks := []contentBlock{
		{Type: "text", Text: "see file"},
		{Type: "resource", Resource: &embeddedResource{URI: "file:///foo.go", Text: "package main"}},
	}
	got := flattenContent(blocks)
	if !strings.Contains(got, "foo.go") {
		t.Errorf("flattenContent should include resource URI: %q", got)
	}
	if !strings.Contains(got, "package main") {
		t.Errorf("flattenContent should include resource text: %q", got)
	}
}

func TestFlattenContentResourceLink(t *testing.T) {
	blocks := []contentBlock{
		{Type: "resource_link", URI: "file:///bar.go"},
	}
	got := flattenContent(blocks)
	if !strings.Contains(got, "bar.go") {
		t.Errorf("flattenContent should include resource_link URI: %q", got)
	}
}

// ---------------------------------------------------------------------------
// mapToolKind tests

func TestMapToolKind(t *testing.T) {
	cases := []struct{ tool, want string }{
		{"read", "read"},
		{"write", "edit"},
		{"edit", "edit"},
		{"apply_patch", "edit"},
		{"bash", "execute"},
		{"bash_output", "execute"},
		{"grep", "search"},
		{"glob", "search"},
		{"webfetch", "fetch"},
		{"question", "other"},
		{"task", "other"},
	}
	for _, tc := range cases {
		if got := mapToolKind(tc.tool); got != tc.want {
			t.Errorf("mapToolKind(%q) = %q, want %q", tc.tool, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// makeTitle tests

func TestMakeTitleWithKnownArg(t *testing.T) {
	title := makeTitle("read", `{"path":"/foo/bar.go"}`)
	if title != "read: /foo/bar.go" {
		t.Errorf("unexpected title: %q", title)
	}
}

func TestMakeTitleNoArgs(t *testing.T) {
	title := makeTitle("task", `{}`)
	if title != "task" {
		t.Errorf("unexpected title: %q", title)
	}
}

func TestMakeTitleTruncation(t *testing.T) {
	longPath := strings.Repeat("a", 80)
	title := makeTitle("read", `{"path":"`+longPath+`"}`)
	if len(title) > len("read: ")+60+len("...") {
		t.Errorf("title not truncated: %q", title)
	}
}
