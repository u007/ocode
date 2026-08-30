package redact

import (
	"path/filepath"
	"strings"
)

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

	return false
}
