// internal/cli/goal.go
package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/orchestrator"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)

// GoalOptions mirrors orchestrator.PipelineOptions for CLI parsing.
type GoalOptions struct {
	UseWorktree   bool
	VerifyMode    string
	MaxIterations int
	Model         string
	Dir           string
	YOLO          bool
}

// ParseGoalArgs parses CLI args for the goal subcommand.
// Flags: --no-worktree, --verify <mode>, --max-iterations <n>, --model <name>, --dir <path>, --yolo
// Remaining args after flags are joined as the goal.
func ParseGoalArgs(args []string) (GoalOptions, string, error) {
	opts := GoalOptions{UseWorktree: true}
	var remaining []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--no-worktree":
			opts.UseWorktree = false
		case "--verify":
			if i+1 >= len(args) {
				return opts, "", fmt.Errorf("--verify requires a value: llm_only | build_llm | build_test_llm")
			}
			i++
			opts.VerifyMode = args[i]
		case "--max-iterations":
			if i+1 >= len(args) {
				return opts, "", fmt.Errorf("--max-iterations requires a number")
			}
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil || n <= 0 {
				return opts, "", fmt.Errorf("--max-iterations must be a positive integer")
			}
			opts.MaxIterations = n
		case "--model", "-m":
			if i+1 >= len(args) {
				return opts, "", fmt.Errorf("--model requires a model name")
			}
			i++
			opts.Model = args[i]
		case "--dir":
			if i+1 >= len(args) {
				return opts, "", fmt.Errorf("--dir requires a path")
			}
			i++
			opts.Dir = args[i]
		case "--yolo":
			opts.YOLO = true
		default:
			remaining = append(remaining, args[i])
		}
	}
	if len(remaining) == 0 {
		return opts, "", fmt.Errorf("goal is required: ocode goal \"<goal>\"")
	}
	goal := ""
	for i, r := range remaining {
		if i > 0 {
			goal += " "
		}
		goal += r
	}
	return opts, goal, nil
}

// Run executes the orchestrator pipeline headlessly.
// Streams status lines to stdout. Returns nil on pass, error on halt/failure.
//
// The pipeline uses the current ocode config (provider, model, API key) via
// the same pattern as `ocode run`: load config, build a client, instantiate
// an agent, and let the pipeline drive it via DispatchSubagent.
func Run(args []string) error {
	opts, goal, err := ParseGoalArgs(args)
	if err != nil {
		return fmt.Errorf("usage error: %w\n\nUsage: ocode goal [--no-worktree] [--verify MODE] [--max-iterations N] [--model MODEL] [--dir PATH] [--yolo] \"<goal>\"", err)
	}

	if opts.Dir != "" {
		if err := os.Chdir(opts.Dir); err != nil {
			return fmt.Errorf("failed to change directory to %s: %w", opts.Dir, err)
		}
		session.SetWorkDir(opts.Dir)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	agent.ApplyAgentConfig(cfg)

	if opts.Model != "" {
		cfg.Model = opts.Model
	}
	if opts.YOLO {
		cfg.Ocode.Permissions.Mode = string(agent.PermissionModeYOLO)
	}

	modelStr := cfg.Model
	if modelStr == "" {
		return fmt.Errorf("no model configured (set OPENCODE_MODEL or model in config)")
	}

	client := agent.NewClient(cfg, modelStr)
	if client == nil {
		return fmt.Errorf("failed to create LLM client for model %q", modelStr)
	}

	tools, lspMgr := tool.LoadBuiltins(cfg, nil)
	parent := agent.NewAgent(client, tools, cfg, lspMgr)
	parent.LoadExternalToolsWithMCP(cfg)
	if opts.YOLO {
		parent.OnPermissionAsk = func(req agent.PermissionRequest) agent.PermissionResponse {
			return agent.PermissionResponse{Level: agent.PermissionAllow}
		}
	}

	fmt.Printf("[Goal] Goal: %s\n", goal)
	fmt.Printf("[Goal] Model: %s\n", modelStr)
	fmt.Printf("[Goal] Worktree: %v\n", opts.UseWorktree)

	pipelineOpts := orchestrator.PipelineOptions{
		UseWorktree:   opts.UseWorktree,
		VerifyMode:    opts.VerifyMode,
		MaxIterations: opts.MaxIterations,
		StatusFunc: func(s orchestrator.State, msg string) {
			fmt.Printf("[%s] %s\n", s, msg)
		},
	}

	pipeline := orchestrator.New(parent, pipelineOpts)
	report, err := pipeline.Run(context.Background(), goal)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[Goal] Error: %v\n", err)
		return err
	}

	fmt.Println()
	fmt.Println(report.FormatMarkdown())

	if !report.Passed {
		return fmt.Errorf("goal halted: see report above")
	}
	return nil
}
