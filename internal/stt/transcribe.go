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
	// Python overrides the interpreter probed for onnx-asr. Empty searches
	// python3 then python on PATH.
	Python string
	// TempDir holds normalised WAV copies. Empty uses os.TempDir.
	TempDir string
	// ModelDir holds downloaded local models, one subdirectory per catalog id.
	// Empty uses <user cache dir>/ocode/stt-models.
	ModelDir string
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

// StatusFor resolves the selected model and each entry's availability.
func StatusFor(opts Options) Status {
	selected := ResolveModel(opts.Model)
	out := Status{Selected: selected, Models: make([]ModelInfo, 0, len(catalog))}
	for _, s := range catalog {
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
			if ok, reason := probeLocal(opts.Python, s.Extra); !ok {
				m.Available = false
				m.Reason = reason
			}
		case EngineOpenAI:
			if opts.OpenAIKey == nil || opts.OpenAIKey() == "" {
				m.Available = false
				m.Reason = "needs an OpenAI API key (connect OpenAI in Settings)"
			}
		}
		out.Models = append(out.Models, m)
	}
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
		text, err := transcribeOnnx(ctx, opts.Python, s.Repo, s.Upstream, filepath.Join(modelDir, id), wav)
		if err != nil {
			return Result{}, err
		}
		return Result{Text: text, Model: id}, nil
	}
	return Result{}, fmt.Errorf("speech-to-text: unsupported engine %q", s.Engine)
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
func transcribeOnnx(ctx context.Context, python, repo, upstream, modelDir, wavPath string) (string, error) {
	py, err := resolvePython(python)
	if err != nil {
		return "", err
	}
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
	cmd := exec.CommandContext(ctx, py, "-I", scriptPath, upstream, repo, modelDir, wavPath)
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

// probeCache remembers each interpreter/module-set probe for a short time, so
// polling the settings UI does not spawn Python on every request.
var probeCache = struct {
	mu      sync.Mutex
	entries map[string]probeEntry
}{entries: map[string]probeEntry{}}

type probeEntry struct {
	at     time.Time
	ok     bool
	reason string
}

const probeTTL = 60 * time.Second

// probeLocal reports whether the local engine can run with python and the
// extra modules a model needs on top of onnx_asr.
func probeLocal(python string, extra []string) (bool, string) {
	key := python + "\x00" + strings.Join(extra, ",")
	probeCache.mu.Lock()
	defer probeCache.mu.Unlock()
	if e, ok := probeCache.entries[key]; ok && time.Since(e.at) < probeTTL {
		return e.ok, e.reason
	}
	ok, reason := doProbe(python, extra)
	probeCache.entries[key] = probeEntry{at: time.Now(), ok: ok, reason: reason}
	return ok, reason
}

func doProbe(python string, extra []string) (bool, string) {
	need := append([]string{"onnx_asr"}, extra...)
	install := `pip install "onnx-asr[cpu,hub]"`
	if len(extra) > 0 {
		install += " " + strings.Join(extra, " ")
	}
	py, err := resolvePython(python)
	if err != nil {
		return false, "needs Python 3 with " + strings.Join(need, ", ") + " (" + install + ")"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	probe := "import " + strings.Join(need, ", ")
	if err := exec.CommandContext(ctx, py, "-I", "-c", probe).Run(); err != nil {
		return false, "needs " + strings.Join(need, ", ") + " in " + py + " (" + install + ")"
	}
	return true, ""
}

func resolvePython(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	for _, name := range []string{"python3", "python"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", errors.New("speech-to-text: python3 not found on PATH")
}
