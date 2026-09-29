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
	"slices"
	"strings"

	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/remote"
)

// Seams so Run is testable without a network round-trip and without writing
// to the user's real ~/.local/share/opencode/projects.json. Production leaves
// them pointing at the real implementations; see remotecli_persist_test.go,
// which drives every project-persistence path through a temp-dir store.
//
// connect/connectWeb are vars (not inlined calls) so a test can simulate both
// outcomes that matter: a host that never establishes, and a host that
// establishes and is then disconnected.
var (
	newStore   = projects.NewStore
	connect    = remote.Connect
	connectWeb = remote.ConnectWeb
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

	store, _, err := newStore()
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

	// established flips the moment the connect reports the host usable (the
	// remote answered over ssh, its platform was detected and ocode was
	// ensured on it). Everything after that point can fail for reasons that
	// still leave a real project — the user hitting Ctrl-C, the tunnel
	// dropping, the remote server refusing to start — so it is the boundary
	// that decides whether this attempt leaves a project entry behind.
	established := false

	connectOpts := remote.ConnectOptions{
		Target: target,
		Path:   path,
		NoSync: noSync,
		Out:    os.Stdout,
		OnEstablished: func() {
			established = true
		},
	}
	// createdByPreWrite records that the --web pre-connect write below
	// introduced a brand-new entry, so a connect that never establishes can
	// roll back exactly that write and nothing else. A project the user
	// already had is never removed by a failed connect.
	createdByPreWrite := false
	if web {
		// SSH only: WSL's ConnectWeb path returns before ever reaching the
		// wait loop the hook drives (Windows already shares WSL2's
		// localhost natively, so extra forwards are a no-op there). Ensure
		// the project entry exists BEFORE connecting — Load()/AddPortMap
		// key off it, and it wouldn't exist yet on a brand-new project
		// (the post-connect AddRemote only runs once the host is known
		// usable).
		if store != nil && target.Kind == remote.KindSSH {
			existedBefore := remoteEntryExists(store, hostKey, path)
			if aerr := store.AddRemote(hostKey, path); aerr == nil {
				createdByPreWrite = !existedBefore
				connectOpts.PortMapHook = &storePortMapHook{
					store: store,
					ref:   projects.ProjectRef{Path: path, Host: hostKey},
				}
			}
		}
		err = connectWeb(connectOpts)
	} else {
		err = connect(connectOpts)
	}

	if store != nil {
		if shouldPersistRemote(hostKey, established) {
			// The host is real, so record the attempt however the session
			// ended — a deliberate disconnect exits Connect with an error
			// too, but this is a project the user wants to reattach to.
			_ = store.AddRemote(hostKey, path)
		} else if createdByPreWrite {
			// The host never became usable, so undo the pre-connect write.
			// Best-effort: the connect error is the one worth reporting.
			if rerr := store.RemoveRemote(hostKey, path); rerr != nil {
				fmt.Fprintf(os.Stderr, "remote: could not remove unused project %s:%s: %v\n", hostKey, path, rerr)
			}
		}
	}

	return err
}

// shouldPersistRemote decides whether a finished connect attempt may leave a
// project entry in the store. Both conditions are required: the host has to
// have been confirmed usable (otherwise a typo'd or offline host silently
// becomes a permanent dead entry in the project list), and there has to be a
// non-blank host to key it on.
func shouldPersistRemote(hostKey string, established bool) bool {
	return established && strings.TrimSpace(hostKey) != ""
}

// remoteEntryExists reports whether the store already holds an entry for this
// exact (host, path) pair. hostKey must already be canonical
// (target.String()), which is the form AddRemote stores and matches on.
func remoteEntryExists(store *projects.Store, hostKey, path string) bool {
	return slices.ContainsFunc(store.List(), func(p projects.Project) bool {
		return p.Host == hostKey && p.Path == path
	})
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
