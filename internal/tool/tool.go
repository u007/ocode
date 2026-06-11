package tool

import (
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
	// The "ast" semantic tool is an opt-in plugin, disabled by default.
	if cfg != nil && cfg.Ocode.Plugins.AST {
		builtins = append(builtins, &AstTool{Mgr: lspMgr})
	}
	return builtins, lspMgr
}
