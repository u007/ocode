package cdp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"runtime"

	"github.com/u007/ocode/internal/tool"
)

// ErrChromeNotFound is returned when no Chrome binary can be located.
var ErrChromeNotFound = errors.New("chrome not found — set browser.chrome_path")

// ErrUnsupportedPlatform is returned on Windows where Chrome mode is not supported.
var ErrUnsupportedPlatform = errors.New("Chrome mode is not supported on Windows yet")

// injectable for tests
var chromeGOOS = runtime.GOOS

var chromeStat = func(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

var chromeLookPath = exec.LookPath

var macOSCandidates = []string{
	"/Applications/Chromium.app/Contents/MacOS/Chromium",
	"/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
	"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
	"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
	"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
}

var linuxCandidates = []string{
	"chromium",
	"chromium-browser",
	"google-chrome",
	"google-chrome-stable",
}

// FindChrome locates the Chrome binary.
// Order: configured path → OCODE_CHROME_PATH env → platform defaults (non-branded preferred).
// Non-branded Chromium/Canary/Edge/Brave are prioritized over branded Google Chrome,
// which from v137 ignores --load-extension and breaks HTR extension preload.
// Canary is included in the non-branded group because the source (launch.go:100)
// confirms Canary honors --load-extension, unlike branded Chrome.
// Explicit chrome_path or OCODE_CHROME_PATH always overrides discovery.
// Returns ErrChromeNotFound or ErrUnsupportedPlatform as appropriate.
func FindChrome(configured string) (string, error) {
	if chromeGOOS == "windows" {
		return "", ErrUnsupportedPlatform
	}
	if configured != "" {
		if _, err := chromeStat(configured); err != nil {
			return "", fmt.Errorf("%w: %s", ErrChromeNotFound, configured)
		}
		return configured, nil
	}
	if env := os.Getenv("OCODE_CHROME_PATH"); env != "" {
		if _, err := chromeStat(env); err != nil {
			return "", fmt.Errorf("%w: %s", ErrChromeNotFound, env)
		}
		return env, nil
	}
	if chromeGOOS == "darwin" {
		for _, p := range macOSCandidates {
			if _, err := chromeStat(p); err == nil {
				return p, nil
			}
		}
		return "", ErrChromeNotFound
	}
	// Linux and other Unix: probe $PATH names.
	for _, name := range linuxCandidates {
		if p, err := chromeLookPath(name); err == nil {
			return p, nil
		}
	}
	return "", ErrChromeNotFound
}

func chromeArgs(tmpDir string) []string {
	return chromeArgsFor(tmpDir, "", true)
}

// chromeArgsFor builds the Chrome argument list. When extDir points at an
// unpacked extension directory (containing manifest.json), --disable-extensions
// is replaced with --load-extension=<dir> so the extension's content scripts
// and background service worker run inside ocode's headless Chrome. The
// profile is persistent when ManagerOptions.ProfileDir is set (so extension
// storage survives restarts); native-messaging relay additionally needs a stable
// extension ID (pinned manifest key), which unpacked loads without a key do
// not provide — see htr.go for the documented limits. Branded Google Chrome
// 137+ ignores --load-extension; Chromium/Canary/Edge/Brave still honor it
// (a warning is logged at launch).
// noSandbox adds --no-sandbox (browser.no_sandbox in ocode config, default
// true for remote/WSL/container compatibility).
func chromeArgsFor(tmpDir, extDir string, noSandbox bool) []string {
	args := []string{
		"--headless=new",
		"--remote-debugging-pipe",
		"--user-data-dir=" + tmpDir,
		"--no-first-run",
		"--no-default-browser-check",
	}
	if hasExtensionDir(extDir) {
		args = append(args, "--load-extension="+extDir)
	} else {
		args = append(args, "--disable-extensions")
	}
	if noSandbox {
		args = append(args, "--no-sandbox")
	}
	return append(args,
		"--disable-background-networking",
		"--disable-sync",
		"--disable-component-update",
		"--window-size=1280,800",
	)
}

// hasExtensionDir reports whether dir looks like an unpacked extension.
func hasExtensionDir(dir string) bool {
	if dir == "" {
		return false
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return false
	}
	mfi, err := os.Stat(filepath.Join(dir, "manifest.json"))
	if err != nil || mfi.IsDir() {
		return false
	}
	return true
}

// isBrandedChrome reports whether chromePath looks like branded Google Chrome
// (as opposed to Chromium/Canary/Edge/Brave), which ignores --load-extension.
func isBrandedChrome(chromePath string) bool {
	base := strings.ToLower(filepath.Base(chromePath))
	if strings.Contains(base, "chromium") || strings.Contains(base, "canary") ||
		strings.Contains(base, "edge") || strings.Contains(base, "brave") {
		return false
	}
	// macOS .app bundle paths carry the edition in the full path.
	lower := strings.ToLower(chromePath)
	if strings.Contains(lower, "canary") || strings.Contains(lower, "chromium") ||
		strings.Contains(lower, "edge") || strings.Contains(lower, "brave") {
		return false
	}
	return true
}

// HTRBrowserCompatibilityNotice reports the known branded-Chrome extension
// preload incompatibility. The browser remains usable, but HTR must be
// disabled rather than silently pretending the extension loaded.
func HTRBrowserCompatibilityNotice(chromePath string) string {
	if chromePath != "" && isBrandedChrome(chromePath) {
		return "HTR automation is unavailable with branded Google Chrome; use Chromium, Chrome Canary, Edge, or Brave for extension preload. Browsing continues without the HTR extension."
	}
	return ""
}

// launchChrome starts a headless Chrome process via the supervisor and returns
// a CDP Conn over the pipe. The caller must call cleanup when done.
// It is the production launcher injected into Manager; tests replace it.
// extDir is an optional unpacked-extension directory ("" = none); when it
// points at a directory containing manifest.json, --load-extension is used
// instead of --disable-extensions (see chromeArgsFor).
func launchChrome(ctx context.Context, chromePath string, sup *tool.ProcessSupervisor, lg *log.Logger, extDir ...string) (*Conn, <-chan int, func(), error) {
	ext := ""
	if len(extDir) > 0 {
		ext = extDir[0]
	}
	return launchChromeWithOptions(ctx, chromePath, sup, lg, ext, "", "", "", true)
}

// profileDir is the persistent --user-data-dir ("" = ephemeral temp profile);
// see prepareProfileDir for lock handling.
func launchChromeWithOptions(ctx context.Context, chromePath string, sup *tool.ProcessSupervisor, lg *log.Logger, ext, socketPath, nativeHostName, profileDir string, noSandbox bool) (*Conn, <-chan int, func(), error) {
	tmpDir, cleanupDir, err := prepareProfileDir(profileDir, lg)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("launch failed: %w", err)
	}

	r3Read, r3Write, err := os.Pipe()
	if err != nil {
		cleanupDir()
		return nil, nil, nil, fmt.Errorf("launch failed: %w", err)
	}
	r4Read, r4Write, err := os.Pipe()
	if err != nil {
		_ = r3Read.Close()
		_ = r3Write.Close()
		cleanupDir()
		return nil, nil, nil, fmt.Errorf("launch failed: %w", err)
	}
	// Child reads from r3Read (fd 3), writes to r4Write (fd 4).
	// Parent keeps r3Write (write end) and r4Read (read end).
	//
	// exec.Command, NOT exec.CommandContext: ctx here is the first caller's
	// request context (the CDP websocket handler's Attach). Chrome's lifetime
	// is owned by the manager + supervisor — tying it to the launching
	// request meant the first socket close (React StrictMode double-mount,
	// tab switch) killed Chrome and every panel saw "chrome exited". ctx
	// still bounds the handshake below.
	if ext != "" {
		if !hasExtensionDir(ext) {
			if lg != nil {
				lg.Printf("browse: htr extension dir %q missing manifest.json — launching without extensions", ext)
			}
			ext = ""
		} else if isBrandedChrome(chromePath) && lg != nil {
			lg.Printf("browse: branded Google Chrome may ignore --load-extension (removed 2025); prefer Chromium/Canary/Edge for extension preload: %s", ext)
		}
	}
	cmd := exec.Command(chromePath, chromeArgsFor(tmpDir, ext, noSandbox)...)
	if socketPath != "" {
		cmd.Env = append(os.Environ(), "HTR_SOCKET_PATH="+socketPath)
		if nativeHostName != "" {
			cmd.Env = append(cmd.Env, "HTR_NATIVE_HOST_NAME="+nativeHostName)
		}
	}
	cmd.ExtraFiles = []*os.File{r3Read, r4Write}
	if lg != nil {
		cmd.Stderr = &logWriter{lg: lg}
	}

	// Start via supervisor.
	rec, err := tool.StartSupervised(sup, cmd, tool.ProcessRegistration{
		ID:   "browse-chrome",
		Name: "headless chrome",
		Kind: tool.ProcessKindBrowser,
	})
	if err != nil {
		_ = r3Read.Close()
		_ = r3Write.Close()
		_ = r4Read.Close()
		_ = r4Write.Close()
		cleanupDir()
		return nil, nil, nil, fmt.Errorf("launch failed: %w", err)
	}
	// Parent no longer needs the child ends.
	_ = r3Read.Close()
	_ = r4Write.Close()

	// Capture PID for generation-aware exit marking. After a crash/relaunch
	// the old waiter must not clobber the new browser record.
	browserPID := rec.PID
	if browserPID == 0 && cmd.Process != nil {
		browserPID = cmd.Process.Pid
	}

	exited := make(chan int, 1)
	go func() {
		err := cmd.Wait()
		code := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else {
				code = 1
			}
		}
		if sup != nil && browserPID != 0 {
			if !sup.MarkExitedPID("browse-chrome", browserPID, code) {
				// PID mismatch means this is a stale waiter from a previous
				// Chrome generation that has already been replaced; do not
				// clobber the new record.
			}
		} else if sup != nil {
			sup.MarkExited("browse-chrome", code)
		}
		select {
		case exited <- code:
		default:
		}
		close(exited)
	}()

	conn := NewConn(r4Read, r3Write)

	cleanup := func() {
		_ = conn.Close()
		_ = r3Write.Close()
		_ = r4Read.Close()
		// Ensure process is terminated; supervisor will handle SIGTERM/SIGKILL
		// but we also try to kill if still running.
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		cleanupDir()
	}

	// Handshake: Browser.getVersion with 10s timeout.
	hctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var ver struct {
		Product string `json:"product"`
	}
	if err := conn.Call(hctx, "", "Browser.getVersion", nil, &ver); err != nil {
		cleanup()
		// Drain exited if needed
		return nil, nil, nil, fmt.Errorf("launch failed: %w", err)
	}

	// Register real Browser.close callback now that we have a live conn.
	_ = sup.RegisterShutdownCallback(func() {
		bctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = conn.Call(bctx, "", "Browser.close", nil, nil)
	})

	return conn, exited, cleanup, nil
}

type logWriter struct {
	lg *log.Logger
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.lg.Printf("chrome: %s", string(p))
	return len(p), nil
}
