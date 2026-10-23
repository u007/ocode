package remote

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// The home-relative files and trees mirrored to the remote host so a remote
// session sees the same skills, agent definitions, CLAUDE.md/AGENTS.md and
// Claude permission settings as the local desktop. This is an ALLOWLIST and it
// is enforced twice: BuildSyncPayload only collects from it, and WritePayload
// refuses any SyncDirHome entry outside it, so a crafted frame cannot write
// anywhere else under the remote $HOME.
var (
	homeSyncFiles = []string{
		".claude/CLAUDE.md",
		".claude/settings.json",
		".config/opencode/AGENTS.md",
		".config/opencode/CLAUDE.md",
		".config/opencode/auto-permission-prompt.md",
		".config/opencode/auto-permission-prompt.local.md",
		".config/opencode/antigravity-accounts.json",
	}
	homeSyncTrees = []string{
		".claude/skills",
		".claude/agents",
		".config/opencode/skills",
		".config/opencode/agents",
		".config/opencode/commands",
		".agents/skills",
	}
)

const (
	// maxHomeSyncFileBytes skips oversized files (bundled binaries, datasets).
	maxHomeSyncFileBytes = 1 << 20
	// maxHomeSyncTotalBytes keeps the base64-inflated frame under
	// maxSyncPayloadBytes. Exceeding it is an error, never a silent partial sync.
	maxHomeSyncTotalBytes = 16 << 20
)

// claudeSettingsSyncKeys are the only top-level keys of ~/.claude/settings.json
// that cross machines. Everything else (env, apiKeyHelper, hooks, statusLine,
// credential helpers) is either a secret or describes the local host.
var claudeSettingsSyncKeys = map[string]bool{
	"permissions":    true,
	"model":          true,
	"enabledPlugins": true,
}

// skippedHomeSyncDirs are never descended into.
var skippedHomeSyncDirs = map[string]bool{".git": true, "node_modules": true}

// allowedHomeSyncPath reports whether rel (slash-separated, relative to $HOME)
// is something the sync may carry.
func allowedHomeSyncPath(rel string) bool {
	if rel == "" || strings.Contains(rel, `\`) || path.IsAbs(rel) || path.Clean(rel) != rel {
		return false
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == ".." || seg == "" {
			return false
		}
	}
	for _, f := range homeSyncFiles {
		if rel == f {
			return true
		}
	}
	for _, tree := range homeSyncTrees {
		if strings.HasPrefix(rel, tree+"/") {
			return !isSecretFileName(path.Base(rel))
		}
	}
	return false
}

func isSecretFileName(base string) bool {
	lower := strings.ToLower(base)
	if strings.HasPrefix(lower, ".env") {
		return true
	}
	switch filepath.Ext(lower) {
	case ".pem", ".key", ".p12", ".pfx":
		return true
	}
	return false
}

// filterClaudeSettings keeps only claudeSettingsSyncKeys. Unparseable content is
// an error: shipping an unfiltered settings.json could leak its env block.
func filterClaudeSettings(content []byte) ([]byte, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(content, &doc); err != nil {
		return nil, fmt.Errorf("parse ~/.claude/settings.json for sync: %w", err)
	}
	kept := map[string]json.RawMessage{}
	for k, v := range doc {
		if claudeSettingsSyncKeys[k] {
			kept[k] = v
		}
	}
	out, err := json.MarshalIndent(kept, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// deniedLinkTargets are home-relative prefixes a symlink may never resolve into:
// following a link from a skills dir to one of these would ship credentials.
var deniedLinkTargets = []string{".ssh", ".aws", ".gnupg", ".kube", ".docker", ".config/gcloud", ".config/gh"}

const maxHomeSyncDepth = 8

// resolveSyncTarget follows symlinks for full and returns the real path, or ok
// false when the target is missing, escapes homeReal, or lands in a denied
// credential directory. Content (and so the payload hash) comes from the target,
// which is what makes a link's edits count as a change.
func resolveSyncTarget(homeReal, full string) (string, bool) {
	real, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(homeReal, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	slash := filepath.ToSlash(rel)
	for _, d := range deniedLinkTargets {
		if slash == d || strings.HasPrefix(slash, d+"/") {
			return "", false
		}
	}
	return real, true
}

// collectHomeSyncFiles gathers the allowlisted files under home in a
// deterministic (sorted) order so PayloadHash is stable across builds. Symlinked
// files and directories are followed (see resolveSyncTarget); Name stays the
// link's path so the remote layout mirrors the local one.
func collectHomeSyncFiles(home string) ([]SyncFile, error) {
	homeReal, err := filepath.EvalSymlinks(home)
	if err != nil {
		return nil, fmt.Errorf("resolve home %s: %w", home, err)
	}
	var out []SyncFile
	total := 0
	add := func(rel string, content []byte, mode fs.FileMode) error {
		total += len(content)
		if total > maxHomeSyncTotalBytes {
			return fmt.Errorf("home sync exceeds %d bytes; trim ~/.claude or ~/.config/opencode skills/agents", maxHomeSyncTotalBytes)
		}
		out = append(out, SyncFile{Dir: SyncDirHome, Name: rel, Content: content, Mode: uint32(mode.Perm())})
		return nil
	}
	// addFile reads rel (a link path under home) through its resolved target.
	addFile := func(rel string) error {
		real, ok := resolveSyncTarget(homeReal, filepath.Join(home, filepath.FromSlash(rel)))
		if !ok {
			return nil
		}
		info, err := os.Stat(real)
		if err != nil {
			return fmt.Errorf("stat %s: %w", real, err)
		}
		if !info.Mode().IsRegular() || info.Size() > maxHomeSyncFileBytes {
			return nil
		}
		content, err := os.ReadFile(real)
		if err != nil {
			return fmt.Errorf("read %s: %w", real, err)
		}
		if rel == ".claude/settings.json" {
			if content, err = filterClaudeSettings(content); err != nil {
				return err
			}
		}
		return add(rel, content, info.Mode())
	}

	for _, rel := range homeSyncFiles {
		if _, err := os.Lstat(filepath.Join(home, filepath.FromSlash(rel))); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if err := addFile(rel); err != nil {
			return nil, err
		}
	}

	// walk lists regular files under the directory at rel, descending into
	// symlinked directories once per real path (cycle guard) up to a depth cap.
	var walk func(rel string, visited map[string]bool, depth int) ([]string, error)
	walk = func(rel string, visited map[string]bool, depth int) ([]string, error) {
		if depth > maxHomeSyncDepth {
			return nil, nil
		}
		realDir, ok := resolveSyncTarget(homeReal, filepath.Join(home, filepath.FromSlash(rel)))
		if !ok || visited[realDir] {
			return nil, nil
		}
		visited[realDir] = true
		defer delete(visited, realDir) // a sibling may legitimately link the same dir
		entries, err := os.ReadDir(realDir)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", realDir, err)
		}
		var files []string
		for _, e := range entries {
			childRel := rel + "/" + e.Name()
			real, ok := resolveSyncTarget(homeReal, filepath.Join(realDir, e.Name()))
			if !ok {
				continue
			}
			info, err := os.Stat(real)
			if err != nil {
				return nil, fmt.Errorf("stat %s: %w", real, err)
			}
			switch {
			case info.IsDir():
				if skippedHomeSyncDirs[e.Name()] {
					continue
				}
				sub, err := walk(childRel, visited, depth+1)
				if err != nil {
					return nil, err
				}
				files = append(files, sub...)
			case info.Mode().IsRegular():
				files = append(files, childRel)
			}
		}
		return files, nil
	}

	for _, tree := range homeSyncTrees {
		if _, err := os.Lstat(filepath.Join(home, filepath.FromSlash(tree))); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		rels, err := walk(tree, map[string]bool{}, 0)
		if err != nil {
			return nil, err
		}
		sort.Strings(rels)
		for _, rel := range rels {
			if !allowedHomeSyncPath(rel) {
				continue
			}
			if err := addFile(rel); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// homeFileMode keeps the owner-exec bit (skill scripts must stay runnable) and
// otherwise writes 0600, the same protection as every other synced file.
func homeFileMode(mode uint32) os.FileMode {
	if mode&0o100 != 0 {
		return 0o700
	}
	return 0o600
}

// homeSyncManifestName records, on the remote, the home-relative paths the last
// sync wrote. Only those paths are ever deleted when the local copy goes away,
// so a file the user created by hand on the host is never touched.
const homeSyncManifestName = "home-sync-manifest.json"

func loadHomeSyncManifest(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("home sync: read manifest %s: %v (no deletions this round)", path, err)
		}
		return nil
	}
	var names []string
	if err := json.Unmarshal(data, &names); err != nil {
		log.Printf("home sync: manifest %s unparseable: %v (no deletions this round)", path, err)
		return nil
	}
	return names
}

// insideHomeSyncTree reports whether the directory rel is strictly inside a
// synced tree, i.e. may be pruned once empty. Tree roots and their parents stay.
func insideHomeSyncTree(rel string) bool {
	for _, tree := range homeSyncTrees {
		if strings.HasPrefix(rel, tree+"/") {
			return true
		}
	}
	return false
}

// applyHomeSyncManifest deletes the files the previous sync wrote that this
// payload no longer carries (local deletion wins), prunes the directories that
// leaves empty, and records the new set. It runs on the remote at the end of
// WritePayload. Every deletion target is re-validated against the allowlist, so
// a tampered manifest cannot remove anything outside it.
func applyHomeSyncManifest(p SyncPayload) error {
	manifestDir, err := resolveSyncDir(SyncDirOcodeData)
	if err != nil {
		return fmt.Errorf("resolve manifest dir: %w", err)
	}
	manifestPath := filepath.Join(manifestDir, homeSyncManifestName)

	current := map[string]bool{}
	for _, f := range p.Files {
		if f.Dir == SyncDirHome {
			current[f.Name] = true
		}
	}
	for _, name := range p.KeepHome {
		current[name] = true
	}
	var home string
	olds := loadHomeSyncManifest(manifestPath)
	if len(olds) == 0 && len(current) == 0 {
		return nil // nothing synced before or now: leave no manifest behind
	}
	for _, old := range olds {
		if current[old] {
			continue
		}
		if !allowedHomeSyncPath(old) {
			log.Printf("home sync: manifest entry %q is not allowlisted, not deleting", old)
			continue
		}
		if home == "" { // resolved lazily: only a deletion needs $HOME
			if home, err = resolveSyncDir(SyncDirHome); err != nil {
				return fmt.Errorf("resolve home: %w", err)
			}
		}
		target := filepath.Join(home, filepath.FromSlash(old))
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove stale synced file %s: %w", target, err)
		}
		for dir := path.Dir(old); insideHomeSyncTree(dir); dir = path.Dir(dir) {
			if err := os.Remove(filepath.Join(home, filepath.FromSlash(dir))); err != nil {
				break // not empty (or already gone): stop pruning
			}
		}
	}

	names := make([]string, 0, len(current))
	for n := range current {
		names = append(names, n)
	}
	sort.Strings(names)
	data, err := json.MarshalIndent(names, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(manifestDir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", manifestDir, err)
	}
	return writeFile0600(manifestPath, data)
}
