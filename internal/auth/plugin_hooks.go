package auth

import (
	"context"
	"sync"
)

// RefreshHook refreshes a stored OAuth credential for a single provider using
// provider-specific logic (e.g. Grok's cookie-based x.com SSO refresh). It
// returns the (possibly refreshed) credential and whether this provider was
// handled by a registered hook. When handled is false the caller falls back to
// the generic refresh-token path.
type RefreshHook func(ctx context.Context, cred Credential) (Credential, bool)

// ProbeHook verifies a stored credential for a single provider using
// provider-specific logic. It returns nil on success or an error describing the
// failure.
type ProbeHook func(ctx context.Context, cred Credential) error

var (
	refreshHooksMu sync.RWMutex
	refreshHooks   = map[string]RefreshHook{}

	probeHooksMu sync.RWMutex
	probeHooks   = map[string]ProbeHook{}
)

// RegisterRefreshHook lets a provider plugin supply a custom credential refresh
// implementation. auth.RefreshIfExpiring delegates to it when present, so
// provider-specific refresh logic lives with the plugin instead of in the
// shared auth layer (which previously special-cased individual providers by
// ID).
func RegisterRefreshHook(id string, fn RefreshHook) {
	refreshHooksMu.Lock()
	defer refreshHooksMu.Unlock()
	refreshHooks[id] = fn
}

// RegisterProbeHook lets a provider plugin supply a custom credential probe
// implementation. auth.TestCredential delegates to it when present.
func RegisterProbeHook(id string, fn ProbeHook) {
	probeHooksMu.Lock()
	defer probeHooksMu.Unlock()
	probeHooks[id] = fn
}

// refreshViaHook runs the registered refresh hook for id, if any.
func refreshViaHook(id string, cred Credential) (Credential, bool) {
	refreshHooksMu.RLock()
	fn, ok := refreshHooks[id]
	refreshHooksMu.RUnlock()
	if !ok {
		return cred, false
	}
	return fn(context.Background(), cred)
}

// probeViaHook runs the registered probe hook for id, if any. The second
// return value reports whether a hook was registered (handled); when false the
// caller falls back to the generic probe path.
func probeViaHook(id string, cred Credential) (bool, error) {
	probeHooksMu.RLock()
	fn, ok := probeHooks[id]
	probeHooksMu.RUnlock()
	if !ok {
		return false, nil
	}
	return true, fn(context.Background(), cred)
}
