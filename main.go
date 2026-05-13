package main

import (
	"fmt"
	"os"

	"github.com/jamesmercstudio/ocode/internal/tui"
)

func main() {
	if err := tui.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
