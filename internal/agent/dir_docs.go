package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// dirDocNames are the filenames trackDirMDTouch looks for in each ancestor
// directory, mirroring the always-on root-level set (mdIsAlwaysOn).
var dirDocNames = []string{"CLAUDE.md", "AGENTS.md", "OCODE.md"}

// dirTouchingTools maps a tool name to the JSON arg key holding the file/dir
// path it operates on. multi_file_edit (edits[].path) is handled separately
// in dirTouchPaths; apply_patch is not covered because its paths live inside
// the patch text.
var dirTouchingTools = map[string]string{
	"read": "path", "write": "path", "edit": "path",
	"multiedit": "file_path", "glob": "path", "list": "path", "grep": "path",
	"replace_lines": "path", "format": "path", "lsp": "path", "ast": "path",
}

// dirTouchPaths extracts the file/dir paths a tool call operated on, for the
// subdirectory-doc trigger. Nil when the tool is not path-touching.
func dirTouchPaths(toolName string, args json.RawMessage) []string {
	if toolName == "multi_file_edit" {
		var params struct {
			Edits []struct {
				Path string `json:"path"`
			} `json:"edits"`
		}
		if err := json.Unmarshal(args, &params); err != nil {
			return nil
		}
		var out []string
		for _, e := range params.Edits {
			if strings.TrimSpace(e.Path) != "" {
				out = append(out, e.Path)
			}
		}
		return out
	}
	pathKey, ok := dirTouchingTools[toolName]
	if !ok {
		return nil
	}
	var params map[string]json.RawMessage
	if err := json.Unmarshal(args, &params); err != nil {
		return nil
	}
	var pathValue string
	if err := json.Unmarshal(params[pathKey], &pathValue); err != nil || strings.TrimSpace(pathValue) == "" {
		return nil
	}
	return []string{pathValue}
}

// trackDirMDTouch is called from executeToolCall after a tool that reads or
// lists a path succeeds. It walks up from the touched path's directory to
// the project root, and for each ancestor not yet seen this session, queues
// any CLAUDE.md/AGENTS.md/OCODE.md found there for injection on the next
// model iteration (see injectDirMDTail). Root-level docs are skipped —
// LoadContext already injects those into the stable system prompt.
func (a *Agent) trackDirMDTouch(toolName string, args json.RawMessage) {
	if strings.TrimSpace(a.workDir) == "" {
		return
	}
	paths := dirTouchPaths(toolName, args)
	if len(paths) == 0 {
		return
	}
	root := a.effectiveWorkDir()
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return
	}
	for _, p := range paths {
		a.trackDirMDPath(absRoot, p)
	}
}

// resetDirMDSeen forgets which subdirectories have had their docs injected.
// Called when the transcript is compacted: the injected blocks were never
// persisted, so after the splice the model no longer has them, and the next
// touch of each directory must re-surface its docs.
func (a *Agent) resetDirMDSeen() {
	a.dirMDMu.Lock()
	defer a.dirMDMu.Unlock()
	a.dirMDSeen = nil
	a.dirMDPending = nil
}

func (a *Agent) trackDirMDPath(absRoot, pathValue string) {
	target := pathValue
	if !filepath.IsAbs(target) {
		target = filepath.Join(absRoot, target)
	}
	target = filepath.Clean(target)
	dir := target
	if info, err := os.Stat(target); err == nil && !info.IsDir() {
		dir = filepath.Dir(target)
	}

	// Walk from dir up to (excluding) absRoot — root-level docs are already
	// part of the always-on system prompt.
	for {
		if dir == absRoot || dir == "." || dir == string(filepath.Separator) {
			break
		}
		rel, err := filepath.Rel(absRoot, dir)
		if err != nil || strings.HasPrefix(rel, "..") {
			break
		}
		a.queueDirMD(dir, rel)
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
}

// queueDirMD checks dir for any doc in dirDocNames and, the first time this
// directory is seen this session, queues its content for the volatile tail.
func (a *Agent) queueDirMD(dir, rel string) {
	a.dirMDMu.Lock()
	if a.dirMDSeen == nil {
		a.dirMDSeen = make(map[string]bool)
	}
	if a.dirMDSeen[dir] {
		a.dirMDMu.Unlock()
		return
	}
	a.dirMDSeen[dir] = true // mark seen even if empty — never rescan this dir
	a.dirMDMu.Unlock()

	for _, name := range dirDocNames {
		path := filepath.Join(dir, name)
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		relPath := filepath.ToSlash(filepath.Join(rel, name))
		block := "--- " + relPath + " ---\n" + string(content)
		a.dirMDMu.Lock()
		a.dirMDPending = append(a.dirMDPending, block)
		a.dirMDMu.Unlock()
	}
}

// drainDirMDPending returns and clears the queued ancestor-doc blocks.
func (a *Agent) drainDirMDPending() []string {
	a.dirMDMu.Lock()
	defer a.dirMDMu.Unlock()
	if len(a.dirMDPending) == 0 {
		return nil
	}
	out := a.dirMDPending
	a.dirMDPending = nil
	return out
}

// injectDirMDTail appends a volatile user-role block listing any
// subdirectory CLAUDE.md/AGENTS.md/OCODE.md docs discovered since the last
// Step (via tool calls that touched files in that subdirectory). No-op when
// nothing new was found — keeps the cached prefix stable across loops with
// no new directory touched, per the append_stable.go contract.
func injectDirMDTail(messages []Message, a *Agent) []Message {
	if a == nil {
		return messages
	}
	blocks := a.drainDirMDPending()
	if len(blocks) == 0 {
		return messages
	}
	var b strings.Builder
	b.WriteString(promptDiscoveryMarker)
	b.WriteString(" subdirectory project docs for files just accessed:\n")
	for _, block := range blocks {
		b.WriteString(block)
		b.WriteString("\n")
	}
	return append(messages, Message{Role: "user", Content: b.String()})
}
