package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// astGrepBin is the CLI binary this tool shells out to.
const astGrepBin = "ast-grep"

// astGrepMaxOutput caps the bytes returned to the model so a broad pattern
// can't flood the context window.
const astGrepMaxOutput = 30 * 1024

// AstGrepTool runs structural (syntax-tree) code search/rewrite by shelling
// out to the `ast-grep` CLI. Unlike the LSP-backed "ast" tool (which answers
// semantic questions — references/definition/callers), ast-grep matches code
// by SHAPE using tree-sitter patterns, e.g. find every `if err != nil { return $E }`.
//
// This tool is an opt-in plugin gated by the `plugins.ast` config flag (toggle
// at runtime with `/plugin enable ast`). It additionally requires the
// `ast-grep` binary on PATH; if it is missing, Execute returns an actionable
// install hint via NoticedError.
type AstGrepTool struct{}

func (t *AstGrepTool) Name() string { return "ast_grep" }
func (t *AstGrepTool) Description() string {
	return "Structural code search/rewrite via the ast-grep CLI: match code by SYNTAX SHAPE using tree-sitter patterns (e.g. 'if err != nil { return $E }'). Use grep for text/regex; use ast for semantic references/definitions."
}
func (t *AstGrepTool) Parallel() bool { return true }

func (t *AstGrepTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name": "ast_grep",
		"description": "Structural code search and rewrite powered by the ast-grep CLI (tree-sitter). Matches by syntax SHAPE, not text. Use metavariables like $VAR / $$$ARGS in patterns. " +
			"Examples: pattern 'console.log($A)' finds all console.log calls; with rewrite 'logger.debug($A)' previews the replacement. " +
			"For plain text/regex use grep; for semantic 'who calls/defines X' use the ast tool.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pattern": map[string]interface{}{
					"type":        "string",
					"description": "ast-grep structural pattern. Use metavariables: $NAME (single node), $$$NAME (variadic, e.g. arg lists). Example: 'func $NAME($$$ARGS) error { $$$ }'.",
				},
				"lang": map[string]interface{}{
					"type":        "string",
					"description": "Language of the pattern (go, rust, python, typescript, tsx, javascript, java, c, cpp, ...). Strongly recommended; ast-grep infers from file extensions otherwise.",
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "File or directory to search. Optional; defaults to the current directory.",
				},
				"rewrite": map[string]interface{}{
					"type":        "string",
					"description": "Optional replacement pattern (may reuse metavariables from 'pattern'). When given, ast-grep PREVIEWS the diff only — it does NOT modify files. Apply edits yourself with edit/multiedit after reviewing.",
				},
			},
			"required": []string{"pattern"},
		},
	}
}

func (t *AstGrepTool) Execute(args json.RawMessage) (string, error) {
	var p struct {
		Pattern string `json:"pattern"`
		Lang    string `json:"lang"`
		Path    string `json:"path"`
		Rewrite string `json:"rewrite"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("ast_grep: invalid params: %w", err)
	}
	if strings.TrimSpace(p.Pattern) == "" {
		return "", fmt.Errorf("ast_grep: 'pattern' is required")
	}
	if p.Path == "" {
		p.Path = "."
	}

	if _, err := exec.LookPath(astGrepBin); err != nil {
		notice := fmt.Sprintf("ast-grep is not installed. To install:\n"+
			"  brew install ast-grep   # macOS\n"+
			"  cargo install ast-grep --locked\n"+
			"  npm i -g @ast-grep/cli\n"+
			"Then retry, or disable this plugin with /plugin disable ast.")
		return "", &NoticedError{Err: fmt.Errorf("%s not found on PATH: %w", astGrepBin, err), Notice: notice}
	}

	// `run` previews only — without --update-all it never writes files.
	argv := []string{"run", "--pattern", p.Pattern, "--color", "never"}
	if p.Lang != "" {
		argv = append(argv, "--lang", p.Lang)
	}
	if p.Rewrite != "" {
		argv = append(argv, "--rewrite", p.Rewrite)
	}
	argv = append(argv, p.Path)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, astGrepBin, argv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("ast_grep: timed out after 30s")
	}
	if err != nil {
		// ast-grep exits 1 when there are simply no matches — not a failure.
		// Any other non-zero status (e.g. 2 for a bad --lang) is a real error;
		// surface stderr so it is visible rather than silently swallowed.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && stderr.Len() == 0 {
			return "No structural matches found", nil
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("ast_grep failed: %s", msg)
	}

	out := strings.TrimRight(stdout.String(), "\n")
	if out == "" {
		return "No structural matches found", nil
	}
	if len(out) > astGrepMaxOutput {
		out = out[:astGrepMaxOutput] + "\n… (output truncated; narrow the pattern or path)"
	}
	return out, nil
}
