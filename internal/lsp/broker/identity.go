// Package broker contains cross-process language-server sharing primitives.
package broker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/u007/ocode/internal/paths"
)

type Identity struct {
	Root              string   `json:"root"`
	Executable        string   `json:"executable"`
	ExecutableVersion string   `json:"executable_version,omitempty"`
	Arguments         []string `json:"arguments,omitempty"`
	LanguageID        string   `json:"language_id"`
	WorkspaceFolders  []string `json:"workspace_folders,omitempty"`
	Initialization    any      `json:"initialization_options,omitempty"`
}

func CanonicalRoot(root string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("broker: empty project root")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	abs = filepath.Clean(abs)
	if runtime.GOOS == "windows" {
		abs = strings.ToLower(abs)
	}
	return abs, nil
}

func NewIdentity(root, command string, args []string, lang string, folders []string, init any) (Identity, error) {
	root, err := CanonicalRoot(root)
	if err != nil {
		return Identity{}, err
	}
	exe, err := exec.LookPath(command)
	if err != nil {
		return Identity{}, err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return Identity{}, err
	}
	if resolved, e := filepath.EvalSymlinks(exe); e == nil {
		exe = resolved
	}
	exe = filepath.Clean(exe)
	if runtime.GOOS == "windows" {
		exe = strings.ToLower(exe)
	}
	version := ""
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if out, e := exec.CommandContext(ctx, exe, "version").Output(); e == nil {
		version = strings.TrimSpace(string(out))
	}
	canonicalFolders := make([]string, 0, len(folders))
	for _, folder := range folders {
		canonical, e := CanonicalRoot(folder)
		if e != nil {
			return Identity{}, e
		}
		canonicalFolders = append(canonicalFolders, canonical)
	}
	sort.Strings(canonicalFolders)
	return Identity{Root: root, Executable: exe, ExecutableVersion: version, Arguments: append([]string(nil), args...), LanguageID: lang, WorkspaceFolders: canonicalFolders, Initialization: init}, nil
}

func (i Identity) Hash() string {
	b, _ := json.Marshal(i)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func MetadataPath(i Identity) (string, error) {
	dir, err := paths.GlobalDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "lsp", paths.ProjectSlug(i.Root), i.Hash()+".json"), nil
}
