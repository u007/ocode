package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// IsOutOfScopePathRequest reports whether req is an out-of-workspace path ask
// whose "always" answer should persist a path root to extra_allowed_paths
// rather than a bash-prefix or tool rule. It covers bash cd-target/path-arg
// asks (which carry OutOfScopePath) and the redirection/env out-of-scope asks.
// Shared by the TUI dialog and the web/desktop resolve endpoint so both
// surfaces classify identically.
func IsOutOfScopePathRequest(req PermissionRequest) bool {
	return req.OutOfScopePath != "" ||
		strings.HasSuffix(req.Rule, ".out_of_scope") ||
		strings.HasSuffix(req.Rule, ".path_pattern")
}

// OutOfScopePathRoot returns the directory root to persist for an
// out-of-workspace path ask: the path itself when it is (or resolves to) a
// directory, else its parent. Returns "" when req is not an out-of-scope ask
// or carries no absolute target.
func OutOfScopePathRoot(req PermissionRequest) string {
	if req.OutOfScopePath != "" {
		return pathRootFromTarget(req.OutOfScopePath)
	}
	if !strings.HasSuffix(req.Rule, ".out_of_scope") && !strings.HasSuffix(req.Rule, ".path_pattern") {
		return ""
	}
	return pathRootFromPermissionArgs(req.Args)
}

func pathRootFromPermissionArgs(args json.RawMessage) string {
	var params struct {
		Path     string `json:"path"`
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return ""
	}
	target := strings.TrimSpace(params.Path)
	if target == "" {
		target = strings.TrimSpace(params.FilePath)
	}
	return pathRootFromTarget(target)
}

// pathRootFromTarget normalizes an absolute target path to the directory root
// to persist: the path itself when it is (or resolves to) a directory, else
// its parent. Returns "" for empty or non-absolute targets.
func pathRootFromTarget(target string) string {
	target = strings.TrimSpace(target)
	if target == "" || !filepath.IsAbs(target) {
		return ""
	}
	if info, err := os.Stat(target); err == nil && info.IsDir() {
		return target
	}
	return filepath.Dir(target)
}
