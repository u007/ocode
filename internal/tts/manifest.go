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
	EngineMelo          EngineID = "melo"
	EngineKokoro        EngineID = "kokoro"
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
//
// MinPython/MaxPython are the inclusive interpreter range for which every
// pinned requirement publishes a compatible wheel. The upper bound is
// load-bearing: pip resolves the requirement set against the venv's own
// interpreter, so a too-new python3 fails the install with a raw resolver dump
// ("Ignored the following versions that require a different python version")
// instead of installing. E.g. kokoro-onnx declares Requires-Python
// <3.14,>=3.10 on every platform while onnxruntime 1.30.0 needs >=3.11, so the
// kokoro range is 3.11-3.13 even though a newer python3 is usually first on
// PATH. MaxPython zero means unbounded, but every shipped runtime sets it.
// The range is enforced when selecting an interpreter for a new install; it is
// deliberately NOT re-checked by Verify, because an existing venv keeps working
// regardless (the cache path is not keyed by interpreter version).
type PythonRuntime struct {
	Requirements []string
	MinPython    [2]int
	MaxPython    [2]int
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

// accepts reports whether an interpreter version lies inside the runtime's
// inclusive MinPython/MaxPython range. A zero MaxPython is unbounded.
func (rt PythonRuntime) accepts(version [2]int) bool {
	if version[0] < rt.MinPython[0] ||
		(version[0] == rt.MinPython[0] && version[1] < rt.MinPython[1]) {
		return false
	}
	if rt.MaxPython != [2]int{} &&
		(version[0] > rt.MaxPython[0] ||
			(version[0] == rt.MaxPython[0] && version[1] > rt.MaxPython[1])) {
		return false
	}
	return true
}

// pythonRange renders the supported interpreter range for user-facing errors
// ("3.11-3.13", or ">=3.11" when no ceiling is pinned).
func (rt PythonRuntime) pythonRange() string {
	lower := fmt.Sprintf("%d.%d", rt.MinPython[0], rt.MinPython[1])
	if rt.MaxPython == [2]int{} {
		return ">=" + lower
	}
	return lower + "-" + fmt.Sprintf("%d.%d", rt.MaxPython[0], rt.MaxPython[1])
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
		// onnxruntime 1.30.0 ships cp311-cp314 wheels for these hosts, and
		// piper-tts is cp39-abi3, so 3.14 is the newest verified interpreter.
		"darwin/arm64": {Requirements: piperRequirements("1.30.0"), MinPython: [2]int{3, 11}, MaxPython: [2]int{3, 14}},
		// onnxruntime 1.22.1 has cp310-cp313 wheels only.
		"darwin/amd64":  {Requirements: piperRequirements("1.22.1"), MinPython: [2]int{3, 10}, MaxPython: [2]int{3, 13}},
		"linux/amd64":   {Requirements: piperRequirements("1.30.0"), MinPython: [2]int{3, 11}, MaxPython: [2]int{3, 14}},
		"linux/arm64":   {Requirements: piperRequirements("1.30.0"), MinPython: [2]int{3, 11}, MaxPython: [2]int{3, 14}},
		"windows/amd64": {Requirements: piperRequirements("1.30.0"), MinPython: [2]int{3, 11}, MaxPython: [2]int{3, 14}},
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
		// kokoro-onnx 0.6.1 declares Requires-Python <3.14 on every platform,
		// so 3.13 is the ceiling regardless of the onnxruntime wheels available
		// (1.30.0 has cp314 wheels; kokoro-onnx is the binding constraint).
		"darwin/arm64": {Requirements: kokoroRequirements("1.30.0"), MinPython: [2]int{3, 11}, MaxPython: [2]int{3, 13}},
		// onnxruntime 1.22.0 has cp310-cp313 wheels only.
		"darwin/amd64":  {Requirements: kokoroRequirements("1.22.0"), MinPython: [2]int{3, 10}, MaxPython: [2]int{3, 13}},
		"linux/amd64":   {Requirements: kokoroRequirements("1.30.0"), MinPython: [2]int{3, 11}, MaxPython: [2]int{3, 13}},
		"linux/arm64":   {Requirements: kokoroRequirements("1.30.0"), MinPython: [2]int{3, 11}, MaxPython: [2]int{3, 13}},
		"windows/amd64": {Requirements: kokoroRequirements("1.30.0"), MinPython: [2]int{3, 11}, MaxPython: [2]int{3, 13}},
	},
}

func kokoroRequirements(onnxruntime string) []string {
	return []string{"kokoro-onnx==0.6.1", "onnxruntime==" + onnxruntime, "soundfile==0.12.1", "espeakng-loader==0.2.4", "phonemizer==3.4.0"}
}

const (
	// meloSourceCommit pins the MeloTTS source tree. MeloTTS publishes no
	// PyPI release (only `pip install -e .` from git), so the source is
	// distributed as a checksum-verified archive instead of a versioned
	// requirement. HEAD of main at 20914537 is the project's last commit
	// (2024-12-24), and a commit-pinned archive is immutable where a
	// branch-pinned one is not.
	meloSourceCommit = "209145371cff8fc3bd60d7be902ea69cbdb7965a"
	// meloHFRevision pins the model repository revision. The English-v2 repo
	// is the one MeloTTS's own README recommends for English.
	meloHFRevision = "main"
	meloVoice      = "EN-US"
)

// meloVersion names the cache generation for this engine. It embeds the pinned
// source commit so a MeloTTS upgrade lands in a fresh cache directory rather
// than colliding with the previous one.
var meloVersion = "melo-0.1.2-en-v2-" + meloSourceCommit[:7]

// meloVoices are the speakers in the MeloTTS-English-v2 config. They are
// selected by name via hps.data.spk2id, so a model/voice override maps
// directly onto these ids.
var meloVoices = []string{
	"EN-US",
	"EN-BR",
	"EN_INDIA",
	"EN-AU",
	"EN-Default",
}

// meloManifest pins MeloTTS (MIT) end to end so nothing is fetched at
// synthesis time.
//
// Three artifact groups are required, and all three are verified downloads:
//
//  1. The MeloTTS source tree. The package is not pip-installed: its setup.py
//     has a PostInstallCommand that shells out to `python -m unidic download`
//     (an unverified multi-hundred-megabyte fetch into the user's home
//     directory during install) and its install_requires reads requirements.txt
//     verbatim, which leaves torch/torchaudio/cached_path/gradio unpinned.
//     Instead the checksum-verified archive is extracted into the cache and
//     put on PYTHONPATH, so every dependency comes from the pins below.
//  2. The MeloTTS-English-v2 config + checkpoint, passed to TTS() as explicit
//     config_path/ckpt_path so download_utils never reaches the network.
//  3. bert-base-uncased, which melo/text/english_bert.py loads by hardcoded id
//     at import time. Without it the first synthesis silently pulls ~440 MB
//     from the Hugging Face Hub, which breaks the offline guarantee; the
//     files are placed in the cache and meloSynth redirects from_pretrained to
//     them.
//
// The two NLTK archives are also pinned rather than downloaded. g2p_en probes
// for `taggers/averaged_perceptron_tagger.zip` and `corpora/cmudict.zip` and
// calls nltk.download() when they are missing, which would write into the
// process working directory. NLTK reads the zips in place, so staging the
// verified archives under NLTK_DATA satisfies the probe with no extraction.
var meloManifest = Manifest{
	Engine:      EngineMelo,
	Version:     meloVersion,
	LicenseName: "MeloTTS MIT; bert-base-uncased Apache-2.0; en_core NLTK data Apache-2.0",
	LicenseURL:  "https://github.com/myshell-ai/MeloTTS/blob/main/LICENSE",
	LicenseText: "MeloTTS: MIT\nbert-base-uncased: Apache-2.0\nNLTK averaged_perceptron_tagger + cmudict: Apache-2.0",
	Voice:       meloVoice,
	Voices:      meloVoices,
	VoiceFiles: []Artifact{
		{
			Name:   "melo-source.tar.gz",
			URL:    "https://github.com/myshell-ai/MeloTTS/archive/" + meloSourceCommit + ".tar.gz",
			SHA256: "ceb9a1a636522fee6489c8496ef5b6b9b3b8c1e8441f402eea00d8a17fbe52b8",
			Size:   5873911,
		},
		{
			Name:   "config.json",
			URL:    "https://huggingface.co/myshell-ai/MeloTTS-English-v2/resolve/" + meloHFRevision + "/config.json",
			SHA256: "fbe2f4196068b472982651148b912387e03efd42d07c771ce126a60408c3118a",
			Size:   2356,
		},
		{
			Name:   "checkpoint.pth",
			URL:    "https://huggingface.co/myshell-ai/MeloTTS-English-v2/resolve/" + meloHFRevision + "/checkpoint.pth",
			SHA256: "794226eb7c1745f3ca281b290613d5f39aa5b0d3b16a117009966f4aaf184757",
			Size:   207769356,
		},
		// bert-base-uncased, staged under bertDirName. Only the safetensors
		// weight file is large; the rest is tokenizer metadata.
		{
			Name:   "bert/config.json",
			URL:    "https://huggingface.co/google-bert/bert-base-uncased/resolve/main/config.json",
			SHA256: "7160e1553ad2ca51d8c1cb066be533db31826e12d173824c1bb0cb1a4f187d20",
			Size:   570,
		},
		{
			Name:   "bert/model.safetensors",
			URL:    "https://huggingface.co/google-bert/bert-base-uncased/resolve/main/model.safetensors",
			SHA256: "68d45e234eb4a928074dfd868cead0219ab85354cc53d20e772753c6bb9169d3",
			Size:   440449768,
		},
		{
			Name:   "bert/tokenizer.json",
			URL:    "https://huggingface.co/google-bert/bert-base-uncased/resolve/main/tokenizer.json",
			SHA256: "ce64fce797c24f68df90b40a3f74f579b336a493db14bd583fd520ea0d8c9a98",
			Size:   466062,
		},
		{
			Name:   "bert/tokenizer_config.json",
			URL:    "https://huggingface.co/google-bert/bert-base-uncased/resolve/main/tokenizer_config.json",
			SHA256: "a025160ef0431f1a392f6f050c1310f4c5d9fb6f275932dbccba73c4d214bf10",
			Size:   48,
		},
		{
			Name:   "bert/vocab.txt",
			URL:    "https://huggingface.co/google-bert/bert-base-uncased/resolve/main/vocab.txt",
			SHA256: "07eced375cec144d27c900241f3e339478dec958f92fddbc551f295c992038a3",
			Size:   231508,
		},
		// NLTK data, staged under nltkDataDirName. Kept as the upstream .zip
		// because nltk.data.find() resolves the zip form directly and g2p_en
		// probes for exactly these two names.
		{
			Name:   "nltk_data/taggers/averaged_perceptron_tagger.zip",
			URL:    "https://raw.githubusercontent.com/nltk/nltk_data/gh-pages/packages/taggers/averaged_perceptron_tagger.zip",
			SHA256: "e1f13cf2532daadfd6f3bc481a49859f0b8ea6432ccdcd83e6a49a5f19008de9",
			Size:   2526731,
		},
		{
			Name:   "nltk_data/corpora/cmudict.zip",
			URL:    "https://raw.githubusercontent.com/nltk/nltk_data/gh-pages/packages/corpora/cmudict.zip",
			SHA256: "d07cca47fd72ad32ea9d8ad1219f85301eeaf4568f8b6b73747506a71fb5afd6",
			Size:   896069,
		},
	},
	Runtime: map[string]PythonRuntime{
		// Only darwin/arm64 is declared. This is the single host where the full
		// install was exercised end to end (venv build, checksum-verified
		// artifacts, offline synthesis), and the design spec requires a host to
		// be advertised as installable only once its pinned runtime is verified
		// rather than merely assumed from an index. Hosts absent from this map
		// are reported as unavailable with that reason instead of being offered
		// and failing mid-install — in particular the linux/* and windows/*
		// default torch wheels drag in multi-gigabyte CUDA runtime packages
		// that have not been vetted here.
		"darwin/arm64": {Requirements: meloRequirements(), MinPython: [2]int{3, 11}, MaxPython: [2]int{3, 12}},
	},
}

// meloRequirements pins MeloTTS's runtime for English synthesis. Every entry
// is an exact name==version spec verified together in one venv on darwin/arm64;
// pip resolves the transitive closure against the index like the other engines.
//
// nltk is held at 3.8.1 deliberately. From 3.9 the POS tagger resource was
// renamed to averaged_perceptron_tagger_eng, but g2p_en still probes for and
// downloads the legacy name, so a newer nltk fails at synthesis with
// "Resource 'averaged_perceptron_tagger_eng' not found" after silently
// re-downloading the data. Pinning keeps the staged archives authoritative.
//
// The Japanese/Korean/Chinese G2P backends are imported unconditionally by
// melo/text/__init__.py even though only English is used, so their
// dependencies must be present for the import to succeed. unidic-lite is used
// rather than unidic to avoid the unidic full-dictionary download that
// MeloTTS's own setup.py triggers.
func meloRequirements() []string {
	return []string{
		"torch==2.14.0",
		"torchaudio==2.11.0",
		"numpy==2.5.3",
		"soundfile==0.14.0",
		"librosa==1.0.0",
		"transformers==5.17.0",
		"cached_path==1.8.10",
		"nltk==3.8.1",
		"g2p-en==2.1.0",
		"num2words==0.5.14",
		"inflect==7.5.0",
		"Unidecode==1.4.0",
		"txtsplit==1.0.0",
		"loguru==0.7.3",
		"langid==1.1.6",
		"mecab-python3==1.0.12",
		"unidic-lite==1.0.8",
		"fugashi==1.5.2",
		"pykakasi==2.3.0",
		"jamo==0.4.1",
		"pypinyin==0.55.0",
		"cn2an==0.5.24",
		"jieba==0.42.1",
		"anyascii==0.3.3",
		"g2pkk==0.1.2",
		"gruut==2.4.0",
		"tqdm==4.70.1",
	}
}

// ManifestFor returns the pinned manifest for an engine, if one exists.
func ManifestFor(id EngineID) (Manifest, bool) {
	if id == EnginePiper {
		return piperManifest, true
	}
	if id == EngineMelo {
		return meloManifest, true
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
	melo := Engine{ID: EngineMelo, Label: "MeloTTS", Availability: AvailabilityUnavailable,
		Reason: "No pinned MeloTTS runtime and model manifest is verified for " + Host() + "."}
	melo.LicenseName = meloManifest.LicenseName
	melo.LicenseURL = meloManifest.LicenseURL
	melo.LicenseText = meloManifest.LicenseText
	melo.LicenseHash = meloManifest.LicenseHash()
	if _, ok := meloManifest.HostRuntime(); ok {
		melo.Availability = AvailabilityInstallable
		melo.Reason = "Accept the license and install to enable."
		melo.VoiceID = meloManifest.Voice
		melo.Voices = meloManifest.Voices
		melo.ManifestVersion = meloManifest.Version
	}
	// Order is the settings-UI presentation order: Browser Native, then the
	// local engines from smallest to largest install footprint. MeloTTS sits
	// between Piper and Kokoro.
	return []Engine{
		{ID: EngineBrowserNative, Label: "Browser Native", Availability: AvailabilityReady, BrowserOnly: true},
		piper,
		melo,
		kokoro,
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
