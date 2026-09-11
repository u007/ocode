// Package tailscale shares the tailscale serve/funnel lifecycle helpers used
// by both the TUI /rc path and the headless/desktop server share endpoint.
// Keeping the CLI parsing in one place avoids drift between the two callers:
// the serve/funnel output format is parsed identically, --set-path mounts are
// sanitized identically, and cleanup removes only the caller's own mount.
package tailscale

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

// SanitizePath returns a tailscale-safe --set-path component derived from an
// id (session ID, or "desktop" for the server share). It strips characters
// tailscale treats specially ("/" as a path separator, "." can break path
// normalization) and falls back to "ocode" when the input is empty, so
// sessions never collapse onto the root mount.
func SanitizePath(id string) string {
	const fallback = "ocode"
	cleaned := make([]rune, 0, len(id))
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-' || r == '_':
			cleaned = append(cleaned, r)
		}
	}
	if len(cleaned) == 0 {
		cleaned = []rune(fallback)
	}
	return "/" + string(cleaned)
}

// URLWithPathPrefix mounts the serve/funnel base URL at exactly pathPrefix.
// Tailscale output can carry a sibling session's existing --set-path entry in
// the parsed URL line; appending would yield a doubled, unroutable prefix
// that tailscale longest-prefix-matches to the stale route. Replace instead.
func URLWithPathPrefix(baseURL, pathPrefix string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return ""
	}
	if pathPrefix == "" {
		return baseURL
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return baseURL + pathPrefix
	}
	u.Path = pathPrefix
	return u.String()
}

// BuildSessionURL appends /session/<id>?token=<token> to a tailscale base URL.
func BuildSessionURL(baseURL, sessionID, token string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return ""
	}
	return fmt.Sprintf("%s/session/%s?token=%s", baseURL, sessionID, token)
}

// DNSName returns the tailnet DNS base URL (e.g. "https://host.ts.net")
// from `tailscale status --json`, or "" on failure.
func DNSName(tailscalePath string) string {
	statusCmd := exec.Command(tailscalePath, "status", "--json")
	var statusOut bytes.Buffer
	statusCmd.Stdout = &statusOut
	if err := statusCmd.Run(); err != nil {
		return ""
	}
	var status struct {
		SELF struct {
			DNSName string `json:"DNSName"`
		} `json:"Self"`
	}
	if json.Unmarshal(statusOut.Bytes(), &status) != nil {
		return ""
	}
	dnsName := strings.TrimSuffix(status.SELF.DNSName, ".")
	if dnsName != "" {
		return fmt.Sprintf("https://%s", dnsName)
	}
	return ""
}

// Running reports whether the tailscale CLI exists and the daemon answers
// `tailscale status`.
func Running() (string, bool) {
	p, err := exec.LookPath("tailscale")
	if err != nil {
		return "", false
	}
	if err := exec.Command(p, "status").Run(); err != nil {
		return "", false
	}
	return p, true
}

// Expose tries `tailscale <cmd> --bg [--set-path] <target>` (cmd is "funnel"
// or "serve") and returns the advertised URL plus the background process.
// The process must be killed when no longer needed; the --set-path mount must
// additionally be removed via RemoveSetPath because --bg detaches the config.
func Expose(tailscalePath, cmd, target, pathPrefix string, wait func(cmd *exec.Cmd) error) (string, *exec.Cmd, string) {
	args := []string{cmd, "--bg"}
	if pathPrefix != "" {
		args = append(args, "--set-path", pathPrefix)
	}
	args = append(args, target)
	serveCmd := exec.Command(tailscalePath, args...)
	var out bytes.Buffer
	serveCmd.Stdout = &out
	serveCmd.Stderr = &out

	if err := serveCmd.Start(); err != nil {
		log.Printf("tailscale %s failed to start: %v", cmd, err)
		return "", nil, ""
	}

	if wait != nil {
		done := make(chan error, 1)
		go func() { done <- wait(serveCmd) }()
		select {
		case <-time.After(2 * time.Second):
		case <-done:
		}
	} else {
		time.Sleep(2 * time.Second)
	}

	output := out.String()
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			return line, serveCmd, ""
		}
	}

	// Parse the one-time enable hint when funnel/serve isn't enabled yet.
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "https://") || strings.HasPrefix(line, "http://") {
			return "", nil, line
		}
	}
	return "", nil, ""
}

// StartExpose makes port reachable via tailscale funnel (public) then serve
// (tailnet-only), mounted at the sanitized id path so instances coexist.
// Returns the public URL including the path prefix, the background process
// for cleanup, and a one-time setup hint when the tailnet needs enabling.
func StartExpose(port int, id string) (url string, proc *exec.Cmd, setupHint string) {
	tailscalePath, ok := Running()
	if !ok {
		return "", nil, ""
	}
	target := fmt.Sprintf("localhost:%d", port)
	pathPrefix := SanitizePath(id)
	wait := func(cmd *exec.Cmd) error { return cmd.Wait() }

	if u, p, hint := Expose(tailscalePath, "funnel", target, pathPrefix, wait); u != "" {
		return URLWithPathPrefix(u, pathPrefix), p, hint
	}
	if u, p, hint := Expose(tailscalePath, "serve", target, pathPrefix, wait); u != "" {
		return URLWithPathPrefix(u, pathPrefix), p, hint
	}
	return URLWithPathPrefix(DNSName(tailscalePath), pathPrefix), nil, ""
}

// RemoveSetPath removes a single --set-path mount (best-effort). Both funnel
// and serve are tried because the caller doesn't track which one succeeded.
func RemoveSetPath(pathPrefix string) {
	if pathPrefix == "" {
		return
	}
	tailscalePath, err := exec.LookPath("tailscale")
	if err != nil {
		return
	}
	for _, cmd := range []string{"funnel", "serve"} {
		c := exec.Command(tailscalePath, cmd, "--set-path", pathPrefix, "off")
		var out bytes.Buffer
		c.Stdout = &out
		c.Stderr = &out
		if err := c.Run(); err != nil {
			log.Printf("tailscale %s --set-path %s off: %v\n  output: %s", cmd, pathPrefix, err, strings.TrimRight(out.String(), "\n"))
		}
	}
}
