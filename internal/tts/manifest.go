// Package tts contains the server-side speech playback boundary.
//
// Local engine manifests are deliberately data, not guessed launch commands.
// An engine is advertised as installable only when a manifest pins its
// runtime and voice artifacts (URL + SHA-256) for the current host, and as
// ready only after those artifacts are verified on disk. Browser Native needs
// no server artifact.
package tts

import (
	"fmt"
	"runtime"
	"unicode/utf8"
)

type EngineID string

const (
	EngineBrowserNative EngineID = "browser-native"
	EnginePiper         EngineID = "piper"
	EngineKokoro        EngineID = "kokoro"
	EngineFishAudio     EngineID = "fish-audio"
	EngineBreeze        EngineID = "breeze"
)

type PlaybackMode string

const (
	PlaybackManual   PlaybackMode = "manual"
	PlaybackAtBottom PlaybackMode = "at-bottom"
)

type Availability string

const (
	AvailabilityReady       Availability = "ready"
	AvailabilityInstallable Availability = "installable"
	AvailabilityUnavailable Availability = "unavailable"
)

type Engine struct {
	ID           EngineID     `json:"id"`
	Label        string       `json:"label"`
	Availability Availability `json:"availability"`
	Reason       string       `json:"reason,omitempty"`
	BrowserOnly  bool         `json:"browser_only"`
	VoiceID      string       `json:"voice_id,omitempty"`
	// Manifest metadata is only populated for engines with a pinned manifest.
	ManifestVersion string `json:"manifest_version,omitempty"`
	LicenseName     string `json:"license_name,omitempty"`
	LicenseURL      string `json:"license_url,omitempty"`
}

type Config struct {
	Engine EngineID     `json:"engine"`
	Voice  string       `json:"voice"`
	Mode   PlaybackMode `json:"mode"`
}

// Artifact is one pinned download. Size is checked before hashing so a
// truncated or replaced upstream file fails fast with a clear error.
type Artifact struct {
	Name   string
	URL    string
	SHA256 string
	Size   int64
}

// PythonRuntime pins the pip requirements installed into a per-manifest venv.
// Requirements are exact "name==version" specs; pip resolves the matching
// wheel for the host and verifies it against the index digest.
type PythonRuntime struct {
	Requirements []string
	MinPython    [2]int
}

// Manifest is the verified install recipe for one local engine + voice.
type Manifest struct {
	Engine      EngineID
	Version     string
	LicenseName string
	LicenseURL  string
	Voice       string
	VoiceFiles  []Artifact
	// Runtime is keyed by Host() (GOOS/GOARCH). Hosts absent here are
	// explicitly unavailable.
	Runtime map[string]PythonRuntime
}

// HostRuntime reports the pinned runtime for the current host.
func (m Manifest) HostRuntime() (PythonRuntime, bool) {
	rt, ok := m.Runtime[Host()]
	return rt, ok
}

const (
	piperVersion = "piper-1.8.0-joe-1"
	piperVoice   = "en_US-joe-medium"
)

// piperManifest pins the piper-tts 1.8.0 Python runtime (GPL-3.0-or-later,
// OHF-Voice/piper1-gpl) and the CC0-dataset en_US-joe-medium voice from
// rhasspy/piper-voices. onnxruntime >= 1.26 ships no macOS x86_64 wheel, so
// Intel Macs pin the last universal2 build. windows/arm64 has no piper-tts
// wheel and is therefore not listed.
var piperManifest = Manifest{
	Engine:      EnginePiper,
	Version:     piperVersion,
	LicenseName: "piper-tts GPL-3.0-or-later; en_US-joe-medium voice dataset CC0",
	LicenseURL:  "https://github.com/OHF-Voice/piper1-gpl/blob/main/COPYING",
	Voice:       piperVoice,
	VoiceFiles: []Artifact{
		{
			Name:   piperVoice + ".onnx",
			URL:    "https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_US/joe/medium/en_US-joe-medium.onnx",
			SHA256: "58afce0321b8d9c46d7cdf9c16500cc55a793b4220212dba6b70fb788b3baf06",
			Size:   63201294,
		},
		{
			Name:   piperVoice + ".onnx.json",
			URL:    "https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_US/joe/medium/en_US-joe-medium.onnx.json",
			SHA256: "3d6d5410b3795cb1950595247ef8f06190719e6fdbfa3a2356d8ec368e1aad33",
			Size:   4794,
		},
	},
	Runtime: map[string]PythonRuntime{
		"darwin/arm64":  {Requirements: piperRequirements("1.30.0"), MinPython: [2]int{3, 11}},
		"darwin/amd64":  {Requirements: piperRequirements("1.22.1"), MinPython: [2]int{3, 11}},
		"linux/amd64":   {Requirements: piperRequirements("1.30.0"), MinPython: [2]int{3, 11}},
		"linux/arm64":   {Requirements: piperRequirements("1.30.0"), MinPython: [2]int{3, 11}},
		"windows/amd64": {Requirements: piperRequirements("1.30.0"), MinPython: [2]int{3, 11}},
	},
}

func piperRequirements(onnxruntime string) []string {
	return []string{"piper-tts==1.8.0", "onnxruntime==" + onnxruntime, "pathvalidate==3.3.1"}
}

// ManifestFor returns the pinned manifest for an engine, if one exists.
func ManifestFor(id EngineID) (Manifest, bool) {
	if id == EnginePiper {
		return piperManifest, true
	}
	return Manifest{}, false
}

const maxVoiceIDRunes = 128

// ValidateVoiceID validates the user-supplied voice/model identifier before it
// is persisted or used as a cache path component. Empty is valid for engines
// that choose their default voice.
func ValidateVoiceID(voice string) error {
	if voice == "" {
		return nil
	}
	if !utf8.ValidString(voice) {
		return fmt.Errorf("voice must be valid UTF-8")
	}
	if utf8.RuneCountInString(voice) > maxVoiceIDRunes {
		return fmt.Errorf("voice must be at most %d characters", maxVoiceIDRunes)
	}
	for _, r := range voice {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return fmt.Errorf("voice may contain only letters, numbers, hyphens, underscores, and periods")
	}
	return nil
}

func DefaultConfig() Config {
	return Config{Engine: EngineBrowserNative, Mode: PlaybackManual}
}

// Catalog lists every engine with its static availability: ready (browser),
// installable (a manifest pins artifacts for this host), or unavailable.
// Installed state is layered on by Supervisor.Catalog.
func Catalog() []Engine {
	piper := Engine{ID: EnginePiper, Label: "Piper", Availability: AvailabilityUnavailable,
		Reason: "No pinned Piper runtime is available for " + Host() + "."}
	if _, ok := piperManifest.HostRuntime(); ok {
		piper.Availability = AvailabilityInstallable
		piper.Reason = "Accept the license and install to enable."
		piper.VoiceID = piperManifest.Voice
		piper.ManifestVersion = piperManifest.Version
		piper.LicenseName = piperManifest.LicenseName
		piper.LicenseURL = piperManifest.LicenseURL
	}
	return []Engine{
		{ID: EngineBrowserNative, Label: "Browser Native", Availability: AvailabilityReady, BrowserOnly: true},
		piper,
		{ID: EngineKokoro, Label: "Kokoro", Availability: AvailabilityUnavailable, Reason: "No pinned cross-platform runtime/export and voice manifest is verified."},
		{ID: EngineFishAudio, Label: "Fish Audio", Availability: AvailabilityUnavailable, Reason: "Runtime packaging and Fish Audio Research License terms are not approved for managed distribution."},
		{ID: EngineBreeze, Label: "Breeze", Availability: AvailabilityUnavailable, Reason: "Breeze model licensing and a pinned runtime/model manifest require product approval."},
	}
}

func EngineForHost(id EngineID) (Engine, bool) {
	for _, engine := range Catalog() {
		if engine.ID == id {
			return engine, true
		}
	}
	return Engine{}, false
}

func Host() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}
