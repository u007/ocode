package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// canonicalToolCallKey returns a collision-safe canonical key for a tool
// call so that two calls with the same tool name and semantically
// equivalent JSON arguments compare equal. Argument parsing uses
// json.Decoder.UseNumber so integral number literals (1 vs 1.0) are NOT
// conflated, and object keys are emitted in deterministic order by
// re-marshalling the decoded value, making the key whitespace-insensitive.
//
// The second return value is false for malformed JSON — such an argument is
// never deduplicated, because we cannot know whether two malformed blobs are
// truly equivalent (they differ beyond whitespace, but the decoder could not
// tell us, and transforming raw bytes would be lossy/order-dependent).
// Callers treat a false key as unique and dispatch normally.
func canonicalToolCallKey(name string, arguments json.RawMessage) (string, bool) {
	dec := json.NewDecoder(bytes.NewReader(arguments))
	dec.UseNumber()
	if err := validateJSONValue(dec); err != nil {
		return "", false
	}
	if _, err := dec.Token(); err != io.EOF {
		return "", false
	}

	dec = json.NewDecoder(bytes.NewReader(arguments))
	dec.UseNumber()
	var v interface{}
	if err := dec.Decode(&v); err != nil {
		return "", false
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", false
	}
	return lengthPrefixedKey(name, string(b)), true
}

// validateJSONValue consumes exactly one JSON value while rejecting duplicate
// object keys. Duplicate keys are deliberately not canonicalized because the
// tool dispatcher rejects them rather than choosing one silently.
func validateJSONValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return fmt.Errorf("object key is not a string")
			}
			if _, exists := seen[name]; exists {
				return fmt.Errorf("duplicate object key %q", name)
			}
			seen[name] = struct{}{}
			if err := validateJSONValue(dec); err != nil {
				return err
			}
		}
		_, err = dec.Token()
		return err
	case '[':
		for dec.More() {
			if err := validateJSONValue(dec); err != nil {
				return err
			}
		}
		_, err = dec.Token()
		return err
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delim)
	}
}

func activeAgentDispatchKey(agentName, prompt, context string) string {
	return lengthPrefixedKey(agentName, strings.TrimSpace(prompt), strings.TrimSpace(context))
}

// activeTaskDispatchKey returns the suppression key for a fresh task dispatch.
// It extends activeAgentDispatchKey with every taskToolParams field that
// changes what the dispatch does or what its result looks like, so two
// concurrent dispatches that share prompt+context but differ in contract,
// identity, or execution mode do NOT suppress each other.
//
// Included: effective spec name, requested agent name (covers the
// agent/subagent_type alias and fallback cases where different requested
// names resolve to the same spec but produce different fallback warnings),
// trimmed prompt, trimmed context, trimmed description, resolved output
// contract, shared-notes flag, background flag (background holds the key for
// the life of the run, sync releases on return), DAG id, and sorted DAG
// deps (predecessor output is prepended to the child's context at schedule
// time, so different deps mean different effective work).
func activeTaskDispatchKey(specName string, params taskToolParams, contract string) string {
	boolFlag := func(b bool) string {
		if b {
			return "1"
		}
		return "0"
	}
	deps := append([]string(nil), params.DAGDeps...)
	for i, d := range deps {
		deps[i] = strings.TrimSpace(d)
	}
	sort.Strings(deps)
	parts := []string{
		specName,
		strings.TrimSpace(params.Agent),
		strings.TrimSpace(params.Prompt),
		strings.TrimSpace(params.Context),
		strings.TrimSpace(params.Description),
		strings.TrimSpace(contract),
		boolFlag(params.SharedNotes),
		boolFlag(params.RunInBackground),
		strings.TrimSpace(params.DAGID),
		strconv.Itoa(len(deps)),
	}
	parts = append(parts, deps...)
	return lengthPrefixedKey(parts...)
}

func lengthPrefixedKey(parts ...string) string {
	var b strings.Builder
	for _, part := range parts {
		b.WriteString(strconv.Itoa(len(part)))
		b.WriteByte(':')
		b.WriteString(part)
	}
	return b.String()
}

// duplicateToolCallIndices returns later duplicate indices mapped to their
// first (winning) index. Invalid argument payloads are omitted rather than
// guessed at, so malformed calls remain independently executable.
func duplicateToolCallIndices(toolCalls []ToolCall) (map[int]int, []int) {
	firstByKey := make(map[string]int)
	rejected := make(map[int]int)
	var rejectedIdx []int
	for i, tc := range toolCalls {
		key, ok := canonicalToolCallKey(tc.Function.Name, json.RawMessage(tc.Function.Arguments))
		if !ok {
			continue
		}
		if first, exists := firstByKey[key]; exists {
			rejected[i] = first
			rejectedIdx = append(rejectedIdx, i)
			continue
		}
		firstByKey[key] = i
	}
	if len(rejected) == 0 {
		return nil, nil
	}
	return rejected, rejectedIdx
}
