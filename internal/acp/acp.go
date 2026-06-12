// Package acp implements the Agent Client Protocol (ACP, agentclientprotocol.com)
// so that `ocode acp` appears in Zed's agent panel alongside Claude Code / Codex.
//
// Transport: JSON-RPC 2.0 newline-delimited over stdio.
// stdin = client → agent; stdout = agent → client (protocol frames only).
// All diagnostics go to stderr via emitDebug / log, never stdout.
package acp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	ver "github.com/u007/ocode/internal/version"
)

// JSON-RPC error codes.
const (
	errParse         = -32700
	errMethodNotFound = -32601
	errInvalidParams  = -32602
	errInternal       = -32603
)

// server holds all mutable state for the lifetime of the ACP process.
type server struct {
	cfg      *config.Config
	sessions map[string]*sessionBridge
	sessMu   sync.Mutex

	// writeMu serialises all stdout writes so goroutines don't interleave frames.
	writeMu sync.Mutex
	writer  *bufio.Writer

	// pendingMu guards the map of in-flight client-bound requests.
	pendingMu     sync.Mutex
	pending       map[int]chan json.RawMessage
	nextRequestID atomic.Int64
}

// Run is the entry point for `ocode acp`. It reads JSON-RPC frames from stdin
// and writes responses/notifications to stdout until stdin is closed.
func Run(args []string) error {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			printUsage()
			return nil
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	agent.ApplyAgentConfig(cfg)

	s := &server{
		cfg:      cfg,
		sessions: make(map[string]*sessionBridge),
		writer:   bufio.NewWriter(os.Stdout),
		pending:  make(map[int]chan json.RawMessage),
	}

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 4*1024*1024), 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		s.dispatch(line)
	}

	return scanner.Err()
}

// dispatch parses one newline-delimited JSON frame and routes it.
func (s *server) dispatch(line string) {
	var frame inFrame
	if err := json.Unmarshal([]byte(line), &frame); err != nil {
		s.sendError(json.RawMessage(`null`), errParse, "parse error")
		return
	}

	// Responses to our client-bound requests arrive with no Method and a known ID.
	if frame.Method == "" {
		s.routeResponse(frame)
		return
	}

	// A notification has no id (or id == null); no response should be sent.
	isNotification := len(frame.ID) == 0 || string(frame.ID) == "null"

	switch frame.Method {
	case "initialize":
		s.handleInitialize(frame)

	case "authenticate":
		if !isNotification {
			s.sendError(frame.ID, errMethodNotFound, "authenticate not supported")
		}

	case "session/new":
		s.handleSessionNew(frame)

	case "session/load":
		if !isNotification {
			s.sendError(frame.ID, errMethodNotFound, "session/load not supported in v1 (loadSession: false)")
		}

	case "session/prompt":
		s.handleSessionPrompt(frame)

	case "session/cancel":
		// Notification — no response.
		s.handleSessionCancel(frame)

	case "session/set_mode":
		if !isNotification {
			s.sendError(frame.ID, errMethodNotFound, "session/set_mode not supported in v1")
		}

	default:
		// Unknown notifications are silently ignored per JSON-RPC spec.
		if !isNotification {
			s.sendError(frame.ID, errMethodNotFound, fmt.Sprintf("method not found: %s", frame.Method))
		}
	}
}

func (s *server) handleInitialize(frame inFrame) {
	var params initializeParams
	if len(frame.Params) > 0 {
		_ = json.Unmarshal(frame.Params, &params)
	}

	protoVer := params.ProtocolVersion
	if protoVer < 1 || protoVer > 1 {
		protoVer = 1
	}

	s.sendResult(frame.ID, initializeResult{
		ProtocolVersion: protoVer,
		AgentInfo: agentInfo{
			Name:    "ocode",
			Version: ver.Version,
		},
		AgentCapabilities: agentCapabilities{
			LoadSession: false,
			PromptCapabilities: promptCapabilities{
				EmbeddedContext: true,
				Image:           false,
				Audio:           false,
			},
		},
		AuthMethods: []interface{}{},
	})
}

func (s *server) handleSessionNew(frame inFrame) {
	var params sessionNewParams
	if len(frame.Params) > 0 {
		if err := json.Unmarshal(frame.Params, &params); err != nil {
			s.sendError(frame.ID, errInvalidParams, "invalid params: "+err.Error())
			return
		}
	}

	bridge, err := newSessionBridge(s.cfg, params.CWD)
	if err != nil {
		s.sendError(frame.ID, errInternal, err.Error())
		return
	}

	s.sessMu.Lock()
	s.sessions[bridge.id] = bridge
	s.sessMu.Unlock()

	s.sendResult(frame.ID, sessionNewResult{SessionID: bridge.id})
}

func (s *server) handleSessionPrompt(frame inFrame) {
	if len(frame.Params) == 0 {
		s.sendError(frame.ID, errInvalidParams, "params required")
		return
	}
	var params sessionPromptParams
	if err := json.Unmarshal(frame.Params, &params); err != nil {
		s.sendError(frame.ID, errInvalidParams, "invalid params: "+err.Error())
		return
	}

	s.sessMu.Lock()
	bridge, ok := s.sessions[params.SessionID]
	s.sessMu.Unlock()

	if !ok {
		s.sendError(frame.ID, errInvalidParams, fmt.Sprintf("unknown session: %s", params.SessionID))
		return
	}

	if !bridge.tryStartPrompt() {
		s.sendError(frame.ID, errInvalidParams, "concurrent prompt for the same session is not allowed")
		return
	}

	// Run the turn on a goroutine so the read loop keeps dispatching
	// (needed for session/cancel and permission-response routing).
	go func() {
		defer bridge.endPrompt()

		sessID := params.SessionID

		sendUpdate := func(su sessionUpdate) {
			s.sendNotify("session/update", sessionUpdateParams{
				SessionID:     sessID,
				SessionUpdate: su,
			})
		}

		sendPermRequest := func(toolName, rule string) string {
			id := int(s.nextRequestID.Add(1))
			ch := make(chan json.RawMessage, 1)

			s.pendingMu.Lock()
			s.pending[id] = ch
			s.pendingMu.Unlock()

			s.sendClientRequest(id, "session/request_permission", permRequestParams{
				SessionID: sessID,
				ToolName:  toolName,
				Rule:      rule,
				Options:   []string{"allow-once", "allow-always", "reject-once"},
			})

			raw := <-ch

			s.pendingMu.Lock()
			delete(s.pending, id)
			s.pendingMu.Unlock()

			var result permResponseResult
			if err := json.Unmarshal(raw, &result); err != nil {
				return "cancelled"
			}
			return result.Selected
		}

		stopReason, err := bridge.prompt(params.Content, sendUpdate, sendPermRequest)
		if err != nil {
			s.sendError(frame.ID, errInternal, "agent error: "+err.Error())
			return
		}
		s.sendResult(frame.ID, sessionPromptResult{StopReason: stopReason})
	}()
}

func (s *server) handleSessionCancel(frame inFrame) {
	var params sessionCancelParams
	if len(frame.Params) > 0 {
		_ = json.Unmarshal(frame.Params, &params)
	}
	s.sessMu.Lock()
	bridge, ok := s.sessions[params.SessionID]
	s.sessMu.Unlock()
	if ok {
		bridge.cancel()
	}
	// session/cancel is a notification — no response.
}

// routeResponse delivers a client response to the pending client-bound request.
func (s *server) routeResponse(frame inFrame) {
	var id int
	if err := json.Unmarshal(frame.ID, &id); err != nil {
		return // not one of our integer-ID requests; ignore
	}
	s.pendingMu.Lock()
	ch, ok := s.pending[id]
	s.pendingMu.Unlock()
	if !ok {
		return
	}
	if frame.Error != nil {
		ch <- json.RawMessage(`null`)
	} else {
		ch <- frame.Result
	}
}

// -- write helpers ----------------------------------------------------------

func (s *server) sendResult(id json.RawMessage, result interface{}) {
	s.writeJSON(outResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func (s *server) sendError(id json.RawMessage, code int, message string) {
	s.writeJSON(outResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}})
}

func (s *server) sendNotify(method string, params interface{}) {
	s.writeJSON(outNotify{JSONRPC: "2.0", Method: method, Params: params})
}

func (s *server) sendClientRequest(id int, method string, params interface{}) {
	s.writeJSON(outRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
}

func (s *server) writeJSON(v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "acp: marshal error: %v\n", err)
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.writer.Write(data)  //nolint:errcheck
	s.writer.WriteByte('\n') //nolint:errcheck
	s.writer.Flush()      //nolint:errcheck
}

func printUsage() {
	fmt.Println("Usage: ocode acp [options]")
	fmt.Println()
	fmt.Println("Run an Agent Client Protocol (ACP) server over stdio.")
	fmt.Println("Zed and other ACP-compatible clients can connect via the agent panel.")
	fmt.Println()
	fmt.Println("Transport: JSON-RPC 2.0, newline-delimited, over stdin/stdout.")
	fmt.Println("Protocol version: 1 (agentclientprotocol.com)")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -h, --help    Show this help message")
}
