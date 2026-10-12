package stt

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/pyinterp"
)

// transcribeOnnxScript is the helper run by the local engine. It is embedded
// so the binary carries its own copy and never depends on a checkout.
//
//go:embed transcribe_onnx.py
var transcribeOnnxScript string

// Options is everything a transcription call needs from its host. The server
// and the TUI each supply their own key lookup and temp dir.
type Options struct {
	// Model is a catalog id. Empty or unknown falls back to DefaultModel.
	Model string
	// OpenAIKey returns the OpenAI API key, or "" when none is configured.
	OpenAIKey func() string
	// OpenAIBaseURL overrides https://api.openai.com/v1 (tests, proxies).
	OpenAIBaseURL string
	// Python overrides the interpreter probed for onnx-asr. Empty searches the
	// Python 3.10+ candidates for the first one that imports onnx_asr.
	Python string
	// TempDir holds normalised WAV copies. Empty uses os.TempDir.
	TempDir string
	// ModelDir holds downloaded local models, one subdirectory per catalog id.
	// Empty uses <user cache dir>/ocode/stt-models.
	ModelDir string
	// InstallApproved lets Transcribe build the managed Python environment and
	// pip-install onnx-asr when no interpreter has it. Without it Transcribe
	// returns an *InstallRequiredError so the host can ask first. A pinned
	// Python is never modified regardless.
	InstallApproved bool
}

// Result is one finished transcription.
type Result struct {
	Text  string `json:"text"`
	Model string `json:"model"`
}

// maxAudioBytes caps one upload. ~25 MB is the OpenAI limit and well above a
// minute of browser-encoded speech.
const maxAudioBytes = 25 << 20

// localTimeout bounds one local decode. The first call also downloads the
// model, which can take several minutes on a slow link.
const localTimeout = 10 * time.Minute

// Status lists every catalog model with availability for this host.
type Status struct {
	Selected string      `json:"selected"`
	Models   []ModelInfo `json:"models"`
}

// ModelAvailable reports whether id is in the list and usable on this host.
func (s Status) ModelAvailable(id string) bool {
	for _, m := range s.Models {
		if m.ID == id {
			return m.Available
		}
	}
	return false
}

// StatusFor resolves the selected model and each entry's availability. Local
// models are probed concurrently: each distinct extras set is its own
// interpreter scan (up to probeTimeout), and probing them one after another
// made the status request wait for the sum. Models sharing a key share the
// in-flight scan through the probe cache.
func StatusFor(opts Options) Status {
	selected := ResolveModel(opts.Model)
	out := Status{Selected: selected, Models: make([]ModelInfo, len(catalog))}
	var wg sync.WaitGroup
	for i, s := range catalog {
		m := ModelInfo{
			ID:          s.ID,
			Label:       s.Label,
			Engine:      s.Engine,
			Languages:   s.Languages,
			SizeMB:      s.SizeMB,
			Description: s.Description,
			Available:   true,
		}
		switch s.Engine {
		case EngineLocal:
			out.Models[i] = m
			wg.Add(1)
			go func(i int, extra []string) {
				defer wg.Done()
				ok, reason := probeLocal(opts.Python, extra)
				if !ok && opts.mayInstall() {
					// A usable base Python means the first transcription can
					// install the rest, so the model is selectable now.
					ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
					defer cancel()
					if _, err := baseInterpreter(ctx); err == nil {
						ok, reason = true, autoInstallNote
						out.Models[i].NeedsInstall = true
					}
				}
				if !ok {
					out.Models[i].Available = false
				}
				out.Models[i].Reason = reason
			}(i, s.Extra)
			continue
		case EngineOpenAI:
			if opts.OpenAIKey == nil || opts.OpenAIKey() == "" {
				m.Available = false
				m.Reason = "needs an OpenAI API key (connect OpenAI in Settings)"
			}
		}
		out.Models[i] = m
	}
	wg.Wait()
	return out
}

// Transcribe decodes audioPath with the selected model. Local engines need
// WAV, so other containers are converted with ffmpeg first.
func Transcribe(ctx context.Context, opts Options, audioPath string) (Result, error) {
	id := ResolveModel(opts.Model)
	s, _ := lookup(id)
	if err := checkSize(audioPath); err != nil {
		return Result{}, err
	}
	switch s.Engine {
	case EngineOpenAI:
		key := ""
		if opts.OpenAIKey != nil {
			key = opts.OpenAIKey()
		}
		if key == "" {
			return Result{}, errors.New("speech-to-text: no OpenAI API key configured")
		}
		text, err := transcribeOpenAI(ctx, key, opts.baseURL(), s.Upstream, audioPath)
		if err != nil {
			return Result{}, err
		}
		return Result{Text: text, Model: id}, nil
	case EngineLocal:
		wav, cleanup, err := normaliseToWAV(ctx, audioPath, opts.TempDir)
		if err != nil {
			return Result{}, err
		}
		defer cleanup()
		modelDir, err := opts.modelRoot()
		if err != nil {
			return Result{}, err
		}
		py, err := localInterpreter(opts.Python, s.Extra)
		if err != nil && opts.mayInstall() {
			if !opts.InstallApproved {
				if req := installRequirement(ctx, s.Extra); req != nil {
					return Result{}, req
				}
			} else {
				py, err = installLocal(ctx, s.Extra)
				if err != nil {
					return Result{}, fmt.Errorf("speech-to-text: installing local model dependencies: %w", err)
				}
			}
		}
		if err != nil {
			return Result{}, fmt.Errorf("speech-to-text: local model: %w", err)
		}
		text, err := transcribeOnnx(ctx, py.Command, s.Repo, s.Upstream, filepath.Join(modelDir, id), wav)
		if err != nil {
			return Result{}, err
		}
		return Result{Text: text, Model: id}, nil
	}
	return Result{}, fmt.Errorf("speech-to-text: unsupported engine %q", s.Engine)
}

// PartialTranscribe decodes the audio rec has captured so far with the same
// Options as a final transcription. It only reads the live file, never
// signals or reopens the capture, and removes its snapshot before returning.
// It writes nothing to stdout or stderr; the caller decides what a failure
// means (a live partial that fails is simply not shown).
func PartialTranscribe(ctx context.Context, opts Options, rec *Recording) (Result, error) {
	snap, err := rec.Snapshot()
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = os.Remove(snap) }()
	// A live partial is polled repeatedly and its failure is simply not shown;
	// it must never start a multi-minute install, whatever the caller approved.
	opts.InstallApproved = false
	return Transcribe(ctx, opts, snap)
}

func (o Options) modelRoot() (string, error) {
	if o.ModelDir != "" {
		return o.ModelDir, nil
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("speech-to-text: no cache dir: %w", err)
	}
	return filepath.Join(cache, "ocode", "stt-models"), nil
}

func (o Options) baseURL() string {
	if o.OpenAIBaseURL != "" {
		return strings.TrimRight(o.OpenAIBaseURL, "/")
	}
	return "https://api.openai.com/v1"
}

func checkSize(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("speech-to-text: %w", err)
	}
	if fi.Size() == 0 {
		return errors.New("speech-to-text: the recording is empty")
	}
	if fi.Size() > maxAudioBytes {
		return fmt.Errorf("speech-to-text: recording exceeds %d MB", maxAudioBytes>>20)
	}
	return nil
}

// transcribeOpenAI posts the file as multipart/form-data. The response is
// read as JSON, which both whisper-1 and gpt-4o-transcribe accept.
func transcribeOpenAI(ctx context.Context, key, baseURL, model, audioPath string) (string, error) {
	f, err := os.Open(audioPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(fw, f); err != nil {
		return "", err
	}
	if err := mw.WriteField("model", model); err != nil {
		return "", err
	}
	if err := mw.Close(); err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/audio/transcriptions", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("speech-to-text: openai request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("speech-to-text: openai returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("speech-to-text: openai response: %w", err)
	}
	return strings.TrimSpace(out.Text), nil
}

// transcribeOnnx runs the embedded helper under python -I (isolated mode, so
// no script or module in the working directory can shadow onnx_asr). The
// helper fetches the model into modelDir as real files and loads it from
// there: onnx-asr's default HF cache uses symlinks into blobs/, which
// onnxruntime rejects for external weight files. Only stdout is parsed;
// stderr carries download progress and is surfaced on failure.
func transcribeOnnx(ctx context.Context, interpreter []string, repo, upstream, modelDir, wavPath string) (string, error) {
	scriptDir, err := os.MkdirTemp("", "ocode-stt-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(scriptDir)
	scriptPath := filepath.Join(scriptDir, "transcribe_onnx.py")
	if err := os.WriteFile(scriptPath, []byte(transcribeOnnxScript), 0o600); err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, localTimeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	args := append(append([]string{}, interpreter[1:]...), "-I", scriptPath, upstream, repo, modelDir, wavPath)
	cmd := exec.CommandContext(ctx, interpreter[0], args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 800 {
			msg = msg[len(msg)-800:]
		}
		return "", fmt.Errorf("speech-to-text: local model failed: %v: %s", err, msg)
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return "", fmt.Errorf("speech-to-text: local model output: %w", err)
	}
	return strings.TrimSpace(out.Text), nil
}

// normaliseToWAV returns a WAV path the local engine can read. WAV input is
// used as-is; anything else (browser webm/opus, mp4) goes through ffmpeg.
func normaliseToWAV(ctx context.Context, in, tempDir string) (string, func(), error) {
	noop := func() {}
	if strings.EqualFold(filepath.Ext(in), ".wav") {
		return in, noop, nil
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", noop, errors.New("speech-to-text: the local model needs ffmpeg to decode this audio format")
	}
	dir, err := os.MkdirTemp(tempDir, "ocode-stt-wav-")
	if err != nil {
		return "", noop, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	out := filepath.Join(dir, "input.wav")
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-y",
		"-i", in, "-ac", "1", "-ar", "16000", out)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		cleanup()
		return "", noop, fmt.Errorf("speech-to-text: ffmpeg decode: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, cleanup, nil
}

// localRange is the interpreter range the local engine runs on. The onnx-asr
// wheel requires Python 3.10 or newer and declares no ceiling.
var localRange = pyinterp.Range{Min: [2]int{3, 10}}

// probeTimeout bounds one interpreter scan. Each candidate is version-checked
// and then imports onnx_asr, which loads onnxruntime and can take seconds.
const probeTimeout = 60 * time.Second

// probeCache remembers each interpreter scan for a short time, so polling the
// settings UI does not re-spawn Python on every request, and so the
// transcription that follows a probe runs under the same interpreter. mu guards
// only the map: a scan runs outside it, so a 60 s scan never blocks status
// polling or Transcribe for other keys.
var probeCache = struct {
	mu      sync.Mutex
	entries map[string]*probeEntry
}{entries: map[string]*probeEntry{}}

// probeEntry is one scan. done is closed once py, err and at are set; callers
// that find an unfinished entry wait on it instead of scanning again.
type probeEntry struct {
	done chan struct{}
	at   time.Time
	py   pyinterp.Spec
	err  error
}

func (e *probeEntry) finished() bool {
	select {
	case <-e.done:
		return true
	default:
		return false
	}
}

const probeTTL = 60 * time.Second

// localInterpreter returns the interpreter the local engine runs under: python
// when set, otherwise the first candidate in localRange that imports onnx_asr
// and the model's extra modules. Status and transcription share the cache, so
// they agree on one interpreter without re-scanning, and concurrent callers for
// the same key share one in-flight scan.
func localInterpreter(python string, extra []string) (pyinterp.Spec, error) {
	key := python + "\x00" + strings.Join(extra, ",")
	probeCache.mu.Lock()
	e, ok := probeCache.entries[key]
	if ok && (!e.finished() || time.Since(e.at) < probeTTL) {
		probeCache.mu.Unlock()
		<-e.done
		return e.py, e.err
	}
	e = &probeEntry{done: make(chan struct{})}
	probeCache.entries[key] = e
	probeCache.mu.Unlock()

	e.py, e.err = scanLocal(python, extra)
	e.at = time.Now()
	probeCache.mu.Lock()
	if errors.Is(e.err, context.DeadlineExceeded) {
		// A timed-out scan says nothing about the host; the next call rescans.
		delete(probeCache.entries, key)
	}
	probeCache.mu.Unlock()
	close(e.done)
	return e.py, e.err
}

// scanLocal runs one uncached interpreter scan under probeTimeout. A pinned
// python that is not an executable is reported by path: Select skips missing
// candidate names silently, which is right for the search list but would hide
// the one path the user configured.
func scanLocal(python string, extra []string) (pyinterp.Spec, error) {
	candidates := pyinterp.Candidates(localRange)
	if managed := managedPython(); managed != "" {
		candidates = append([][]string{{managed}}, candidates...)
	}
	if python != "" {
		if _, err := exec.LookPath(python); err != nil {
			return pyinterp.Spec{}, fmt.Errorf("configured python %q is not an executable: %w", python, err)
		}
		candidates = [][]string{{python}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	return pyinterp.Select(ctx, localRange, candidates, localModules(extra))
}

// localModules is every module the local engine imports: onnx_asr plus the
// model's extras.
func localModules(extra []string) []string {
	return append([]string{"onnx_asr"}, extra...)
}

// probeLocal reports whether the local engine can run, with the reason when it
// cannot. The reason names every interpreter scanned and how it was rejected,
// and the install hint targets any Python 3.10+ the user can pip into.
func probeLocal(python string, extra []string) (bool, string) {
	if _, err := localInterpreter(python, extra); err != nil {
		hint := `pip install "onnx-asr[cpu,hub]"`
		if len(extra) > 0 {
			hint += " " + strings.Join(extra, " ")
		}
		return false, err.Error() + "; install into a Python 3.10+ with: " + hint
	}
	return true, ""
}
