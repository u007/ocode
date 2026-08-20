package lsp

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/u007/ocode/internal/lsp/broker"
)

// DefaultDaemonIdleTimeout is how long the daemon keeps its real language
// server alive with zero connected broker clients before exiting. Long
// enough that a quick app restart reconnects instead of re-paying gopls's
// indexing cost; short enough not to leak a server forever after the last
// interested ocode process is gone.
const DefaultDaemonIdleTimeout = 5 * time.Minute

// RunDaemon is the entry point for the hidden `ocode lsp-daemon` subcommand.
// It is spawned (detached, not waited on) by Manager.ClientForExt's shared
// broker path the first time it finds no reachable daemon for a given
// (project root, file extension); see spawnDaemon in manager.go.
//
// Exactly one daemon should end up serving a given (root, ext): StartOnce
// serializes startup under the metadata file's lock, so a second spawn that
// loses the race (finds a now-reachable daemon published by the winner)
// exits immediately rather than starting a second real language server.
func RunDaemon(args []string) error {
	fs := flag.NewFlagSet("lsp-daemon", flag.ContinueOnError)
	root := fs.String("root", "", "project root the language server is scoped to")
	ext := fs.String("ext", "", "file extension this daemon serves, e.g. .go")
	idle := fs.Duration("idle", DefaultDaemonIdleTimeout, "exit after this long with zero connected clients")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *root == "" || *ext == "" {
		return fmt.Errorf("lsp-daemon: --root and --ext are required")
	}

	spec, ok := serversByExt[*ext]
	if !ok {
		return fmt.Errorf("lsp-daemon: no language server configured for %q", *ext)
	}

	identity, err := broker.NewIdentity(*root, spec.cmd, spec.args, spec.langID, nil, nil)
	if err != nil {
		return fmt.Errorf("lsp-daemon: resolve identity: %w", err)
	}
	metadataPath, err := broker.MetadataPath(identity)
	if err != nil {
		return fmt.Errorf("lsp-daemon: resolve metadata path: %w", err)
	}

	wonRace := false
	if _, err := broker.StartOnce(metadataPath, func(m broker.Metadata) error {
		// Metadata already exists — probe whether its owner is actually
		// reachable. A live owner means this spawn lost the startup race;
		// returning nil here tells StartOnce to keep that metadata as-is
		// and skip start(). A dead owner returns an error so StartOnce
		// removes the stale file and calls start() below.
		conn, dialErr := broker.Connect(context.Background(), m, "lsp-daemon-probe", 1, 0)
		if dialErr != nil {
			return dialErr
		}
		_ = conn.Close()
		wonRace = true
		return nil
	}, func() (broker.Metadata, error) {
		real, err := NewClient(spec.cmd, spec.args...)
		if err != nil {
			return broker.Metadata{}, err
		}
		if err := real.Initialize(*root, spec.langID); err != nil {
			_ = real.Close()
			return broker.Metadata{}, err
		}
		meta, err := broker.NewMetadata(identity, 0, os.Getpid())
		if err != nil {
			_ = real.Close()
			return broker.Metadata{}, err
		}
		upstream := newDaemonUpstream(real, *idle, func() {
			_ = real.Close()
			_ = broker.RemoveMetadataIfOwner(metadataPath, meta.BrokerID)
			os.Exit(0)
		})
		l, err := broker.Listen(meta, upstream.handleConn)
		if err != nil {
			_ = real.Close()
			return broker.Metadata{}, err
		}
		wonRace = true
		// l's acceptLoop runs in its own goroutine (started inside
		// broker.Listen) and stays reachable via that goroutine's closure,
		// so no reference needs to be kept here.
		return l.Metadata(), nil
	}); err != nil {
		return fmt.Errorf("lsp-daemon: %w", err)
	}

	if !wonRace {
		// Unreachable in practice (both branches above set wonRace), kept
		// as an explicit guard rather than silently exiting non-zero.
		return fmt.Errorf("lsp-daemon: startup completed without a listener or a reachable existing daemon")
	}
	select {}
}
