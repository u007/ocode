package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
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
	if existed {
		setOjsonlWriteState(path, s)
	}
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

// appendOjsonlSession appends new messages and an optional metadata line to
// a .ojsonl session file. If the file does not exist yet, it creates it with
// a header line (line 1) followed by the initial batch. Title changes trigger
// a header rewrite via temp file + rename.
func appendOjsonlSession(path, id string, createdAt time.Time, newMessages []agent.Message, metadata map[string]any, title string, titleGenerated bool) error {
	needsHeaderRewrite := false

	state, existed, err := getOjsonlWriteState(path)
	if err != nil {
		return err
	}

	if !existed {
		// Brand-new session: write header + initial messages + meta in one shot.
		headerLine, err := encodeHeaderLine(ojsonlHeader{
			V:              ojsonlSchemaVersion,
			ID:             id,
			CreatedAt:      createdAt,
			Title:          title,
			TitleGenerated: titleGenerated,
		})
		if err != nil {
			return err
		}
		body, err := encodeOjsonlBody(newMessages, metadata)
		if err != nil {
			return err
		}
		content := append(headerLine, body...)
		if err := os.WriteFile(path, content, 0644); err != nil {
			return fmt.Errorf("create ojsonl session %s: %w", path, err)
		}
		setOjsonlWriteState(path, ojsonlWriteState{
			count: len(newMessages), title: title, titleGenerated: titleGenerated,
		})
		return nil
	}

	if title != "" && (title != state.title || titleGenerated != state.titleGenerated) {
		needsHeaderRewrite = true
	}

	if needsHeaderRewrite {
		if err := rewriteOjsonlHeader(path, title, titleGenerated); err != nil {
			return err
		}
		state.title = title
		state.titleGenerated = titleGenerated
	}

	if len(newMessages) == 0 && metadata == nil {
		// No new data to append; header rewrite (if any) already done.
		setOjsonlWriteState(path, state)
		return nil
	}

	body, err := encodeOjsonlBody(newMessages, metadata)
	if err != nil {
		return err
	}
	if len(body) > 0 {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("open ojsonl session %s for append: %w", path, err)
		}
		defer f.Close()
		if _, err := f.Write(body); err != nil {
			return fmt.Errorf("append ojsonl session %s: %w", path, err)
		}
	}

	state.count += len(newMessages)
	setOjsonlWriteState(path, state)
	return nil
}

// encodeOjsonlBody builds the msg lines for newMessages followed by one
// meta line (if metadata != nil) as a single contiguous buffer, so the
// caller can write it with one Write() call.
func encodeOjsonlBody(newMessages []agent.Message, metadata map[string]any) ([]byte, error) {
	var buf []byte
	for _, m := range newMessages {
		line, err := encodeMsgLine(m)
		if err != nil {
			return nil, err
		}
		buf = append(buf, line...)
	}
	if metadata != nil {
		line, err := encodeMetaLine(metadata)
		if err != nil {
			return nil, err
		}
		buf = append(buf, line...)
	}
	return buf, nil
}

// rewriteOjsonlHeader replaces line 1 of an existing .ojsonl file with a
// new title, preserving the original created_at, via write-to-temp +
// rename. Never truncates or overwrites the original file in place.
func rewriteOjsonlHeader(path, title string, titleGenerated bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s for header rewrite: %w", path, err)
	}
	nl := bytes.IndexByte(data, '\n')
	if nl < 0 {
		return fmt.Errorf("ojsonl file %s has no header line to rewrite", path)
	}
	oldHeader, err := decodeHeaderLine(data[:nl])
	if err != nil {
		return fmt.Errorf("ojsonl file %s: %w", path, err)
	}
	rest := data[nl+1:]

	headerLine, err := encodeHeaderLine(ojsonlHeader{
		V:              ojsonlSchemaVersion,
		ID:             oldHeader.ID,
		CreatedAt:      oldHeader.CreatedAt,
		Title:          title,
		TitleGenerated: titleGenerated,
	})
	if err != nil {
		return err
	}

	newContent := append(headerLine, rest...)

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(newContent); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp file for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file for %s: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file into %s: %w", path, err)
	}
	return nil
}

// loadOjsonlSession streams an entire .ojsonl file into a Session. If the
// final line is syntactically incomplete (e.g. a crash mid-append left a
// torn write), it is dropped with a logged warning and the rest of the
// session loads normally — only the last line can be partial in an
// append-only file, so a corrupt line anywhere else is a hard error.
// readOjsonlListMeta reads only the header line (line 1) of a .ojsonl file
// plus the file mtime for updated_at — no scanning past line 1. This is the
// cheap path used by ListRefsPaginated.
func readOjsonlListMeta(path string, modTime time.Time) (ocodeMeta, error) {
	f, err := os.Open(path)
	if err != nil {
		return ocodeMeta{}, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return ocodeMeta{}, fmt.Errorf("read ojsonl header from %s: %w", path, err)
		}
		return ocodeMeta{}, fmt.Errorf("ojsonl file %s is empty", path)
	}

	header, err := decodeHeaderLine(scanner.Bytes())
	if err != nil {
		return ocodeMeta{}, fmt.Errorf("ojsonl file %s: %w", path, err)
	}

	return ocodeMeta{
		ID:        header.ID,
		Title:     header.Title,
		UpdatedAt: modTime,
	}, nil
}

func loadOjsonlSession(path string) (*Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	var lines [][]byte
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("ojsonl file %s is empty", path)
	}

	header, err := decodeHeaderLine(lines[0])
	if err != nil {
		return nil, fmt.Errorf("ojsonl file %s has corrupt header: %w", path, err)
	}

	s := &Session{
		ID:             header.ID,
		Title:          header.Title,
		TitleGenerated: header.TitleGenerated,
		CreatedAt:      header.CreatedAt,
		UpdatedAt:      info.ModTime(),
	}

	for i := 1; i < len(lines); i++ {
		line := lines[i]
		isLast := i == len(lines)-1

		typ, terr := peekRecordType(line)
		if terr != nil {
			if isLast {
				log.Printf("session: dropping truncated last line in %s: %v", path, terr)
				break
			}
			return nil, fmt.Errorf("ojsonl file %s line %d is corrupt: %w", path, i+1, terr)
		}

		switch typ {
		case "msg":
			m, derr := decodeMsgLine(line)
			if derr != nil {
				if isLast {
					log.Printf("session: dropping truncated last line in %s: %v", path, derr)
					break
				}
				return nil, fmt.Errorf("ojsonl file %s line %d is corrupt: %w", path, i+1, derr)
			}
			s.Messages = append(s.Messages, m)
		case "meta":
			meta, derr := decodeMetaLine(line)
			if derr != nil {
				if isLast {
					log.Printf("session: dropping truncated last line in %s: %v", path, derr)
					break
				}
				return nil, fmt.Errorf("ojsonl file %s line %d is corrupt: %w", path, i+1, derr)
			}
			s.Metadata = meta
		}
	}

	return s, nil
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
