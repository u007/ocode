package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/jamesmercstudio/ocode/internal/tui"
)

func main() {
	sessionID := flag.String("session", "", "Session ID to continue")
	cont := flag.Bool("continue", false, "Continue the last session")
	flag.Parse()

	if err := tui.Run(*sessionID, *cont); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
