package artifacts

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
)

// ManifestHash returns a deterministic sha256 over the sorted (path, blob_hash)
// pairs of a version's files. It is stable across publishes of an identical
// tree and changes whenever any path or blob differs. Metadata such as size,
// mode and mime is deliberately excluded so a chmod or mime-detection change
// alone does not invalidate the manifest.
func ManifestHash(files []File) string {
	items := make([]string, 0, len(files))
	for _, f := range files {
		items = append(items, f.Path+"\x00"+f.BlobHash)
	}
	sort.Strings(items)
	h := sha256.New()
	for _, it := range items {
		h.Write([]byte(it))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}
