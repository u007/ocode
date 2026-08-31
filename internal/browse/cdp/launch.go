package cdp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
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
	"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
	"/Applications/Chromium.app/Contents/MacOS/Chromium",
	"/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
	"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
	"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
}

var linuxCandidates = []string{
	"google-chrome",
	"google-chrome-stable",
	"chromium",
	"chromium-browser",
}

// FindChrome locates the Chrome binary.
// Order: configured path → OCODE_CHROME_PATH env → platform defaults.
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
	return []string{
		"--headless=new",
		"--remote-debugging-pipe",
		"--user-data-dir=" + tmpDir,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-extensions",
		"--disable-background-networking",
		"--disable-sync",
		"--disable-component-update",
		"--window-size=1280,800",
	}
}

// launchChrome starts a headless Chrome process via the supervisor and returns
// a CDP Conn over the pipe. The caller must call cleanup when done.
// It is the production launcher injected into Manager; tests replace it.
func launchChrome(ctx context.Context, chromePath string, sup *tool.ProcessSupervisor, lg *log.Logger) (*Conn, <-chan int, func(), error) {
	tmpDir, err := os.MkdirTemp("", "ocode-browse-*")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("launch failed: %w", err)
	}
	cleanupDir := func() { _ = os.RemoveAll(tmpDir) }

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
	cmd := exec.CommandContext(ctx, chromePath, chromeArgs(tmpDir)...)
	cmd.ExtraFiles = []*os.File{r3Read, r4Write}
	if lg != nil {
		cmd.Stderr = &logWriter{lg: lg}
	}

	// Start via supervisor.
	if _, err := tool.StartSupervised(sup, cmd, tool.ProcessRegistration{
		ID:   "browse-chrome",
		Name: "headless chrome",
		Kind: tool.ProcessKindBrowser,
	}); err != nil {
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
		sup.MarkExited("browse-chrome", code)
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
