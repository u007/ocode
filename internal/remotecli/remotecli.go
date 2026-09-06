// Package remotecli implements the `ocode remote` command family: connect
// to (and provision) ocode on a remote SSH host, and the hidden
// `remote-receive-config` command the remote side runs to accept a synced
// credential/config payload. See
// docs/superpowers/specs/2026-08-29-remote-ssh/ for the design.
package remotecli

import (
	"fmt"
	"io"
	"os"

	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/remote"
)

// Run dispatches `ocode remote <[user@]host> [path] [--web] [--no-sync]`.
func Run(args []string) error {
	target, path, noSync, web, err := parseArgs(args)
	if err != nil {
		return err
	}
	// path here is only ever non-empty when the user explicitly typed one —
	// the FindLastRemote/"~" defaulting below hasn't run yet. --web launches
	// `ocode serve --remote` on the remote with no workdir argument (it ends
	// up rooted at the ssh session's default directory, typically $HOME),
	// and a second --web invocation with a different path would silently
	// reuse the first invocation's already-running server anyway — so
	// reject an explicit path outright rather than silently ignoring it.
	if web && path != "" {
		return fmt.Errorf("ocode remote --web does not yet support selecting a specific project path; omit the path argument, or connect without --web for TUI mode")
	}

	store, _, err := projects.NewStore()
	if err != nil {
		// Non-fatal: recent-project lookup/recording is a convenience, not
		// required for a connect to succeed.
		store = nil
	}

	hostKey := target.String()
	if path == "" {
		if store != nil {
			if p, ok := store.FindLastRemote(hostKey); ok {
				path = p.Path
			}
		}
		if path == "" {
			path = "~"
		}
	}

	connectOpts := remote.ConnectOptions{
		Target: target,
		Path:   path,
		NoSync: noSync,
		Out:    os.Stdout,
	}
	if web {
		err = remote.ConnectWeb(connectOpts)
	} else {
		err = remote.Connect(connectOpts)
	}

	if store != nil {
		// Record the attempt regardless of how the session ended (a
		// deliberate disconnect exits Connect with an error too, but the
		// project itself is still the one the user wants to reattach to
		// next time).
		_ = store.AddRemote(hostKey, path)
	}

	return err
}

func parseArgs(args []string) (target remote.Target, path string, noSync bool, web bool, err error) {
	var positional []string
	for _, a := range args {
		if a == "--no-sync" {
			noSync = true
			continue
		}
		if a == "--web" {
			web = true
			continue
		}
		if len(a) > 0 && a[0] == '-' {
			return remote.Target{}, "", false, false, fmt.Errorf("unknown flag %q (usage: ocode remote <[user@]host> [path] [--web] [--no-sync])", a)
		}
		positional = append(positional, a)
	}
	if len(positional) == 0 {
		return remote.Target{}, "", false, false, fmt.Errorf("usage: ocode remote <[user@]host> [path] [--web] [--no-sync]")
	}
	target, err = remote.ParseTarget(positional[0])
	if err != nil {
		return remote.Target{}, "", false, false, err
	}
	if len(positional) > 1 {
		path = positional[1]
	}
	if len(positional) > 2 {
		return remote.Target{}, "", false, false, fmt.Errorf("too many arguments (usage: ocode remote <[user@]host> [path] [--web] [--no-sync])")
	}
	return target, path, noSync, web, nil
}

// RunReceiveConfig implements the hidden `ocode remote-receive-config`
// command: reads a framed sync payload from stdin, validates it, and writes
// its files. Never logs payload contents — only a single machine-readable
// OK/error line on stdout, matching the spec.
func RunReceiveConfig(args []string) error {
	return runReceiveConfig(os.Stdin, os.Stdout)
}

func runReceiveConfig(in io.Reader, out io.Writer) error {
	data, err := io.ReadAll(in)
	if err != nil {
		fmt.Fprintf(out, "ERROR read stdin: %v\n", err)
		return err
	}

	_, payload, err := remote.DecodeAndVerifyFrame(data)
	if err != nil {
		fmt.Fprintf(out, "ERROR %v\n", err)
		return err
	}

	if err := remote.WritePayload(payload); err != nil {
		fmt.Fprintf(out, "ERROR %v\n", err)
		return err
	}

	fmt.Fprintln(out, "OK")
	return nil
}
