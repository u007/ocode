package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/u007/ocode/internal/redact"
	"github.com/u007/ocode/internal/tool"
)

// toolDigestSensitiveIgnoreGlobs are the ripgrep excludes the explainer's grep
// injects into every run. They mirror the name rules of redact.IsSensitiveFile,
// isSecretMaterialPath, isRepoMetadataPath and isLoginStatePath. Globs without
// a slash match at any depth; the directory rules are anchored with **. The
// search policy's Blocked callback still drops any hit a glob misses.
var toolDigestSensitiveIgnoreGlobs = []string{
	".env", ".env.*",
	"*.pem", "*.key", "*.p12", "*.pfx", "*.secrets",
	"id_rsa*", "id_dsa*", "id_ecdsa*", "id_ed25519*",
	".npmrc", ".netrc", ".pypirc", ".pgpass",
	"*credentials*", "secrets.*",
	"auth.json", "ocodeconfig.json", "opencode.json",
	".git",
	"**/.github/workflows/**",
	"**/.aws/**",
	"**/browse/login-states/**",
}

// canonicalProjectRoot resolves workDir to its real absolute path. Every
// containment check compares against this form, so a symlinked project root
// (for example /var on macOS) does not look like an escape.
func canonicalProjectRoot(workDir string) (string, error) {
	if workDir == "" {
		return "", errors.New("explainer file tools: the project root is unknown")
	}
	abs, err := filepath.Abs(workDir)
	if err != nil {
		return "", fmt.Errorf("explainer file tools: project root %q: %w", workDir, err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("explainer file tools: project root %q: %w", workDir, err)
	}
	return real, nil
}

// validateProjectPath resolves p against the project root and returns its
// canonical absolute form. It walks p one component at a time and resolves each
// existing symlink, so a link cannot make a later ".." look lexically inside.
// A component that does not exist yet is joined lexically under the deepest
// real ancestor. Any result outside the canonical root is an error.
func validateProjectPath(workDir, p string) (string, error) {
	root, err := canonicalProjectRoot(workDir)
	if err != nil {
		return "", err
	}
	if p == "" {
		return root, nil
	}
	cur := root
	rest := p
	if filepath.IsAbs(p) {
		vol := filepath.VolumeName(p)
		cur = vol + string(filepath.Separator)
		rest = p[len(vol):]
	}
	missing := false
	for _, c := range strings.FieldsFunc(rest, func(r rune) bool {
		return r == '/' || r == filepath.Separator
	}) {
		switch c {
		case ".":
			continue
		case "..":
			cur = filepath.Dir(cur)
			continue
		}
		next := filepath.Join(cur, c)
		if !missing {
			info, lerr := os.Lstat(next)
			switch {
			case errors.Is(lerr, fs.ErrNotExist):
				missing = true
			case lerr != nil:
				return "", fmt.Errorf("explainer file tools: %q: %w", p, lerr)
			case info.Mode()&fs.ModeSymlink != 0:
				target, serr := filepath.EvalSymlinks(next)
				if serr != nil {
					return "", fmt.Errorf("explainer file tools: %q: %w", p, serr)
				}
				next = target
			}
		}
		cur = next
	}
	if !pathWithinProjectRoot(cur, root) {
		return "", fmt.Errorf("explainer file tools: %q is outside the project", p)
	}
	return cur, nil
}

// pathWithinProjectRoot reports whether canonical child is root or beneath it.
func pathWithinProjectRoot(child, root string) bool {
	rel, err := filepath.Rel(root, child)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// sensitiveNamePath applies the four sensitive-path predicates to one path.
// workDir resolves a relative path for the login-state check.
func sensitiveNamePath(workDir, p string) bool {
	return redact.IsSensitiveFile(p) ||
		isSecretMaterialPath(p) ||
		isRepoMetadataPath(p) ||
		isLoginStatePath(p, workDir)
}

// sensitivePathFilter reports whether the explainer must not read, list or
// search p. It checks the supplied path and its canonical form. A path that
// does not resolve inside the project is blocked too.
func sensitivePathFilter(workDir, p string) bool {
	if sensitiveNamePath(workDir, p) {
		return true
	}
	canon, err := validateProjectPath(workDir, p)
	if err != nil {
		return true
	}
	return sensitiveNamePath(workDir, canon)
}

// specialFilePath reports whether canon is an existing entry that is neither a
// regular file nor a directory: a FIFO, device, or socket. Opening such an
// entry can block forever, so it is refused before any open.
func specialFilePath(canon string) bool {
	info, err := os.Stat(canon)
	if err != nil {
		return false
	}
	return !info.IsDir() && !info.Mode().IsRegular()
}

// toolDigestSearchBlocked is the Blocked callback the explainer's walks use. It
// resolves each entry once and blocks it when the entry escapes the project,
// names a sensitive path, or is a special file.
func toolDigestSearchBlocked(workDir string) func(string) bool {
	return func(p string) bool {
		canon, err := validateProjectPath(workDir, p)
		if err != nil {
			return true
		}
		return sensitiveNamePath(workDir, p) ||
			sensitiveNamePath(workDir, canon) ||
			specialFilePath(canon)
	}
}

// toolDigestFileTool wraps one of read, glob, grep or list for the explainer.
// It validates the path parameter, refuses sensitive targets, rewrites the path
// to its canonical project-relative form, and runs the inner tool under the
// project root with the search policy. read refuses a sensitive path outright;
// the search tools instead filter their walks.
type toolDigestFileTool struct {
	inner   tool.Tool
	workDir string
	read    bool
}

func (w *toolDigestFileTool) Name() string                       { return w.inner.Name() }
func (w *toolDigestFileTool) Description() string                { return w.inner.Description() }
func (w *toolDigestFileTool) Definition() map[string]interface{} { return w.inner.Definition() }
func (w *toolDigestFileTool) Parallel() bool                     { return w.inner.Parallel() }

func (w *toolDigestFileTool) Execute(args json.RawMessage) (string, error) {
	return w.ExecuteCtx(context.Background(), args)
}

// ExecuteCtx validates the call and runs the inner tool. The model's other
// fields pass through unchanged. Only path is rewritten, and the search policy
// travels on ctx, so the model cannot drop its exclusions.
func (w *toolDigestFileTool) ExecuteCtx(ctx context.Context, args json.RawMessage) (string, error) {
	root, err := canonicalProjectRoot(w.workDir)
	if err != nil {
		return "", err
	}
	var params map[string]json.RawMessage
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("%s: invalid arguments: %w", w.Name(), err)
	}
	if raw, ok := params["path"]; ok {
		var p string
		if err := json.Unmarshal(raw, &p); err != nil {
			return "", fmt.Errorf("%s: invalid path: %w", w.Name(), err)
		}
		if p != "" {
			canon, err := validateProjectPath(root, p)
			if err != nil {
				return "", fmt.Errorf("%s: %w", w.Name(), err)
			}
			if sensitiveNamePath(root, p) || sensitiveNamePath(root, canon) {
				return "", fmt.Errorf("%s: %q is a sensitive path and is not available to the explainer", w.Name(), p)
			}
			if specialFilePath(canon) {
				return "", fmt.Errorf("%s: %q is not a regular file or directory", w.Name(), p)
			}
			rel, err := filepath.Rel(root, canon)
			if err != nil {
				return "", fmt.Errorf("%s: %q: %w", w.Name(), p, err)
			}
			if rel == "." {
				rel = ""
			}
			rewritten, err := json.Marshal(rel)
			if err != nil {
				return "", fmt.Errorf("%s: %w", w.Name(), err)
			}
			params["path"] = rewritten
			if args, err = json.Marshal(params); err != nil {
				return "", fmt.Errorf("%s: %w", w.Name(), err)
			}
		}
	}

	ctx = tool.WithWorkDir(ctx, root)
	if !w.read {
		ctx = tool.WithSearchPolicy(ctx, tool.SearchPolicy{
			Blocked:     toolDigestSearchBlocked(root),
			IgnoreGlobs: toolDigestSensitiveIgnoreGlobs,
		})
	}
	if ct, ok := w.inner.(interface {
		ExecuteCtx(context.Context, json.RawMessage) (string, error)
	}); ok {
		return ct.ExecuteCtx(ctx, args)
	}
	return w.inner.Execute(args)
}

// newToolDigestFileTools returns the four wrapped file tools for snap's project.
func newToolDigestFileTools(snap toolDigestSnapshot) []tool.Tool {
	return []tool.Tool{
		&toolDigestFileTool{inner: tool.ReadTool{}, workDir: snap.WorkDir, read: true},
		&toolDigestFileTool{inner: tool.GlobTool{}, workDir: snap.WorkDir},
		&toolDigestFileTool{inner: tool.GrepTool{}, workDir: snap.WorkDir},
		&toolDigestFileTool{inner: tool.ListTool{}, workDir: snap.WorkDir},
	}
}

// registerToolDigestFileTools adds the wrapped read, glob, grep and list tools
// to box. A non-empty project root that cannot be resolved is a registration
// error. An empty root registers the tools, and every call then refuses.
func registerToolDigestFileTools(box *toolDigestToolbox, snap toolDigestSnapshot) error {
	if snap.WorkDir != "" {
		if _, err := canonicalProjectRoot(snap.WorkDir); err != nil {
			return err
		}
	}
	for _, t := range newToolDigestFileTools(snap) {
		box.add(t)
	}
	return nil
}
