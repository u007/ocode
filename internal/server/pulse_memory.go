package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/u007/ocode/internal/memory"
	"github.com/u007/ocode/internal/secretfile"
)

// The assistant's own memory, separate from internal/memory's user/project
// files: notes the operator asked it to keep across conversations.
//
//	<pulse root>/memory/global.md
//	<pulse root>/memory/projects/<slug>.md   (<slug> = memory.ProjectSlug)

const (
	// pulseMemoryTokenBudget is the per-file ceiling in tokens. There is no
	// tokenizer here, so the byte cap is derived from the conservative estimate
	// the decision-state budget uses (~3 bytes per token).
	pulseMemoryTokenBudget = 30_000
	pulseMemoryBytesPerTok = 3
	// pulseMemoryCap is the per-file size limit in bytes; larger writes are rejected.
	pulseMemoryCap = pulseMemoryTokenBudget * pulseMemoryBytesPerTok
	// pulseMemoryPromptCap clips one memory in the per-turn prompt section.
	pulseMemoryPromptCap = 8 * 1024

	pulseMemoryScopeGlobal  = "global"
	pulseMemoryScopeProject = "project"

	pulseMemoryOpen  = "[ocode:pulse-memory]"
	pulseMemoryClose = "[/ocode:pulse-memory]"
)

// pulseSlugCache memoises memory.ProjectSlug, which spawns `git rev-parse`
// (~130 ms) and would otherwise run once per live project on every turn.
var pulseSlugCache sync.Map

func pulseProjectSlug(projectPath string) string {
	if v, ok := pulseSlugCache.Load(projectPath); ok {
		return v.(string)
	}
	slug := memory.ProjectSlug(projectPath)
	pulseSlugCache.Store(projectPath, slug)
	return slug
}

// pulseMemoryFile resolves the file for a scope. A project scope needs a
// project_path that is one of allowedProjectRoots(), which is the boundary and
// is not widened here.
func (h *Handler) pulseMemoryFile(scope, projectPath string) (string, error) {
	root, err := pulseAssistantRoot()
	if err != nil {
		return "", err
	}
	switch scope {
	case pulseMemoryScopeGlobal:
		if projectPath != "" {
			return "", errors.New("project_path is only valid with scope \"project\"")
		}
		return filepath.Join(root, "memory", "global.md"), nil
	case pulseMemoryScopeProject:
		if projectPath == "" {
			return "", errors.New("scope \"project\" requires project_path")
		}
		allowed := false
		for _, r := range h.allowedProjectRoots() {
			if r == projectPath {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", fmt.Errorf("project_path %q is not a project registered with this server", projectPath)
		}
		slug := pulseProjectSlug(projectPath)
		if slug == "" {
			return "", fmt.Errorf("cannot derive a memory slug for %q", projectPath)
		}
		return filepath.Join(root, "memory", "projects", slug+".md"), nil
	default:
		return "", fmt.Errorf("scope must be %q or %q, got %q", pulseMemoryScopeGlobal, pulseMemoryScopeProject, scope)
	}
}

func (h *Handler) pulseMemoryTools() []*pulseTool {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	scopeProps := map[string]any{
		"scope":        map[string]any{"type": "string", "enum": []string{pulseMemoryScopeGlobal, pulseMemoryScopeProject}, "description": "global, or project for one project's notes"},
		"project_path": str("Project root, required for scope project"),
	}
	writeProps := map[string]any{"content": str(fmt.Sprintf("The COMPLETE new memory text (replaces the old; max about %d tokens). Read first, keep only what is still important, and drop stale notes.", pulseMemoryTokenBudget))}
	for k, v := range scopeProps {
		writeProps[k] = v
	}
	return []*pulseTool{
		{
			name:  "memory_read",
			desc:  "Read your own memory file (notes you were asked to keep). Empty content means nothing is saved yet.",
			props: scopeProps,
			run:   h.pulseMemoryReadTool,
		},
		{
			name: "memory_write",
			desc: "Replace one of your own memory files with new content. Use it when the operator asks you to remember something or to forget it.",
			// No `ask`: unlike the other write tools this never touches another
			// session, a project or the user's files, only the assistant's own
			// notes under its private root, size-capped and atomically replaced.
			// An approval dialog per remembered fact would only train the
			// operator to click through dialogs that matter.
			props: writeProps,
			run:   h.pulseMemoryWriteTool,
		},
	}
}

func (h *Handler) pulseMemoryReadTool(raw json.RawMessage) (string, error) {
	var args struct {
		Scope       string `json:"scope"`
		ProjectPath string `json:"project_path"`
	}
	if err := decodePulseArgs(raw, &args); err != nil {
		return "", err
	}
	path, err := h.pulseMemoryFile(args.Scope, args.ProjectPath)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return marshalPulseString(map[string]any{"scope": args.Scope, "path": path, "content": string(data)})
}

func (h *Handler) pulseMemoryWriteTool(raw json.RawMessage) (string, error) {
	var args struct {
		Scope       string  `json:"scope"`
		ProjectPath string  `json:"project_path"`
		Content     *string `json:"content"`
	}
	if err := decodePulseArgs(raw, &args); err != nil {
		return "", err
	}
	if args.Content == nil {
		return "", errors.New("content is required (an empty string clears the memory)")
	}
	if len(*args.Content) > pulseMemoryCap {
		return "", fmt.Errorf("memory is %d bytes, over the ~%d token cap (%d bytes); keep only the important notes and condense it", len(*args.Content), pulseMemoryTokenBudget, pulseMemoryCap)
	}
	path, err := h.pulseMemoryFile(args.Scope, args.ProjectPath)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := secretfile.WriteFileAtomic(path, []byte(*args.Content), 0o600); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return marshalPulseString(map[string]any{"scope": args.Scope, "path": path, "bytes": len(*args.Content)})
}

// pulseMemorySection renders the per-turn memory section: global memory plus
// the memory of every project with a row on the board (deduped, sorted by
// path). Empty when nothing is saved. It rides the user-role tail with the
// board because it can change between turns (memory_write, new live projects).
func (h *Handler) pulseMemorySection(rows []PulseRow) (string, error) {
	root, err := pulseAssistantRoot()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	add := func(label, path string) error {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		text := strings.TrimSpace(string(data))
		if text == "" {
			return nil
		}
		if len(text) > pulseMemoryPromptCap {
			cut := pulseMemoryPromptCap
			for cut > 0 && !utf8.RuneStart(text[cut]) {
				cut--
			}
			text = text[:cut] + "\n(truncated, use memory_read)"
		}
		fmt.Fprintf(&b, "\n## %s\n%s\n", label, text)
		return nil
	}
	if err := add("global", filepath.Join(root, "memory", "global.md")); err != nil {
		return "", err
	}
	seen := map[string]bool{}
	var projects []string
	for _, r := range rows {
		if r.ProjectPath != "" && !seen[r.ProjectPath] {
			seen[r.ProjectPath] = true
			projects = append(projects, r.ProjectPath)
		}
	}
	sort.Strings(projects)
	for _, p := range projects {
		slug := pulseProjectSlug(p)
		if slug == "" {
			continue
		}
		if err := add("project "+p, filepath.Join(root, "memory", "projects", slug+".md")); err != nil {
			return "", err
		}
	}
	if b.Len() == 0 {
		return "", nil
	}
	return pulseMemoryOpen + "\nYour saved notes (edit with memory_write):" + b.String() + pulseMemoryClose, nil
}
