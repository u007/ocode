package desktop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/remote"
)

// WorkspaceMode indicates where an ocode workspace runs.
type WorkspaceMode int

const (
	// WorkspaceLocal: ocode server runs on the local machine.
	WorkspaceLocal WorkspaceMode = iota
	// WorkspaceRemoteSSH: ocode server runs on a remote SSH host;
	// the desktop app is a thin UI/control plane connected through
	// an SSH tunnel.
	WorkspaceRemoteSSH
)

// Workspace represents an ocode workspace session, either local
// or remote-SSH. Local workspaces are handled by StartServer
// directly; remote workspaces use RemoteWorkspace + RemoteProxy.
type Workspace struct {
	Mode      WorkspaceMode
	ID        string // workspace ID (UUID for remote; path-based for local)
	Target    remote.Target
	RemotePath string
	Remote    *remote.RemoteWorkspace
	Proxy     *RemoteProxy
}

// WorkspaceConfig is the saved configuration for a remote
// SSH workspace (persisted on disk so the next desktop
// launch can auto-connect).
type WorkspaceConfig struct {
	Mode       WorkspaceMode `json:"mode"`
	TargetHost string        `json:"targetHost"`
	TargetPort int           `json:"targetPort,omitempty"`
	RemotePath string        `json:"remotePath"`
}

// SaveWorkspaceConfig persists the workspace config to disk
// so the next desktop launch auto-connects.
func SaveWorkspaceConfig(cfg WorkspaceConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	dir, err := paths.OcodeGlobalDataDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "workspace.json"), data, 0o600)
}

// LoadWorkspaceConfig returns the saved workspace config,
// or an error if none exists.
func LoadWorkspaceConfig() (WorkspaceConfig, error) {
	var cfg WorkspaceConfig
	dir, err := paths.OcodeGlobalDataDir()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "workspace.json"))
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse workspace config: %w", err)
	}
	return cfg, nil
}

// OpenRemoteWorkspace creates a remote SSH workspace session:
// connects to the remote host, provisions/starts the server,
// and establishes the SSH tunnel. The reverse proxy is created
// separately by startRemoteServer (which needs the local token).
func OpenRemoteWorkspace(target remote.Target, path string) (*Workspace, error) {
	rw, err := remote.NewRemoteWorkspace(target, path, "", nil)
	if err != nil {
		return nil, fmt.Errorf("create remote workspace: %w", err)
	}
	if err := rw.Connect(); err != nil {
		return nil, fmt.Errorf("connect remote workspace: %w", err)
	}
	return &Workspace{
		Mode:       WorkspaceRemoteSSH,
		ID:         rw.WorkspaceID,
		Target:     target,
		RemotePath: path,
		Remote:     rw,
	}, nil
}

// Close disconnects the workspace: stops the proxy (no-op for
// ReverseProxy) and tears down the SSH tunnel. The remote server
// stays running for resume on next connect.
func (w *Workspace) Close() error {
	if w.Proxy != nil {
		// RemoteProxy has no resources to clean up
		// (ReverseProxy doesn't hold connections long-term).
		w.Proxy = nil
	}
	if w.Remote != nil {
		if err := w.Remote.Disconnect(); err != nil {
			return err
		}
		w.Remote = nil
	}
	return nil
}

// IsLocal returns true if this workspace runs on the local machine.
func (w *Workspace) IsLocal() bool {
	return w.Mode == WorkspaceLocal
}

// APIURL returns the local URL the webview should use to reach
// the ocode API. For local workspaces this is handled by
// StartServer. For remote workspaces it's the tunnel URL.
func (w *Workspace) APIURL() string {
	if w.Mode == WorkspaceRemoteSSH && w.Remote != nil {
		return w.Remote.APIURL()
	}
	return ""
}
