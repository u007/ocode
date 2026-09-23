package server

import (
	"github.com/u007/ocode/internal/browse/cdp"
	"github.com/u007/ocode/internal/config"
)

// resolveManagedHTROptions builds the managed `htrcli serve` options from the
// persisted browser config, applying the same resolution StartBrowse uses:
// Chrome discovery for native-host registration, the compatibility gate, the
// ocode-managed socket path, and the embedded/override asset extraction. When
// HTR is disabled, or a gate fails, Enabled is false and notice explains why
// (empty when nothing is wrong). Callers treat a false Enabled as "do not
// start" rather than an error so browsing can continue without HTR.
func resolveManagedHTROptions(browser config.BrowserConfig) (cdp.HTROptions, string) {
	htr := cdp.HTROptions{
		Enabled:        browser.HTREnabled,
		CliPath:        browser.HTRCliPath,
		ExtensionDir:   browser.HTRExtensionPath,
		Port:           browser.HTRPort,
		SocketPath:     browser.HTRSocketPath,
		NativeHostName: browser.HTRNativeHostName,
		BrowserPath:    browser.ChromePath,
	}
	if !htr.Enabled {
		return htr, ""
	}
	if htr.BrowserPath == "" {
		if browserPath, err := cdp.FindChrome(""); err == nil {
			htr.BrowserPath = browserPath
		}
	}
	if notice := cdp.HTRBrowserCompatibilityNotice(htr.BrowserPath); notice != "" {
		htr.Enabled = false
		return htr, notice
	}
	socketPath, err := cdp.ResolveHTRSocketPath(htr.SocketPath)
	if err != nil {
		htr.Enabled = false
		return htr, "HTR automation is unavailable: " + err.Error() + ". Browsing continues without the HTR extension."
	}
	htr.SocketPath = socketPath
	hostName := htr.NativeHostName
	if hostName == "" {
		hostName = cdp.DefaultHTRNativeHostName
	}
	assets, err := cdp.ResolveHTRAssetsForHost(htr.ExtensionDir, htr.CliPath, hostName)
	if err != nil {
		htr.Enabled = false
		return htr, "HTR automation is unavailable: " + err.Error() + ". Browsing continues without the HTR extension."
	}
	htr.ExtensionDir = assets.ExtensionDir
	htr.CliPath = assets.CliPath
	return htr, ""
}
