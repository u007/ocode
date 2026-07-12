package discovery

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	artifactDownloadAttempts = 3
	artifactDownloadTimeout  = 20 * time.Minute
	artifactRetryDelay       = 250 * time.Millisecond
)

// EnsureArtifact downloads a into dir/a.Dest if missing or sha-mismatched, verifies
// sha256, and writes atomically (temp + rename). For Archive="tar.gz", the
// downloaded tarball is verified against SHA256, then a single file named
// Dest is extracted (and chmod +x if a.Exec). Re-download is skipped when the
// cached file already matches. Progress is emitted to the debug log so the TUI
// Log tab shows what the local embed server is doing.
func EnsureArtifact(a Artifact, dir string) error {
	dest := filepath.Join(dir, a.Dest)
	if a.URL == "" {
		return fmt.Errorf("artifact %s: URL not pinned in manifest", a.Dest)
	}
	if a.Archive != "" && a.Archive != ArchiveGZ {
		return fmt.Errorf("artifact %s: unsupported archive format %q", a.Dest, a.Archive)
	}
	// Cache check:
	//   - Raw files: the destination file exists with the expected SHA.
	//   - Archives: a sidecar marker next to dest records the archive SHA
	//     we successfully extracted from. We use the marker (not the
	//     extracted file's path/SHA) because the extracted file lives at
	//     a versioned path (e.g. dir/llama-b9747/llama-server) that we
	//     don't want to hardcode here — and because the archive SHA is
	//     the right invalidation key (a re-released llama.cpp binary
	//     with the same extracted name but a different tarball must
	//     trigger re-extraction).
	if a.Archive == ArchiveNone {
		if _, err := os.Stat(dest); err == nil {
			if got, err := sha256File(dest); err == nil && got == a.SHA256 {
				emitUserDiscoveryDebug("DISCOVERY", "artifact cached: "+a.Dest)
				return nil
			}
		}
	} else {
		ok, err := extractionMarkerMatches(markerPath(dest), a.SHA256)
		if err == nil && ok {
			emitUserDiscoveryDebug("DISCOVERY", "artifact cached: "+a.Dest)
			// Ensure dylib symlinks exist even for cached artifacts (handles
			// legacy caches extracted before this fix was added).
			ensureDylibSymlinks(dir)
			return nil
		}
	}
	if a.SHA256 == "" {
		return fmt.Errorf("artifact %s: SHA256 not pinned in manifest (refusing to download unverified)", a.Dest)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir artifact dir: %w", err)
	}
	emitUserDiscoveryDebug("DISCOVERY", fmt.Sprintf("downloading %s …", a.Dest))
	tmp := filepath.Join(dir, a.Dest+a.tmpSuffix())
	var downloadErr error
	for attempt := 1; attempt <= artifactDownloadAttempts; attempt++ {
		if attempt > 1 {
			delay := time.Duration(1<<(attempt-2)) * artifactRetryDelay
			emitDiscoveryDebug("WARN", fmt.Sprintf(
				"retrying %s download (%d/%d) in %s after: %v",
				a.Dest, attempt, artifactDownloadAttempts, delay, downloadErr,
			))
			time.Sleep(delay)
		}
		if err := downloadArtifactOnce(a, tmp); err != nil {
			downloadErr = err
			continue
		}
		downloadErr = nil
		break
	}
	if downloadErr != nil {
		return fmt.Errorf("download %s after %d attempt(s): %w", a.URL, artifactDownloadAttempts, downloadErr)
	}
	if a.Archive == ArchiveNone {
		mode := os.FileMode(0o644)
		if a.Exec {
			mode = 0o755
		}
		if err := os.Chmod(tmp, mode); err != nil {
			os.Remove(tmp)
			return fmt.Errorf("chmod artifact: %w", err)
		}
		if err := os.Rename(tmp, dest); err != nil {
			return fmt.Errorf("rename artifact: %w", err)
		}
	} else {
		// Extract a single file named Dest from the tarball. The tarball is
		// typically wrapped in a top-level directory (e.g. llama-b9747/), so
		// the actual path of the extracted file is returned (not just
		// dir/Dest). Side-by-side libraries in the same tarball are also
		// extracted to dir so the binary's dynamic linker resolves them
		// (e.g. libllama.0.dylib on macOS).
		leafPath, err := extractTarGZ(tmp, dir, a.Dest)
		if err != nil {
			os.Remove(tmp)
			return fmt.Errorf("extract %s: %w", a.Dest, err)
		}
		if a.Exec {
			if err := os.Chmod(leafPath, 0o755); err != nil {
				os.Remove(tmp)
				return fmt.Errorf("chmod artifact: %w", err)
			}
		}
		// Write the marker so subsequent runs skip re-downloading. Marker is
		// keyed to dest (not the versioned leafPath) so a future llama.cpp
		// release with a different version directory but the same binary
		// name still invalidates correctly.
		if err := writeExtractionMarker(markerPath(dest), a.SHA256); err != nil {
			os.Remove(tmp)
			return fmt.Errorf("write extraction marker: %w", err)
		}
		// Remove the cached tarball to reclaim disk — the extracted files
		// are what we actually use. (Re-download is cheap if the cache is
		// wiped, but on a clean machine the tarball is hundreds of MB.)
		os.Remove(tmp)
	}
	emitUserDiscoveryDebug("DISCOVERY", fmt.Sprintf("artifact ready: %s", a.Dest))
	return nil
}

func downloadArtifactOnce(a Artifact, tmp string) error {
	client := &http.Client{Timeout: artifactDownloadTimeout}
	resp, err := client.Get(a.URL)
	if err != nil {
		return fmt.Errorf("download %s: %w", a.URL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: status %d", a.URL, resp.StatusCode)
	}
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open artifact tmp: %w", err)
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("download %s: %w", a.URL, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("close artifact tmp: %w", err)
	}
	gotSHA := hex.EncodeToString(h.Sum(nil))
	if gotSHA != a.SHA256 {
		os.Remove(tmp)
		return fmt.Errorf("sha256 mismatch for %s: got %s want %s", a.Dest, gotSHA, a.SHA256)
	}
	return nil
}

// tmpSuffix returns the temp-file suffix used during download. Archives get a
// distinctive ".tar.gz.tmp" so a crashed mid-extract run leaves an obvious
// file rather than something that looks like the final artifact.
func (a Artifact) tmpSuffix() string {
	if a.Archive == ArchiveGZ {
		return ".tar.gz.tmp"
	}
	return ".tmp"
}

// markerPath returns the path of the sidecar file used to remember which
// archive SHA a given extraction came from. Storing the archive SHA in a
// sidecar (not the extracted file's SHA) means a future llama.cpp release with
// the same extracted `llama-server` filename but a different tarball will
// correctly invalidate the cache.
func markerPath(dest string) string { return dest + ".extracted-from" }

// writeExtractionMarker writes the archive SHA next to dest.
func writeExtractionMarker(markerPath, archiveSHA string) error {
	return os.WriteFile(markerPath, []byte(archiveSHA+"\n"), 0o644)
}

// extractionMarkerMatches reports whether the marker file exists and contains
// the expected archive SHA. A missing marker is reported as (false, nil) so
// callers can use this as a "do we need to (re)download?" predicate without
// also having to check for os.IsNotExist.
func extractionMarkerMatches(markerPath, archiveSHA string) (bool, error) {
	b, err := os.ReadFile(markerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return strings.TrimRight(string(b), "\n") == archiveSHA, nil
}

// extractTarGZ opens a tar.gz file, finds the entry whose basename matches
// leaf, and extracts that entry (and any sibling files under the same
// top-level prefix) into destDir. It returns the absolute path where the
// requested leaf was written, so the caller can chmod +x the right file
// (the leaf may live under a top-level subdirectory, e.g.
// destDir/llama-b9747/llama-server for the llama.cpp release tarball).
//
// Sibling libraries in the same top-level prefix are extracted too so the
// dynamic loader can find them via DYLD_LIBRARY_PATH / LD_LIBRARY_PATH set
// at spawn (see libDirForBinary in localserver.go).
func extractTarGZ(archivePath, destDir, leaf string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	leafBase := path.Base(leaf)
	// Find the top-level prefix once so we can use it to scope sidecar
	// libraries; we don't pre-require it to be present, but if it is, the
	// `prefix/leaf` form is the canonical match.
	var prefix string
	first := true
	var leafOut string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("read tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			continue
		}
		// Only allow entries whose path is a clean relative path (defense
		// against zip-slip — tar archives can encode `..` segments).
		clean := path.Clean(hdr.Name)
		if strings.HasPrefix(clean, "..") || strings.Contains(clean, "/../") {
			return "", fmt.Errorf("unsafe entry path %q in archive", hdr.Name)
		}
		if first {
			// First entry's first path component is the top-level prefix.
			if i := strings.IndexByte(clean, '/'); i > 0 {
				prefix = clean[:i+1]
			}
			first = false
		}
		// Match either "<prefix><leaf>" or a bare "<leaf>" entry, plus any
		// sibling libraries under the same top-level dir.
		baseName := path.Base(clean)
		isLeaf := baseName == leafBase
		isSibling := prefix != "" && strings.HasPrefix(clean, prefix)
		if !isLeaf && !isSibling {
			continue
		}
		out := filepath.Join(destDir, clean)
		// Final defense: ensure out is still inside destDir.
		rel, err := filepath.Rel(destDir, out)
		if err != nil || strings.HasPrefix(rel, "..") {
			return "", fmt.Errorf("archive entry escapes dest dir: %q", hdr.Name)
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return "", fmt.Errorf("mkdir for entry: %w", err)
		}
		outFile, err := os.OpenFile(out, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			return "", fmt.Errorf("open entry: %w", err)
		}
		if _, err := io.Copy(outFile, tr); err != nil {
			outFile.Close()
			return "", fmt.Errorf("extract entry %s: %w", hdr.Name, err)
		}
		if err := outFile.Close(); err != nil {
			return "", fmt.Errorf("close entry %s: %w", hdr.Name, err)
		}
		if isLeaf {
			leafOut = out
		}
	}
	if leafOut == "" {
		return "", fmt.Errorf("archive did not contain a %q entry", leaf)
	}
	// After extraction, create macOS dylib symlinks so the dynamic linker
	// can find version-triple-named files (e.g. libllama-common.0.0.9747.dylib)
	// by their short install name (libllama-common.0.dylib). This is a no-op
	// on non-macOS platforms (no .dylib files) or when no symlinks are needed.
	ensureDylibSymlinks(destDir)
	return leafOut, nil
}

// ensureDylibSymlinks scans destDir recursively for .dylib files whose
// names include a full version triple (e.g. libfoo.1.2.3.dylib) and
// creates symlinks from the short name (libfoo.1.dylib) that the macOS
// dynamic linker expects — matching the install name recorded inside
// each dylib. This is a no-op on non-macOS platforms (no .dylib files)
// or when no version-triple dylibs are present.
func ensureDylibSymlinks(destDir string) {
	entries, err := os.ReadDir(destDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		full := filepath.Join(destDir, e.Name())
		if e.IsDir() {
			ensureDylibSymlinks(full)
			continue
		}
		if !strings.HasSuffix(e.Name(), ".dylib") {
			continue
		}
		short := shortDylibName(e.Name())
		if short == "" || short == e.Name() {
			continue
		}
		shortPath := filepath.Join(destDir, short)
		if _, err := os.Lstat(shortPath); os.IsNotExist(err) {
			os.Symlink(e.Name(), shortPath)
		}
	}
}

// shortDylibName strips all but the first version component from a
// .dylib filename. Examples:
//
//	libllama-common.0.0.9747.dylib → libllama-common.0.dylib
//	libggml.0.15.2.dylib          → libggml.0.dylib
//	libllama-server-impl.dylib    → "" (no version components)
func shortDylibName(name string) string {
	if !strings.HasSuffix(name, ".dylib") {
		return ""
	}
	// Strip .dylib
	base := name[:len(name)-6]
	// Split on dots — e.g. "libllama-common.0.0.9747" → [libllama-common, 0, 0, 9747]
	parts := strings.Split(base, ".")
	if len(parts) < 3 {
		// No version components (e.g. libfoo.dylib or libfoo-impl.dylib).
		return ""
	}
	// parts[0] is the library name, parts[1] onward are version components.
	// parts[1] is the major version — that's what the dynamic linker needs.
	// Verify parts[1] is numeric so we don't create bogus symlinks for
	// unversioned multi-part names.
	if !isNumeric(parts[1]) {
		return ""
	}
	return parts[0] + "." + parts[1] + ".dylib"
}

func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

// sha256File streams a file through sha256 (no full-file buffering).
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
