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

	"github.com/u007/ocode/internal/config"
)

type openFileRequest struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
	// Mode selects the opener: ""/"editor" uses the configured GUI editor
	// with system-opener fallback (previous behavior); "os" forces the OS
	// default application (used by PreviewHost "Open with OS app" for
	// native-fidelity pptx/docx playback). Any other value is rejected.
	Mode string `json:"mode,omitempty"`
	// ProjectRoot optionally anchors relative paths (same allowlist as
	// HandleFileContent's project_root). Empty falls back to workDir.
	ProjectRoot string `json:"project_root,omitempty"`
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
	switch req.Mode {
	case "", "editor", "os":
	default:
		writeError(w, http.StatusBadRequest, "mode must be \"editor\" or \"os\"")
		return
	}

	abs, err := h.resolveOpenPath(req.Path, req.ProjectRoot)
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

	if err := openResolvedPath(abs, req.Line, req.Mode); err != nil {
		log.Printf("[open] failed to open %q: %v", abs, err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": abs, "status": "opened"})
}

// resolveOpenPath anchors reqPath under projectRoot when provided (same
// allowlist as HandleFileContent's fileContentRootFor), else workDir.
func (h *Handler) resolveOpenPath(reqPath, projectRoot string) (string, error) {
	if projectRoot != "" {
		root, ok := h.fileContentRootFor(projectRoot)
		if !ok {
			return "", fmt.Errorf("project_root is not an allowed project root")
		}
		p := reqPath
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, p)
		}
		p = filepath.Clean(p)
		rel, err := filepath.Rel(root, p)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("path is outside the project root")
		}
		return p, nil
	}
	return h.resolveWithinWorkdir(reqPath)
}

// openResolvedPath honors mode: "os" forces the OS default application
// (native-fidelity playback for pptx/docx); anything else uses the
// configured editor with system-opener fallback.
func openResolvedPath(absPath string, line int, mode string) error {
	if mode == "os" {
		name, args := systemOpener(absPath)
		return startDetached(name, args)
	}
	return openPathInEditor(absPath, line)
}

// resolveWithinWorkdir cleans path (relative to the server working dir) and
// confirms the result stays inside that working dir.
//
// Uses h.workDir, not a raw os.Getwd() — a Finder/Dock-launched desktop .app
// starts with process cwd "/" (see cmd/ocode-desktop/main.go and
// handler_tui_status.go for the same fallback hazard elsewhere), and "/" as
// the workdir would make every absolute path pass the "stays inside the
// working dir" check below, defeating the LFI guard this function exists
// for. h.workDir is set explicitly by the desktop boot path via SetWorkDir
// before serving; os.Getwd() is only a fallback for the rare case a Handler
// is used directly without ever calling SetWorkDir (e.g. some tests).
func (h *Handler) resolveWithinWorkdir(path string) (string, error) {
	wd := h.workDir
	if wd == "" {
		var err error
		wd, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("cannot determine working directory: %w", err)
		}
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
		setProcGroup(c)
	}
	return c.Start()
}
