package remote

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
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

	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = sup.Shutdown(ctx)
	}()

	transport := NewSSHTransport(opts.Target, sup)
	progress := NewProgress(out, fmt.Sprintf("Connecting to %s…", opts.Target.String()))

	// 1. Reachability.
	progress.Start("reachable", "ssh reachable")
	if res, err := transport.Exec("true"); err != nil {
		progress.Fail(namedError("reachable", "ssh", res.Stderr, err), "check the host, your ssh_config, and known_hosts")
		return err
	}
	progress.Done("")

	// 2. Platform detect.
	progress.Start("platform", "platform detect")
	goos, goarch, err := DetectPlatform(transport)
	if err != nil {
		progress.Fail(err, "")
		return err
	}
	progress.Done(goos + "/" + goarch)

	// 3-5. Ensure binary (build/upload/verify), broken into progress rows.
	ver := version.Version
	if BinaryExists(transport, ver) {
		progress.Start("build", fmt.Sprintf("ocode v%s", ver))
		progress.Done("already installed")
	} else {
		progress.Start("build", fmt.Sprintf("building ocode v%s for %s/%s", ver, goos, goarch))
		build, err := PrepareLocalBuild(goos, goarch, opts.ModuleDir)
		if err != nil {
			progress.Fail(err, "install Go, or run from an ocode source checkout")
			return err
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
			return err
		}
		progress.Done("")

		progress.Start("verify", "installing + verifying")
		if err := ActivateAndVerify(transport, ver); err != nil {
			progress.Fail(err, "")
			return err
		}
		progress.Done("")

		_ = GCVersions(transport)
	}

	// 6. Credential sync (unless --no-sync).
	if opts.NoSync {
		progress.Start("sync", "credentials synced")
		progress.Warn("skipped (--no-sync)")
	} else if err := runSyncStage(progress, transport, opts.Target.String(), ver); err != nil {
		// Per spec: a sync failure does not abort the connect — it's
		// marked failed in the progress output and the flow continues to
		// launch, since the remote may already have working keys.
		_ = err
	}

	// 7. Multiplex detect.
	progress.Start("multiplex", "checking for tmux/screen")
	mux := DetectMultiplexer(transport)
	if warn := ResumeWarning(mux); warn != "" {
		progress.Warn(warn)
	} else {
		progress.Done(mux.String())
	}

	// 8. Launch.
	progress.Start("launch", "launching remote TUI")
	remoteCmd := shellQuote(RemoteBinaryPath(ver)) + " " + shellQuote(opts.Path)
	launchCmd := WrapLaunch(mux, opts.Path, remoteCmd)
	progress.Done("")

	return transport.ExecInteractive(launchCmd)
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
		shellQuote(RemoteBinaryPath(ver))+" remote-receive-config",
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
