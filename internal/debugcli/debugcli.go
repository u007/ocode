// Package debugcli implements the `ocode debug` subcommand family: small,
// stable introspection utilities for external tooling (review scripts,
// dashboards) that need ocode's project-scoping logic — project slug, data
// directories — without reimplementing the hashing/resolution rules in
// another language.
package debugcli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/u007/ocode/internal/paths"
)

// Run dispatches a `ocode debug <subcommand> [args...]` invocation.
func Run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ocode debug project-slug [path]")
	}
	switch args[0] {
	case "project-slug":
		return runProjectSlug(args[1:])
	default:
		return fmt.Errorf("unknown debug subcommand %q (want: project-slug)", args[0])
	}
}

type projectSlugOutput struct {
	Root          string `json:"root"`
	Slug          string `json:"slug"`
	GlobalDataDir string `json:"globalDataDir"`
	SessionsDir   string `json:"sessionsDir"`
}

// runProjectSlug prints, as JSON, the project root, slug, global data dir,
// and per-project sessions dir ocode would use for the given path (default:
// current directory) — the same values internal/paths computes for the
// agent, so a reviewer script can locate a project's session files without
// re-deriving the slug hash itself.
func runProjectSlug(args []string) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	wd, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", dir, err)
	}

	root := paths.ProjectRoot(wd)
	slug := paths.ProjectSlug(wd)

	globalDataDir, err := paths.GlobalDataDir()
	if err != nil {
		return fmt.Errorf("resolve global data dir: %w", err)
	}
	sessionsDir, err := paths.ProjectSessionsDir(slug)
	if err != nil {
		return fmt.Errorf("resolve sessions dir: %w", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(projectSlugOutput{
		Root:          root,
		Slug:          slug,
		GlobalDataDir: globalDataDir,
		SessionsDir:   sessionsDir,
	})
}
