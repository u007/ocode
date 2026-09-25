package desktop

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

// executablePathProbeTimeout bounds the login-shell PATH probe. A user's shell
// configuration is executable code and can block (for example while waiting
// for a network-mounted home directory), so desktop startup must not wait
// indefinitely for it.
const executablePathProbeTimeout = 2 * time.Second

const (
	executablePathProbePrefix = "\x1eOCODE_PATH\x1f"
	executablePathProbeSuffix = "\x1e/OCODE_PATH\x1f"
	executablePathProbeScript = `printf '\036OCODE_PATH\037%s\036/OCODE_PATH\037\n' "$PATH"`
)

// executablePathProbe is a seam for tests. The production implementation runs
// the selected login shell and returns its raw stdout.
type executablePathProbe func(context.Context, string, ...string) (string, error)

// executablePathOptions contains the small set of process inputs needed to
// compute an effective executable PATH. Keeping these injectable lets tests
// exercise all platform branches without launching a shell or mutating the
// test process environment.
type executablePathOptions struct {
	goos    string
	pathSep string
	home    string
	current string
	getenv  func(string) string
	exists  func(string) bool
	join    func(...string) string
	probe   executablePathProbe
}

// EnsureExecutablePath hydrates the desktop process PATH before the embedded
// server starts. Finder/Dock and other GUI launchers commonly provide only
// the system PATH, which omits user toolchains such as ~/go/bin, ~/.cargo/bin,
// Homebrew, and version managers. LSP discovery ultimately uses
// exec.LookPath, so hydrating this one process environment fixes discovery for
// the server, the LSP manager, and every other server-side child process.
//
// The inherited PATH is never discarded: shell-provided and conventional user
// directories are appended to it, preserving application-specific precedence.
func EnsureExecutablePath() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Printf("desktop: resolve home for executable PATH: %v", err)
		home = ""
	}
	current := os.Getenv("PATH")
	opts := executablePathOptions{
		goos:    runtime.GOOS,
		pathSep: string(os.PathListSeparator),
		home:    home,
		current: current,
		getenv:  os.Getenv,
		exists:  executableDirectoryExists,
		join:    filepath.Join,
		probe:   probeLoginShellPath,
	}
	effective, changed := ensureExecutablePath(opts)
	if !changed {
		return false
	}
	if err := os.Setenv("PATH", effective); err != nil {
		log.Printf("desktop: set hydrated executable PATH: %v", err)
		return false
	}
	log.Printf("desktop: hydrated executable PATH with user toolchain directories")
	return true
}

// ensureExecutablePath computes the effective PATH without mutating the
// process. It is separate from EnsureExecutablePath so platform behavior and
// shell-probe fallback can be tested deterministically.
func ensureExecutablePath(opts executablePathOptions) (string, bool) {
	opts = normalizeExecutablePathOptions(opts)
	currentEntries := splitExecutablePath(opts.current, opts.pathSep)
	entries := make([]string, 0, len(currentEntries))
	seen := make(map[string]struct{}, len(currentEntries))
	add := func(path string) {
		if path == "" {
			return
		}
		key := executablePathKey(opts.goos, path)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		entries = append(entries, path)
	}
	for _, entry := range currentEntries {
		add(entry)
	}

	if opts.goos != "windows" {
		if shell := executableLoginShell(opts.getenv); shell != "" {
			ctx, cancel := context.WithTimeout(context.Background(), executablePathProbeTimeout)
			probed, err := opts.probe(ctx, shell, "-l", "-c", executablePathProbeScript)
			cancel()
			if err != nil {
				log.Printf("desktop: probe executable PATH with %s -l -c: %v", shell, err)
			} else if extracted := extractExecutablePath(probed); extracted != "" {
				for _, entry := range splitExecutablePath(extracted, opts.pathSep) {
					add(entry)
				}
			} else {
				log.Printf("desktop: executable PATH probe with %s returned no PATH", shell)
			}
		}
	}

	for _, dir := range executableToolchainDirs(opts) {
		if opts.exists(dir) {
			add(dir)
		}
	}

	effective := strings.Join(entries, opts.pathSep)
	return effective, effective != opts.current
}

func normalizeExecutablePathOptions(opts executablePathOptions) executablePathOptions {
	if opts.goos == "" {
		opts.goos = runtime.GOOS
	}
	if opts.pathSep == "" {
		opts.pathSep = string(os.PathListSeparator)
	}
	if opts.getenv == nil {
		opts.getenv = func(string) string { return "" }
	}
	if opts.current == "" {
		opts.current = opts.getenv("PATH")
	}
	if opts.home == "" {
		opts.home = firstNonEmpty(opts.getenv("HOME"), opts.getenv("USERPROFILE"))
	}
	if opts.exists == nil {
		opts.exists = executableDirectoryExists
	}
	if opts.join == nil {
		opts.join = filepath.Join
	}
	if opts.probe == nil {
		opts.probe = probeLoginShellPath
	}
	return opts
}

func splitExecutablePath(path, separator string) []string {
	if path == "" {
		return nil
	}
	return strings.Split(path, separator)
}

func executablePathKey(goos, path string) string {
	if goos == "windows" {
		return strings.ToLower(path)
	}
	return path
}

func executableDirectoryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func executableLoginShell(getenv func(string) string) string {
	for _, candidate := range []string{getenv("SHELL"), "/bin/zsh", "/bin/bash", "/bin/sh"} {
		shell := loginShell(candidate)
		if shell == "" {
			continue
		}
		if info, err := os.Stat(shell); err == nil && !info.IsDir() {
			return shell
		}
	}
	return ""
}

func loginShell(shell string) string {
	base := strings.ToLower(filepath.Base(shell))
	base = strings.TrimSuffix(base, ".exe")
	switch base {
	case "zsh", "bash", "sh":
		return shell
	default:
		return ""
	}
}

func probeLoginShellPath(ctx context.Context, shell string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, shell, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return "", fmt.Errorf("%w: %s", err, detail)
		}
		return "", err
	}
	return string(out), nil
}

func extractExecutablePath(raw string) string {
	start := strings.Index(raw, executablePathProbePrefix)
	if start < 0 {
		return strings.TrimSpace(raw)
	}
	start += len(executablePathProbePrefix)
	end := strings.Index(raw[start:], executablePathProbeSuffix)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(raw[start : start+end])
}

// executableToolchainDirs returns existing-directory candidates that are
// useful across common package managers. Login-shell probing remains the
// primary mechanism because it also captures Homebrew, nvm, conda, and custom
// setup; these candidates cover GUI launches where those tools are installed
// in conventional locations but the shell profile is unavailable or slow.
func executableToolchainDirs(opts executablePathOptions) []string {
	home := opts.home
	if home == "" {
		return nil
	}

	dirs := make([]string, 0, 16)
	add := func(path string) {
		if path != "" {
			dirs = append(dirs, path)
		}
	}
	addEnvPath := func(value string) {
		for _, path := range splitExecutablePath(value, opts.pathSep) {
			if path != "" {
				add(path)
			}
		}
	}
	addEnvPath(opts.getenv("GOBIN"))
	if goPath := opts.getenv("GOPATH"); goPath != "" {
		for _, root := range splitExecutablePath(goPath, opts.pathSep) {
			add(opts.join(root, "bin"))
		}
	}
	addEnvPath(opts.getenv("PNPM_HOME"))

	if opts.goos == "windows" {
		profile := firstNonEmpty(opts.getenv("USERPROFILE"), home)
		add(opts.join(profile, "go", "bin"))
		add(opts.join(profile, ".cargo", "bin"))
		add(opts.join(profile, ".local", "bin"))
		add(opts.join(profile, "bin"))
		if appData := opts.getenv("APPDATA"); appData != "" {
			add(opts.join(appData, "npm"))
		}
		if localAppData := opts.getenv("LOCALAPPDATA"); localAppData != "" {
			add(opts.join(localAppData, "pnpm"))
			add(opts.join(localAppData, "Microsoft", "WindowsApps"))
		}
		if scoop := opts.getenv("SCOOP"); scoop != "" {
			add(opts.join(scoop, "shims"))
		}
		if choco := opts.getenv("ChocolateyInstall"); choco != "" {
			add(opts.join(choco, "bin"))
		}
		return dirs
	}

	add(opts.join(home, "go", "bin"))
	add(opts.join(home, ".cargo", "bin"))
	add(opts.join(home, ".local", "bin"))
	add(opts.join(home, "bin"))
	if opts.goos == "darwin" {
		add(opts.join(home, "Library", "pnpm"))
		add("/opt/homebrew/bin")
		add("/usr/local/bin")
	} else {
		add(opts.join(home, ".local", "share", "pnpm"))
		add("/usr/local/bin")
		add("/snap/bin")
	}

	if nvmBin := opts.getenv("NVM_BIN"); nvmBin != "" {
		add(nvmBin)
	}
	nvmRoot := firstNonEmpty(opts.getenv("NVM_DIR"), opts.join(home, ".nvm"))
	nvmPattern := opts.join(nvmRoot, "versions", "node", "*", "bin")
	matches, globErr := filepath.Glob(nvmPattern)
	if globErr != nil {
		// intentionally not logged: NVM_DIR is user-controlled and a malformed
		// pattern is a harmless absence of optional version-manager entries.
		return dirs
	}
	// Prefer the shell-selected NVM_BIN. If it is unavailable, add only the
	// newest discovered version rather than polluting PATH with every Node
	// version installed on the machine.
	slices.SortFunc(matches, func(a, b string) int { return cmp.Compare(b, a) })
	if len(matches) > 0 {
		add(matches[0])
	}
	return dirs
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
