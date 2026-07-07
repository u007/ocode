package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/u007/ocode/internal/knowledge"
)

// doc_search get_top bounds: maximum number of match bodies returned inline,
// and the per-body truncation length. Both exist to keep a single
// search-with-content call's output size bounded.
const (
	docSearchMaxTop       = 5
	docSearchMaxBodyChars = 4000
)

// newDocTools resolves the OKF bundle and returns doc tools wrapping a Store.
// Returns error when no bundle is found at workDir.
func newDocTools(workDir string) ([]DocTool, error) {
	bundle, ok := knowledge.DetectBundle(workDir)
	if !ok {
		return nil, fmt.Errorf("no OKF knowledge bundle found at %s/docs — run /docs init first", workDir)
	}
	store := knowledge.NewStore(bundle)
	return []DocTool{
		&DocSearchTool{store: store},
		&DocGetTool{store: store},
		&DocWriteTool{store: store},
		&DocDeprecateTool{store: store},
	}, nil
}

// DocTool is the interface for knowledge doc tools.
type DocTool interface {
	Name() string
	Description() string
	Parallel() bool
	Definition() map[string]interface{}
	Execute(args json.RawMessage) (string, error)
}

// DocSearchTool searches the knowledge bundle.
type DocSearchTool struct {
	store *knowledge.Store
}

func (t *DocSearchTool) Name() string        { return "doc_search" }
func (t *DocSearchTool) Description() string { return "Search the project's OKF knowledge bundle for documents matching a query, filtered by tags and/or type. Queries are tokenized into words (ALL must appear somewhere in the doc). Results are ranked by relevance and paginated." }
func (t *DocSearchTool) Parallel() bool      { return true }

func (t *DocSearchTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "doc_search",
		"description": "Search curated knowledge docs (OKF bundle under docs/). Use for why/decision/playbook/gotcha questions. Results are sorted by relevance, paginated.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query (matched against title, description, body).",
				},
				"tags": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Optional tags to filter by (AND logic).",
				},
				"type": map[string]interface{}{
					"type":        "string",
					"description": "Optional document type filter (e.g. Decision, Playbook, Schema, Gotcha).",
				},
				"page": map[string]interface{}{
					"type":        "integer",
					"description": "Page number (1-based, default 1).",
				},
				"get_top": map[string]interface{}{
					"type":        "integer",
					"description": "Number of top-ranked matches to also return the full document body for (0 = metadata only, the default). Bounded to 5. Lets a single call both find and read docs, avoiding a separate doc_get round-trip.",
				},
			},
			"required": []string{"query"},
		},
	}
}

func (t *DocSearchTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Query   string   `json:"query"`
		Tags    []string `json:"tags"`
		DocType string   `json:"type"`
		Page    int      `json:"page"`
		GetTop  int      `json:"get_top"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid doc_search arguments: %w", err)
	}
	if params.Query == "" {
		return "", fmt.Errorf("query is required")
	}
	if params.Page < 1 {
		params.Page = 1
	}

	// Convert 1-based page to 0-based for Search (paginate uses 0-based).
	zeroBasedPage := params.Page - 1

	results, total, err := t.store.Search(params.Query, params.Tags, params.DocType, zeroBasedPage, 20)
	if err != nil {
		return "", fmt.Errorf("doc_search failed: %w", err)
	}

	if len(results) == 0 {
		return fmt.Sprintf("No matching documents found (0 total)."), nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d matching document(s) (page %d, %d total):\n\n", len(results), params.Page, total))
	for i, doc := range results {
		status := ""
		if doc.Status == "deprecated" {
			status = " (deprecated)"
		}
		b.WriteString(fmt.Sprintf("%d. **%s**%s — %s\n", i+1, doc.Title, status, doc.Description))
		b.WriteString(fmt.Sprintf("   Path: `%s`", doc.Path))
		if doc.Type != "" {
			b.WriteString(fmt.Sprintf(", Type: %s", doc.Type))
		}
		if len(doc.Tags) > 0 {
			b.WriteString(fmt.Sprintf(", Tags: %s", strings.Join(doc.Tags, ", ")))
		}
		b.WriteString("\n\n")
	}
	// Optional: return the full body of the top-N matches inline so a single
	// doc_search can both find AND read docs, avoiding a separate doc_get
	// round-trip. Backward-compatible: get_top defaults to 0 (metadata only),
	// identical to prior behaviour. Bounded (docSearchMaxTop) and each body is
	// truncated (docSearchMaxBodyChars) with a pointer to doc_get for the rest.
	if params.GetTop > 0 {
		top := params.GetTop
		if top > docSearchMaxTop {
			top = docSearchMaxTop
		}
		if top > len(results) {
			top = len(results)
		}
		b.WriteString(fmt.Sprintf("\n--- Full content of top %d match(es) ---\n\n", top))
		for i := 0; i < top; i++ {
			doc := results[i]
			body := doc.Body
			if len(body) > docSearchMaxBodyChars {
				body = body[:docSearchMaxBodyChars] + fmt.Sprintf("\n… [truncated; full text via doc_get `%s`]", doc.Path)
			}
			b.WriteString(fmt.Sprintf("### %s\nPath: `%s`\n\n%s\n\n", doc.Title, doc.Path, body))
		}
	}
	if total > params.Page*20 {
		b.WriteString(fmt.Sprintf("(More results available — use page %d)\n", params.Page+1))
	}
	return b.String(), nil
}

// DocGetTool retrieves a single document by path.
type DocGetTool struct {
	store *knowledge.Store
}

func (t *DocGetTool) Name() string        { return "doc_get" }
func (t *DocGetTool) Description() string { return "Retrieve a single knowledge document by its bundle-relative path." }
func (t *DocGetTool) Parallel() bool      { return true }

func (t *DocGetTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "doc_get",
		"description": "Get the full content of one knowledge document by bundle-relative path (e.g. decisions/foo.md). Returns frontmatter and body.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Bundle-relative path to the document (e.g. decisions/architecture.md).",
				},
			},
			"required": []string{"path"},
		},
	}
}

func (t *DocGetTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid doc_get arguments: %w", err)
	}
	if params.Path == "" {
		return "", fmt.Errorf("path is required")
	}

	doc, err := t.store.Get(params.Path)
	if err != nil {
		return "", fmt.Errorf("doc_get: %w", err)
	}
	if doc == nil {
		return fmt.Sprintf("Document not found at path: %s", params.Path), nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("# %s\n\n", doc.Title))
	if doc.Type != "" {
		b.WriteString(fmt.Sprintf("**Type:** %s  \n", doc.Type))
	}
	if doc.Description != "" {
		b.WriteString(fmt.Sprintf("**Description:** %s  \n", doc.Description))
	}
	if doc.Resource != "" {
		b.WriteString(fmt.Sprintf("**Resource:** %s  \n", doc.Resource))
	}
	if len(doc.Tags) > 0 {
		b.WriteString(fmt.Sprintf("**Tags:** %s  \n", strings.Join(doc.Tags, ", ")))
	}
	if doc.Status == "deprecated" {
		b.WriteString(fmt.Sprintf("**Status:** deprecated  \n"))
		if doc.DeprecatedReason != "" {
			b.WriteString(fmt.Sprintf("**Deprecated reason:** %s  \n", doc.DeprecatedReason))
		}
	}
	b.WriteString("\n---\n\n")
	b.WriteString(doc.Body)
	return b.String(), nil
}

// DocWriteTool creates or updates a knowledge document.
type DocWriteTool struct {
	store *knowledge.Store
}

func (t *DocWriteTool) Name() string        { return "doc_write" }
func (t *DocWriteTool) Description() string { return "Create or update a knowledge document in the OKF bundle. The store enforces that `type` is present, reserved files cannot be overwritten, and paths are bundle-relative. Index and log are regenerated automatically." }
func (t *DocWriteTool) Parallel() bool      { return false }

func (t *DocWriteTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "doc_write",
		"description": "Create or update a knowledge doc. Path is bundle-relative (e.g. decisions/api-design.md). Type examples: Decision, Playbook, Schema, Gotcha. Index and log are auto-maintained. Never delete — use doc_deprecate instead.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Bundle-relative path (e.g. decisions/api-design.md). Must be within docs/ and not a reserved file (index.md, log.md).",
				},
				"type": map[string]interface{}{
					"type":        "string",
					"description": "Document type. Examples: Decision, Playbook, Schema, Gotcha, Concept, Guide.",
				},
				"title": map[string]interface{}{
					"type":        "string",
					"description": "Human-readable title.",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Short description for the index listing.",
				},
				"resource": map[string]interface{}{
					"type":        "string",
					"description": "URL or reference to the source resource.",
				},
				"tags": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Tags for filtering.",
				},
				"body": map[string]interface{}{
					"type":        "string",
					"description": "Document body in markdown.",
				},
			},
			"required": []string{"path", "type"},
		},
	}
}

func (t *DocWriteTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Path        string   `json:"path"`
		DocType     string   `json:"type"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Resource    string   `json:"resource"`
		Tags        []string `json:"tags"`
		Body        string   `json:"body"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid doc_write arguments: %w", err)
	}
	if params.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	if params.DocType == "" {
		return "", fmt.Errorf("type is required — examples: Decision, Playbook, Schema, Gotcha")
	}

	if err := t.store.Write(params.Path, params.DocType, params.Title, params.Description, params.Resource, params.Tags, params.Body); err != nil {
		return "", fmt.Errorf("doc_write: %w", err)
	}
	slog.Debug("doc_write: created/updated document", "path", params.Path, "type", params.DocType)
	return fmt.Sprintf("Document created/updated at `%s`. The index and change log have been updated.", params.Path), nil
}

// DocDeprecateTool deprecates a knowledge document.
type DocDeprecateTool struct {
	store *knowledge.Store
}

func (t *DocDeprecateTool) Name() string        { return "doc_deprecate" }
func (t *DocDeprecateTool) Description() string { return "Mark a knowledge document as deprecated. The document stays on disk until /docs cleanup removes it. A reason is required." }
func (t *DocDeprecateTool) Parallel() bool      { return false }

func (t *DocDeprecateTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "doc_deprecate",
		"description": "Deprecate a knowledge doc (set status=deprecated). Never deletes — use this instead. Requires a reason.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Bundle-relative path to the document to deprecate.",
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "Why this document is being deprecated.",
				},
			},
			"required": []string{"path", "reason"},
		},
	}
}

func (t *DocDeprecateTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Path   string `json:"path"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid doc_deprecate arguments: %w", err)
	}
	if params.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	if params.Reason == "" {
		return "", fmt.Errorf("reason is required")
	}

	if err := t.store.Deprecate(params.Path, params.Reason); err != nil {
		return "", fmt.Errorf("doc_deprecate: %w", err)
	}
	slog.Debug("doc_deprecate: deprecated document", "path", params.Path, "reason", params.Reason)
	return fmt.Sprintf("Document `%s` has been deprecated. Reason: %s. The index and change log have been updated.", params.Path, params.Reason), nil
}
