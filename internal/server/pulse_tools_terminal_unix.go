//go:build !windows

package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/termtabs"
)

// pulseTerminalNewTool starts a new local pty shell in a project and registers
// it so terminal_read/screen and the shared tab list can reach it. With a host
// the shell is started on that remote machine instead (pulseRemoteTerminalNew).
//
// Unix-only: the pty bridge is built on github.com/creack/pty, so the method is
// split out of pulse_tools_write.go (which must compile on Windows too). The
// Windows stub lives in pulse_tools_terminal_windows.go.
func (h *Handler) pulseTerminalNewTool(raw json.RawMessage) (string, error) {
	var args struct {
		ProjectPath string `json:"project_path"`
		Host        string `json:"host"`
		Title       string `json:"title"`
	}
	if err := decodePulseArgs(raw, &args); err != nil {
		return "", err
	}
	if host := strings.TrimSpace(args.Host); host != "" {
		return h.pulseRemoteTerminalNew(host, args.ProjectPath, args.Title)
	}
	projectRoot := strings.TrimSpace(args.ProjectPath)
	if projectRoot == "" {
		return "", errors.New("project_path is required")
	}
	if h.projectHostFor(projectRoot) != "" {
		return "", fmt.Errorf("project_path %q belongs to a remote project; pass its host", projectRoot)
	}
	expanded, err := projects.ExpandHome(projectRoot)
	if err != nil {
		return "", fmt.Errorf("expand project path %q: %w", projectRoot, err)
	}
	projectRoot = expanded
	if projectRoot == "" {
		return "", errors.New("project_path is required")
	}
	allowed := false
	for _, root := range h.allowedProjectRoots() {
		if projectRoot == root {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("project %q is not a registered project root", projectRoot)
	}

	// Read the access gate and the shell override under h.mu, exactly as
	// HandleTerminalWS does: h.cfg is written under h.mu by the settings
	// handlers, so an unlocked read would race them.
	h.mu.Lock()
	available := h.terminalAccessAllowed()
	shellOverride := ""
	if h.cfg != nil {
		shellOverride = h.cfg.Ocode.TerminalShell
	}
	h.mu.Unlock()
	if !available {
		return "", errors.New("terminals are disabled on this server (they require authentication or a loopback bind)")
	}

	id := fmt.Sprintf("pulse-term-%d", time.Now().UnixNano())
	// Reserve BEFORE spawning. completeCreate only publishes a session whose id
	// was reserved (terminal_session_table.go), so without this the shell is
	// started and then immediately killed on every call. The deferred abandon
	// releases the reservation if the spawn fails between reserve and publish,
	// mirroring the WebSocket path in handler_terminal.go.
	existing, created, _ := h.terminalSessions.reserveForProject(id, projectRoot)
	if !created {
		if existing != nil {
			// Only reachable if the per-call id was somehow already live;
			// report it idempotently rather than erroring.
			return marshalPulseString(map[string]any{"terminal_id": id, "project_path": projectRoot})
		}
		return "", errors.New("terminal table is sealed for shutdown")
	}
	completedCreate := false
	defer func() {
		if !completedCreate {
			h.terminalSessions.abandon(id)
		}
	}()

	cmd := terminalShellCommand(config.ResolveTerminalShell(shellOverride))
	cmd.Dir = projectRoot
	sess, err := h.startTerminalShell(id, projectRoot, cmd)
	if err != nil {
		return "", fmt.Errorf("start terminal in %s: %w", projectRoot, err)
	}
	if !h.terminalSessions.completeCreate(id, sess) {
		sess.kill()
		return "", errors.New("terminal table is sealed for shutdown")
	}
	completedCreate = true

	title := strings.TrimSpace(args.Title)
	if title == "" {
		title = "Terminal"
	}
	if h.termTabsStore != nil {
		// AddTerminal, not Get then Set: the append must not overwrite a tab another
		// client or server process added between the two.
		if err := h.termTabsStore.AddTerminal(projectRoot, termtabs.Terminal{ID: id, Title: title}); err != nil {
			log.Printf("pulse terminal_new: persist tab list: %v", err)
		}
		h.bus.Publish("terminal_tabs_changed", "", "", nil)
	}
	return marshalPulseString(map[string]any{"terminal_id": id, "project_path": projectRoot})
}
