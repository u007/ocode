package session

import (
	"encoding/json"
	"fmt"
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
