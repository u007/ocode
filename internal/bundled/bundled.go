// Package bundled materializes the skills and plugin agents that ship inside
// the ocode binary so they are available even when no disk copy exists.
//
// Why extraction instead of in-memory fs.FS readers:
// The skill and agent loaders are path-based (they os.ReadDir / os.ReadFile
// real directories). Rather than refactor every reader to accept an fs.FS, we
// extract the embedded tree once per binary version into a version/hash-scoped
// directory under the global data dir, then surface that directory to the
// existing loaders as just another (lowest-precedence) search path. Disk-based
// skills/agents therefore always override the embedded copy by construction.
package bundled

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/version"
)

// EmbeddedSkills is the embedded `skills/` tree (rooted at the repo root, so
// entries look like "skills/<name>/SKILL.md"). Set by main at startup.
var EmbeddedSkills fs.FS

// EmbeddedPlugins is the embedded `.opencode/plugins/` tree (entries look like
// ".opencode/plugins/<name>/plugin.json"). Set by main at startup.
var EmbeddedPlugins fs.FS

// SkillsDir and PluginsDir are the resolved on-disk locations populated by
// EnsureExtracted. Loaders read these; they are empty until extraction runs.
var SkillsDir string

// PluginsDir is the resolved on-disk location of the extracted plugins tree.
var PluginsDir string

// SetEmbeddedSkills registers the embedded skills FS (rooted at repo root).
func SetEmbeddedSkills(f fs.FS) { EmbeddedSkills = f }

// SetEmbeddedPlugins registers the embedded plugins FS (rooted at repo root).
func SetEmbeddedPlugins(f fs.FS) { EmbeddedPlugins = f }

// EnsureExtracted materializes the embedded skills and plugins into a
// version/hash-scoped directory under GlobalDataDir and records the resulting
// paths in SkillsDir/PluginsDir. It is safe to call multiple times; extraction
// runs once per scope (guarded by a marker file). A non-nil error leaves the
// dirs empty so callers simply skip the embedded fallback.
func EnsureExtracted() error {
	scope := scopeKey()
	base, err := paths.GlobalDataDir()
	if err != nil {
		return err
	}
	root := filepath.Join(base, "bundled", scope)
	skillsTarget := filepath.Join(root, "skills")
	pluginsTarget := filepath.Join(root, "plugins")
	marker := filepath.Join(root, ".extracted")

	if _, err := os.Stat(marker); err == nil {
		SkillsDir, PluginsDir = skillsTarget, pluginsTarget
		return nil
	}

	if err := os.MkdirAll(skillsTarget, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(pluginsTarget, 0o755); err != nil {
		return err
	}
	if EmbeddedSkills != nil {
		if err := extractFromEmbedded(EmbeddedSkills, skillsTarget, pluginsTarget); err != nil {
			return err
		}
	}
	// When both embeds point at the same FS (desktop bundles everything under
	// one directory), avoid extracting twice.
	if EmbeddedPlugins != nil && EmbeddedPlugins != EmbeddedSkills {
		if err := extractFromEmbedded(EmbeddedPlugins, skillsTarget, pluginsTarget); err != nil {
			return err
		}
	}
	if err := os.WriteFile(marker, []byte(scope), 0o644); err != nil {
		return err
	}
	SkillsDir, PluginsDir = skillsTarget, pluginsTarget
	return nil
}

// extractFromEmbedded walks an embedded FS and writes any skills/ or
// .opencode/plugins/ entries into the matching target directory. It is
// shape-agnostic: it handles both the root build (paths like "skills/...")
// and the desktop build (paths like "embedded-assets/skills/...").
func extractFromEmbedded(fsys fs.FS, skillsTarget, pluginsTarget string) error {
	return fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(p, "embedded-assets/")
		var dst string
		switch {
		case strings.HasPrefix(rel, "skills/"):
			dst = filepath.Join(skillsTarget, strings.TrimPrefix(rel, "skills/"))
		case strings.HasPrefix(rel, ".opencode/plugins/"):
			dst = filepath.Join(pluginsTarget, strings.TrimPrefix(rel, ".opencode/plugins/"))
		default:
			return nil
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
}

// scopeKey returns a stable, content-sensitive key for the extraction dir:
// "<version>-<hash12>". The version segment makes different releases naturally
// separate; the hash segment invalidates the cache when the embedded content
// changes within the same version (e.g. local dev rebuilds).
func scopeKey() string {
	h := sha256.New()
	if EmbeddedSkills != nil {
		hashFS(h, EmbeddedSkills)
	}
	if EmbeddedPlugins != nil {
		hashFS(h, EmbeddedPlugins)
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if len(sum) > 12 {
		sum = sum[:12]
	}
	v := version.Version
	if v == "" {
		v = "dev"
	}
	return fmt.Sprintf("%s-%s", sanitize(v), sum)
}

func sanitize(s string) string {
	return strings.NewReplacer("/", "_", " ", "_", ":", "_", "\\", "_").Replace(s)
}

func hashFS(h interface{ Write([]byte) (int, error) }, fsys fs.FS) {
	_ = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		h.Write([]byte(p))
		if b, e := fs.ReadFile(fsys, p); e == nil {
			h.Write(b)
		}
		return nil
	})
}
