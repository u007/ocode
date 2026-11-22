package redact

import (
	"path/filepath"
	"runtime"
	"strings"
)

// loginStateDirSegments is the browser login-state directory as two consecutive
// path segments: <paths.GlobalDataDir()>/browse/login-states. It is the twin of
// internal/agent (loginStateSegment in permissions.go) and of internal/boxsession
// (loginStateDirName, DefaultLoginStateDir). This package cannot import internal/agent,
// so the agent package's TestLoginStateRedactTwinAgrees pins the two together.
var loginStateDirSegments = [2]string{"browse", "login-states"}

// caseFoldLoginStates reports whether login-state segments match ignoring case.
// It is the twin of loginStateCaseFold in internal/agent/permissions.go: APFS
// (the macOS default) and NTFS fold case, Linux does not.
func caseFoldLoginStates() bool {
	return runtime.GOOS == "darwin" || runtime.GOOS == "windows"
}

// isLoginStateSegments reports whether path names the login-state directory or
// anything beneath it, by path segments: browse and login-states as consecutive
// segments. A sibling such as login-states-old is a different segment and does
// not match. fold lowers both sides before comparing, as the agent twin does.
func isLoginStateSegments(path string, fold bool) bool {
	segs := strings.Split(filepath.ToSlash(path), "/")
	eq := func(got, want string) bool {
		if fold {
			return strings.ToLower(got) == strings.ToLower(want)
		}
		return got == want
	}
	for i := 0; i+1 < len(segs); i++ {
		if eq(segs[i], loginStateDirSegments[0]) && eq(segs[i+1], loginStateDirSegments[1]) {
			return true
		}
	}
	return false
}

// sensitiveFilePatterns returns true when the given file path matches a
// sensitive-file pattern. The check is case-insensitive for the basename.
// Patterns are sorted alphabetically for maintainability:
//
//	.env, .env.*
//	*.pem, *.key
//	id_rsa*, id_dsa*, id_ecdsa*, id_ed25519*
//	*.pfx, *.p12
//	.npmrc, .netrc
//	*credentials*, secrets.*, .pgpass
//	auth.json, ocodeconfig.json, opencode.json
//	browse/login-states/** (as consecutive path segments)
func IsSensitiveFile(path string) bool {
	if path == "" {
		return false
	}
	base := strings.ToLower(filepath.Base(path))

	// .env and .env.*
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		return true
	}

	// *.pem, *.key
	if strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".key") {
		return true
	}

	// id_rsa*, id_dsa*, id_ecdsa*, id_ed25519*
	if strings.HasPrefix(base, "id_rsa") ||
		strings.HasPrefix(base, "id_dsa") ||
		strings.HasPrefix(base, "id_ecdsa") ||
		strings.HasPrefix(base, "id_ed25519") {
		return true
	}

	// *.pfx, *.p12
	if strings.HasSuffix(base, ".pfx") || strings.HasSuffix(base, ".p12") {
		return true
	}

	// .npmrc, .netrc
	if base == ".npmrc" || base == ".netrc" {
		return true
	}

	// *credentials*
	if strings.Contains(base, "credentials") {
		return true
	}

	// secrets.*
	if strings.HasPrefix(base, "secrets.") {
		return true
	}

	// .pgpass
	if base == ".pgpass" {
		return true
	}

	// ocode's own config/credential stores. The global config and data dirs are
	// pre-authorized roots in the permission layer (auto-allowed without a
	// prompt), so reads of these files via agent tools must still route through
	// secret masking — the directory allow-list intentionally does not carry a
	// name-based sensitive-path veto that would re-introduce prompts.
	if base == "auth.json" || base == "ocodeconfig.json" || base == "opencode.json" {
		return true
	}

	// The browser login-state directory (cookies and storage of a remote browser).
	// It is treated exactly as auth.json is: the same tool-result branch masks it.
	// Matched by segments, not basename, because the directory is the unit.
	if isLoginStateSegments(path, caseFoldLoginStates()) {
		return true
	}

	return false
}
