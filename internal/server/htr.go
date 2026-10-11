package server

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/u007/ocode/internal/browse/cdp"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// resolveManagedHTROptions builds the managed `htrcli serve` options from the
// persisted browser config, applying the same resolution StartBrowse uses:
// Chrome discovery for native-host registration, the compatibility gate, the
// ocode-managed socket path, and the embedded/override asset extraction. When
// HTR is disabled, or a gate fails, Enabled is false and notice explains why
// (empty when nothing is wrong). Callers treat a false Enabled as "do not
// start" rather than an error so browsing can continue without HTR.
//
// browser.HTRShared selects the shared daemon: one `htrcli serve` serving both
// ocode and the user's own extension, with its coordinates read from htrcli's
// config rather than invented here. When it is off, Port/SocketPath keep
// today's ocode-managed 3846/managed-socket values unchanged, so the rollback
// really is byte-for-byte the old behaviour.
func resolveManagedHTROptions(browser config.BrowserConfig) (cdp.HTROptions, string) {
	htr := cdp.HTROptions{
		Enabled:        browser.HTREnabled,
		CliPath:        browser.HTRCliPath,
		ExtensionDir:   browser.HTRExtensionPath,
		Port:           browser.HTRPort,
		SocketPath:     browser.HTRSocketPath,
		NativeHostName: browser.HTRNativeHostName,
		BrowserPath:    browser.ChromePath,
	}
	if !htr.Enabled {
		return htr, ""
	}
	htr.Shared = cdp.ResolveSharedDaemon(cdp.HTRSharedInput{
		Token:        browser.HTRToken,
		Shared:       browser.HTRShared,
		LegacyPort:   browser.HTRPort,
		LegacySocket: browser.HTRSocketPath,
	})
	if htr.Shared.Mode == "shared" {
		// Shared mode's coordinates are htrcli's, not ours. Assigning them here
		// (rather than in a caller) means the later ResolveHTRSocketPath call
		// sees an already-absolute path and passes it straight through, so the
		// legacy socket-directory creation below never runs in shared mode.
		htr.Port = htr.Shared.Port
		htr.SocketPath = htr.Shared.Socket
		if err := ensureSharedSocketDir(htr.Shared.Socket, htr.Shared.AdoptOnly); err != nil {
			htr.Enabled = false
			return htr, "HTR automation is unavailable: " + err.Error() + ". Browsing continues without the HTR extension."
		}
	}
	if htr.BrowserPath == "" {
		if browserPath, err := cdp.FindChrome(""); err == nil {
			htr.BrowserPath = browserPath
		}
	}
	if notice := cdp.HTRBrowserCompatibilityNotice(htr.BrowserPath); notice != "" {
		htr.Enabled = false
		return htr, notice
	}
	socketPath, err := cdp.ResolveHTRSocketPath(htr.SocketPath)
	if err != nil {
		htr.Enabled = false
		return htr, "HTR automation is unavailable: " + err.Error() + ". Browsing continues without the HTR extension."
	}
	htr.SocketPath = socketPath
	hostName := htr.NativeHostName
	if hostName == "" {
		hostName = cdp.DefaultHTRNativeHostName
	}
	assets, err := cdp.ResolveHTRAssetsForHost(htr.ExtensionDir, htr.CliPath, hostName)
	if err != nil {
		htr.Enabled = false
		return htr, "HTR automation is unavailable: " + err.Error() + ". Browsing continues without the HTR extension."
	}
	htr.ExtensionDir = assets.ExtensionDir
	htr.CliPath = assets.CliPath
	return htr, ""
}

// ensureSharedSocketDir creates the parent directory of the shared daemon's
// Unix socket. cdp.ResolveHTRSocketPath does this for the ocode-managed socket,
// but the shared path (~/.htrcli/daemon.sock) comes from the pure resolver in
// cdp.ResolveSharedDaemon, which touches no filesystem, so nothing else would.
//
// It only acts when ocode may actually spawn, i.e. when AdoptOnly is false: an
// adopt-only resolution means ocode never starts a daemon, so it has no reason
// to create a directory in the user's home for one it will not launch. In
// practice the directory already exists there anyway — a non-adopt-only shared
// resolution is only possible when htrcli's config.json was readable, and that
// file lives in this very directory — so this is a safety net for a daemon that
// would otherwise fail to bind its socket.
//
// socket is not a path on Windows, where shared mode uses a loopback endpoint
// instead of a Unix-domain socket; filepath.Dir would yield a nonsense relative
// directory there, so skip it.
func ensureSharedSocketDir(socket string, adoptOnly bool) error {
	if adoptOnly || runtime.GOOS == "windows" || strings.TrimSpace(socket) == "" {
		return nil
	}
	dir := filepath.Dir(socket)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create HTR shared socket dir: %w", err)
	}
	return nil
}

// ErrHTRDisabled reports that HTR is switched off in config, so no daemon
// options were resolved and nothing was started. It is a sentinel rather than
// a plain error because a caller on a startup path must be able to tell "the
// user turned this off" (quiet, expected, not worth an error-level log line on
// every launch) apart from "we tried and it failed".
var ErrHTRDisabled = errors.New("HTR is disabled")

// EnsureSharedHTRDaemon resolves and ensures the managed `htrcli serve` daemon
// for a caller that is not the browse server — notably a plain TUI session,
// which never reaches StartBrowse and would otherwise leave the user's own
// browser extension with nothing to attach to.
//
// It performs exactly the resolution StartBrowse performs
// (resolveManagedHTROptions) and then hands the result to cdp.EnsureHTRServe,
// so a daemon started from here is the same daemon, on the same coordinates,
// under the same lease bookkeeping, as one started by the desktop app. A healthy
// existing daemon is reused, never restarted.
//
// Everything is non-fatal: HTR being off or a gate failing comes back as an
// error for the caller to log, and nothing here can take down a session. sup
// may be nil, in which case an already-running daemon is still adopted but
// nothing is spawned — the same contract cdp.EnsureHTRServe documents.
func EnsureSharedHTRDaemon(sup *tool.ProcessSupervisor, browser config.BrowserConfig, lg *log.Logger) (cdp.HTRStatus, error) {
	opts, notice := resolveManagedHTROptions(browser)
	if notice != "" {
		return cdp.HTRStatus{}, errors.New(notice)
	}
	if !opts.Enabled {
		return cdp.HTRStatus{}, ErrHTRDisabled
	}
	return cdp.EnsureHTRServe(sup, opts, lg)
}
