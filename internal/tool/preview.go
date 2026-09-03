package tool

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// PreviewOpenSentinel prefixes the preview_open tool result. The web
// PreviewHost's usePreviewActivation hook scans chat tool results for this
// prefix and auto-opens the referenced file in the sidebar — no extra SSE
// plumbing needed; the chat stream already carries tool results to the SPA.
const PreviewOpenSentinel = "PREVIEW_OPEN:"

// previewOpenKinds is the allowlist of sidebar-previewable extensions.
// Text/code fall back to the existing Monaco editor tab; binaries render
// via pdf.js (pdf), docx-preview (docx), jszip slide parser (pptx),
// mermaid.js (mmd/md with diagrams), or images.
var previewOpenKinds = map[string]string{
	".pdf":  "pdf",
	".docx": "docx",
	".pptx": "pptx",
	".png":  "image",
	".jpg":  "image",
	".jpeg": "image",
	".gif":  "image",
	".webp": "image",
	".svg":  "image",
	".mmd":  "mermaid",
	".md":   "text",
	".txt":  "text",
	".ts":   "text",
	".tsx":  "text",
	".js":   "text",
	".jsx":  "text",
	".go":   "text",
	".py":   "text",
	".json": "text",
	".yaml": "text",
	".yml":  "text",
	".html": "text",
	".css":  "text",
}

// PreviewOpenTool lets the LLM activate the sidebar preview/editor on a
// file: "show this deck in the sidebar", "open the spec for editing".
// It validates the path only (existence + allowlist); the actual bytes
// flow through GET /api/files/raw or /api/files/content so confinement
// stays in the server handlers, not the tool.
type PreviewOpenTool struct{}

func (t *PreviewOpenTool) Name() string { return "preview_open" }

func (t *PreviewOpenTool) Description() string {
	return "Open a file in the sidebar preview (browser tab stays available). Use when the user asks to preview, present, or edit a file: PDFs (multi-page), Word docs, PowerPoint decks (slides + present mode), mermaid diagrams (clickable nodes), images, or text/code (editable). Returns a PREVIEW_OPEN directive the web UI auto-opens."
}

func (t *PreviewOpenTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        t.Name(),
			"description": t.Description(),
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Workspace-relative file path to preview (e.g. docs/deck.pptx). Must stay inside the project root.",
					},
					"page": map[string]interface{}{
						"type":        "integer",
						"description": "Optional 1-based initial page/slide to show (PDF, PPTX). Defaults to 1.",
					},
				},
				"required": []string{"path"},
			},
		},
	}
}

func (t *PreviewOpenTool) Parallel() bool { return true }

func (t *PreviewOpenTool) Execute(args json.RawMessage) (string, error) {
	var a struct {
		Path string `json:"path"`
		Page int    `json:"page"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	p := strings.TrimSpace(a.Path)
	if p == "" {
		return "", fmt.Errorf("path is required")
	}
	clean := filepath.Clean(p)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path must be workspace-relative and stay inside the project root")
	}
	kind, ok := previewOpenKinds[strings.ToLower(filepath.Ext(clean))]
	if !ok {
		return "", fmt.Errorf("file type is not sidebar-previewable (pdf, docx, pptx, mmd/md, images, text/code)")
	}
	page := a.Page
	if page < 0 {
		return "", fmt.Errorf("page must be >= 1")
	}
	if page == 0 {
		page = 1
	}
	return fmt.Sprintf("%s%s|kind=%s|page=%d", PreviewOpenSentinel, clean, kind, page), nil
}
