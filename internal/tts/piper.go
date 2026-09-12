package tts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// installedMarker is written last, after every artifact and the venv are
// verified, so its presence with a matching version means "installed".
const installedMarker = "installed.json"

type installedRecord struct {
	Version string `json:"version"`
	Python  string `json:"python"`
}

// pythonSpec pins the interpreter used for a venv. Command is the argv prefix
// (["python3"] or ["py", "-3"]).
type pythonSpec struct {
	Command []string
	Version [2]int
}

var pythonVersionRE = regexp.MustCompile(`Python (\d+)\.(\d+)`)

// findPython returns the first interpreter on this host that satisfies
// rt.MinPython. Candidates are probed in order; the first that runs and
// reports a new enough version wins. Every candidate is version-checked, so a
// too-old python3 on PATH never silently wins over a newer install.
func findPython(ctx context.Context, rt PythonRuntime) (pythonSpec, error) {
	var candidates [][]string
	if runtime.GOOS == "windows" {
		candidates = [][]string{{"py", "-3"}, {"python"}, {"python3"}}
	} else {
		candidates = [][]string{{"python3"}, {"python"}}
		for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin"} {
			candidates = append(candidates, []string{filepath.Join(dir, "python3")})
		}
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates, []string{filepath.Join(home, ".pyenv", "shims", "python3")})
		}
		if runtime.GOOS == "darwin" {
			matches, _ := filepath.Glob("/Library/Frameworks/Python.framework/Versions/3.*/bin/python3")
			for _, m := range matches {
				candidates = append(candidates, []string{m})
			}
		}
	}
	var tried []string
	for _, argv := range candidates {
		bin, err := exec.LookPath(argv[0])
		if err != nil {
			continue
		}
		args := append([]string{}, argv[1:]...)
		out, err := exec.CommandContext(ctx, bin, append(args, "--version")...).CombinedOutput()
		if err != nil {
			tried = append(tried, strings.Join(argv, " ")+" (failed: "+strings.TrimSpace(string(out))+")")
			continue
		}
		m := pythonVersionRE.FindSubmatch(out)
		if m == nil {
			tried = append(tried, strings.Join(argv, " ")+" (unparseable version)")
			continue
		}
		major, _ := strconv.Atoi(string(m[1]))
		minor, _ := strconv.Atoi(string(m[2]))
		if major > rt.MinPython[0] || (major == rt.MinPython[0] && minor >= rt.MinPython[1]) {
			return pythonSpec{Command: append([]string{bin}, argv[1:]...), Version: [2]int{major, minor}}, nil
		}
		tried = append(tried, fmt.Sprintf("%s (%d.%d)", strings.Join(argv, " "), major, minor))
	}
	return pythonSpec{}, fmt.Errorf("no Python >= %d.%d found (tried: %s)", rt.MinPython[0], rt.MinPython[1], strings.Join(tried, ", "))
}

func venvPython(venv string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(venv, "Scripts", "python.exe")
	}
	return filepath.Join(venv, "bin", "python")
}

// piperInstaller performs the download/verify/venv steps for one manifest
// under one cache directory. progress is called with a 0-100 value and a
// human-readable step label; it must not block.
type piperInstaller struct {
	root     string
	manifest Manifest
	client   *http.Client
	progress func(pct int, step string)
}

func (p *piperInstaller) dir() (string, error) {
	return CacheDirChecked(p.root, p.manifest.Engine, p.manifest.Voice, p.manifest.Version)
}

// Install downloads the voice artifacts, creates the venv, installs the
// pinned requirements, verifies the import, and finally writes the marker.
// Any failure leaves no marker so a retry starts from a clean verification.
func (p *piperInstaller) Install(ctx context.Context) error {
	rt, ok := p.manifest.HostRuntime()
	if !ok {
		return fmt.Errorf("no pinned runtime for host %s", Host())
	}
	dir, err := p.dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	_ = os.Remove(filepath.Join(dir, installedMarker))

	p.progress(2, "locating python")
	py, err := findPython(ctx, rt)
	if err != nil {
		return err
	}

	total := int64(0)
	for _, a := range p.manifest.VoiceFiles {
		total += a.Size
	}
	var done int64
	for _, a := range p.manifest.VoiceFiles {
		dst := filepath.Join(dir, a.Name)
		if verifyArtifact(dst, a) == nil {
			done += a.Size
			continue
		}
		p.progress(5+int(done*40/max(total, 1)), "downloading "+a.Name)
		if err := p.downloadWithRetry(ctx, a, dst, func(n int64) {
			p.progress(5+int((done+n)*40/max(total, 1)), "downloading "+a.Name)
		}); err != nil {
			return fmt.Errorf("download %s: %w", a.Name, err)
		}
		done += a.Size
	}

	venv := filepath.Join(dir, "venv")
	p.progress(50, "creating python environment")
	if err := os.RemoveAll(venv); err != nil {
		return fmt.Errorf("reset venv: %w", err)
	}
	if out, err := runCmd(ctx, py.Command, "-m", "venv", venv); err != nil {
		return fmt.Errorf("create venv: %w: %s", err, out)
	}
	p.progress(60, "installing piper-tts")
	pipArgs := append([]string{"-m", "pip", "install", "--disable-pip-version-check", "--no-input", "--quiet"}, rt.Requirements...)
	if out, err := runCmd(ctx, []string{venvPython(venv)}, pipArgs...); err != nil {
		return fmt.Errorf("pip install: %w: %s", err, out)
	}
	p.progress(92, "verifying runtime")
	if out, err := runCmd(ctx, []string{venvPython(venv)}, "-c", "import piper, onnxruntime"); err != nil {
		return fmt.Errorf("verify runtime import: %w: %s", err, out)
	}
	rec, err := json.Marshal(installedRecord{Version: p.manifest.Version, Python: strings.Join(py.Command, " ")})
	if err != nil {
		return fmt.Errorf("encode marker: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, installedMarker), rec, 0o644); err != nil {
		return fmt.Errorf("write marker: %w", err)
	}
	p.progress(100, "installed")
	return nil
}

// Verify reports whether the cache directory holds a complete, checksum-valid
// install of this manifest.
func (p *piperInstaller) Verify() error {
	dir, err := p.dir()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(dir, installedMarker))
	if err != nil {
		return fmt.Errorf("not installed: %w", err)
	}
	var rec installedRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return fmt.Errorf("corrupt install marker: %w", err)
	}
	if rec.Version != p.manifest.Version {
		return fmt.Errorf("installed version %q does not match manifest %q", rec.Version, p.manifest.Version)
	}
	for _, a := range p.manifest.VoiceFiles {
		if err := verifyArtifact(filepath.Join(dir, a.Name), a); err != nil {
			return err
		}
	}
	if _, err := os.Stat(venvPython(filepath.Join(dir, "venv"))); err != nil {
		return fmt.Errorf("venv python missing: %w", err)
	}
	return nil
}

func verifyArtifact(path string, a Artifact) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() != a.Size {
		return fmt.Errorf("%s: size %d, want %d", a.Name, info.Size(), a.Size)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != a.SHA256 {
		return fmt.Errorf("%s: checksum %s, want %s", a.Name, got, a.SHA256)
	}
	return nil
}

// downloadAttempts bounds transient-failure retries (design: three bounded
// attempts with backoff, then the user retries manually).
const downloadAttempts = 3

func (p *piperInstaller) downloadWithRetry(ctx context.Context, a Artifact, dst string, onProgress func(int64)) error {
	var errs []error
	for attempt := range downloadAttempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return errors.Join(append(errs, ctx.Err())...)
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
		}
		err := p.download(ctx, a, dst, onProgress)
		if err == nil {
			return nil
		}
		log.Printf("tts: download %s attempt %d/%d failed: %v", a.Name, attempt+1, downloadAttempts, err)
		errs = append(errs, fmt.Errorf("attempt %d: %w", attempt+1, err))
		if ctx.Err() != nil {
			break
		}
	}
	return errors.Join(errs...)
}

func (p *piperInstaller) download(ctx context.Context, a Artifact, dst string, onProgress func(int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %s", resp.Status)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".tts-download-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	var n int64
	buf := make([]byte, 256<<10)
	for {
		read, rerr := resp.Body.Read(buf)
		if read > 0 {
			if _, werr := tmp.Write(buf[:read]); werr != nil {
				_ = tmp.Close()
				return werr
			}
			n += int64(read)
			if n > a.Size {
				_ = tmp.Close()
				return fmt.Errorf("upstream file larger than pinned size %d", a.Size)
			}
			onProgress(n)
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				break
			}
			_ = tmp.Close()
			return rerr
		}
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return InstallVerified(tmpName, dst, a.SHA256)
}

func runCmd(ctx context.Context, argv []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, argv[0], append(append([]string{}, argv[1:]...), args...)...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// piperSynth runs one synthesis under the shared process supervisor. It
// returns after the WAV at outPath is complete.
func piperSynth(ctx context.Context, sup *tool.ProcessSupervisor, root string, m Manifest, id string, text, outPath string) error {
	dir, err := CacheDirChecked(root, m.Engine, m.Voice, m.Version)
	if err != nil {
		return err
	}
	model := filepath.Join(dir, m.Voice+".onnx")
	cmd := exec.CommandContext(ctx, venvPython(filepath.Join(dir, "venv")),
		"-m", "piper", "--model", model, "--config", model+".json", "--output_file", outPath)
	cmd.Stdin = strings.NewReader(text + "\n")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = io.Discard
	if _, err := tool.StartSupervised(sup, cmd, tool.ProcessRegistration{
		ID:      "tts-piper-" + id,
		Name:    "piper synth",
		Command: "python -m piper",
		Kind:    tool.ProcessKindTTS,
	}); err != nil {
		return fmt.Errorf("start piper: %w", err)
	}
	err = cmd.Wait()
	code := 0
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	if err != nil {
		sup.MarkKilled("tts-piper-"+id, code)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("piper exited: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	sup.MarkExited("tts-piper-"+id, code)
	info, err := os.Stat(outPath)
	if err != nil || info.Size() == 0 {
		return fmt.Errorf("piper produced no audio: %s", strings.TrimSpace(stderr.String()))
	}
	return nil
}

const synthTimeout = 10 * time.Minute
