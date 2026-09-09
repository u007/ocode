// Package clitools implements the `/plugin tools` feature: detection and
// installation of common CLI utilities (fd, ripgrep, fzf, eza, bat, GNU grep)
// on the current platform.
//
// Each tool carries per-platform install recipes. Detection uses
// exec.LookPath against the agent's PATH; installation shells out to the
// platform package manager. Output is captured, never inherited — the TUI
// runs in alt-screen (see AGENTS.md, "TUI Output Safety").
package clitools

import (
	"bytes"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Tool describes one installable CLI utility.
type Tool struct {
	// Name is the canonical command name as found on PATH (e.g. "rg").
	Name string
	// Aliases are additional command names checked by Detect (e.g. "fd" is
	// packaged on some distros as "fdfind").
	Aliases []string
	// Description is a short human-readable purpose line.
	Description string
	// Project is the upstream homepage, shown in listings.
	Project string
	// install maps package-manager name to install args (without the
	// manager itself, which the installer prepends).
	install map[string][]string
}

// PackageManager identifies a supported install backend.
type PackageManager string

const (
	PMBrew    PackageManager = "brew"
	PMApt     PackageManager = "apt"
	PMDnf     PackageManager = "dnf"
	PMApk     PackageManager = "apk"
	PMPacman  PackageManager = "pacman"
	PMWinget  PackageManager = "winget"
	PMChoco   PackageManager = "choco"
	PMScoop   PackageManager = "scoop"
	PMCargo   PackageManager = "cargo"
	PMNpm     PackageManager = "npm"
	PMGo      PackageManager = "go"
	PMFreeBSD PackageManager = "pkg"
)

// installOrder is the preferred probe order per platform.
var installOrder = map[string][]PackageManager{
	"darwin":  {PMBrew, PMCargo, PMNpm, PMGo},
	"linux":   {PMApt, PMDnf, PMPacman, PMApk, PMCargo, PMNpm, PMGo},
	"windows": {PMWinget, PMChoco, PMScoop, PMCargo, PMNpm, PMGo},
	"freebsd": {PMFreeBSD, PMCargo, PMNpm, PMGo},
}

// managerCommand returns the command line for the manager's probe/install.
func managerCommand(pm PackageManager, sub string, args []string) (string, []string) {
	switch pm {
	case PMBrew:
		return "brew", append([]string{sub}, args...)
	case PMApt:
		// apt always needs sudo for install; probe is a plain dpkg query.
		if sub == "install" {
			return "sudo", append([]string{"apt-get", "install", "-y"}, args...)
		}
		return "apt-cache", append([]string{"show"}, args...)
	case PMDnf:
		return "dnf", append([]string{sub}, args...)
	case PMPacman:
		if sub == "install" {
			return "sudo", append([]string{"pacman", "-S", "--noconfirm"}, args...)
		}
		return "pacman", append([]string{"-Si"}, args...)
	case PMApk:
		if sub == "install" {
			return "apk", append([]string{"add"}, args...)
		}
		return "apk", append([]string{"info", "-e"}, args...)
	case PMWinget:
		return "winget", append([]string{"install", "-e", "--accept-source-agreements", "--accept-package-agreements"}, args...)
	case PMChoco:
		return "choco", append([]string{"install", "-y"}, args...)
	case PMScoop:
		return "scoop", append([]string{"install"}, args...)
	case PMCargo:
		return "cargo", append([]string{"install", "--locked"}, args...)
	case PMNpm:
		return "npm", append([]string{"install", "-g"}, args...)
	case PMGo:
		return "go", append([]string{"install"}, args...)
	case PMFreeBSD:
		if sub == "install" {
			return "sudo", append([]string{"pkg", "install", "-y"}, args...)
		}
		return "pkg", append([]string{"search", "-q"}, args...)
	}
	return "", nil
}

// availableManagers returns the probe order for the current GOOS, filtered to
// managers actually present on PATH.
func availableManagers() []PackageManager {
	order, ok := installOrder[runtime.GOOS]
	if !ok {
		order = []PackageManager{PMCargo, PMNpm, PMGo}
	}
	var found []PackageManager
	for _, pm := range order {
		bin, _ := managerCommand(pm, "probe", nil)
		if bin == "" {
			continue
		}
		if _, err := exec.LookPath(bin); err == nil {
			found = append(found, pm)
		}
	}
	return found
}

// Manager returns the first available package manager for the platform, or
// empty string when none is found.
func Manager() PackageManager {
	if m := availableManagers(); len(m) > 0 {
		return m[0]
	}
	return ""
}

// Detect reports whether the tool is installed on PATH. It checks Name and
// every Alias; returns the resolved command name ("" when absent).
func (t Tool) Detect() (string, bool) {
	candidates := append([]string{t.Name}, t.Aliases...)
	for _, c := range candidates {
		if _, err := exec.LookPath(c); err == nil {
			return c, true
		}
	}
	return "", false
}

// DetectAll probes every catalog tool at once and returns per-tool status.
func DetectAll() []ToolStatus {
	tools := Catalog()
	out := make([]ToolStatus, 0, len(tools))
	for _, t := range tools {
		cmd, ok := t.Detect()
		out = append(out, ToolStatus{
			Tool:    t,
			Found:   ok,
			Command: cmd,
		})
	}
	return out
}

// ToolStatus is the result of probing one tool.
type ToolStatus struct {
	Tool    Tool
	Found   bool
	Command string // resolved binary name (Name or an alias)
}

// installRecipes returns the install args for a tool under a manager, or nil
// when the manager cannot provide this tool.
func (t Tool) installRecipes(pm PackageManager) []string {
	if t.install == nil {
		return nil
	}
	return t.install[string(pm)]
}

// InstallResult captures the outcome of one install attempt.
type InstallResult struct {
	Tool    string
	Manager PackageManager
	Ok      bool
	Output  string // captured stdout+stderr (tail)
	Err     error
}

// installTimeout bounds a single package-manager invocation. Some managers
// (apt update, winget) are slow but not unbounded.
const installTimeout = 10 * time.Minute

// Install shells out to the platform package manager. It tries each available
// manager in preferred order until one succeeds. Output is captured, never
// inherited (TUI alt-screen safety).
func Install(toolName string) InstallResult {
	tool, ok := FindTool(toolName)
	if !ok {
		return InstallResult{Tool: toolName, Err: fmt.Errorf("unknown tool %q", toolName)}
	}

	managers := availableManagers()
	if len(managers) == 0 {
		return InstallResult{Tool: tool.Name, Err: fmt.Errorf("no supported package manager found on PATH for %s (tried %v)", runtime.GOOS, orderNames(installOrder[runtime.GOOS]))}
	}

	var combined bytes.Buffer
	for _, pm := range managers {
		args := tool.installRecipes(pm)
		if args == nil {
			continue
		}
		bin, argv := managerCommand(pm, "install", args)
		if bin == "" {
			continue
		}
		cmd := exec.Command(bin, argv...)
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		done := make(chan error, 1)
		go func() { done <- cmd.Run() }()
		select {
		case err := <-done:
			combined.WriteString(fmt.Sprintf("$ %s %s\n", bin, strings.Join(argv, " ")))
			combined.Write(tail(buf.Bytes(), 8192))
			if err == nil {
				return InstallResult{Tool: tool.Name, Manager: pm, Ok: true, Output: combined.String()}
			}
			combined.WriteString(fmt.Sprintf("exit: %v\n", err))
		case <-time.After(installTimeout):
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			return InstallResult{Tool: tool.Name, Manager: pm, Err: fmt.Errorf("%s install timed out after %s", bin, installTimeout), Output: combined.String()}
		}
	}
	return InstallResult{Tool: tool.Name, Err: fmt.Errorf("no available manager could install %s", tool.Name), Output: combined.String()}
}

// FindTool looks up a tool by canonical name or alias.
func FindTool(name string) (Tool, bool) {
	for _, t := range Catalog() {
		if t.Name == name {
			return t, true
		}
		for _, a := range t.Aliases {
			if a == name {
				return t, true
			}
		}
	}
	return Tool{}, false
}

func orderNames(pms []PackageManager) []string {
	out := make([]string, 0, len(pms))
	for _, pm := range pms {
		out = append(out, string(pm))
	}
	return out
}

// tail keeps at most n bytes, prefixing an elision marker when trimmed.
func tail(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return append([]byte("… (truncated)\n"), b[len(b)-n:]...)
}
