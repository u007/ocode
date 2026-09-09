package cdp

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/u007/ocode/internal/filelock"
	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/version"
)

// The Makefile replaces this archive with the unpacked extension and the
// platform-matched htrcli binaries before install/desktop builds. A small
// placeholder is committed so ordinary source builds remain valid and simply
// report that the optional HTR bundle is unavailable.
//
//go:embed htr-assets.zip
var embeddedHTRArchive []byte

const (
	htrExtensionArchivePath = "extension/"
	htrBinaryArchivePath    = "htrcli/"
	htrAssetStampName       = ".archive-sha256"
)

// HTRAssetPaths is the resolved runtime location of the managed extension and
// htrcli binary. Empty values mean that this build has no bundled HTR assets.
type HTRAssetPaths struct {
	ExtensionDir string
	CliPath      string
}

// pinExtensionKey gives an unpacked embedded extension a stable Chrome ID so
// its native-messaging allow-list can be precise. Development overrides are
// never rewritten; they may provide OCODE_HTR_EXTENSION_ORIGIN instead.
func pinExtensionKey(extensionDir string) error {
	manifestPath := filepath.Join(extensionDir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parse HTR extension manifest: %w", err)
	}
	if key, _ := manifest["key"].(string); key != "" {
		return nil
	}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("generate HTR extension key: %w", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return fmt.Errorf("marshal HTR extension key: %w", err)
	}
	manifest["key"] = base64.StdEncoding.EncodeToString(der)
	updated, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(manifestPath, append(updated, '\n'), 0o644)
}

func extensionRuntimeDirName(hostName string) string {
	digest := sha256.Sum256([]byte(hostName))
	return fmt.Sprintf("extension-%x", digest[:4])
}

func rewriteNativeHostName(extensionDir, hostName string) error {
	return filepath.WalkDir(extensionDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		updated := bytes.ReplaceAll(data, []byte("com.htrcontrol.host"), []byte(hostName))
		if bytes.Equal(data, updated) {
			return nil
		}
		return os.WriteFile(path, updated, 0o644)
	})
}

func openEmbeddedHTRArchive() (*zip.Reader, error) {
	reader, err := zip.NewReader(bytes.NewReader(embeddedHTRArchive), int64(len(embeddedHTRArchive)))
	if err != nil {
		return nil, fmt.Errorf("invalid embedded HTR bundle: %w", err)
	}
	return reader, nil
}

func findEmbeddedFile(reader *zip.Reader, name string) *zip.File {
	for _, file := range reader.File {
		if file.Name == name {
			return file
		}
	}
	return nil
}

func extractEmbeddedTree(reader *zip.Reader, prefix, dst string) error {
	for _, file := range reader.File {
		if !strings.HasPrefix(file.Name, prefix) || file.Name == prefix {
			continue
		}
		if strings.HasSuffix(file.Name, "/.DS_Store") || strings.Contains(file.Name, "/.DS_Store/") {
			continue
		}
		rel := strings.TrimPrefix(file.Name, prefix)
		if filepath.IsAbs(filepath.FromSlash(rel)) || rel == "." || strings.HasPrefix(rel, "../") || strings.Contains(rel, "/../") || strings.Contains(rel, `\`) {
			return fmt.Errorf("unsafe HTR archive path %q", file.Name)
		}
		if rel == "" || strings.HasSuffix(file.Name, "/") {
			if err := os.MkdirAll(filepath.Join(dst, rel), 0o755); err != nil {
				return err
			}
			continue
		}
		out := filepath.Join(dst, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		in, err := file.Open()
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(in)
		_ = in.Close()
		if readErr != nil {
			return readErr
		}
		mode := os.FileMode(0o644)
		if strings.HasPrefix(file.Name, htrBinaryArchivePath) {
			mode = 0o755
		}
		if err := os.WriteFile(out, data, mode); err != nil {
			return err
		}
	}
	return nil
}

func extractEmbeddedFile(file *zip.File, dst string) error {
	in, err := file.Open()
	if err != nil {
		return err
	}
	data, readErr := io.ReadAll(in)
	_ = in.Close()
	if readErr != nil {
		return readErr
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".htr-asset-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return replaceHTRFile(tmpName, dst)
}

func htrArchiveDigest() string {
	digest := sha256.Sum256(embeddedHTRArchive)
	return fmt.Sprintf("%x", digest[:])
}

func writeHTRAssetStamp(path, digest string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".htr-stamp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := fmt.Fprintln(tmp, digest); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return replaceHTRFile(tmpName, path)
}

func hasHTRAssetStamp(path, digest string) bool {
	data, err := os.ReadFile(path)
	return err == nil && strings.TrimSpace(string(data)) == digest
}

func validateExtractedExtension(dir string) error {
	manifestPath := filepath.Join(dir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}
	if _, ok := manifest["manifest_version"]; !ok {
		return fmt.Errorf("manifest has no manifest_version")
	}
	return filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Name() == ".DS_Store" {
			return fmt.Errorf("unexpected .DS_Store in extracted extension")
		}
		return nil
	})
}

func validateExtractedBinary(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() || info.Size() == 0 {
		return fmt.Errorf("extracted htrcli is empty or a directory")
	}
	return nil
}

// ResolveHTRAssets returns development overrides when configured, otherwise
// extracts the embedded release assets into a versioned global runtime dir.
// Extraction is idempotent and never mutates a user's source checkout.
func ResolveHTRAssets(extensionOverride, cliOverride string) (HTRAssetPaths, error) {
	return ResolveHTRAssetsForHost(extensionOverride, cliOverride, DefaultHTRNativeHostName)
}

// ResolveHTRAssetsForHost is ResolveHTRAssets with the native host name used
// by the embedded extension. Each name gets its own runtime extraction path,
// so changing the configured name cannot reuse an extension rewritten for a
// previous setting.
func ResolveHTRAssetsForHost(extensionOverride, cliOverride, hostName string) (HTRAssetPaths, error) {
	pathsOut := HTRAssetPaths{ExtensionDir: resolveExtensionDir(extensionOverride)}
	if cliOverride != "" {
		bin, err := ResolveHTRCliBinary(cliOverride)
		if err != nil {
			return HTRAssetPaths{}, err
		}
		pathsOut.CliPath = bin
	}

	reader, archiveErr := openEmbeddedHTRArchive()
	if archiveErr != nil {
		// A source checkout without prepared assets is supported when both
		// development overrides are supplied. Otherwise the daemon cannot be
		// launched from this build and the caller will show the fallback notice.
		return pathsOut, nil
	}

	root, err := paths.OcodeGlobalDataDir()
	if err != nil {
		return HTRAssetPaths{}, fmt.Errorf("resolve HTR runtime dir: %w", err)
	}
	dir := filepath.Join(root, "htr", version.Version)
	extensionDirName := extensionRuntimeDirName(hostName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return HTRAssetPaths{}, fmt.Errorf("create HTR runtime dir: %w", err)
	}
	lockPath := filepath.Join(dir, "assets.lock")
	if err := filelock.WithFileLock(lockPath, func() error {
		return resolveEmbeddedHTRAssets(reader, dir, extensionDirName, hostName, extensionOverride, &pathsOut)
	}); err != nil {
		return HTRAssetPaths{}, err
	}
	return pathsOut, nil
}

func resolveEmbeddedHTRAssets(reader *zip.Reader, dir, extensionDirName, hostName, extensionOverride string, pathsOut *HTRAssetPaths) error {
	digest := htrArchiveDigest()

	if pathsOut.ExtensionDir == "" && findEmbeddedFile(reader, htrExtensionArchivePath+"manifest.json") != nil {
		ext := filepath.Join(dir, extensionDirName)
		stamp := filepath.Join(dir, extensionDirName+"-"+htrAssetStampName)
		if !hasHTRAssetStamp(stamp, digest) || validateExtractedExtension(ext) != nil {
			if err := os.RemoveAll(ext); err != nil {
				return fmt.Errorf("remove invalid HTR extension: %w", err)
			}
			tmp, err := os.MkdirTemp(dir, extensionDirName+".tmp-*")
			if err != nil {
				return fmt.Errorf("create HTR extension temp dir: %w", err)
			}
			defer os.RemoveAll(tmp)
			if err := extractEmbeddedTree(reader, htrExtensionArchivePath, tmp); err != nil {
				return fmt.Errorf("extract HTR extension: %w", err)
			}
			if err := pinExtensionKey(tmp); err != nil {
				return fmt.Errorf("pin HTR extension identity: %w", err)
			}
			if err := validateExtractedExtension(tmp); err != nil {
				return fmt.Errorf("validate HTR extension: %w", err)
			}
			if err := os.Rename(tmp, ext); err != nil {
				return fmt.Errorf("install extracted HTR extension: %w", err)
			}
			if err := writeHTRAssetStamp(stamp, digest); err != nil {
				return fmt.Errorf("record HTR extension digest: %w", err)
			}
		}
		if err := pinExtensionKey(ext); err != nil {
			return fmt.Errorf("pin HTR extension identity: %w", err)
		}
		if extensionOverride == "" {
			if err := rewriteNativeHostName(ext, hostName); err != nil {
				return fmt.Errorf("namespace HTR extension native host: %w", err)
			}
		}
		pathsOut.ExtensionDir = ext
	}

	assetName := runtime.GOOS + "-" + runtime.GOARCH
	if pathsOut.CliPath == "" && findEmbeddedFile(reader, htrBinaryArchivePath+assetName) != nil {
		bin := filepath.Join(dir, "htrcli", assetName)
		stamp := filepath.Join(dir, "htrcli-"+assetName+"-"+htrAssetStampName)
		if !hasHTRAssetStamp(stamp, digest) || validateExtractedBinary(bin) != nil {
			if err := os.Remove(bin); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove invalid embedded htrcli: %w", err)
			}
			if err := extractEmbeddedFile(findEmbeddedFile(reader, htrBinaryArchivePath+assetName), bin); err != nil {
				return fmt.Errorf("extract embedded htrcli: %w", err)
			}
			if err := validateExtractedBinary(bin); err != nil {
				return fmt.Errorf("validate embedded htrcli: %w", err)
			}
			if err := writeHTRAssetStamp(stamp, digest); err != nil {
				return fmt.Errorf("record htrcli digest: %w", err)
			}
		}
		pathsOut.CliPath = bin
	}

	return nil
}
