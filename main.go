package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"

	"github.com/jamesmercstudio/ocode/internal/acp"
	"github.com/jamesmercstudio/ocode/internal/agent"
	"github.com/jamesmercstudio/ocode/internal/mcpcli"
	"github.com/jamesmercstudio/ocode/internal/models"
	"github.com/jamesmercstudio/ocode/internal/runcli"
	"github.com/jamesmercstudio/ocode/internal/server"
	"github.com/jamesmercstudio/ocode/internal/skill"
	"github.com/jamesmercstudio/ocode/internal/tui"
	"github.com/jamesmercstudio/ocode/internal/version"
)

//go:embed all:web/dist
var webAssets embed.FS

//go:embed all:skills
var bundledSkills embed.FS

func webFS() fs.FS {
	f, err := fs.Sub(webAssets, "web/dist")
	if err != nil {
		return nil
	}
	return f
}

// bundledSkillsFS exposes the embedded skills/ tree to the skill package
// as a plain fs.FS rooted at the repo root (the natural embed shape).
// The installer descends into "skills/" itself; this keeps the embed
// path explicit in main.go and matches the way the real binary
// discovers its skills.
func bundledSkillsFS() fs.FS {
	return bundledSkills
}

func main() {
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
			if err := server.Run(os.Args[2:], webFS()); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "web":
			args := append([]string{"--open"}, os.Args[2:]...)
			if err := server.Run(args, webFS()); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "acp":
			if err := acp.Run(); err != nil {
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
			if fsys := bundledSkillsFS(); fsys != nil {
				skill.SetBundledFS(fsys)
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
	if fsys := bundledSkillsFS(); fsys != nil {
		skill.SetBundledFS(fsys)
	}

	opts := tui.RunOptions{}
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
