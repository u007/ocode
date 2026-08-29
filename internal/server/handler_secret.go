package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/secretfile"
	"github.com/u007/ocode/internal/secretjob"
)

// resolveSecretPath resolves path (absolute, or relative to h.workDir) and
// confirms it falls inside an allowed project root or extra-allowed-path
// (see fileTreeRootFor), mirroring the confinement HandleFileContent already
// applies. It also resolves symlinks so the returned path agrees with
// paths.ProjectRoot (which does the same) about where the project's key
// file lives — otherwise a symlinked temp/project dir makes secretjob.Walk's
// key-file exclusion silently fail to match.
func (h *Handler) resolveSecretPath(path string) (string, error) {
	if path == "" {
		path = h.workDir
	}
	if !filepath.IsAbs(path) && h.workDir != "" {
		path = filepath.Join(h.workDir, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	if _, ok := h.fileTreeRootFor(abs); !ok {
		return "", fmt.Errorf("path is outside the working directory")
	}
	return abs, nil
}

type secretInitRequest struct {
	Path              string `json:"path,omitempty"`
	Passphrase        string `json:"passphrase"`
	ConfirmPassphrase string `json:"confirm_passphrase"`
}

// HandleSecretInit handles POST /api/secret/init: creates the project's age
// key, wrapped under the given passphrase.
func (h *Handler) HandleSecretInit(w http.ResponseWriter, r *http.Request) {
	var req secretInitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Passphrase == "" {
		writeError(w, http.StatusBadRequest, "passphrase is required")
		return
	}
	if req.Passphrase != req.ConfirmPassphrase {
		writeError(w, http.StatusBadRequest, "passphrases did not match")
		return
	}

	dir, err := h.resolveSecretPath(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	root := paths.ProjectRoot(dir)
	keyPath := secretfile.ProjectKeyPath(root)
	if _, err := secretfile.GenerateProjectKey(keyPath, req.Passphrase); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"key_path": keyPath})
}

// HandleSecretScan handles GET /api/secret/scan?path=&mode=encrypt|decrypt:
// reports whether path is a file or directory and, for a directory, how
// many files an encrypt/decrypt of it would touch, so the web UI can show an
// accurate confirmation prompt before the client commits to the operation.
func (h *Handler) HandleSecretScan(w http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("mode")
	if mode != "encrypt" && mode != "decrypt" {
		writeError(w, http.StatusBadRequest, "mode must be encrypt or decrypt")
		return
	}

	abs, err := h.resolveSecretPath(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		writeError(w, http.StatusNotFound, "path not found")
		return
	}

	resp := map[string]any{"path": abs, "is_dir": info.IsDir()}
	if info.IsDir() {
		root := paths.ProjectRoot(abs)
		keyPath := secretfile.ProjectKeyPath(root)
		files, err := secretjob.Walk(abs, keyPath, mode == "encrypt")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		resp["file_count"] = len(files)
	}
	writeJSON(w, http.StatusOK, resp)
}

type secretTransformRequest struct {
	Path              string `json:"path"`
	Passphrase        string `json:"passphrase"`
	ConfirmPassphrase string `json:"confirm_passphrase,omitempty"`
}

type secretTransformResponse struct {
	Status string `json:"status"` // "done" | "started"
	JobID  string `json:"job_id,omitempty"`
	Total  int    `json:"total,omitempty"`
}

// HandleSecretEncrypt handles POST /api/secret/encrypt.
func (h *Handler) HandleSecretEncrypt(w http.ResponseWriter, r *http.Request) {
	h.handleSecretTransform(w, r, true)
}

// HandleSecretDecrypt handles POST /api/secret/decrypt.
func (h *Handler) HandleSecretDecrypt(w http.ResponseWriter, r *http.Request) {
	h.handleSecretTransform(w, r, false)
}

func (h *Handler) handleSecretTransform(w http.ResponseWriter, r *http.Request, encrypt bool) {
	var req secretTransformRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Passphrase == "" {
		writeError(w, http.StatusBadRequest, "passphrase is required")
		return
	}
	if encrypt && req.Passphrase != req.ConfirmPassphrase {
		writeError(w, http.StatusBadRequest, "passphrases did not match")
		return
	}

	abs, err := h.resolveSecretPath(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		writeError(w, http.StatusNotFound, "path not found")
		return
	}

	root := paths.ProjectRoot(abs)
	if !info.IsDir() {
		root = paths.ProjectRoot(filepath.Dir(abs))
	}
	keyPath := secretfile.ProjectKeyPath(root)
	if _, err := os.Stat(keyPath); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("no project key at %s; run secret init first", keyPath))
		return
	}
	key, err := secretfile.UnlockProjectKey(keyPath, req.Passphrase)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	if !info.IsDir() {
		if err := secretjob.TransformFile(key, abs, encrypt); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, secretTransformResponse{Status: "done"})
		return
	}

	files, err := secretjob.Walk(abs, keyPath, encrypt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(files) == 0 {
		writeJSON(w, http.StatusOK, secretTransformResponse{Status: "done", Total: 0})
		return
	}

	jobID, err := h.startSecretJob(root, key, files, encrypt)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, secretTransformResponse{Status: "started", JobID: jobID, Total: len(files)})
}

// secretProgressEvent is the secret_progress SSE payload.
type secretProgressEvent struct {
	JobID   string `json:"job_id"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Current string `json:"current,omitempty"`
}

func (h *Handler) startSecretJob(root string, key *secretfile.ProjectKey, files []string, encrypt bool) (string, error) {
	total := len(files)
	return h.secretJobs.Start(root, key, files, encrypt,
		func(jobID string, done, total int, current string) {
			rel, err := filepath.Rel(root, current)
			if err != nil {
				rel = current
			}
			h.bus.Publish("secret_progress", root, "", secretProgressEvent{JobID: jobID, Done: done, Total: total, Current: rel})
		},
		func(jobID string, err error) {
			switch {
			case err == nil:
				h.bus.Publish("secret_done", root, "", map[string]any{"job_id": jobID, "total": total})
			case err == secretjob.ErrCancelled:
				h.bus.Publish("secret_cancelled", root, "", map[string]string{"job_id": jobID})
			default:
				h.bus.Publish("secret_error", root, "", map[string]string{"job_id": jobID, "error": err.Error()})
			}
		},
	)
}

// HandleSecretCancel handles POST /api/secret/cancel: stops a running
// directory-wide encrypt/decrypt job before its next file.
func (h *Handler) HandleSecretCancel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		JobID string `json:"job_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.JobID == "" {
		writeError(w, http.StatusBadRequest, "job_id is required")
		return
	}
	cancelled := h.secretJobs.Cancel(req.JobID)
	writeJSON(w, http.StatusOK, map[string]bool{"cancelled": cancelled})
}

type secretRekeyRequest struct {
	Path                 string `json:"path,omitempty"`
	OldPassphrase        string `json:"old_passphrase"`
	NewPassphrase        string `json:"new_passphrase"`
	ConfirmNewPassphrase string `json:"confirm_new_passphrase"`
}

// HandleSecretRekey handles POST /api/secret/rekey: re-wraps the project's
// existing key under a new passphrase without touching any encrypted files.
func (h *Handler) HandleSecretRekey(w http.ResponseWriter, r *http.Request) {
	var req secretRekeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.OldPassphrase == "" || req.NewPassphrase == "" {
		writeError(w, http.StatusBadRequest, "old_passphrase and new_passphrase are required")
		return
	}
	if req.NewPassphrase != req.ConfirmNewPassphrase {
		writeError(w, http.StatusBadRequest, "passphrases did not match")
		return
	}

	dir, err := h.resolveSecretPath(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	root := paths.ProjectRoot(dir)
	keyPath := secretfile.ProjectKeyPath(root)
	if _, err := os.Stat(keyPath); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("no project key at %s; run secret init first", keyPath))
		return
	}

	if err := secretfile.RekeyProjectKey(keyPath, req.OldPassphrase, req.NewPassphrase); err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"key_path": keyPath})
}
