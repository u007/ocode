//go:build darwin

package computer

import (
	"context"
	"embed"
	"fmt"
	"os"
)

//go:embed cgevent_darwin.js
var cgeventJS embed.FS

// darwinDisplaySize runs the embedded JXA script with
// the "screen" op to get display width and height in points.
func darwinDisplaySize(ctx context.Context, r commandRunner) (int, int, error) {
	out, err := r.run(ctx, "osascript", darwinArgs("screen")...)
	if err != nil {
		return 0, 0, err
	}
	var w, h int
	_, err = fmt.Sscanf(out, "%d %d", &w, &h)
	if err != nil {
		return 0, 0, fmt.Errorf("parse screen size: %w", err)
	}
	return w, h, nil
}

func init() {
	// Write the embedded JXA script to a temp file at startup.
	f, err := os.CreateTemp("", "cgevent-*.js")
	if err != nil {
		return
	}
	name := f.Name()
	b, _ := cgeventJS.ReadFile("cgevent_darwin.js")
	f.Write(b)
	f.Close()
	scriptPath = name
}
