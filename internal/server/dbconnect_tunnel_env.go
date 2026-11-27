package server

import (
	"fmt"
	"strconv"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/dbconnect"
	"github.com/u007/ocode/internal/remote"
)

// dbTunnelHasEnv reports whether any tunnel field uses a "${VAR}" reference.
// Ports use companion *_env fields because the numeric fields must stay numbers
// for older binaries; string fields hold "${VAR}" directly. Only a value that
// IsTemplate counts: a literal string, including one with a bare "$NAME", is
// not an env reference.
func dbTunnelHasEnv(t *config.DBTunnel) bool {
	if t == nil {
		return false
	}
	return dbconnect.IsTemplate(t.Host) ||
		dbconnect.IsTemplate(t.RemoteHost) ||
		dbconnect.IsTemplate(t.IdentityFile) ||
		dbconnect.IsTemplate(t.AuthMethod) ||
		dbconnect.IsTemplate(t.PortEnv) ||
		dbconnect.IsTemplate(t.LocalPortEnv) ||
		dbconnect.IsTemplate(t.RemotePortEnv)
}

// dbConnectionNeedsProject reports whether a connection reads files in a
// project. It must count tunnel env references too: a sealed URL with a
// templated tunnel still needs the project's env file.
func dbConnectionNeedsProject(c config.DBConnection) bool {
	return c.EnvTemplate != "" || c.EnvFile != "" || dbTunnelHasEnv(c.Tunnel)
}

// dbResolveTunnel expands a tunnel descriptor's env references and validates
// the RESULT, so a value from .env.staging cannot bypass the host, path or
// port checks. The password is deliberately excluded: it is a sealed envelope
// that must never be expanded.
//
// The returned descriptor has the *_env fields cleared, so the cached grant
// holds only resolved values that ssh can use directly.
func dbResolveTunnel(t *config.DBTunnel, lookup func(string) (string, bool), envErr func() error) (*config.DBTunnel, error) {
	if t == nil {
		return nil, nil
	}
	if dbTunnelHasEnv(t) {
		// A malformed or unreadable env file voids EVERY value, so no field
		// may be accepted as literal while the file is broken.
		if err := envErr(); err != nil {
			return nil, fmt.Errorf("cannot read the project's env files: %w", err)
		}
	}
	resolved := *t
	strings := []struct {
		name  string
		dst   *string
		value string
	}{
		{"host", &resolved.Host, t.Host},
		{"remote_host", &resolved.RemoteHost, t.RemoteHost},
		{"identity_file", &resolved.IdentityFile, t.IdentityFile},
		{"auth_method", &resolved.AuthMethod, t.AuthMethod},
	}
	for _, f := range strings {
		// A literal value was copied verbatim above. Only a braced reference
		// makes a value a template, so a literal path such as
		// "/Users/me/.ssh/id_$work" is never run through ExpandEnv.
		if !dbconnect.IsTemplate(f.value) {
			continue
		}
		value, err := dbconnect.ExpandEnv(f.value, lookup)
		if err != nil {
			// The error names the variable, never its value.
			return nil, fmt.Errorf("tunnel %s: %w", f.name, err)
		}
		*f.dst = value
	}
	ports := []struct {
		name      string
		value     string
		dst       *int
		allowZero bool
	}{
		{"port", t.PortEnv, &resolved.Port, false},
		{"local_port", t.LocalPortEnv, &resolved.LocalPort, true},
		{"remote_port", t.RemotePortEnv, &resolved.RemotePort, false},
	}
	for _, p := range ports {
		if p.value == "" {
			continue
		}
		raw := p.value
		if dbconnect.IsTemplate(raw) {
			value, err := dbconnect.ExpandEnv(raw, lookup)
			if err != nil {
				return nil, fmt.Errorf("tunnel %s: %w", p.name, err)
			}
			raw = value
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > 65535 || (n == 0 && !p.allowZero) {
			// The resolved value is deliberately not echoed: a port is not a
			// secret, but the message should name the field and stay short.
			lowest := "1"
			if p.allowZero {
				lowest = "0"
			}
			return nil, fmt.Errorf("tunnel %s must resolve to a port between %s and 65535", p.name, lowest)
		}
		*p.dst = n
	}
	resolved.PortEnv, resolved.LocalPortEnv, resolved.RemotePortEnv = "", "", ""
	if err := remote.ValidateTunnelDescriptor(&resolved); err != nil {
		return nil, fmt.Errorf("invalid ssh tunnel after env expansion: %w", err)
	}
	return &resolved, nil
}
