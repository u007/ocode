package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"os"

	"github.com/u007/ocode/internal/acp"
	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/bundled"
	"github.com/u007/ocode/internal/mcpcli"
	"github.com/u007/ocode/internal/models"
	// Registers the OpenAI/Codex provider plugin via its init(). Without this
	// blank import the plugin is never registered, so providerplugin.Get("openai")
	// returns ok=false and ChatGPT OAuth tokens are misrouted to
	// api.openai.com/v1/chat/completions (401 missing_scope) instead of the
	// Codex backend.
	_ "github.com/u007/ocode/internal/plugin/codex"
	"github.com/u007/ocode/internal/cli"
	"github.com/u007/ocode/internal/runcli"
	"github.com/u007/ocode/internal/server"
	"github.com/u007/ocode/internal/skill"
	"github.com/u007/ocode/internal/tui"
	"github.com/u007/ocode/internal/version"
	"github.com/u007/ocode/web"
)

//go:embed all:skills
var bundledSkills embed.FS

//go:embed all:.opencode/plugins
var bundledPlugins embed.FS

//go:embed deepseek-v4-flash.OCODE.md
var bundledModelConfigs embed.FS

// bundledSkillsFS exposes the embedded skills/ tree to the skill package
// as a plain fs.FS rooted at the repo root (the natural embed shape).
// The installer descends into "skills/" itself; this keeps the embed
// path explicit in main.go and matches the way the real binary
// discovers its skills.
func bundledSkillsFS() fs.FS {
	return bundledSkills
}

// bundledModelConfigFS exposes the embedded model-specific OCODE.md files
// (e.g. deepseek-v4-flash.OCODE.md) as a plain fs.FS. The agent package
// uses these as a fallback when no disk-based model context file is found,
// ensuring every build ships with its own model instructions baked in.
func bundledModelConfigFS() fs.FS {
	return bundledModelConfigs
}

// registerBundled wires the embedded skills and plugins file systems into the
// runtime loaders and materializes them to disk as the lowest-precedence
// fallback. This lets a bare binary serve its bundled skills/agents even when
// no disk copy exists, while any disk copy still overrides the embedded one.
func registerBundled() {
	if fsys := bundledSkillsFS(); fsys != nil {
		skill.SetBundledFS(fsys)
		bundled.SetEmbeddedSkills(fsys)
	}
	bundled.SetEmbeddedPlugins(bundledPlugins)
	if err := bundled.EnsureExtracted(); err != nil {
		log.Printf("ocode: bundled asset extraction failed: %v", err)
	}
}

func main() {
	// Check for help flag at top level before any subcommand
	if len(os.Args) > 1 && (os.Args[1] == "-h" || os.Args[1] == "--help") {
		printUsage()
		return
	}

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "--version", "-version":
			fmt.Println(version.Version)
			return
		case "mcp":
			if err := mcpcli.Run(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "run":
			if err := runcli.Run(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "serve":
			// Register the embedded skills FS so that /api/skills and the
			// discovery system can find bundled skills.
			registerBundled()
			if fsys := bundledModelConfigFS(); fsys != nil {
				agent.SetBundledModelConfigFS(fsys)
			}
			if err := server.Run(os.Args[2:], web.FS()); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "web":
			// Register the embedded skills FS so that /api/skills and the
			// discovery system can find bundled skills.
			registerBundled()
			if fsys := bundledModelConfigFS(); fsys != nil {
				agent.SetBundledModelConfigFS(fsys)
			}
			args := append([]string{"--open"}, os.Args[2:]...)
			if err := server.Run(args, web.FS()); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "acp":
			if err := acp.Run(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "goal":
			if err := cli.Run(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "models":
			if err := models.Run(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "skills":
			// Register the embedded skills tree before delegating.
			// Safe to call here: no goroutines have started yet.
			registerBundled()
			if fsys := bundledModelConfigFS(); fsys != nil {
				agent.SetBundledModelConfigFS(fsys)
			}
			if err := skill.Run(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		}
	}

	agent.PreloadRegistry()

	// Register the embedded skills FS once so both the TUI and the
	// skill package can discover bundled skills at runtime.
	registerBundled()

	// Register the embedded model-specific OCODE.md files so they are
	// available as a fallback when no disk-based model context file is
	// found. This ensures every build ships with its own model instructions.
	if fsys := bundledModelConfigFS(); fsys != nil {
		agent.SetBundledModelConfigFS(fsys)
	}

	opts := tui.RunOptions{WebFS: web.FS()}
	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "-session":
			if i+1 < len(os.Args) {
				opts.SessionID = os.Args[i+1]
				i++
			}
		case "-continue":
			opts.Continue = true
		case "-yolo", "--yolo", "--dangerously-skip-permissions":
			// --dangerously-skip-permissions is the OpenCode-compatible alias
			// for YOLO mode: auto-approve every permission request without
			// prompting the user.
			opts.YOLO = true
		case "-permission-mode", "--permission-mode":
			if i+1 < len(os.Args) {
				mode := os.Args[i+1]
				if mode != "auto" && mode != "off" {
					fmt.Fprintf(os.Stderr, "ocode: invalid --permission-mode %q (want auto or off)\n", mode)
					os.Exit(2)
				}
				opts.PermissionMode = mode
				i++
			}
		}
	}

	if err := tui.Run(opts); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("ocode - AI coding agent")
	fmt.Println()
	fmt.Println("Usage: ocode [global options] <command> [command options]")
	fmt.Println()
	fmt.Println("Global Options:")
	fmt.Println("  -h, --help       Show this help message")
	fmt.Println("  --version        Show version information")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  run              Run a prompt non-interactively (headless)")
	fmt.Println("  serve            Start the HTTP server with web UI")
	fmt.Println("  web              Start server and open browser (alias for 'serve --open')")
	fmt.Println("  acp              Run Agent Client Protocol (ACP) server (Zed integration)")
	fmt.Println("  mcp              Manage MCP (Model Context Protocol) servers")
	fmt.Println("  models           List available models")
	fmt.Println("  skills           Manage skills")
	fmt.Println("  version          Show version information")
	fmt.Println()
	fmt.Println("TUI Mode (no command):")
	fmt.Println("  ocode [options]  Start the interactive TUI")
	fmt.Println()
	fmt.Println("TUI Options:")
	fmt.Println("  -session <id>    Resume a specific session by ID")
	fmt.Println("  -continue        Continue the most recent session")
	fmt.Println("  -yolo, --yolo, --dangerously-skip-permissions")
	fmt.Println("                   Auto-approve all permission prompts")
	fmt.Println("  --permission-mode <auto|off>")
	fmt.Println("                   Set permission mode (default: auto)")
	fmt.Println()
	fmt.Println("Run 'ocode <command> -h' for command-specific help.")
}
