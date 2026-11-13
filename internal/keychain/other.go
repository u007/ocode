//go:build !darwin && !linux && !windows

package keychain

func newPlatformStore(opts Options) (Store, bool) {
	return nil, false
}
