package server

import (
	"context"
	"testing"
	"time"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
)

// A Server that is constructed but never served must leave the process-global
// config/auth hook lists untouched. Those callbacks live until Shutdown, so
// registering them in New pinned the handler forever for every handler-only
// caller — which is what the whole test package does. Serve is the only entry
// that needs them.
func TestSyncHooksInstallOnServeNotConstruction(t *testing.T) {
	setHomeTree(t, t.TempDir())

	beforeCfg := config.OnConfigSaved.Len()
	beforeAuth := auth.OnCredentialsSaved.Len()

	srv := New("127.0.0.1:0", "", "", nil)
	if got := len(srv.removeSyncHooks); got != 0 {
		t.Fatalf("New tracked %d sync hooks, want 0 (Serve installs them)", got)
	}
	if got := config.OnConfigSaved.Len(); got != beforeCfg {
		t.Fatalf("New registered a config.OnConfigSaved hook: %d -> %d", beforeCfg, got)
	}
	if got := auth.OnCredentialsSaved.Len(); got != beforeAuth {
		t.Fatalf("New registered an auth.OnCredentialsSaved hook: %d -> %d", beforeAuth, got)
	}

	srv.registerSyncHooks()
	if got := len(srv.removeSyncHooks); got != 2 {
		t.Fatalf("registerSyncHooks tracked %d hooks, want 2 (config + auth)", got)
	}
	if got := config.OnConfigSaved.Len(); got != beforeCfg+1 {
		t.Fatalf("config.OnConfigSaved has %d hooks after registerSyncHooks, want %d", got, beforeCfg+1)
	}
	if got := auth.OnCredentialsSaved.Len(); got != beforeAuth+1 {
		t.Fatalf("auth.OnCredentialsSaved has %d hooks after registerSyncHooks, want %d", got, beforeAuth+1)
	}

	// A second Serve-generation call must not add duplicates.
	srv.registerSyncHooks()
	if got := config.OnConfigSaved.Len(); got != beforeCfg+1 {
		t.Fatalf("registerSyncHooks is not idempotent: config hooks %d, want %d", got, beforeCfg+1)
	}
	if got := auth.OnCredentialsSaved.Len(); got != beforeAuth+1 {
		t.Fatalf("registerSyncHooks is not idempotent: auth hooks %d, want %d", got, beforeAuth+1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Logf("shutdown: %v", err)
	}
	if got := len(srv.removeSyncHooks); got != 0 {
		t.Fatalf("Shutdown left %d sync hooks tracked", got)
	}
	if got := config.OnConfigSaved.Len(); got != beforeCfg {
		t.Fatalf("Shutdown left a config.OnConfigSaved hook: %d, want %d", got, beforeCfg)
	}
	if got := auth.OnCredentialsSaved.Len(); got != beforeAuth {
		t.Fatalf("Shutdown left an auth.OnCredentialsSaved hook: %d, want %d", got, beforeAuth)
	}
}

// Shutdown removes the sync hooks and marks the server down. A Serve that
// starts after (or races) Shutdown must not re-install them: nothing would
// remove them again, so they would pin the dead handler for the process.
func TestSyncHooksNotReinstalledAfterShutdown(t *testing.T) {
	setHomeTree(t, t.TempDir())

	beforeCfg := config.OnConfigSaved.Len()
	beforeAuth := auth.OnCredentialsSaved.Len()

	srv := New("127.0.0.1:0", "", "", nil)
	srv.registerSyncHooks()
	if got := config.OnConfigSaved.Len(); got != beforeCfg+1 {
		t.Fatalf("registerSyncHooks: config hooks %d, want %d", got, beforeCfg+1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)

	// The late registration (a Serve after Shutdown) must install nothing.
	srv.registerSyncHooks()
	if got := len(srv.removeSyncHooks); got != 0 {
		t.Fatalf("registerSyncHooks after Shutdown tracked %d hooks, want 0", got)
	}
	if got := config.OnConfigSaved.Len(); got != beforeCfg {
		t.Fatalf("registerSyncHooks after Shutdown left config hook: %d, want %d", got, beforeCfg)
	}
	if got := auth.OnCredentialsSaved.Len(); got != beforeAuth {
		t.Fatalf("registerSyncHooks after Shutdown left auth hook: %d, want %d", got, beforeAuth)
	}
}
