package codex

import (
	"regexp"
	"strconv"
	"strings"
)

var allowedCodexModels = map[string]bool{
	"gpt-5.5":             true,
	"gpt-5.4":             true,
	"gpt-5.4-mini":        true,
	"gpt-5.3-codex":       true,
	"gpt-5.3-codex-spark": true,
	"gpt-5.2":             true,
}

// versionRE matches gpt-X.Y where X and Y are single-digit integers.
// Multi-digit minor versions (e.g. "5.40") are rejected to avoid
// confusion with float comparison ("5.40" == 5.4).
var versionRE = regexp.MustCompile(`^gpt-(\d+)\.(\d)$`)

// isAllowed returns true if the model is in the explicit allowlist or
// passes the semantic filter (major.minor > 5.4).
func isAllowed(modelID string) bool {
	// Strip any suffixes like provider prefix
	m := modelID
	if idx := strings.LastIndex(m, "/"); idx >= 0 {
		m = m[idx+1:]
	}

	if allowedCodexModels[m] {
		return true
	}

	match := versionRE.FindStringSubmatch(m)
	if match == nil {
		return false
	}
	major, err1 := strconv.Atoi(match[1])
	minor, err2 := strconv.Atoi(match[2])
	if err1 != nil || err2 != nil {
		return false
	}
	// major > 5, or major == 5 and minor > 4
	return major > 5 || (major == 5 && minor > 4)
}
