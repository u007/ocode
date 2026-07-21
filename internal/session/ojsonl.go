package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/u007/ocode/internal/agent"
)

const ojsonlSchemaVersion = 1

// ojsonlHeader is line 1 of a .ojsonl session file. It is rewritten
// (via temp file + rename, never in place) only when the title changes.
type ojsonlHeader struct {
	V              int       `json:"v"`
	ID             string    `json:"id"`
	CreatedAt      time.Time `json:"created_at"`
	Title          string    `json:"title"`
	TitleGenerated bool      `json:"title_generated,omitempty"`
}

// ojsonlMsgRecord is the JSON structure of each msg-type line.
type ojsonlMsgRecord struct {
	Type               string                   `json:"type"`
	Role               string                   `json:"role"`
	Content            string                   `json:"content,omitempty"`
	Images             []agent.Image            `json:"images,omitempty"`
	ReasoningContent   string                   `json:"reasoning_content,omitempty"`
	Signature          string                   `json:"signature,omitempty"`
	ToolCalls          []agent.ToolCall         `json:"tool_calls,omitempty"`
	ToolID             string                   `json:"tool_call_id,omitempty"`
	OpenAIResponseItems []map[string]interface{} `json:"openai_response_items,omitempty"`
	Notice             string                   `json:"notice,omitempty"`
}

// ojsonlMetaRecord is the JSON structure of each meta-type line.
type ojsonlMetaRecord struct {
	Type     string         `json:"type"`
	Metadata map[string]any `json:"metadata"`
}

// encodeHeaderLine serializes a header record as a single JSON line with trailing '\n'.
func encodeHeaderLine(h ojsonlHeader) ([]byte, error) {
	data, err := json.Marshal(h)
	if err != nil {
		return nil, fmt.Errorf("encode ojsonl header: %w", err)
	}
	return append(data, '\n'), nil
}

// decodeHeaderLine parses a header record from a single JSON line.
func decodeHeaderLine(line []byte) (ojsonlHeader, error) {
	var h ojsonlHeader
	if err := json.Unmarshal(line, &h); err != nil {
		return ojsonlHeader{}, fmt.Errorf("decode ojsonl header: %w", err)
	}
	return h, nil
}

// encodeMsgLine serializes a message as a single JSON line with trailing '\n'.
func encodeMsgLine(m agent.Message) ([]byte, error) {
	rec := ojsonlMsgRecord{
		Type:               "msg",
		Role:               m.Role,
		Content:            m.Content,
		Images:             m.Images,
		ReasoningContent:   m.ReasoningContent,
		Signature:          m.Signature,
		ToolCalls:          m.ToolCalls,
		ToolID:             m.ToolID,
		OpenAIResponseItems: m.OpenAIResponseItems,
		Notice:             m.Notice,
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return nil, fmt.Errorf("encode ojsonl msg record: %w", err)
	}
	return append(data, '\n'), nil
}

// decodeMsgLine parses a message from a single JSON line.
func decodeMsgLine(line []byte) (agent.Message, error) {
	var rec ojsonlMsgRecord
	if err := json.Unmarshal(line, &rec); err != nil {
		return agent.Message{}, fmt.Errorf("decode ojsonl msg record: %w", err)
	}
	return agent.Message{
		Role:               rec.Role,
		Content:            rec.Content,
		Images:             rec.Images,
		ReasoningContent:   rec.ReasoningContent,
		Signature:          rec.Signature,
		ToolCalls:          rec.ToolCalls,
		ToolID:             rec.ToolID,
		OpenAIResponseItems: rec.OpenAIResponseItems,
		Notice:             rec.Notice,
	}, nil
}

// encodeMetaLine serializes a metadata map as a single JSON line with trailing '\n'.
func encodeMetaLine(metadata map[string]any) ([]byte, error) {
	rec := ojsonlMetaRecord{
		Type:     "meta",
		Metadata: metadata,
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return nil, fmt.Errorf("encode ojsonl meta record: %w", err)
	}
	return append(data, '\n'), nil
}

func decodeMetaLine(line []byte) (map[string]any, error) {
	var rec ojsonlMetaRecord
	if err := json.Unmarshal(line, &rec); err != nil {
		return nil, fmt.Errorf("decode ojsonl meta record: %w", err)
	}
	return rec.Metadata, nil
}

// ojsonlSessionPath returns the .ojsonl file path for a session in dir.
func ojsonlSessionPath(dir, id string) string {
	return filepath.Join(dir, id+".ojsonl")
}

// ojsonlWriteState tracks how many msg records have been persisted for a
// session, plus the current header title, so Save() knows what to append.
type ojsonlWriteState struct {
	count          int
	title          string
	titleGenerated bool
}

var (
	ojsonlState   = make(map[string]ojsonlWriteState)
	ojsonlStateMu sync.RWMutex
)

// getOjsonlWriteState returns the cached write state for a path, falling
// back to a bootstrap scan if not yet cached. The bool indicates whether
// the file already exists on disk (true) or is brand new (false).
func getOjsonlWriteState(path string) (ojsonlWriteState, bool, error) {
	ojsonlStateMu.RLock()
	s, ok := ojsonlState[path]
	ojsonlStateMu.RUnlock()
	if ok {
		return s, true, nil
	}
	s, existed, err := bootstrapOjsonlState(path)
	if err != nil {
		return ojsonlWriteState{}, false, err
	}
	setOjsonlWriteState(path, s)
	return s, existed, nil
}

func setOjsonlWriteState(path string, s ojsonlWriteState) {
	ojsonlStateMu.Lock()
	ojsonlState[path] = s
	ojsonlStateMu.Unlock()
}

// bootstrapOjsonlState reads line 1 (header) for the current title, then
// scans the rest of the file counting only "msg" lines — never "meta"
// lines or the header itself, or the persisted-count cache would be off
// and the next Save() would skip real messages or duplicate them.
// existed=false and a zero state is returned when the file does not exist
// yet (this is a brand-new session).
func bootstrapOjsonlState(path string) (ojsonlWriteState, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ojsonlWriteState{}, false, nil
		}
		return ojsonlWriteState{}, false, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return ojsonlWriteState{}, false, fmt.Errorf("read header from %s: %w", path, err)
		}
		return ojsonlWriteState{}, true, fmt.Errorf("ojsonl file %s has no header line", path)
	}
	header, err := decodeHeaderLine(scanner.Bytes())
	if err != nil {
		return ojsonlWriteState{}, true, fmt.Errorf("ojsonl file %s: %w", path, err)
	}

	count := 0
	for scanner.Scan() {
		typ, err := peekRecordType(scanner.Bytes())
		if err != nil {
			// A corrupt non-header line during bootstrap is treated the same
			// as at load time: only tolerable on the true last line, handled
			// by the loader (Task 5). Bootstrap only needs the count, so
			// skip lines it can't classify rather than fail the whole save path.
			continue
		}
		if typ == "msg" {
			count++
		}
	}
	if err := scanner.Err(); err != nil {
		return ojsonlWriteState{}, true, fmt.Errorf("scan %s: %w", path, err)
	}

	return ojsonlWriteState{count: count, title: header.Title, titleGenerated: header.TitleGenerated}, true, nil
}

// peekRecordType reads only the "type" field of a msg/meta record line,
// without decoding the rest — used both to bootstrap the persisted-message
// count cheaply and to dispatch decoding on load.
func peekRecordType(line []byte) (string, error) {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(line, &probe); err != nil {
		return "", fmt.Errorf("peek ojsonl record type: %w", err)
	}
	if probe.Type != "msg" && probe.Type != "meta" {
		return "", fmt.Errorf("peek ojsonl record type: unknown type %q", probe.Type)
	}
	return probe.Type, nil
}
