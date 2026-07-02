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

// NoticedError wraps an error with a user-facing notice that should be shown
// in the transcript but not sent to the LLM. Tools return this when they
// encounter a recoverable problem that the user should know about (e.g. an
// LSP server not being installed).
type NoticedError struct {
	Err    error
	Notice string // User-facing message shown in the transcript
}

func (e *NoticedError) Error() string  { return e.Err.Error() }
func (e *NoticedError) Unwrap() error  { return e.Err }

// NoticeSentinel is the prefix used in assistant messages that carry a
// transient user-facing notice. The TUI strips this prefix and renders the
// remainder as a non-LLM transient message.
const NoticeSentinel = "NOTICE:"

// LoadBuiltins returns the full set of built-in tools (always-on + opt-in)
// plus the shared LSP manager that backs the `lsp` and `ast` tools. The
// caller is responsible for closing the manager when the tool set is no
// longer in use (typically when the agent/session that owns it is torn down)
// — failing to do so leaks the underlying language-server processes.
func LoadBuiltins(cfg *config.Config) ([]Tool, *lsp.Manager) {
	if cfg != nil {
		setExtraAllowedPaths(cfg.Ocode.ExtraAllowedPaths)
	} else {
		setExtraAllowedPaths(nil)
	}
	// One shared LSP manager so the lsp + ast tools reuse a single server per
	// project instead of each spawning its own.
	lspMgr := lsp.NewManager(".")
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
	// OCR tool — registered when enabled and an OCR model is configured.
	// The runtime gate (/ocr enable|disable) controls the tool's availability
	// via the config, so the tool itself double-checks before executing.
	builtins = append(builtins, &OcrTool{Config: cfg})
	return builtins, lspMgr
}
