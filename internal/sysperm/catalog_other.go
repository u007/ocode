//go:build !darwin

package sysperm

import (
	"context"
	"runtime"
)

// builtinEntries returns an informational catalog on platforms with no
// per-application consent gate. The settings section still renders so the
// cross-platform UI has a consistent shape.
func builtinEntries() []Entry { return genericEntries(runtime.GOOS) }

// detectEntry is always not_required off macOS.
func detectEntry(context.Context, Entry) Status { return StatusNotRequired }

// requestEntry is informational off macOS; there is nothing to request.
func requestEntry(_ context.Context, e Entry) RequestResult {
	return RequestResult{
		ID:      e.ID,
		Status:  StatusNotRequired,
		Message: "No explicit OS permission is required on this platform.",
	}
}

// PlatformSupported reports whether the OS has real per-application grants.
func PlatformSupported() bool { return false }
