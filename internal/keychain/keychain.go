package keychain

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/u007/ocode/internal/paths"
)

// ErrNotFound is returned by Store.Get (and Store.Delete) when the requested
// secret does not exist.
var ErrNotFound = errors.New("keychain: not found")

// Store is an OS-native (or encrypted-file fallback) secret store.
type Store interface {
	Get(service, account string) (string, error) // ErrNotFound if absent
	Set(service, account, secret string) error
	Delete(service, account string) error
}

// Options configure a Store created by New.
type Options struct {
	Path           string // encrypted-file path (fallback only)
	MasterPassword string // master password for the encrypted fallback
}

// Option is a functional option for New.
type Option func(*Options)

// WithPath sets the encrypted-file path used by the fallback store.
func WithPath(p string) Option {
	return func(o *Options) { o.Path = p }
}

// WithMasterPassword sets the master password used by the encrypted fallback.
func WithMasterPassword(p string) Option {
	return func(o *Options) { o.MasterPassword = p }
}

// New probes for a native secret-store tool and returns it when available;
// otherwise it returns an encrypted-file fallback store.
func New(opts ...Option) Store {
	o := Options{}
	for _, fn := range opts {
		fn(&o)
	}
	if o.Path == "" {
		if d, err := paths.GlobalDataDir(); err == nil {
			o.Path = filepath.Join(d, "keychain.json")
		} else {
			o.Path = filepath.Join(os.TempDir(), "ocode-keychain.json")
		}
	}
	if s, ok := newPlatformStore(o); ok && s != nil {
		return s
	}
	return newFallbackStore(o)
}

// newPlatformStoreFn is indirected so tests can force native availability
// without depending on which secret-store binaries are installed on the host.
var newPlatformStoreFn = newPlatformStore

// NativeAvailable reports whether an OS-native secret store is present. It
// probes newPlatformStore directly and never constructs the machineID-derived
// fallback store, so callers can refuse to persist secrets when only the
// fallback is available.
func NativeAvailable() bool {
	s, ok := newPlatformStoreFn(Options{})
	return ok && s != nil
}
