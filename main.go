package main

import (
	"fmt"
	"os"

	"github.com/jamesmercstudio/ocode/internal/mcpcli"
	"github.com/jamesmercstudio/ocode/internal/tui"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		if err := mcpcli.Run(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	sessionID := ""
	cont := false
	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "-session":
			if i+1 < len(os.Args) {
				sessionID = os.Args[i+1]
				i++
			}
		case "-continue":
			cont = true
		}
	}

	if err := tui.Run(sessionID, cont); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
