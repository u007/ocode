package cdp

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type nativeHostManifest struct {
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Path           string   `json:"path"`
	Type           string   `json:"type"`
	AllowedOrigins []string `json:"allowed_origins"`
}

func browserFamily(browserPath string) string {
	lower := strings.ToLower(browserPath)
	base := strings.ToLower(filepath.Base(browserPath))
	switch {
	case strings.Contains(lower, "brave"):
		return "brave"
	case strings.Contains(lower, "edge"):
		return "edge"
	case strings.Contains(lower, "canary"):
		return "canary"
	case strings.Contains(lower, "chromium"):
		return "chromium"
	case strings.Contains(base, "chrome"):
		return "chrome"
	default:
		return "chrome"
	}
}

func nativeMessagingDir(browserPath string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "darwin":
		app := map[string]string{
			"chrome":   filepath.Join("Google", "Chrome"),
			"canary":   filepath.Join("Google", "Chrome Canary"),
			"chromium": "Chromium",
			"edge":     filepath.Join("Microsoft Edge"),
			"brave":    filepath.Join("BraveSoftware", "Brave-Browser"),
		}[browserFamily(browserPath)]
		return filepath.Join(home, "Library", "Application Support", app, "NativeMessagingHosts"), nil
	case "linux":
		profile := map[string]string{
			"chrome":   "google-chrome",
			"canary":   "google-chrome-unstable",
			"chromium": "chromium",
			"edge":     "microsoft-edge",
			"brave":    filepath.Join("BraveSoftware", "Brave-Browser"),
		}[browserFamily(browserPath)]
		return filepath.Join(home, ".config", profile, "NativeMessagingHosts"), nil
	case "windows":
		return filepath.Join(home, "AppData", "Local", "ocode", "htr", "native-hosts"), nil
	default:
		return "", fmt.Errorf("unsupported native-messaging platform: %s", runtime.GOOS)
	}
}

func nativeMessagingRegistryRoot(browserPath string) string {
	return map[string]string{
		"chrome":   `HKCU\Software\Google\Chrome\NativeMessagingHosts`,
		"canary":   `HKCU\Software\Google\Chrome\NativeMessagingHosts`,
		"chromium": `HKCU\Software\Chromium\NativeMessagingHosts`,
		"edge":     `HKCU\Software\Microsoft\Edge\NativeMessagingHosts`,
		"brave":    `HKCU\Software\BraveSoftware\Brave-Browser\NativeMessagingHosts`,
	}[browserFamily(browserPath)]
}

func nativeHostAllowedOrigins(extensionDir string) ([]string, error) {
	if origin := strings.TrimSpace(os.Getenv("OCODE_HTR_EXTENSION_ORIGIN")); origin != "" {
		return []string{origin}, nil
	}
	data, err := os.ReadFile(filepath.Join(extensionDir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("read extension manifest for native host: %w", err)
	}
	var manifest struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse extension manifest for native host: %w", err)
	}
	if manifest.Key == "" {
		return nil, fmt.Errorf("extension has no stable key; set OCODE_HTR_EXTENSION_ORIGIN for a development override")
	}
	der, err := base64.StdEncoding.DecodeString(manifest.Key)
	if err != nil {
		return nil, fmt.Errorf("decode extension key: %w", err)
	}
	hash := sha256.Sum256(der)
	var id strings.Builder
	for _, b := range hash[:16] {
		id.WriteByte('a' + (b >> 4))
		id.WriteByte('a' + (b & 0x0f))
	}
	return []string{"chrome-extension://" + id.String() + "/"}, nil
}

// ensureNativeHostManifest owns only the namespaced ocode host manifest. It
// never reads, rewrites, or removes com.htrcontrol.host.
func ensureNativeHostManifest(name, binaryPath, extensionDir, browserPath string) error {
	if err := validateHTRNativeHostName(name); err != nil {
		return err
	}
	dir, err := nativeMessagingDir(browserPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create native host directory: %w", err)
	}
	manifest := nativeHostManifest{
		Name:        name,
		Description: "ocode-managed HTR NControl native messaging host",
		Path:        binaryPath,
		Type:        "stdio",
	}
	manifest.AllowedOrigins, err = nativeHostAllowedOrigins(extensionDir)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, name+".json")
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write native host manifest: %w", err)
	}
	if runtime.GOOS == "windows" {
		key := nativeMessagingRegistryRoot(browserPath) + `\` + name
		cmd := exec.Command("reg", "ADD", key, "/ve", "/t", "REG_SZ", "/d", path, "/f")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("register native host in Windows registry: %w (%s)", err, strings.TrimSpace(string(output)))
		}
	}
	return nil
}
