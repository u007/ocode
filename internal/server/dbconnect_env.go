package server

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/u007/ocode/internal/dbconnect"
	"github.com/u007/ocode/internal/envfile"
)

// dbEnvLookup builds the lookup used to resolve an environment template.
//
// By default values come from the process environment first, then from the
// dotenv files in projectRoot (.env, then .env.local). When envFile is set,
// ONLY that named file is read and it wins over the process environment: a
// connection that selects .env.staging must not silently resolve to a value
// exported in the shell.
//
// A projectRoot of "" means "no project": only the process environment is
// consulted. That is the right behavior for a connection used outside any
// project.
//
// The dotenv files are read LAZILY, on the first variable actually looked
// up, and then cached for the lifetime of the returned lookup. Building the
// lookup must stay cheap: a request whose value contains no variable
// reference must not touch the filesystem.
//
// The second return loads the files itself if needed and reports why they
// could not be read. A load failure contributes no file values, so EVERY key then
// looks unset — the caller MUST consult envErr before reporting "environment
// variable X is not set", or one malformed line elsewhere in the file sends the
// user hunting for a variable that is right there.
func dbEnvLookup(projectRoot, envFile string) (func(string) (string, bool), func() error) {
	return dbLookup(projectRoot, envFile, true)
}

// dbFileEnvLookup is dbEnvLookup with the process environment removed.
//
// A named envFile is read exactly as dbEnvLookup reads it: the file is
// authoritative and the process environment was never consulted. With no
// envFile, values come from .env and .env.local in projectRoot ONLY. A key
// they do not define is unset, even when the server process exports it.
//
// It exists for a connection a project's .ocode/db.json ships. The repo chooses
// the URL template, so it must not be able to name a server secret such as
// "${SECRET}" and have it dialled out to an attacker-chosen host. Global
// connections, which the user saved on this machine, keep dbEnvLookup.
//
// A load failure is reported by the second return, as in dbEnvLookup.
func dbFileEnvLookup(projectRoot, envFile string) (func(string) (string, bool), func() error) {
	return dbLookup(projectRoot, envFile, false)
}

// dbLookup is the shared body of dbEnvLookup and dbFileEnvLookup.
// useProcessEnv selects whether the process environment is a fallback for an
// unnamed envFile. Every other rule is the same for both.
func dbLookup(projectRoot, envFile string, useProcessEnv bool) (func(string) (string, bool), func() error) {
	var (
		once      sync.Once
		fileVals  map[string]string
		loadError error
	)
	named := strings.TrimSpace(envFile) != ""
	load := func() {
		once.Do(func() {
			fileVals = map[string]string{}
			name, err := envfile.NormalizeName(envFile)
			if err != nil {
				loadError = err
				return
			}
			if projectRoot == "" {
				if name != "" {
					loadError = fmt.Errorf("an env file needs a project")
				}
				return
			}
			if name == "" {
				values, err := envfile.Load(projectRoot)
				if err != nil {
					// A malformed .env must not silently resolve to a different
					// database than the user expects. Log it (without the values),
					// contribute no file values, and let the caller name the real
					// cause instead of a phantom missing variable.
					log.Printf("dbconnect: read env files in %s: %v", projectRoot, err)
					loadError = err
					return
				}
				fileVals = values
				return
			}
			values, err := envfile.LoadNamed(projectRoot, name)
			if err != nil {
				log.Printf("dbconnect: read env file %s in %s: %v", name, projectRoot, err)
				loadError = err
				return
			}
			fileVals = values
		})
	}
	lookup := func(name string) (string, bool) {
		load()
		if loadError != nil {
			// A named file that could not be read must NOT fall back to the
			// process environment: that would connect to the wrong database.
			// A file-only lookup never falls back either.
			if named || !useProcessEnv {
				return "", false
			}
			return envfile.Lookup(name, nil)
		}
		if named || !useProcessEnv {
			// The named file is authoritative. A key it does not define is
			// unset even if the shell exports it: falling back would connect
			// a staging connection to the local database. The same holds for
			// a file-only lookup, which has no named file but no fallback.
			v, ok := fileVals[name]
			return v, ok
		}
		return envfile.Lookup(name, fileVals)
	}
	return lookup, func() error {
		load()
		return loadError
	}
}

// dbTunnelNeedsEnvFileMsg is shown when a tunnel uses ${VAR} but the connection
// names no env file. It never reaches the process environment instead.
const dbTunnelNeedsEnvFileMsg = "SSH tunnel fields that use ${VAR} need an env file; select one for this connection"

// dbTunnelEnvLookup builds the lookup for a tunnel's ${VAR} references.
//
// Tunnel references read the named env file ONLY. The process environment is
// never consulted: a tunnel host of "${SOME_SECRET}" would otherwise send a
// server-side secret out as an SSH hostname. The URL template keeps
// dbEnvLookup's precedence; this helper does not change it.
//
// The named-file branch IS dbEnvLookup's (it already never falls back to the
// process environment when a file is selected), so the file is read by the same
// code. An empty envFile resolves nothing and reports dbTunnelNeedsEnvFileMsg.
func dbTunnelEnvLookup(projectRoot, envFile string) (func(string) (string, bool), func() error) {
	if strings.TrimSpace(envFile) == "" {
		return func(string) (string, bool) { return "", false },
			func() error { return errors.New(dbTunnelNeedsEnvFileMsg) }
	}
	return dbEnvLookup(projectRoot, envFile)
}

// dbResolveTemplate resolves a connection's environment template into a
// usable connection URL.
//
// The template is expanded against the environment of projectRoot, and the
// RESULT is validated as a Postgres URL. Validating after expansion is
// required: a template cannot be validated before it is resolved, because
// "${DATABASE_URL}" is not a URL.
//
// The resolved URL is a secret. It is returned to the caller for in-memory
// use only and is never logged, returned to the client, or written to disk.
func dbResolveTemplate(template, projectRoot, envFile string) (string, error) {
	lookup, envErr := dbEnvLookup(projectRoot, envFile)
	return dbResolveTemplateWithLookup(template, lookup, envErr)
}

// dbResolveTemplateWithLookup shares one lookup between a connection's URL and
// its tunnel, so both see the same env file contents.
func dbResolveTemplateWithLookup(template string, lookup func(string) (string, bool), envErr func() error) (string, error) {
	expanded, err := dbconnect.ExpandEnv(template, lookup)
	if err != nil {
		// A dotenv file that cannot be read (a duplicate key, an unterminated
		// quote, a permissions problem) voids EVERY value in it, so a template
		// then fails as if its variable were never set. Name the real cause:
		// "DATABASE_URL is not set" sends the user looking in the one place the
		// value already is. The load error names a path, line and key, never a
		// value, so it is safe to show and to log.
		if loadErr := envErr(); loadErr != nil {
			return "", fmt.Errorf("cannot read the project's env files: %w", loadErr)
		}
		// Otherwise the error names the missing variable, never its value, so
		// it is safe to log and to show the user.
		return "", err
	}
	if _, err := dbconnect.ParseURL(expanded); err != nil {
		// ParseURL's error text never contains the URL (see its doc comment),
		// so the password cannot leak through this message.
		return "", fmt.Errorf("resolved connection url is not a valid postgres url: %w", err)
	}
	return expanded, nil
}
