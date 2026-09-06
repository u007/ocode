package remote

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/u007/ocode/internal/tool"
	"github.com/u007/ocode/internal/version"
)

// ConnectOptions configures a single `ocode remote` invocation.
type ConnectOptions struct {
	Target Target
	// Path is the remote project directory, verbatim (remote-side "~"
	// expansion applies since it's passed through a shell command).
	// Callers resolve the "omitted → recent project, else $HOME" default
	// before calling Connect — Connect itself always launches into a
	// concrete path.
	Path string
	// NoSync skips the credential/config sync stage entirely.
	NoSync bool
	// ModuleDir overrides cross-compile source-root detection (tests only;
	// "" auto-detects from the running executable / cwd).
	ModuleDir string
	// Out receives staged progress; nil defaults to os.Stdout.
	Out io.Writer
}

// Connect runs the full Phase-1 connect flow: reachability, platform
// detect, ensure binary, credential sync, multiplex detect, launch. It
// blocks until the remote TUI exits (or the connection drops). See
// docs/superpowers/specs/2026-08-29-remote-ssh/02-phase1-connect.md.
func Connect(opts ConnectOptions) error {
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	if opts.Path == "" {
		return fmt.Errorf("internal error: Connect requires a resolved Path")
	}

	progress := NewProgress(out, fmt.Sprintf("Connecting to %s…", opts.Target.String()))
	transport, sup, err := runPrepareStages(opts, progress)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = sup.Shutdown(ctx)
	}()
	if err != nil {
		return err
	}

	progress.Start("multiplex", "checking for tmux/screen")
	mux := DetectMultiplexer(transport)
	if warn := ResumeWarning(mux); warn != "" {
		progress.Warn(warn)
	} else {
		progress.Done(mux.String())
	}

	progress.Start("launch", "launching remote TUI")
	remoteCmd := shellQuotePath(RemoteBinaryPath(version.Version)) + " " + shellQuotePath(opts.Path)
	launchCmd := WrapLaunch(mux, opts.Path, remoteCmd)
	progress.Done("")

	return transport.ExecInteractive(launchCmd)
}

// runPrepareStages executes the shared reachability → platform-detect →
// ensure-binary → credential-sync stages (1-4 of the spec's numbering; TUI
// mode additionally runs multiplex-detect as its own stage 5, web mode
// never does — see 01-architecture.md "Session resume on disconnect").
// Returns the constructed Transport plus the supervisor it was built with,
// so ConnectWeb can register the tunnel process on the same supervisor
// (and Connect can pass it through to ExecInteractive unchanged, exactly
// as before this refactor). The caller owns shutting the supervisor down.
func runPrepareStages(opts ConnectOptions, progress *Progress) (Transport, *tool.ProcessSupervisor, error) {
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	transport, err := newTransportForTarget(opts.Target, sup)
	if err != nil {
		progress.Fail(err, "")
		return nil, sup, err
	}

	progress.Start("reachable", transport.Describe()+" reachable")
	if res, err := transport.Exec("true"); err != nil {
		progress.Fail(namedError("reachable", transport.Describe(), res.Stderr, err), "check the host, your ssh_config, and known_hosts")
		return nil, sup, err
	}
	progress.Done("")

	progress.Start("platform", "platform detect")
	goos, goarch, err := DetectPlatform(transport)
	if err != nil {
		progress.Fail(err, "")
		return nil, sup, err
	}
	progress.Done(goos + "/" + goarch)

	ver := version.Version
	if BinaryExists(transport, ver) {
		progress.Start("build", fmt.Sprintf("ocode v%s", ver))
		progress.Done("already installed")
	} else {
		progress.Start("build", fmt.Sprintf("building ocode v%s for %s/%s", ver, goos, goarch))
		build, err := PrepareLocalBuild(goos, goarch, opts.ModuleDir)
		if err != nil {
			progress.Fail(err, "install Go, or run from an ocode source checkout")
			return nil, sup, err
		}
		if !build.Reused {
			defer os.Remove(build.Path)
			progress.Done("cross-compiled")
		} else {
			progress.Done("reused local binary")
		}

		progress.Start("upload", "uploading")
		if err := UploadBinary(transport, ver, build.Path); err != nil {
			progress.Fail(err, "")
			return nil, sup, err
		}
		progress.Done("")

		progress.Start("verify", "installing + verifying")
		if err := ActivateAndVerify(transport, ver); err != nil {
			progress.Fail(err, "")
			return nil, sup, err
		}
		progress.Done("")

		_ = GCVersions(transport)
	}

	if opts.NoSync {
		progress.Start("sync", "credentials synced")
		progress.Warn("skipped (--no-sync)")
	} else if err := runSyncStage(progress, transport, opts.Target.String(), ver); err != nil {
		_ = err
	}

	return transport, sup, nil
}

// newTransportForTarget builds the Transport implementation for t.Kind,
// after validating the target is usable on this OS (KindWSL requires
// Windows — see target.go's validateTargetOS, added in Task 12).
func newTransportForTarget(t Target, sup *tool.ProcessSupervisor) (Transport, error) {
	if err := validateTargetOS(t.Kind, runtime.GOOS); err != nil {
		return nil, err
	}
	switch t.Kind {
	case KindWSL:
		return NewWSLTransport(t.Distro, sup), nil
	default:
		return NewSSHTransport(t, sup), nil
	}
}

// ConnectWebOptions extends ConnectOptions with web-mode-only knobs.
type ConnectWebOptions struct {
	ConnectOptions
	// OpenBrowser is called with the local URL to open once the tunnel (or,
	// for WSL, the direct localhost port) is ready. Defaults to the
	// platform opener; overridable in tests.
	OpenBrowser func(url string) error
}

// ConnectWeb runs the shared prepare stages, then discovers-or-launches a
// detached remote server, tunnels it to a local port (skipped for WSL —
// Windows forwards WSL2 localhost natively), and opens the browser with a
// one-time token in the URL fragment. Unlike Connect, it does not block on
// the remote process: it blocks supervising the local tunnel (SSH targets)
// until the user disconnects (Ctrl-C) or the tunnel dies. The remote server
// itself is a detached long-lived process and outlives this call by design.
func ConnectWeb(opts ConnectOptions) error {
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	if opts.Path == "" {
		return fmt.Errorf("internal error: ConnectWeb requires a resolved Path")
	}

	progress := NewProgress(out, fmt.Sprintf("Connecting to %s (web)…", opts.Target.String()))
	transport, sup, err := runPrepareStages(opts, progress)
	if err != nil {
		return err
	}

	progress.Start("server", "discovering or starting remote server")
	state, reused, err := EnsureRemoteServer(transport, version.Version)
	if err != nil {
		progress.Fail(err, "check ~/.ocode/remote/serve.log on the remote")
		return err
	}
	if reused {
		progress.Done("reusing existing server")
	} else {
		progress.Done("started fresh")
	}

	if opts.Target.Kind == KindWSL {
		progress.Start("open", "opening browser")
		url := fmt.Sprintf("http://localhost:%d/#token=%s", state.Port, state.Token)
		if err := openBrowserURL(url); err != nil {
			progress.Fail(err, "")
			return err
		}
		progress.Done("")
		fmt.Fprintln(out, "Remote server running inside WSL; this command can now exit — the server keeps running.")
		return nil
	}

	progress.Start("tunnel", "opening SSH tunnel")
	localPort, tunnelCmd, err := startTunnelWithRetry(sup, opts.Target, state.Port)
	if err != nil {
		progress.Fail(err, "")
		return err
	}
	progress.Done(fmt.Sprintf("localhost:%d → remote:%d", localPort, state.Port))

	progress.Start("open", "opening browser")
	url := fmt.Sprintf("http://localhost:%d/#token=%s", localPort, state.Token)
	if err := openBrowserURL(url); err != nil {
		progress.Fail(err, "")
		// The tunnel is already running and registered on sup at this
		// point — without an explicit shutdown here, superviseTunnel (the
		// only other place that tears the tunnel down) is never reached on
		// this path, orphaning the ssh -N -L subprocess indefinitely.
		shutdownSupervisor(sup)
		return err
	}
	progress.Done("")

	fmt.Fprintln(out, "Tunnel active. Press Ctrl-C to close it (the remote server keeps running).")
	return superviseTunnel(sup, tunnelCmd)
}

// shutdownSupervisor shuts sup down with the standard 5s grace timeout used
// everywhere a connect flow tears down its supervisor. Errors are
// intentionally swallowed — the caller is already on an error/exit path and
// a shutdown failure has nothing further for it to do.
func shutdownSupervisor(sup *tool.ProcessSupervisor) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = sup.Shutdown(ctx)
}

// openBrowserURL opens url in the platform default browser. Duplicated
// (deliberately, it's five lines) rather than exported from
// internal/server, to avoid remotecli/remote depending on the server
// package for one helper.
func openBrowserURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return fmt.Errorf("unsupported platform %q for opening a browser", runtime.GOOS)
	}
	return cmd.Start()
}

// startTunnelWithRetry picks a free local port and starts the tunnel; per
// the spec's error-handling table, a bind failure (port grabbed by another
// process between FreeLocalPort's probe and ssh's actual bind — an
// accepted, narrow TOCTOU race) gets exactly one retry with a fresh port
// before the failure is surfaced with ssh's own stderr.
func startTunnelWithRetry(sup *tool.ProcessSupervisor, target Target, remotePort int) (localPort int, tunnelCmd *exec.Cmd, err error) {
	for attempt := 0; attempt < 2; attempt++ {
		localPort, err = FreeLocalPort()
		if err != nil {
			return 0, nil, err
		}
		tunnelCmd, err = StartTunnel(sup, target, localPort, remotePort)
		if err == nil {
			return localPort, tunnelCmd, nil
		}
	}
	return 0, nil, fmt.Errorf("open ssh tunnel after retry: %w", err)
}

// superviseTunnel blocks until either the tunnel process exits on its own
// (reported as an error — the connection is gone) or the user sends
// SIGINT/SIGTERM (reported as nil — a clean, requested disconnect). The
// tunnel's own process group (StartSupervised via setProcGroup) is separate
// from this process's, so a terminal Ctrl-C does not reach it automatically
// — this signal handler explicitly kills it on the way out.
func superviseTunnel(sup *tool.ProcessSupervisor, tunnelCmd *exec.Cmd) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	waitCh := make(chan error, 1)
	go func() { waitCh <- tunnelCmd.Wait() }()

	select {
	case err := <-waitCh:
		if err != nil {
			return fmt.Errorf("tunnel closed unexpectedly: %w", err)
		}
		return fmt.Errorf("tunnel closed unexpectedly")
	case <-sigCh:
		shutdownSupervisor(sup)
		<-waitCh
		return nil
	}
}

// runSyncStage builds and pushes the credential/config payload, honoring
// the per-host skip-if-unchanged cache. It never returns an error that
// should abort the connect — failures are rendered as a warned stage and
// swallowed, matching "connect continues" in the spec's error handling.
func runSyncStage(progress *Progress, transport Transport, hostKey, ver string) error {
	progress.Start("sync", "syncing credentials")

	payload, err := BuildSyncPayload()
	if err != nil {
		progress.Warn("build payload failed: " + err.Error())
		return err
	}
	hash, err := PayloadHash(payload)
	if err != nil {
		progress.Warn("hash payload failed: " + err.Error())
		return err
	}
	if cached, ok := CachedHash(hostKey); ok && cached == hash {
		progress.Done("unchanged, skipped")
		return nil
	}

	frame, err := EncodeFrame(ver, payload)
	if err != nil {
		progress.Warn("encode payload failed: " + err.Error())
		return err
	}

	res, err := transport.ExecStdin(
		shellQuotePath(RemoteBinaryPath(ver))+" remote-receive-config",
		bytes.NewReader(frame),
	)
	if err != nil || res.ExitCode != 0 {
		if err == nil {
			err = fmt.Errorf("exit %d", res.ExitCode)
		}
		progress.Warn(fmt.Sprintf("remote-receive-config failed: %v: %s", err, res.Stderr))
		return err
	}

	if err := SetCachedHash(hostKey, hash); err != nil {
		// Non-fatal: the sync itself succeeded, only the local cache write
		// failed — next connect just re-syncs unnecessarily.
		progress.Done("synced (cache write failed)")
		return nil
	}
	progress.Done("synced")
	return nil
}
