package tool

import (
	"context"
	"encoding/json"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/lsp"
)

type Tool interface {
	Name() string
	Description() string
	Definition() map[string]interface{}
	Execute(args json.RawMessage) (string, error)
	Parallel() bool
}

// ImageResultTool is an optional extension of Tool for tools that can return
// the raw bytes of an image file (subject to the tool's own path confinement).
// The agent calls ExecuteImage only after it has decided the target is a
// decodable image and the active model can see images; it then resizes and
// embeds the bytes as a vision block. Keeping the read inside the tool
// preserves confinement — the agent never opens an arbitrary model-supplied
// path itself.
type ImageResultTool interface {
	Tool
	ExecuteImage(args json.RawMessage) (raw []byte, mimeType string, err error)
}

// ContextualTool is an optional extension of Tool for tools that need a
// context to access the per-agent snapshot store and tool call ID. The agent
// calls ExecuteCtx when available; Execute is the fallback for callers that
// have no context (tests, TUI direct calls).
type ContextualTool interface {
	Tool
	ExecuteCtx(ctx context.Context, args json.RawMessage) (string, error)
}

// StreamingTool is an optional extension of Tool for tools that can emit
// incremental output as they execute (e.g. a long-running shell command).
// When a tool implements StreamingTool, the agent loop calls ExecuteStream
// with an emit callback instead of the synchronous Execute, and the TUI
// renders each chunk live alongside the tool call. Tools that do not
// implement it fall back to the synchronous Execute (the emit callback is
// nil in that path). The final string returned by ExecuteStream is the
// canonical, complete result used for the conversation; the streamed chunks
// are a live preview that the TUI replaces with that canonical result.
type StreamingTool interface {
	Tool
	ExecuteStream(args json.RawMessage, emit func(chunk string)) (string, error)
}

// NoticedError wraps an error with a user-facing notice that should be shown
// in the transcript but not sent to the LLM. Tools return this when they
// encounter a recoverable problem that the user should know about (e.g. an
// LSP server not being installed).
type NoticedError struct {
	Err    error
	Notice string // User-facing message shown in the transcript
}

func (e *NoticedError) Error() string { return e.Err.Error() }
func (e *NoticedError) Unwrap() error { return e.Err }

// NoticeSentinel is the prefix used in assistant messages that carry a
// transient user-facing notice. The TUI strips this prefix and renders the
// remainder as a non-LLM transient message.
const NoticeSentinel = "NOTICE:"

// InitBuiltinTools builds the canonical set of built-in tools using an
// existing LSP manager and config. This is the single source of truth for
// which tools are available — both LoadBuiltins and the TUI's
// getInitialTools call this to keep the tool list in sync.
func InitBuiltinTools(lspMgr *lsp.Manager, cfg *config.Config) []Tool {
	if cfg != nil {
		setExtraAllowedPaths(cfg.Ocode.ExtraAllowedPaths)
	} else {
		setExtraAllowedPaths(nil)
	}
	builtins := []Tool{
		&ReadTool{},
		&UndoTool{},
		&WriteTool{Config: cfg},
		&ReplaceLinesToolImpl{Config: cfg},
		&DeleteTool{},
		&GlobTool{},
		&GrepTool{},
		&BashTool{},
		&EditTool{Config: cfg},
		&MultiEditTool{Config: cfg},
		&MultiFileEditTool{Config: cfg},
		&PatchTool{},
		&TodoWriteTool{},
		&TodoReadTool{},
		&SkillTool{},
		&QuestionTool{},
		&WebFetchTool{},
		&WebSearchTool{},
		&RepoCloneTool{},
		&RepoOverviewTool{},
		&PlanEnterTool{},
		&PlanExitTool{},
		&ListTool{},
		&LSPTool{Mgr: lspMgr},
		&LSPDiagnosticsTool{Mgr: lspMgr},
		&FormatTool{Config: cfg},
		&GitHubPRTool{},
		&GitHubIssueTool{},
		&GitHubWorkflowTool{},
	}
	// The "ast" semantic tool (LSP-backed) is registered by default whenever a
	// language server is available on PATH — no plugin toggle required.
	if lsp.AnyServerInstalled() {
		builtins = append(builtins, &AstTool{Mgr: lspMgr})
	}
	// ast-grep (structural search/rewrite via the ast-grep CLI) is the opt-in
	// plugin, gated by plugins.ast (toggle with /plugin enable ast).
	if cfg != nil && cfg.Ocode.Plugins.AST {
		builtins = append(builtins, &AstGrepTool{})
	}
	// OCR tool — always registered so the model knows it exists. When OCR
	// is disabled or no model is configured, the tool's Execute method
	// returns a NoticedError telling the user to use /ocr enable and
	// /ocr model to configure it.
	builtins = append(builtins, &OcrTool{Config: cfg})
	return builtins
}

// LoadBuiltins returns the full set of built-in tools (always-on + opt-in)
// plus a fresh shared LSP manager that backs the `lsp` and `ast` tools. The
// caller is responsible for closing the manager when the tool set is no
// longer in use (typically when the agent/session that owns it is torn down)
// — failing to do so leaks the underlying language-server processes.
//
// Prefer InitBuiltinTools when you already have an LSP manager (e.g. the
// TUI caches one per session) so both the manager and the tool list come
// from a single source of truth.
func LoadBuiltins(cfg *config.Config) ([]Tool, *lsp.Manager) {
	lspMgr := lsp.NewManager(".")
	return InitBuiltinTools(lspMgr, cfg), lspMgr
}
