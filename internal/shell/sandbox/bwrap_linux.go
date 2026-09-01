//go:build linux

package sandbox

import "os"

// bwrapUsable reports whether the trusted bubblewrap binary exists and is
// executable. Trusted absolute path only — never a $PATH lookup (PATH
// shadowing could substitute a hostile binary).
func bwrapUsable() bool {
	fi, err := os.Stat(bwrapReadOnlyAbs)
	return err == nil && fi.Mode()&0o111 != 0
}
