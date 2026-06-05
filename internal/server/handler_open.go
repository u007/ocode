package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/u007/ocode/internal/config"
)

type openFileRequest struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
}

// HandleOpenFile opens a file referenced from a rendered chat message in the
// user's editor (or the system default opener). The path is resolved against
// the server's working directory and must stay inside it — opening arbitrary
// absolute paths would be an LFI-shaped risk if the server is ever exposed.
func (h *Handler) HandleOpenFile(w http.ResponseWriter, r *http.Request) {
	var req openFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	abs, err := resolveWithinWorkdir(req.Path)
	if err != nil {
		log.Printf("[open] rejected path %q: %v", req.Path, err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if info, statErr := os.Stat(abs); statErr != nil || info.IsDir() {
		log.Printf("[open] not a file: %q (err=%v)", abs, statErr)
		writeError(w, http.StatusNotFound, "file not found")
		return
	}

	if err := openPathInEditor(abs, req.Line); err != nil {
		log.Printf("[open] failed to open %q: %v", abs, err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": abs, "status": "opened"})
}

// resolveWithinWorkdir cleans path (relative to the server working dir) and
// confirms the result stays inside that working dir.
func resolveWithinWorkdir(path string) (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot determine working directory: %w", err)
	}
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(wd, abs)
	}
	abs = filepath.Clean(abs)
	rel, err := filepath.Rel(wd, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path is outside the working directory")
	}
	return abs, nil
}

// guiEditors maps the binary name of known GUI/standalone editors to whether
// they support the `--goto file:line` jump syntax (code family). The server is
// headless (no TTY), so terminal editors like vim/nano can't run here — for
// those we fall back to the system opener.
var guiEditors = map[string]bool{
	"code":          true,
	"code-insiders": true,
	"cursor":        true,
	"windsurf":      true,
	"vscodium":      true,
	"codium":        true,
	"zed":           false,
	"subl":          false,
	"sublime_text":  false,
	"gvim":          false,
	"mate":          false,
	"idea":          false,
	"webstorm":      false,
	"goland":        false,
	"pycharm":       false,
}

func openPathInEditor(absPath string, line int) error {
	cfg, _ := config.Load()
	if cfg != nil {
		_ = config.LoadOcodeConfig(cfg)
	}
	var ocode *config.OcodeConfig
	if cfg != nil {
		ocode = &cfg.Ocode
	}
	editor := config.ResolveEditor(ocode)

	cmdParts := strings.Fields(editor)
	if len(cmdParts) > 0 {
		bin := filepath.Base(cmdParts[0])
		if supportsGoto, ok := guiEditors[bin]; ok {
			if _, err := exec.LookPath(cmdParts[0]); err == nil {
				args := cmdParts[1:]
				if supportsGoto && line > 0 {
					args = append(args, "--goto", fmt.Sprintf("%s:%d", absPath, line))
				} else {
					args = append(args, absPath)
				}
				log.Printf("[open] launching editor %q file=%q line=%d", editor, absPath, line)
				return startDetached(cmdParts[0], args)
			}
			log.Printf("[open] editor %q not found in PATH; falling back to system opener", cmdParts[0])
		}
	}
	// Terminal editor (no TTY here) or unknown editor: use the system opener.
	return startDetached(systemOpener(absPath))
}

// systemOpener returns the OS default opener command and args for a path.
func systemOpener(path string) (string, []string) {
	switch runtime.GOOS {
	case "darwin":
		return "open", []string{path}
	case "windows":
		return "cmd", []string{"/c", "start", "", path}
	default:
		return "xdg-open", []string{path}
	}
}

func startDetached(name string, args []string) error {
	c := exec.Command(name, args...)
	if runtime.GOOS != "windows" {
		c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	return c.Start()
}
