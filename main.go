package main

import (
	"fmt"
	"os"

	"github.com/jamesmercstudio/ocode/internal/acp"
	"github.com/jamesmercstudio/ocode/internal/agent"
	"github.com/jamesmercstudio/ocode/internal/mcpcli"
	"github.com/jamesmercstudio/ocode/internal/models"
	"github.com/jamesmercstudio/ocode/internal/runcli"
	"github.com/jamesmercstudio/ocode/internal/server"
	"github.com/jamesmercstudio/ocode/internal/tui"
	"github.com/jamesmercstudio/ocode/internal/version"
)

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
			if err := server.Run(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "web":
			args := append([]string{"--open"}, os.Args[2:]...)
			if err := server.Run(args); err != nil {
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
		}
	}

	agent.PreloadRegistry()

	sessionID := ""
	cont := false
	yolo := false
	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "-session":
			if i+1 < len(os.Args) {
				sessionID = os.Args[i+1]
				i++
			}
		case "-continue":
			cont = true
		case "-yolo", "--yolo":
			yolo = true
		}
	}

	if err := tui.Run(sessionID, cont, yolo); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
