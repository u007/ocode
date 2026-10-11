package computer

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"

	"github.com/u007/ocode/internal/tool"
)

// Window listings cross the process boundary as one tab-separated line per
// window: id, app, title, x, y, w, h, minimized(0|1), focused(0|1). Helpers
// must replace tabs and newlines in app and title with spaces.
const windowListFields = 9

var (
	darwinWindowID  = regexp.MustCompile(`^[0-9]+:[0-9]+$`)
	decimalWindowID = regexp.MustCompile(`^[0-9]+$`)
	x11WindowID     = regexp.MustCompile(`^0x[0-9a-fA-F]+$`)
)

// checkWindowID rejects ids that are not in the platform's id format, so a
// model-supplied string never reaches a helper as anything but an id.
func checkWindowID(re *regexp.Regexp, id string) error {
	if !re.MatchString(id) {
		return fmt.Errorf("computer window: invalid window id %q (use an id from `window list`)", id)
	}
	return nil
}

func checkWindowState(state string) error {
	switch state {
	case tool.WindowMinimize, tool.WindowRestore, tool.WindowMaximize, tool.WindowClose:
		return nil
	}
	return fmt.Errorf("computer window: unknown state %q", state)
}

// parseWindowList parses helper output in the format above.
func parseWindowList(out string) ([]tool.WindowInfo, error) {
	var wins []tool.WindowInfo
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#skipped\t") {
			// The driver script could not read this window; drop it, keep the rest.
			log.Printf("[COMPUTER] window list: skipped unreadable window: %s", strings.TrimPrefix(line, "#skipped\t"))
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != windowListFields {
			return nil, fmt.Errorf("computer window: malformed list line %q", line)
		}
		var n [4]int
		for i := range n {
			v, err := strconv.Atoi(f[3+i])
			if err != nil {
				return nil, fmt.Errorf("computer window: bad bounds in %q: %w", line, err)
			}
			n[i] = v
		}
		wins = append(wins, tool.WindowInfo{
			ID: f[0], App: f[1], Title: f[2],
			X: n[0], Y: n[1], W: n[2], H: n[3],
			Minimized: f[7] == "1", Focused: f[8] == "1",
		})
	}
	return wins, nil
}

// cleanField makes a value safe for the tab/newline-delimited list format.
func cleanField(s string) string {
	return strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(s)
}
