package artifacts

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// BlobStore is a content-addressed blob store rooted at dir/blobs.
type BlobStore struct {
	mu   sync.Mutex
	root string // blobs root directory
}

// NewBlobStore creates (or opens) a blob store rooted at dir/blobs.
func NewBlobStore(dir string) (*BlobStore, error) {
	root := filepath.Join(dir, "blobs")
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, fmt.Errorf("artifacts: create blob root: %w", err)
	}
	return &BlobStore{root: root}, nil
}

// validateHash rejects empty, path-separator-containing, or traversal hashes.
func (b *BlobStore) validateHash(hash string) error {
	if hash == "" {
		return fmt.Errorf("artifacts: empty hash")
	}
	if strings.ContainsAny(hash, `/\`) {
		return fmt.Errorf("artifacts: invalid hash (path separator)")
	}
	if strings.Contains(hash, "..") {
		return fmt.Errorf("artifacts: invalid hash (traversal)")
	}
	return nil
}

func (b *BlobStore) pathFor(hash string) (string, error) {
	if err := b.validateHash(hash); err != nil {
		return "", err
	}
	var p string
	if len(hash) < 2 {
		p = filepath.Join(b.root, hash)
	} else {
		p = filepath.Join(b.root, hash[:2], hash)
	}
	rel, err := filepath.Rel(b.root, p)
	if err != nil {
		return "", fmt.Errorf("artifacts: hash escapes blob root: %w", err)
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("artifacts: hash escapes blob root")
	}
	return p, nil
}

// Put stores data content-addressed and returns its sha256 hex hash.
func (b *BlobStore) Put(data []byte) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	p, err := b.pathFor(hash)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(p); err == nil {
		return hash, nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return "", fmt.Errorf("artifacts: create blob dir: %w", err)
	}
	if err := os.WriteFile(p, data, 0644); err != nil {
		return "", fmt.Errorf("artifacts: write blob: %w", err)
	}
	return hash, nil
}

// Get reads a blob by hash.
func (b *BlobStore) Get(hash string) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	p, err := b.pathFor(hash)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("artifacts: blob not found: %s", hash)
		}
		return nil, fmt.Errorf("artifacts: read blob: %w", err)
	}
	return data, nil
}

// Has reports whether a blob exists.
func (b *BlobStore) Has(hash string) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	p, err := b.pathFor(hash)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(p)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// GCUnreferenced deletes blobs whose hash is not in referenced.
func (b *BlobStore) GCUnreferenced(referenced map[string]bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	err := filepath.Walk(b.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(b.root, path)
		if err != nil {
			return err
		}
		parts := strings.Split(rel, string(os.PathSeparator))
		if len(parts) != 2 {
			return nil
		}
		hash := parts[1]
		if referenced[hash] {
			return nil
		}
		if rmErr := os.Remove(path); rmErr != nil {
			return rmErr
		}
		// attempt to remove now-empty parent subdir
		_ = os.Remove(filepath.Dir(path))
		return nil
	})
	if err != nil {
		return err
	}
	// clean any remaining empty subdirectories
	entries, err := os.ReadDir(b.root)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		_ = os.Remove(filepath.Join(b.root, e.Name()))
	}
	return nil
}
