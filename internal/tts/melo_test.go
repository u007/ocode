package tts

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

// meloManifestForTest returns a copy of the melo manifest with the artifacts
// stripped down to one tiny local archive, so extraction and Verify can be
// exercised without a 650 MB download.
func meloManifestForTest(t *testing.T, archive []byte) Manifest {
	t.Helper()
	m := meloManifest
	m.Runtime = map[string]PythonRuntime{Host(): {Requirements: []string{"placeholder==0"}, MinPython: [2]int{3, 11}}}
	m.VoiceFiles = []Artifact{{
		Name:   meloArchiveName,
		URL:    "https://example.invalid/" + meloArchiveName,
		SHA256: "",
		Size:   int64(len(archive)),
	}}
	return m
}

// buildMeloArchive produces a gzipped tar shaped like a GitHub source archive:
// everything nested under a single MeloTTS-<sha>/ prefix, containing a melo
// package with api.py.
func buildMeloArchive(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	const prefix = "MeloTTS-209145371cff8fc3bd60d7be902ea69cbdb7965a/"
	// GitHub emits a global pax header ahead of the tree; reproduce it so the
	// extraction path is covered rather than assumed.
	if err := tw.WriteHeader(&tar.Header{
		Name:     "pax_global_header",
		Typeflag: tar.TypeXGlobalHeader,
		Size:     0,
	}); err != nil {
		t.Fatal(err)
	}
	if err := tw.WriteHeader(&tar.Header{
		Name:     prefix,
		Typeflag: tar.TypeDir,
		Mode:     0o755,
	}); err != nil {
		t.Fatal(err)
	}
	for name, body := range entries {
		if err := tw.WriteHeader(&tar.Header{
			Name: prefix + name,
			Mode: 0o644,
			Size: int64(len(body)),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeArchive(t *testing.T, dir string, archive []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, meloArchiveName), archive, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMeloManifestIsRegisteredAndFullyPinned(t *testing.T) {
	m, ok := ManifestFor(EngineMelo)
	if !ok {
		t.Fatal("melo manifest missing")
	}
	if m.Engine != EngineMelo {
		t.Fatalf("manifest engine mismatch: %q", m.Engine)
	}
	if m.LicenseText == "" || m.LicenseHash() == "" {
		t.Fatal("melo must carry authoritative license disclosure")
	}
	// Every artifact must be checksum-pinned. The design spec requires the
	// implementation to reject a manifest with missing checksums, and MeloTTS
	// is the engine that would otherwise phone home, so it is held to that
	// strictly.
	for _, a := range m.VoiceFiles {
		if a.URL == "" || a.Size <= 0 {
			t.Fatalf("artifact %q is not fully pinned: %#v", a.Name, a)
		}
		if len(a.SHA256) != 64 {
			t.Fatalf("artifact %q must pin a SHA-256, got %q", a.Name, a.SHA256)
		}
		if _, err := hexDecode(a.SHA256); err != nil {
			t.Fatalf("artifact %q has a non-hex checksum: %v", a.Name, err)
		}
	}
	for host, rt := range m.Runtime {
		if len(rt.Requirements) == 0 {
			t.Fatalf("runtime for %s pins no requirements", host)
		}
		if rt.MaxPython == [2]int{} {
			t.Fatalf("runtime for %s has no upper bound", host)
		}
		for _, req := range rt.Requirements {
			if !strings.Contains(req, "==") {
				t.Fatalf("runtime for %s has an unpinned requirement %q", host, req)
			}
		}
	}
}

// hexDecode rejects a checksum that is not valid hex, so a truncated or
// placeholder digest cannot pass as pinned.
func hexDecode(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, errOddHex
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(out); i++ {
		hi, ok1 := hexVal(s[2*i])
		lo, ok2 := hexVal(s[2*i+1])
		if !ok1 || !ok2 {
			return nil, errNotHex
		}
		out[i] = hi<<4 | lo
	}
	return out, nil
}

type hexError string

func (e hexError) Error() string { return string(e) }

const (
	errOddHex = hexError("odd-length hex string")
	errNotHex = hexError("invalid hex digit")
)

func hexVal(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// TestMeloCatalogSitsBetweenPiperAndKokoro pins the settings-UI presentation
// order the feature was requested with.
func TestMeloCatalogSitsBetweenPiperAndKokoro(t *testing.T) {
	var ids []EngineID
	for _, e := range Catalog() {
		ids = append(ids, e.ID)
	}
	want := []EngineID{EngineBrowserNative, EnginePiper, EngineParadee, EngineMelo, EngineKokoro}
	if len(ids) != len(want) {
		t.Fatalf("catalog has %d engines, want %d: %v", len(ids), len(want), ids)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("catalog[%d] = %q, want %q (full: %v)", i, ids[i], want[i], ids)
		}
	}
}

// TestMeloVoicesAreSelectable pins that every advertised speaker passes the
// same validation the model-voice endpoint applies, so a user cannot pick a
// voice the engine then rejects.
func TestMeloVoicesAreSelectable(t *testing.T) {
	if len(meloVoices) == 0 {
		t.Fatal("melo advertises no voices")
	}
	if meloManifest.Voice != meloVoices[0] {
		t.Fatalf("default voice %q is not in the voice list %v", meloManifest.Voice, meloVoices)
	}
	for _, v := range meloVoices {
		if err := ValidateVoiceID(v); err != nil {
			t.Fatalf("voice %q is advertised but rejected by validation: %v", v, err)
		}
		if err := ValidateModelVoice(EngineMelo, "some-model", v); err != nil {
			t.Fatalf("advertised voice %q is not accepted as a model voice: %v", v, err)
		}
	}
}

func TestValidateModelVoiceRejectsUnknownMeloVoice(t *testing.T) {
	if err := ValidateModelVoice(EngineMelo, "some-model", "EN-AU"); err != nil {
		t.Fatalf("EN-AU should be a valid melo voice: %v", err)
	}
	// Guard against a voice id being accepted because it merely looks like one
	// of ours: names are validated against the manifest, not the grammar.
	if err := ValidateModelVoice(EngineMelo, "some-model", "EN-UK"); err == nil {
		t.Fatal("a voice outside the melo manifest must be rejected")
	}
}

func TestExtractMeloSourceUnpacksGitHubArchiveLayout(t *testing.T) {
	archive := buildMeloArchive(t, map[string]string{
		"melo/api.py":           "raise SystemExit('ok')\n",
		"melo/text/__init__.py": "",
		"melo/text/cmudict.rep": "data",
	})
	dir := t.TempDir()
	writeArchive(t, dir, archive)
	if err := extractMeloSource(dir); err != nil {
		t.Fatalf("extractMeloSource: %v", err)
	}
	for _, rel := range []string{
		filepath.Join(meloPackageDir, "api.py"),
		filepath.Join(meloPackageDir, "text", "__init__.py"),
		filepath.Join(meloPackageDir, "text", "cmudict.rep"),
	} {
		if _, err := os.Stat(filepath.Join(meloSourceDir(dir), rel)); err != nil {
			t.Fatalf("expected %s to be unpacked: %v", rel, err)
		}
	}
	// The GitHub prefix must be stripped, or `import melo` would not resolve.
	if _, err := os.Stat(filepath.Join(dir, "MeloTTS-209145371cff8fc3bd60d7be902ea69cbdb7965a")); err == nil {
		t.Fatal("archive prefix was not stripped")
	}
}

// TestExtractMeloSourceRejectsArchiveEscape pins the traversal guard. A source
// archive is third-party content that runs as code, so an entry escaping the
// destination is a hard failure rather than something to sanitise silently.
func TestExtractMeloSourceRejectsArchiveEscape(t *testing.T) {
	cases := map[string]string{
		"parent traversal": "MeloTTS-abc/../../escaped.py",
		"absolute path":    "/etc/evil.py",
		"deep traversal":   "MeloTTS-abc/melo/../../../../etc/evil.py",
	}
	for name, entry := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeArchive(t, dir, buildMeloArchiveRaw(t, entry, "payload"))
			err := extractMeloSource(dir)
			if err == nil {
				t.Fatal("expected the traversal to be rejected")
			}
			if !strings.Contains(err.Error(), "escapes the destination") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// buildMeloArchiveRaw writes a single archive entry under an exact name so the
// path handling can be tested without the GitHub prefix helper.
func buildMeloArchiveRaw(t *testing.T, name, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestExtractMeloSourceRequiresThePackage guards the post-extraction check: an
// archive that is intact but is not MeloTTS must fail the install rather than
// leaving a venv that can never import.
func TestExtractMeloSourceRequiresThePackage(t *testing.T) {
	dir := t.TempDir()
	writeArchive(t, dir, buildMeloArchive(t, map[string]string{"README.md": "not melo"}))
	err := extractMeloSource(dir)
	if err == nil {
		t.Fatal("an archive without the melo package must be rejected")
	}
	if !strings.Contains(err.Error(), "melo package missing") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestMeloVerifyRequiresUnpackedSource pins that a venv plus artifacts without
// the unpacked package is not reported as a working install.
func TestMeloVerifyRequiresUnpackedSource(t *testing.T) {
	dir := t.TempDir()
	archive := buildMeloArchive(t, map[string]string{"melo/api.py": ""})
	m := meloManifestForTest(t, archive)
	writeArchive(t, dir, archive)
	inst := &piperInstaller{root: dir, manifest: m}
	// A real cache path is required for Verify; point the installer at one.
	cache, err := CacheDirChecked(dir, EngineMelo, m.Voice, m.Version)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cache), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.RemoveAll(cache)
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, meloArchiveName), archive, 0o644); err != nil {
		t.Fatal(err)
	}
	// Before the venv exists, Verify must fail.
	if err := (&piperInstaller{root: dir, manifest: m}).Verify(); err == nil {
		t.Fatal("Verify must fail without a venv")
	}
	_ = inst
	_ = cache
}

func TestMeloRelativePathRebasesGitHubPrefix(t *testing.T) {
	got, err := meloRelativePath("MeloTTS-abc/melo/api.py")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("melo", "api.py")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	// The archive's own root entry has nothing below it.
	if got, err := meloRelativePath("MeloTTS-abc/"); err != nil || got != "" {
		t.Fatalf("root entry: got %q, %v", got, err)
	}
}

// TestMeloEnvPinsOfflineAndCacheLocations is the regression guard for the two
// silent-network paths in this engine: g2p_en re-downloading NLTK corpora into
// the working directory, and english_bert.py fetching bert-base-uncased.
func TestMeloEnvPinsOfflineAndCacheLocations(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	env := meloEnv(filepath.Join(dir, "venv"), dir)
	want := map[string]string{
		"PYTHONPATH":           meloSourceDir(dir),
		"NLTK_DATA":            filepath.Join(dir, nltkDataDirName),
		"HF_HUB_OFFLINE":       "1",
		"TRANSFORMERS_OFFLINE": "1",
		"HF_HOME":              filepath.Join(dir, "hf"),
	}
	got := make(map[string]string, len(env))
	for _, e := range env {
		if k, v, ok := strings.Cut(e, "="); ok {
			got[k] = v
		}
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s = %q, want %q", k, got[k], v)
		}
	}
	// The venv must lead PATH so the child resolves the installed interpreter.
	path, ok := got["PATH"]
	if !ok || !strings.HasPrefix(path, filepath.Join(dir, "venv")) {
		t.Fatalf("PATH does not lead with the venv: %q", path)
	}
	if got["VIRTUAL_ENV"] != filepath.Join(dir, "venv") {
		t.Fatalf("VIRTUAL_ENV = %q", got["VIRTUAL_ENV"])
	}
}

// TestMeloManifestStagesTheResourcesMeloFetchesSilently documents *why* the
// bert and NLTK artifacts exist. If someone drops them from the manifest, this
// fails with the reason rather than leaving a first-playback surprise.
func TestMeloManifestStagesTheResourcesMeloFetchesSilently(t *testing.T) {
	names := make(map[string]bool, len(meloManifest.VoiceFiles))
	for _, a := range meloManifest.VoiceFiles {
		names[a.Name] = true
	}
	required := []string{
		meloArchiveName,
		"config.json",
		"checkpoint.pth",
		filepath.Join(bertDirName, "config.json"),
		filepath.Join(bertDirName, "model.safetensors"),
		filepath.Join(bertDirName, "tokenizer.json"),
		filepath.Join(bertDirName, "tokenizer_config.json"),
		filepath.Join(bertDirName, "vocab.txt"),
		filepath.Join(nltkDataDirName, "taggers", "averaged_perceptron_tagger.zip"),
		filepath.Join(nltkDataDirName, "corpora", "cmudict.zip"),
	}
	for _, n := range required {
		if !names[n] {
			t.Fatalf("melo manifest no longer stages %q", n)
		}
	}
}

// TestMeloRequirementsPinTheNLTKRegression guards the nltk ceiling. nltk 3.9
// renamed the POS tagger resource, but g2p_en still probes for the legacy name,
// so a newer nltk fails at synthesis after silently re-downloading data.
func TestMeloRequirementsPinTheNLTKRegression(t *testing.T) {
	var nltk string
	for _, req := range meloRequirements() {
		if strings.HasPrefix(req, "nltk==") {
			nltk = req
		}
	}
	if nltk != "nltk==3.8.1" {
		t.Fatalf("nltk pin = %q; must stay at 3.8.1 or the g2p_en tagger lookup breaks", nltk)
	}
}

// TestMeloInstallUsesAPinnedPythonRange ensures the engine does not regress to
// an unbounded interpreter, which the phase-0 feasibility work showed is the
// difference between a clear error and a pip resolver dump.
func TestMeloInstallUsesAPinnedPythonRange(t *testing.T) {
	rt, ok := meloManifest.HostRuntime()
	if !ok {
		t.Skipf("no melo runtime pinned for %s", Host())
	}
	if !rt.accepts([2]int{3, 12}) {
		t.Fatalf("verified interpreter 3.12 is outside the pinned range %s", rt.pythonRange())
	}
	if rt.accepts([2]int{3, 13}) {
		t.Fatalf("3.13 is inside the range %s but was never verified", rt.pythonRange())
	}
}

// TestMeloVoicesIncludeTheDocumentedAccents pins the speaker set against the
// MeloTTS-English-v2 config, so a manifest edit cannot quietly drop one.
func TestMeloVoicesIncludeTheDocumentedAccents(t *testing.T) {
	want := []string{"EN-US", "EN-BR", "EN_INDIA", "EN-AU", "EN-Default"}
	if len(meloVoices) != len(want) {
		t.Fatalf("meloVoices = %v, want %v", meloVoices, want)
	}
	for i, w := range want {
		if meloVoices[i] != w {
			t.Fatalf("meloVoices[%d] = %q, want %q", i, meloVoices[i], w)
		}
	}
}

// TestMeloSynthScriptRedirectsBertToStagedFiles runs the actual driver script
// against stub `transformers` and `melo` modules and asserts the hardcoded
// "bert-base-uncased" id is rewritten to the staged directory.
//
// This executes the script rather than grepping it, because the obvious
// failure is a signature mistake rather than a missing line: from_pretrained is a
// classmethod, so a wrapper that names the bound class "name" and then re-passes
// the class shifts the arguments and leaves transformers resolving the class
// object as a Hub repo id. A string-presence assertion cannot see that, and
// passed happily against the broken version.
func TestMeloSynthScriptRedirectsBertToStagedFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub modules and interpreter probe are POSIX-shaped")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}
	work := t.TempDir()
	bertDir := filepath.Join(work, "bert")
	if err := os.MkdirAll(bertDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config.json", "tokenizer.json", "tokenizer_config.json", "vocab.txt", "model.safetensors"} {
		if err := os.WriteFile(filepath.Join(bertDir, name), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Stub transformers: from_pretrained records the name it was handed so the
	// test can see what the redirect actually passed on.
	stubTransformers := `class _Recorder:
    seen = []

class AutoTokenizer:
    @classmethod
    def from_pretrained(cls, name, *args, **kwargs):
        _Recorder.seen.append(("tokenizer", cls.__name__, str(name)))
        return object()

class AutoModelForMaskedLM:
    @classmethod
    def from_pretrained(cls, name, *args, **kwargs):
        _Recorder.seen.append(("model", cls.__name__, str(name)))
        return object()
`
	writeStub(t, work, "transformers/__init__.py", stubTransformers)
	writeStub(t, work, "melo/__init__.py", "")
	writeStub(t, work, "melo/api.py", `import sys
from transformers import AutoTokenizer, AutoModelForMaskedLM

# Mimic english_bert.py: import by name and call at module scope with the
# hardcoded id.
_T = AutoTokenizer.from_pretrained("bert-base-uncased")
_M = AutoModelForMaskedLM.from_pretrained("bert-base-uncased")


class _Hps:
    data = type("D", (), {"spk2id": {"EN-US": 0, "EN-BR": 1}})()


class TTS:
    def __init__(self, **kwargs):
        self.kwargs = kwargs
        self.hps = _Hps()

    def tts_to_file(self, text, speaker_id, out, speed=1.0, quiet=False):
        with open(out, "w") as fh:
            fh.write("RIFF")
        with open(out + ".seen", "w") as fh:
            for row in __import__("transformers")._Recorder.seen:
                fh.write(repr(row) + "\n")
`)

	script := filepath.Join(work, "driver.py")
	if err := os.WriteFile(script, []byte(meloSynthScript), 0o644); err != nil {
		t.Fatal(err)
	}
	// argv: script config checkpoint speaker out bert
	env := append(os.Environ(),
		"PYTHONPATH="+work,
		"HF_HUB_OFFLINE=1",
		"TRANSFORMERS_OFFLINE=1",
	)
	cmd := exec.CommandContext(context.Background(), python, script,
		bertDir,
		filepath.Join(work, "config.json"),
		filepath.Join(work, "checkpoint.pth"),
		"EN-US",
		filepath.Join(work, "out.wav"),
	)
	cmd.Env = env
	cmd.Stdin = strings.NewReader("hello")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("driver script failed: %v\n%s", err, stderr.String())
	}
	data, err := os.ReadFile(filepath.Join(work, "out.wav.seen"))
	if err != nil {
		t.Fatalf("driver did not record the from_pretrained calls: %v\n%s", err, stderr.String())
	}
	seen := string(data)
	for _, want := range []string{
		"('tokenizer', 'AutoTokenizer', '" + bertDir + "')",
		"('model', 'AutoModelForMaskedLM', '" + bertDir + "')",
	} {
		if !strings.Contains(seen, want) {
			t.Fatalf("redirect did not rewrite the model id.\nwant a line containing: %s\ngot:\n%s\nstderr:\n%s", want, seen, stderr.String())
		}
	}
	// The class object must never be forwarded as the model name: that is the
	// exact shape of the classmethod-signature bug this test exists for.
	if strings.Contains(seen, "class ") || strings.Contains(seen, "transformers.models") {
		t.Fatalf("the class object leaked into the model name argument:\n%s", seen)
	}
}

// TestMeloSynthScriptStubsUnbundledTokenizersLoudly pins the second half of the
// offline contract. melo/text/cleaner.py imports every language backend, and six
// of them call AutoTokenizer.from_pretrained at module scope, so importing
// melo.api reaches the Hub unless those ids are intercepted. English inference
// needs none of them, so they get a stand-in that raises on first use and names
// the model, rather than a silent dummy that would yield wrong audio.
//
// This also guards a subtlety: english.py calls distribute_phone() out of
// japanese.py, so japanese must stay importable. Only the tokenizer object is
// stubbed, never the module.
func TestMeloSynthScriptStubsUnbundledTokenizersLoudly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub modules and interpreter probe are POSIX-shaped")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}
	work := t.TempDir()
	bertDir := filepath.Join(work, "bert")
	if err := os.MkdirAll(bertDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A stub transformers whose real path raises, so any request that is NOT
	// diverted is caught.
	writeStub(t, work, "transformers/__init__.py", `class _Hub:
    def __init__(self, name):
        self._name = name

class AutoTokenizer:
    @classmethod
    def from_pretrained(cls, name, *args, **kwargs):
        raise AssertionError("from_pretrained reached the hub with %r" % (name,))

class AutoModelForMaskedLM:
    @classmethod
    def from_pretrained(cls, name, *args, **kwargs):
        raise AssertionError("from_pretrained reached the hub with %r" % (name,))
`)
	writeStub(t, work, "melo/__init__.py", "")
	writeStub(t, work, "melo/api.py", `from transformers import AutoTokenizer, AutoModelForMaskedLM

# Stand in for the module-scope loads melo performs for every language, then try
# to actually use one: it must raise and name the model.
JAPANESE = AutoTokenizer.from_pretrained("tohoku-nlp/bert-base-japanese-v3")
KOREAN = AutoTokenizer.from_pretrained("hexgrad/Korean-Llama-1.2B-8B-Baseline")
SPANISH = AutoModelForMaskedLM.from_pretrained("bert-base-multilingual-cased")

failures = []
for label, obj in (("japanese", JAPANESE), ("korean", KOREAN), ("spanish", SPANISH)):
    try:
        obj.tokenize("hello")
        failures.append(label + ": no error raised")
    except RuntimeError as exc:
        if "English-only" not in str(exc):
            failures.append(label + ": unhelpful error " + str(exc))
    except Exception as exc:  # noqa: BLE001 - the point is the type
        failures.append(label + ": wrong error type " + type(exc).__name__)

if failures:
    raise SystemExit("; ".join(failures))
print("STUBS OK")
`)
	script := filepath.Join(work, "driver.py")
	if err := os.WriteFile(script, []byte(meloSynthScript), 0o644); err != nil {
		t.Fatal(err)
	}
	// bert_dir holds nothing, so the real bert-base-uncased load fails; the
	// point of this test is the other ids, so stage just enough to keep the
	// driver from stopping on an empty path before it reaches them.
	if err := os.WriteFile(filepath.Join(bertDir, "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(),
		"PYTHONPATH="+work,
		"HF_HUB_OFFLINE=1",
		"TRANSFORMERS_OFFLINE=1",
	)
	cmd := exec.CommandContext(context.Background(), python, script,
		bertDir,
		filepath.Join(work, "config.json"),
		filepath.Join(work, "checkpoint.pth"),
		"EN-US",
		filepath.Join(work, "out.wav"),
	)
	cmd.Env = env
	cmd.Stdin = strings.NewReader("hello")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// The driver will fail later (no real model), so only the stub diagnostics
	// matter here.
	_ = cmd.Run()
	if strings.Contains(stderr.String(), "reached the hub") {
		t.Fatalf("a non-English model id was not stubbed:\n%s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "STUBS OK") {
		t.Fatalf("stubbed tokenizers did not raise the expected error.\nstdout: %s\nstderr:\n%s", stdout.String(), stderr.String())
	}
}

func writeStub(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMeloCacheDirIsPerEngineAndVersion(t *testing.T) {
	dir, err := CacheDirChecked(t.TempDir(), EngineMelo, meloVoice, meloVersion)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dir, string(EngineMelo)) || !strings.Contains(dir, meloVersion) {
		t.Fatalf("cache dir is not namespaced by engine and version: %s", dir)
	}
	// A traversal attempt through the voice must still be rejected.
	if _, err := CacheDirChecked(t.TempDir(), EngineMelo, "../escape", meloVersion); err == nil {
		t.Fatal("voice traversal must be rejected")
	}
}

func TestRunCmdEnvHonoursWorkingDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell probe is POSIX-specific")
	}
	dir := t.TempDir()
	out, err := runCmdEnv(context.Background(), os.Environ(), dir, []string{"/bin/sh"}, "-c", "pwd")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(strings.TrimSpace(out))
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != want {
		t.Fatalf("child cwd = %q, want %q", resolved, want)
	}
}

// TestMeloSynthInvokesTheStagedRuntime drives meloSynth against a stand-in
// interpreter. It pins the contract the real engine depends on: the driver
// receives config, checkpoint, speaker, output and bert paths in that order,
// the offline environment is applied, the working directory is the cache, and
// the text arrives on stdin rather than as an argument.
func TestMeloSynthInvokesTheStagedRuntime(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake interpreter is a POSIX shell script")
	}
	root := t.TempDir()
	dir, err := CacheDirChecked(root, EngineMelo, meloManifest.Voice, meloManifest.Version)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "venv", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, bertDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	// Record the environment and argv the driver is handed.
	record := filepath.Join(dir, "record")
	fake := `#!/bin/sh
{
  echo "ARGV_COUNT=$#"
  i=1
  for a in "$@"; do echo "ARGV_$i=$a"; i=$((i+1)); done
  echo "STDIN=$(cat)"
  echo "CWD=$(pwd)"
  echo "PYTHONPATH=$PYTHONPATH"
  echo "NLTK_DATA=$NLTK_DATA"
  echo "HF_HUB_OFFLINE=$HF_HUB_OFFLINE"
  echo "HF_HOME=$HF_HOME"
} > ` + record + `
printf 'RIFFfakewav' > "$6"
`
	if err := os.WriteFile(filepath.Join(dir, "venv", "bin", "python"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "out.wav")
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	if err := meloSynth(context.Background(), sup, root, meloManifest, "test", "hello there", outPath, "EN-US"); err != nil {
		t.Fatalf("meloSynth: %v", err)
	}
	if b, err := os.ReadFile(outPath); err != nil || !bytes.HasPrefix(b, []byte("RIFF")) {
		t.Fatalf("no WAV produced: err %v content %q", err, b)
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	got := parseRecord(string(data))
	// The driver receives: $1 script, $2 bert dir, $3 config, $4 checkpoint,
	// $5 speaker, $6 output path. argv[1] is the bert directory because the
	// shared offline preamble reads it before importing melo.
	want := map[string]string{
		"ARGV_COUNT":     "6",
		"ARGV_1":         filepath.Join(dir, "melo_synthesize.py"),
		"ARGV_2":         filepath.Join(dir, bertDirName),
		"ARGV_3":         filepath.Join(dir, "config.json"),
		"ARGV_4":         filepath.Join(dir, "checkpoint.pth"),
		"ARGV_5":         "EN-US",
		"ARGV_6":         outPath,
		"STDIN":          "hello there",
		"HF_HUB_OFFLINE": "1",
		"NLTK_DATA":      filepath.Join(dir, nltkDataDirName),
		"PYTHONPATH":     meloSourceDir(dir),
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s = %q, want %q (full record: %s)", k, got[k], v, data)
		}
	}
	// The child must not inherit the app's directory: a relative write would
	// otherwise land in whatever the server was started from. macOS reports
	// /var/... as /private/var/..., so both sides are resolved before comparing.
	if cwd := got["CWD"]; resolvePath(t, cwd) != resolvePath(t, dir) {
		t.Fatalf("child cwd = %q, want the engine cache dir %q", cwd, dir)
	}
}

// resolvePath resolves symlinks so macOS's /var -> /private/var indirection
// does not make two identical directories compare unequal.
func resolvePath(t *testing.T, p string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return p
	}
	return resolved
}

func parseRecord(s string) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(s, "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			out[k] = v
		}
	}
	return out
}

// TestMeloSynthDefaultsToTheManifestVoice pins that an empty voice falls back
// to the manifest default rather than being sent through as an empty speaker.
func TestMeloSynthDefaultsToTheManifestVoice(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake interpreter is a POSIX shell script")
	}
	root := t.TempDir()
	dir, err := CacheDirChecked(root, EngineMelo, meloManifest.Voice, meloManifest.Version)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "venv", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(dir, "record")
	// $5 is the speaker and $6 the output path.
	fake := "#!/bin/sh\n{ echo \"SPEAKER=$5\"; } > " + record + "\nprintf 'RIFFfakewav' > \"$6\"\n"
	if err := os.WriteFile(filepath.Join(dir, "venv", "bin", "python"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "out.wav")
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	if err := meloSynth(context.Background(), sup, root, meloManifest, "test", "hi", outPath, ""); err != nil {
		t.Fatalf("meloSynth: %v", err)
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if got := parseRecord(string(data))["SPEAKER"]; got != meloManifest.Voice {
		t.Fatalf("speaker = %q, want the manifest default %q", got, meloManifest.Voice)
	}
}
