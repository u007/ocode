package config

// AutoGrantKeyEqual reports whether two grants have the same identity key
// (kind, language, source mode, command, path, cwd, hashes).
func AutoGrantKeyEqual(a, b AutoGrant) bool {
	return autoGrantKey(a) == autoGrantKey(b)
}
