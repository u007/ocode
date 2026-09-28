package desktop

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// StorageMigrationPath carries the webview's localStorage from the old
// http://127.0.0.1:PORT origin to https://127.0.0.1:PORT (the webview moved to
// TLS for HTTP/2). The SPA side is web/src/lib/desktopStorageMigration.ts.
const StorageMigrationPath = "/api/desktop/storage-migration"

// storageMigrationMarker records that the move has happened, next to
// desktop-port. Once present the shell opens the https origin directly.
const storageMigrationMarker = "desktop-https-migrated"

// maxStorageMigrationBody bounds the export: browsers cap localStorage at
// ~5–10 MB per origin, stored as UTF-16, so 32 MB of JSON is ample.
const maxStorageMigrationBody = 32 << 20

// storageMigration is a one-shot mailbox: the http page POSTs its entries, the
// https page GETs them once. Consuming the payload writes the marker.
type storageMigration struct {
	token      string
	markerPath string

	mu      sync.Mutex
	payload []byte
}

func markerPath() (string, error) {
	path, err := portFilePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(path), storageMigrationMarker), nil
}

// newStorageMigration returns the mailbox when this launch must migrate, or
// nil. Migration is needed only when the server is back on the saved port (the
// old http origin's storage lives under that exact port) and the marker is
// absent. A first-ever launch has no old origin, so it just writes the marker.
// A launch on a drifted port leaves the marker unwritten so a later launch on
// the saved port can still migrate.
func newStorageMigration(savedPort, boundPort int, token string) *storageMigration {
	path, err := markerPath()
	if err != nil {
		log.Printf("desktop: storage migration: resolve marker path: %v", err)
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		log.Printf("desktop: storage migration: stat marker %s: %v", path, err)
		return nil
	}
	if savedPort == 0 {
		writeMigrationMarker(path)
		return nil
	}
	if boundPort != savedPort {
		return nil
	}
	return &storageMigration{token: token, markerPath: path}
}

func writeMigrationMarker(path string) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Printf("desktop: storage migration: create marker dir: %v", err)
		return
	}
	if err := os.WriteFile(path, []byte("1\n"), 0o600); err != nil {
		log.Printf("desktop: storage migration: write marker %s: %v", path, err)
	}
}

func (m *storageMigration) authorized(r *http.Request) bool {
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && subtle.ConstantTimeCompare([]byte(got), []byte(m.token)) == 1
}

func (m *storageMigration) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !m.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodPost:
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxStorageMigrationBody))
		if err != nil {
			log.Printf("desktop: storage migration: read export: %v", err)
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}
		var entries map[string]string
		if err := json.Unmarshal(body, &entries); err != nil {
			log.Printf("desktop: storage migration: decode export: %v", err)
			http.Error(w, "body must be a JSON object of strings", http.StatusBadRequest)
			return
		}
		m.mu.Lock()
		m.payload = body
		m.mu.Unlock()
		log.Printf("desktop: storage migration: received %d entries", len(entries))
		w.WriteHeader(http.StatusNoContent)
	case http.MethodGet:
		m.mu.Lock()
		payload := m.payload
		m.payload = nil
		m.mu.Unlock()
		if payload == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeMigrationMarker(m.markerPath)
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(payload); err != nil {
			log.Printf("desktop: storage migration: write import: %v", err)
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
