package server

import (
	"fmt"
	"log"
	"sync"

	"github.com/u007/ocode/internal/dbconnect"
	"github.com/u007/ocode/internal/envfile"
)

// dbEnvLookup builds the lookup used to resolve an environment template.
//
// Values come from the process environment first, then from the dotenv files
// in projectRoot (.env, then .env.local). The precedence is process env >
// .env.local > .env, so a value exported in the shell or injected by CI
// always beats the file, while .env.local still overrides the committed .env.
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
// The second return reports why the dotenv files could not be read, once a
// lookup has run. A load failure contributes no file values, so EVERY key then
// looks unset — the caller MUST consult envErr before reporting "environment
// variable X is not set", or one malformed line elsewhere in the file sends the
// user hunting for a variable that is right there.
func dbEnvLookup(projectRoot string) (func(string) (string, bool), func() error) {
	var (
		once      sync.Once
		fileVals  map[string]string
		loadError error
	)
	load := func() {
		once.Do(func() {
			if projectRoot == "" {
				fileVals = map[string]string{}
				return
			}
			values, err := envfile.Load(projectRoot)
			if err != nil {
				// A malformed .env must not silently resolve to a different
				// database than the user expects. Log it (without the values),
				// contribute no file values, and let the caller name the real
				// cause instead of a phantom missing variable.
				log.Printf("dbconnect: read env files in %s: %v", projectRoot, err)
				loadError = err
				fileVals = map[string]string{}
				return
			}
			fileVals = values
		})
	}
	lookup := func(name string) (string, bool) {
		load()
		if loadError != nil {
			// A file that could not be read contributes nothing; the process
			// environment is still consulted.
			return envfile.Lookup(name, nil)
		}
		return envfile.Lookup(name, fileVals)
	}
	return lookup, func() error {
		load()
		return loadError
	}
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
func dbResolveTemplate(template, projectRoot string) (string, error) {
	lookup, envErr := dbEnvLookup(projectRoot)
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
