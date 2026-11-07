package artifacts

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestManifestHashDeterministicAndOrderIndependent(t *testing.T) {
	a := []File{
		{Path: "index.html", BlobHash: "aaa"},
		{Path: "app.js", BlobHash: "bbb"},
	}
	b := []File{
		{Path: "app.js", BlobHash: "bbb"},
		{Path: "index.html", BlobHash: "aaa"},
	}
	if ManifestHash(a) != ManifestHash(b) {
		t.Fatalf("manifest hash depends on slice order: %s vs %s", ManifestHash(a), ManifestHash(b))
	}
}

func TestManifestHashChangesOnContentOrPath(t *testing.T) {
	base := []File{{Path: "index.html", BlobHash: "aaa"}}
	if ManifestHash(base) == ManifestHash([]File{{Path: "index.html", BlobHash: "bbb"}}) {
		t.Fatal("manifest hash ignored blob hash")
	}
	if ManifestHash(base) == ManifestHash([]File{{Path: "other.html", BlobHash: "aaa"}}) {
		t.Fatal("manifest hash ignored path")
	}
}

func TestManifestHashIgnoresMetadata(t *testing.T) {
	a := []File{{Path: "x", BlobHash: "h", Size: 1, Mode: "0644", Mime: "text/plain"}}
	b := []File{{Path: "x", BlobHash: "h", Size: 99, Mode: "0600", Mime: "application/octet-stream"}}
	if ManifestHash(a) != ManifestHash(b) {
		t.Fatal("manifest hash must not depend on size/mode/mime")
	}
}

func TestManifestHashEmpty(t *testing.T) {
	want := sha256.Sum256(nil)
	if got := ManifestHash(nil); got != hex.EncodeToString(want[:]) {
		t.Fatalf("ManifestHash(nil) = %s, want sha256 of empty", got)
	}
}
