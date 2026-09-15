package remotecli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/remote"
)

// storePortMapHook implements remote.PortMapHook against a persisted
// projects.Store entry, for the /port command in one connected `ocode
// remote <host> --web` session. Restricted to KindSSH targets by the caller
// (remotecli.Run) — WSL never needs extra forwards (Windows already shares
// WSL2's localhost natively).
type storePortMapHook struct {
	store *projects.Store
	ref   projects.ProjectRef
}

func (h *storePortMapHook) Load() ([]remote.ProjectPortMap, error) {
	maps, err := h.store.PortMaps(h.ref)
	if err != nil {
		return nil, err
	}
	out := make([]remote.ProjectPortMap, len(maps))
	for i, m := range maps {
		out[i] = remote.ProjectPortMap{RemotePort: m.RemotePort, LocalPort: m.LocalPort, Enabled: m.Enabled}
	}
	return out, nil
}

// Handle parses one stdin line typed at the --web foreground prompt. Only
// lines starting with "/port" are recognized; everything else returns
// ok=false so the caller can show a generic hint.
func (h *storePortMapHook) Handle(fm *remote.ForwardManager, line string) (string, bool) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 || fields[0] != "/port" {
		return "", false
	}
	if len(fields) == 1 {
		fields = append(fields, "status")
	}
	switch fields[1] {
	case "status":
		return h.status(fm), true
	case "add":
		if len(fields) < 3 {
			return "usage: /port add <remotePort>[:<localPort>]", true
		}
		return h.add(fm, fields[2]), true
	case "remove":
		if len(fields) < 3 {
			return "usage: /port remove <remotePort>", true
		}
		return h.remove(fm, fields[2]), true
	case "enable":
		if len(fields) < 3 {
			return "usage: /port enable <remotePort>", true
		}
		return h.setEnabled(fm, fields[2], true), true
	case "disable":
		if len(fields) < 3 {
			return "usage: /port disable <remotePort>", true
		}
		return h.setEnabled(fm, fields[2], false), true
	default:
		return "unknown /port subcommand " + fields[1] + "; try status|add|remove|enable|disable", true
	}
}

func (h *storePortMapHook) status(fm *remote.ForwardManager) string {
	maps, err := h.store.PortMaps(h.ref)
	if err != nil {
		return "port maps: " + err.Error()
	}
	if len(maps) == 0 {
		return "no extra port maps for this project. Add one: /port add <remotePort>[:<localPort>]"
	}
	var b strings.Builder
	b.WriteString("port maps:\n")
	for _, m := range maps {
		state := "disabled"
		if m.Enabled {
			state = "enabled (not connected)"
			if fm.IsLive(m.RemotePort) {
				state = "live"
			}
		}
		fmt.Fprintf(&b, "  remote:%d -> localhost:%d  [%s]\n", m.RemotePort, m.LocalPort, state)
	}
	return strings.TrimRight(b.String(), "\n")
}

// parsePort validates a single TCP port number.
func parsePort(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 || n > 65535 {
		return 0, fmt.Errorf("invalid port %q", s)
	}
	return n, nil
}

// parsePortSpec parses "<remotePort>[:<localPort>]", defaulting localPort to
// remotePort (matching the fixed tunnel's own same-number convention).
func parsePortSpec(spec string) (remotePort, localPort int, err error) {
	parts := strings.SplitN(spec, ":", 2)
	remotePort, err = parsePort(parts[0])
	if err != nil {
		return 0, 0, err
	}
	localPort = remotePort
	if len(parts) == 2 {
		if localPort, err = parsePort(parts[1]); err != nil {
			return 0, 0, err
		}
	}
	return remotePort, localPort, nil
}

func (h *storePortMapHook) add(fm *remote.ForwardManager, spec string) string {
	remotePort, localPort, err := parsePortSpec(spec)
	if err != nil {
		return err.Error()
	}
	if err := h.store.AddPortMap(h.ref, remotePort, localPort); err != nil {
		return "port maps: " + err.Error()
	}
	if err := fm.Start(remote.ProjectPortMap{RemotePort: remotePort, LocalPort: localPort, Enabled: true}); err != nil {
		return "saved, but failed to open now: " + err.Error()
	}
	return fmt.Sprintf("added: localhost:%d -> remote:%d", localPort, remotePort)
}

func (h *storePortMapHook) remove(fm *remote.ForwardManager, arg string) string {
	remotePort, err := parsePort(arg)
	if err != nil {
		return err.Error()
	}
	_ = fm.Stop(remotePort)
	if err := h.store.RemovePortMap(h.ref, remotePort); err != nil {
		return "port maps: " + err.Error()
	}
	return fmt.Sprintf("removed remote port %d", remotePort)
}

func (h *storePortMapHook) setEnabled(fm *remote.ForwardManager, arg string, enabled bool) string {
	remotePort, err := parsePort(arg)
	if err != nil {
		return err.Error()
	}
	if err := h.store.SetPortMapEnabled(h.ref, remotePort, enabled); err != nil {
		return "port maps: " + err.Error()
	}
	if !enabled {
		_ = fm.Stop(remotePort)
		return fmt.Sprintf("disabled remote port %d", remotePort)
	}
	maps, err := h.store.PortMaps(h.ref)
	if err != nil {
		return "port maps: " + err.Error()
	}
	for _, m := range maps {
		if m.RemotePort == remotePort {
			if err := fm.Start(remote.ProjectPortMap{RemotePort: m.RemotePort, LocalPort: m.LocalPort, Enabled: true}); err != nil {
				return "enabled, but failed to open now: " + err.Error()
			}
			break
		}
	}
	return fmt.Sprintf("enabled remote port %d", remotePort)
}
