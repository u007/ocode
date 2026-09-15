// Package tts contains the server-side speech playback boundary.
//
// Local engine manifests are deliberately data, not guessed launch commands.
// An engine is advertised as installable only when a manifest pins its
// runtime and voice artifacts (URL + SHA-256) for the current host, and as
// ready only after those artifacts are verified on disk. Browser Native needs
// no server artifact.
package tts

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"runtime"
	"strings"
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
	Voices       []string     `json:"voices,omitempty"`
	// Manifest metadata is only populated for engines with a pinned manifest.
	ManifestVersion string `json:"manifest_version,omitempty"`
	LicenseName     string `json:"license_name,omitempty"`
	LicenseURL      string `json:"license_url,omitempty"`
	LicenseText     string `json:"license_text,omitempty"`
	LicenseHash     string `json:"license_hash,omitempty"`
}

type Config struct {
	Engine     EngineID          `json:"engine"`
	Voice      string            `json:"voice"`
	Mode       PlaybackMode      `json:"mode"`
	ModelVoice map[string]string `json:"model_voice,omitempty"`
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
	// LicenseText is the exact, immutable disclosure shown before consent.
	LicenseText string
	Voice       string
	Voices      []string
	VoiceFiles  []Artifact
	// Runtime is keyed by Host() (GOOS/GOARCH). Hosts absent here are
	// explicitly unavailable.
	Runtime map[string]PythonRuntime
}

// LicenseHash returns the stable SHA-256 hash for the exact license disclosure
// associated with the manifest. Callers must hash LicenseText rather than a
// client-provided label so a disclosure change requires fresh consent.
func (m Manifest) LicenseHash() string {
	sum := sha256.Sum256([]byte(m.LicenseText))
	return hex.EncodeToString(sum[:])
}

// HostRuntime reports the pinned runtime for the current host.
func (m Manifest) HostRuntime() (PythonRuntime, bool) {
	rt, ok := m.Runtime[Host()]
	return rt, ok
}

const (
	piperVersion = "piper-1.8.0-joe-1"
	piperVoice   = "en_US-joe-medium"

	kokoroVersion = "kokoro-v1.0-voices-v1.0"
	kokoroVoice   = "af_sarah"
)

var kokoroVoices = []string{
	"af_sarah",
	"af_bella",
	"af_nicole",
	"af_sky",
	"af_alloy",
	"af_aoede",
	"af_jessica",
	"af_kore",
	"af_river",
	"am_adam",
	"am_echo",
	"am_eric",
	"am_fenrir",
	"am_liam",
	"am_michael",
	"am_onyx",
	"am_puck",
	"bf_alice",
	"bf_emma",
	"bf_isabella",
	"bf_lily",
	"bm_daniel",
	"bm_fable",
	"bm_george",
	"bm_lewis",
}

// piperManifest pins the piper-tts 1.8.0 Python runtime (GPL-3.0-or-later,
// PyPI) and en_US-joe-medium voice dataset (CC0, Hugging Face). Pinned
// requirements use onnxruntime 1.22.1 on darwin/amd64 and 1.30.0 on modern
// darwin/arm64, Linux, and Windows. Artifacts are pinned by URL and
// exact file size; checksum verification is disabled (rely on complete
// download).
var piperManifest = Manifest{
	Engine:      EnginePiper,
	Version:     piperVersion,
	LicenseName: "piper-tts GPL-3.0-or-later; en_US-joe-medium voice dataset CC0",
	LicenseURL:  "https://github.com/OHF-Voice/piper1-gpl/blob/main/COPYING",
	LicenseText: "piper-tts: GPL-3.0-or-later\nen_US-joe-medium voice dataset: CC0 1.0 Universal",
	Voice:       piperVoice,
	Voices:      []string{piperVoice},
	VoiceFiles: []Artifact{
		{
			Name:   "en_US-joe-medium.onnx",
			URL:    "https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_US/joe/medium/en_US-joe-medium.onnx",
			SHA256: "",
			Size:   63201294,
		},
		{
			Name:   "en_US-joe-medium.onnx.json",
			URL:    "https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_US/joe/medium/en_US-joe-medium.onnx.json",
			SHA256: "",
			Size:   4794,
		},
	},
	Runtime: map[string]PythonRuntime{
		"darwin/arm64":  {Requirements: piperRequirements("1.30.0"), MinPython: [2]int{3, 11}},
		"darwin/amd64":  {Requirements: piperRequirements("1.22.1"), MinPython: [2]int{3, 10}},
		"linux/amd64":   {Requirements: piperRequirements("1.30.0"), MinPython: [2]int{3, 11}},
		"linux/arm64":   {Requirements: piperRequirements("1.30.0"), MinPython: [2]int{3, 11}},
		"windows/amd64": {Requirements: piperRequirements("1.30.0"), MinPython: [2]int{3, 11}},
	},
}

func piperRequirements(onnxruntime string) []string {
	return []string{"piper-tts==1.8.0", "onnxruntime==" + onnxruntime, "pathvalidate==3.3.1"}
}

// kokoroManifest pins the kokoro-onnx Python runtime (MIT) and
// Apache-2.0 model artifacts from ApacheOne/kokoro-onnx on Hugging Face.
// The model (kokoro-v1.0.onnx) and voices (voices-v1.0.bin) are pinned
// with verified SHA-256 checksums. Voice selection is per-voice (e.g.
// "af_sarah") via the voice parameter in kokoro.create().
var kokoroManifest = Manifest{
	Engine:      EngineKokoro,
	Version:     kokoroVersion,
	LicenseName: "kokoro-onnx MIT; kokoro model Apache-2.0",
	LicenseURL:  "https://github.com/thewh1teagle/kokoro-onnx/blob/main/LICENSE",
	LicenseText: "kokoro-onnx: MIT\nkokoro model: Apache-2.0",
	Voice:       kokoroVoice,
	Voices:      kokoroVoices,
	VoiceFiles: []Artifact{
		{
			Name:   "kokoro-v1.0.onnx",
			URL:    "https://huggingface.co/ApacheOne/kokoro-onnx/resolve/main/kokoro-onnx/kokoro-v1.0.onnx",
			SHA256: "7d5df8ecf7d4b1878015a32686053fd0eebe2bc377234608764cc0ef3636a6c5",
			Size:   325532387,
		},
		{
			Name:   "voices-v1.0.bin",
			URL:    "https://huggingface.co/ApacheOne/kokoro-onnx/resolve/main/kokoro-onnx/voices-v1.0.bin",
			SHA256: "d19762d46cf0e6648cb28a7711df1637aad15818185d13f4ff840d57f2f6dfed",
			Size:   26124436,
		},
	},
	Runtime: map[string]PythonRuntime{
		"darwin/arm64":  {Requirements: kokoroRequirements("1.30.0"), MinPython: [2]int{3, 11}},
		"darwin/amd64":  {Requirements: kokoroRequirements("1.22.0"), MinPython: [2]int{3, 10}},
		"linux/amd64":   {Requirements: kokoroRequirements("1.30.0"), MinPython: [2]int{3, 11}},
		"linux/arm64":   {Requirements: kokoroRequirements("1.30.0"), MinPython: [2]int{3, 11}},
		"windows/amd64": {Requirements: kokoroRequirements("1.30.0"), MinPython: [2]int{3, 11}},
	},
}

func kokoroRequirements(onnxruntime string) []string {
	return []string{"kokoro-onnx==0.6.1", "onnxruntime==" + onnxruntime, "soundfile==0.12.1", "espeakng-loader==0.2.4", "phonemizer==3.4.0"}
}

// ManifestFor returns the pinned manifest for an engine, if one exists.
func ManifestFor(id EngineID) (Manifest, bool) {
	if id == EnginePiper {
		return piperManifest, true
	}
	if id == EngineKokoro {
		return kokoroManifest, true
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

const maxModelIDRunes = 256

// ValidateModelID validates the identifier used to associate a voice with a
// chat model. Model IDs are provider-defined, so the boundary only enforces a
// bounded, valid UTF-8 value without imposing a provider-specific grammar.
func ValidateModelID(model string) error {
	if model == "" {
		return fmt.Errorf("model must not be empty")
	}
	if strings.TrimSpace(model) != model {
		return fmt.Errorf("model must not have leading or trailing whitespace")
	}
	if !utf8.ValidString(model) {
		return fmt.Errorf("model must be valid UTF-8")
	}
	if utf8.RuneCountInString(model) > maxModelIDRunes {
		return fmt.Errorf("model must be at most %d characters", maxModelIDRunes)
	}
	for _, r := range model {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("model must not contain control characters")
		}
	}
	return nil
}

// ValidateModelVoice validates one engine/model voice override against the
// engine's authoritative manifest. An empty voice is accepted as the clear
// operation used by the targeted endpoint.
func ValidateModelVoice(engine EngineID, model, voice string) error {
	if _, ok := ManifestFor(engine); !ok {
		return fmt.Errorf("engine %q has no voice manifest", engine)
	}
	if err := ValidateModelID(model); err != nil {
		return err
	}
	if voice == "" {
		return nil
	}
	if err := ValidateVoiceID(voice); err != nil {
		return err
	}
	manifest, _ := ManifestFor(engine)
	for _, candidate := range manifest.Voices {
		if candidate == voice {
			return nil
		}
	}
	return fmt.Errorf("voice %q is not available for engine %q", voice, engine)
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
	piper.LicenseName = piperManifest.LicenseName
	piper.LicenseURL = piperManifest.LicenseURL
	piper.LicenseText = piperManifest.LicenseText
	piper.LicenseHash = piperManifest.LicenseHash()
	if _, ok := piperManifest.HostRuntime(); ok {
		piper.Availability = AvailabilityInstallable
		piper.Reason = "Accept the license and install to enable."
		piper.VoiceID = piperManifest.Voice
		piper.Voices = piperManifest.Voices
		piper.ManifestVersion = piperManifest.Version
	}
	kokoro := Engine{ID: EngineKokoro, Label: "Kokoro", Availability: AvailabilityUnavailable,
		Reason: "No pinned Kokoro runtime/export and voice manifest is verified."}
	kokoro.LicenseName = kokoroManifest.LicenseName
	kokoro.LicenseURL = kokoroManifest.LicenseURL
	kokoro.LicenseText = kokoroManifest.LicenseText
	kokoro.LicenseHash = kokoroManifest.LicenseHash()
	if _, ok := kokoroManifest.HostRuntime(); ok {
		kokoro.Availability = AvailabilityInstallable
		kokoro.Reason = "Accept the license and install to enable."
		kokoro.VoiceID = kokoroManifest.Voice
		kokoro.Voices = kokoroManifest.Voices
		kokoro.ManifestVersion = kokoroManifest.Version
	}
	return []Engine{
		{ID: EngineBrowserNative, Label: "Browser Native", Availability: AvailabilityReady, BrowserOnly: true},
		piper,
		kokoro,
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
