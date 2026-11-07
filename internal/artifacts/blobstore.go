package artifacts

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BlobStore is a content-addressed blob store rooted at dir/blobs.
type BlobStore struct {
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

func (b *BlobStore) pathFor(hash string) string {
	if len(hash) < 2 {
		return filepath.Join(b.root, hash)
	}
	return filepath.Join(b.root, hash[:2], hash)
}

// Put stores data content-addressed and returns its sha256 hex hash.
func (b *BlobStore) Put(data []byte) (string, error) {
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	p := b.pathFor(hash)
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
	data, err := os.ReadFile(b.pathFor(hash))
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
	_, err := os.Stat(b.pathFor(hash))
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
	return filepath.Walk(b.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		// Only files two levels deep (blobs/<xx>/<hash>).
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
		return os.Remove(path)
	})
}
