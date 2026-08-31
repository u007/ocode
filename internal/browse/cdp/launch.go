package cdp

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// ErrChromeNotFound is returned when no Chrome binary can be located.
var ErrChromeNotFound = errors.New("chrome not found — set browser.chrome_path")

// ErrUnsupportedPlatform is returned on Windows where Chrome mode is not supported.
var ErrUnsupportedPlatform = errors.New("Chrome mode is not supported on Windows yet")

// injectable for tests
var chromeGOOS = runtime.GOOS

var chromeStat = func(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

var chromeLookPath = exec.LookPath

var macOSCandidates = []string{
	"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
	"/Applications/Chromium.app/Contents/MacOS/Chromium",
	"/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
	"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
	"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
}

var linuxCandidates = []string{
	"google-chrome",
	"google-chrome-stable",
	"chromium",
	"chromium-browser",
}

// FindChrome locates the Chrome binary.
// Order: configured path → OCODE_CHROME_PATH env → platform defaults.
// Returns ErrChromeNotFound or ErrUnsupportedPlatform as appropriate.
func FindChrome(configured string) (string, error) {
	if chromeGOOS == "windows" {
		return "", ErrUnsupportedPlatform
	}
	if configured != "" {
		if _, err := chromeStat(configured); err != nil {
			return "", fmt.Errorf("%w: %s", ErrChromeNotFound, configured)
		}
		return configured, nil
	}
	if env := os.Getenv("OCODE_CHROME_PATH"); env != "" {
		if _, err := chromeStat(env); err != nil {
			return "", fmt.Errorf("%w: %s", ErrChromeNotFound, env)
		}
		return env, nil
	}
	if chromeGOOS == "darwin" {
		for _, p := range macOSCandidates {
			if _, err := chromeStat(p); err == nil {
				return p, nil
			}
		}
		return "", ErrChromeNotFound
	}
	// Linux and other Unix: probe $PATH names.
	for _, name := range linuxCandidates {
		if p, err := chromeLookPath(name); err == nil {
			return p, nil
		}
	}
	return "", ErrChromeNotFound
}
