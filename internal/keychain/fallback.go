package keychain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/u007/ocode/internal/encryption"
)

type fallbackStore struct {
	path string
	pw   string
	mu   sync.Mutex
}

func newFallbackStore(opts Options) Store {
	pw := opts.MasterPassword
	if pw == "" {
		pw = machineID()
	}
	combined := machineID() + ":" + pw
	return &fallbackStore{path: opts.Path, pw: combined}
}

func machineID() string {
	if b, err := os.ReadFile("/etc/machine-id"); err == nil {
		id := strings.TrimSpace(string(b))
		if id != "" {
			return id
		}
	}
	if b, err := os.ReadFile("/var/lib/dbus/machine-id"); err == nil {
		id := strings.TrimSpace(string(b))
		if id != "" {
			return id
		}
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		u := "unknown"
		if cu, err := os.UserHomeDir(); err == nil {
			u = cu
		}
		return h + "/" + u
	}
	return "ocode-default-machine"
}

func fallbackKey(service, account string) string {
	return service + "\x00" + account
}

type fallbackDoc struct {
	Entries map[string]string `json:"entries"`
}

func (f *fallbackStore) readEntriesLocked() (map[string]string, error) {
	data, err := os.ReadFile(f.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("keychain: read fallback: %w", err)
	}
	if len(data) == 0 {
		return map[string]string{}, nil
	}
	plain, err := encryption.DecryptJSON(string(data), f.pw)
	if err != nil {
		return nil, fmt.Errorf("keychain: decrypt fallback: %w", err)
	}
	var doc fallbackDoc
	if err := json.Unmarshal(plain, &doc); err != nil {
		return nil, fmt.Errorf("keychain: parse fallback: %w", err)
	}
	if doc.Entries == nil {
		doc.Entries = map[string]string{}
	}
	return doc.Entries, nil
}

func (f *fallbackStore) writeEntriesLocked(entries map[string]string) error {
	doc := fallbackDoc{Entries: entries}
	plain, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("keychain: marshal fallback: %w", err)
	}
	enc, err := encryption.EncryptJSON(plain, f.pw)
	if err != nil {
		return fmt.Errorf("keychain: encrypt fallback: %w", err)
	}
	if dir := filepath.Dir(f.path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("keychain: mkdir fallback: %w", err)
		}
	}
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(enc), 0600); err != nil {
		return fmt.Errorf("keychain: write fallback: %w", err)
	}
	if err := os.Rename(tmp, f.path); err != nil {
		return fmt.Errorf("keychain: rename fallback: %w", err)
	}
	return nil
}

func (f *fallbackStore) Get(service, account string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	entries, err := f.readEntriesLocked()
	if err != nil {
		return "", err
	}
	v, ok := entries[fallbackKey(service, account)]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (f *fallbackStore) Set(service, account, secret string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	entries, err := f.readEntriesLocked()
	if err != nil {
		return err
	}
	entries[fallbackKey(service, account)] = secret
	return f.writeEntriesLocked(entries)
}

func (f *fallbackStore) Delete(service, account string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	entries, err := f.readEntriesLocked()
	if err != nil {
		return err
	}
	if _, ok := entries[fallbackKey(service, account)]; !ok {
		return ErrNotFound
	}
	delete(entries, fallbackKey(service, account))
	return f.writeEntriesLocked(entries)
}
