package main

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

// maxStateBytes bounds one stored value. The journal and the session index are small by design.
const maxStateBytes = 8 << 20

var stateNameRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// stateStore keeps small opaque JSON documents per (workbook, key). With a data dir they survive a
// bridge restart (writes are atomic: temp file + rename); without one they live in memory.
type stateStore struct {
	dir string
	mu  sync.Mutex
	mem map[string][]byte
}

func newStateStore(dir string) (*stateStore, error) {
	if dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	return &stateStore{dir: dir, mem: map[string][]byte{}}, nil
}

func (s *stateStore) path(wb, key string) string { return filepath.Join(s.dir, wb, key+".json") }

func (s *stateStore) get(wb, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dir == "" {
		b, ok := s.mem[wb+"/"+key]
		return b, ok, nil
	}
	b, err := os.ReadFile(s.path(wb, key))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	return b, err == nil, err
}

func (s *stateStore) put(wb, key string, b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dir == "" {
		s.mem[wb+"/"+key] = append([]byte(nil), b...)
		return nil
	}
	p := s.path(wb, key)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

func validState(w http.ResponseWriter, r *http.Request) (wb, key string, ok bool) {
	wb, key = r.PathValue("workbook"), r.PathValue("key")
	if !stateNameRE.MatchString(wb) || !stateNameRE.MatchString(key) || wb == "." || wb == ".." || key == "." || key == ".." {
		http.Error(w, "invalid workbook or key", http.StatusBadRequest)
		return "", "", false
	}
	return wb, key, true
}

func (s *srv) getState(w http.ResponseWriter, r *http.Request) {
	wb, key, ok := validState(w, r)
	if !ok {
		return
	}
	b, found, err := s.state.get(wb, key)
	if err != nil {
		http.Error(w, "state read failed", http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

func (s *srv) putState(w http.ResponseWriter, r *http.Request) {
	wb, key, ok := validState(w, r)
	if !ok {
		return
	}
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxStateBytes))
	if err != nil {
		http.Error(w, "body too large or unreadable", http.StatusRequestEntityTooLarge)
		return
	}
	if len(b) == 0 || b[0] != '{' && b[0] != '[' {
		http.Error(w, "state must be a JSON object or array", http.StatusBadRequest)
		return
	}
	if err := s.state.put(wb, key, b); err != nil {
		http.Error(w, "state write failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
