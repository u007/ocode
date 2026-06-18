package server

import (
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// uploadFileInfo is the JSON shape returned by the /api/uploads listing.
type uploadFileInfo struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Modtime string `json:"modtime"` // RFC3339
	Mime    string `json:"mime"`
}

// uploadDir returns the absolute path of the configured upload directory,
// creating it if needed. Honors cfg.Ocode.UploadDir when set; otherwise falls
// back to <workDir>/.ocode/uploads.
func (h *Handler) uploadDir() (string, error) {
	var dir string
	if h.cfg != nil && h.cfg.Ocode.UploadDir != "" {
		dir = h.cfg.Ocode.UploadDir
	} else {
		dir = filepath.Join(h.workDir, ".ocode", "uploads")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// HandleUploads dispatches on HTTP method: GET lists files, POST stores
// uploaded multipart files, DELETE removes a file by ?name=<base>.
func (h *Handler) HandleUploads(w http.ResponseWriter, r *http.Request) {
	dir, err := h.uploadDir()
	if err != nil {
		log.Printf("uploads: resolve dir: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to resolve upload directory")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.handleUploadsList(w, dir)
	case http.MethodPost:
		h.handleUploadsPost(w, r, dir)
	case http.MethodDelete:
		h.handleUploadsDelete(w, r, dir)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleUploadsList(w http.ResponseWriter, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("uploads: list: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list upload directory")
		return
	}

	files := make([]uploadFileInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, uploadFileInfo{
			Name:    info.Name(),
			Size:    info.Size(),
			Modtime: info.ModTime().UTC().Format(time.RFC3339),
			Mime:    mime.TypeByExtension(filepath.Ext(info.Name())),
		})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Modtime > files[j].Modtime
	})

	writeJSON(w, http.StatusOK, files)
}

const maxUploadSize = 32 << 20 // 32 MiB

func (h *Handler) handleUploadsPost(w http.ResponseWriter, r *http.Request, dir string) {
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		log.Printf("uploads: parse multipart: %v", err)
		writeError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}

	form := r.MultipartForm
	if form == nil {
		writeError(w, http.StatusBadRequest, "no multipart form provided")
		return
	}
	parts := form.File["file"]
	if len(parts) == 0 {
		// Accept "files" too so simple callers (single-file "file" or "files"
		// field) work without strict naming.
		parts = form.File["files"]
	}
	if len(parts) == 0 {
		writeError(w, http.StatusBadRequest, "no files provided in 'file' field")
		return
	}

	saved := make([]uploadFileInfo, 0, len(parts))
	for _, fh := range parts {
		info, err := h.saveUpload(dir, fh)
		if err != nil {
			log.Printf("uploads: save %s: %v", fh.Filename, err)
			writeError(w, http.StatusInternalServerError, "failed to save upload")
			return
		}
		saved = append(saved, info)
	}

	writeJSON(w, http.StatusOK, saved)
}

func (h *Handler) saveUpload(dir string, fh *multipart.FileHeader) (uploadFileInfo, error) {
	src, err := fh.Open()
	if err != nil {
		return uploadFileInfo{}, err
	}
	defer src.Close()

	name := filepath.Base(fh.Filename)
	dstPath := filepath.Join(dir, name)
	dst, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return uploadFileInfo{}, err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return uploadFileInfo{}, err
	}

	info, err := os.Stat(dstPath)
	if err != nil {
		return uploadFileInfo{}, err
	}

	return uploadFileInfo{
		Name:    name,
		Size:    info.Size(),
		Modtime: info.ModTime().UTC().Format(time.RFC3339),
		Mime:    mime.TypeByExtension(filepath.Ext(name)),
	}, nil
}

func (h *Handler) handleUploadsDelete(w http.ResponseWriter, r *http.Request, dir string) {
	name := filepath.Base(r.URL.Query().Get("name"))
	if name == "" || name == "." || name == "/" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	target := filepath.Join(dir, name)
	if err := os.Remove(target); err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}
		log.Printf("uploads: delete %s: %v", target, err)
		writeError(w, http.StatusInternalServerError, "failed to delete file")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleUploadFile serves a single upload by name. Append ?download=1 to
// force a Content-Disposition: attachment header (browser save-as).
func (h *Handler) HandleUploadFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	dir, err := h.uploadDir()
	if err != nil {
		log.Printf("uploads: resolve dir: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to resolve upload directory")
		return
	}

	name := filepath.Base(r.URL.Query().Get("name"))
	if name == "" || name == "." || name == "/" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	target := filepath.Join(dir, name)

	f, err := os.Open(target)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}
		log.Printf("uploads: open %s: %v", target, err)
		writeError(w, http.StatusInternalServerError, "failed to open file")
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		log.Printf("uploads: stat %s: %v", target, err)
		writeError(w, http.StatusInternalServerError, "failed to stat file")
		return
	}

	contentType := mime.TypeByExtension(filepath.Ext(name))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	}

	http.ServeContent(w, r, name, info.ModTime(), f)
}
