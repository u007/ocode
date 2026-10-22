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
func dbEnvLookup(projectRoot string) func(string) (string, bool) {
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
				// database than the user expects. Log it (without the values)
				// and continue with the process environment only; the
				// connection then fails with an explicit "not set" error
				// naming the variable.
				log.Printf("dbconnect: read env files in %s: %v", projectRoot, err)
				loadError = err
				fileVals = map[string]string{}
				return
			}
			fileVals = values
		})
	}
	return func(name string) (string, bool) {
		load()
		if loadError != nil {
			// A file that could not be read contributes nothing. The caller
			// reports the missing variable, which names the variable the
			// user needs to set.
			if v, ok := envfile.Lookup(name, nil); ok {
				return v, true
			}
			return "", false
		}
		return envfile.Lookup(name, fileVals)
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
	expanded, err := dbconnect.ExpandEnv(template, dbEnvLookup(projectRoot))
	if err != nil {
		// The error names the missing variable but never its value, so it is
		// safe to log and to show the user.
		return "", err
	}
	if _, err := dbconnect.ParseURL(expanded); err != nil {
		// ParseURL's error text never contains the URL (see its doc comment),
		// so the password cannot leak through this message.
		return "", fmt.Errorf("resolved connection url is not a valid postgres url: %w", err)
	}
	return expanded, nil
}
