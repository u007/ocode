package tts

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/u007/ocode/internal/tool"
)

// MeloTTS is unpacked from a checksum-verified source archive rather than
// pip-installed. Its setup.py registers a post-install hook that shells out to
// `python -m unidic download` — an unpinned, unverified, user-home-writing
// download during install — and its install_requires reads requirements.txt
// verbatim, leaving torch and friends unpinned. Both conflict with the
// manifest contract, so the archive is unpacked to the cache and imported from
// there with the pinned requirements installed into the venv.
const (
	// meloSourceDirName holds the unpacked melo package.
	meloSourceDirName = "melo-src"
	// meloArchiveName is the verified source artifact in the manifest.
	meloArchiveName = "melo-source.tar.gz"
	// meloPackageDir is the importable package inside the unpacked tree.
	meloPackageDir = "melo"
)

// bertDirName stages the bert-base-uncased files that
// melo/text/english_bert.py loads by hardcoded model id.
const bertDirName = "bert"

// nltkDataDirName stages the NLTK corpora g2p_en probes for. NLTK reads a
// resource directly from its .zip, so the verified archives are used as-is.
const nltkDataDirName = "nltk_data"

func meloSourceDir(dir string) string { return filepath.Join(dir, meloSourceDirName) }

// meloEnv builds the environment the MeloTTS runtime needs. Two of these
// entries are load-bearing rather than cosmetic:
//
//   - NLTK_DATA points at the staged corpora. g2p_en calls nltk.download() when
//     they are missing, and with the default paths that writes into the process
//     working directory.
//   - HF_HUB_OFFLINE plus the from_pretrained redirect in the synth script keep
//     english_bert.py off the Hugging Face Hub. The model is already staged, so
//     any network attempt is a bug, and failing fast surfaces it instead of
//     silently pulling ~440 MB into the user's cache.
//
// ORT_DISABLE_TELEMETRY is carried from the shared synth environment for the
// same cwd-hygiene reason documented on applySynthProcessEnv.
func meloEnv(venv, dir string) []string {
	env := append([]string{}, os.Environ()...)
	set := append([]string{
		"PYTHONPATH=" + meloSourceDir(dir),
		"NLTK_DATA=" + filepath.Join(dir, nltkDataDirName),
		"HF_HUB_OFFLINE=1",
		"TRANSFORMERS_OFFLINE=1",
		"TOKENIZERS_PARALLELISM=false",
		"HF_HOME=" + filepath.Join(dir, "hf"),
		"ORT_DISABLE_TELEMETRY=1",
	}, pythonVenvEnv(venv)...)
	return append(env, set...)
}

// pythonVenvEnv returns the VIRTUAL_ENV/PATH entries that make an interpreter
// path resolve to a venv without activating it.
func pythonVenvEnv(venv string) []string {
	if venv == "" {
		return nil
	}
	bin := "bin"
	if os.PathSeparator == '\\' {
		bin = "Scripts"
	}
	binDir := filepath.Join(venv, bin)
	path := binDir
	if p := os.Getenv("PATH"); p != "" {
		path = binDir + string(os.PathListSeparator) + p
	}
	return []string{"VIRTUAL_ENV=" + venv, "PATH=" + path}
}

// extractMeloSource unpacks the verified MeloTTS archive into the cache.
//
// GitHub archives nest everything under a single `MeloTTS-<sha>/` directory, so
// entries are rebased onto meloSourceDir. Extraction is atomic-ish: it builds a
// sibling temporary directory and renames it into place, so an interrupted
// install never leaves a half-unpacked package that Verify would accept. Only
// regular files and directories are materialised, and no entry may escape the
// destination.
func extractMeloSource(dir string) error {
	archive := filepath.Join(dir, meloArchiveName)
	f, err := os.Open(archive)
	if err != nil {
		return fmt.Errorf("open %s: %w", meloArchiveName, err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("read %s: %w", meloArchiveName, err)
	}
	defer gz.Close()

	dest := meloSourceDir(dir)
	staging, err := os.MkdirTemp(dir, ".melo-src-*")
	if err != nil {
		return fmt.Errorf("stage melo source: %w", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("unpack %s: %w", meloArchiveName, err)
		}
		// GitHub writes a pax_global_header entry that carries no payload.
		if header.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		rel, err := meloRelativePath(header.Name)
		if err != nil {
			return fmt.Errorf("unpack %s: %w", meloArchiveName, err)
		}
		if rel == "" {
			continue
		}
		target := filepath.Join(staging, rel)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("unpack %s: %w", meloArchiveName, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("unpack %s: %w", meloArchiveName, err)
			}
			if err := writeArchiveFile(target, tr, os.FileMode(header.Mode).Perm()); err != nil {
				return fmt.Errorf("unpack %s: %w", meloArchiveName, err)
			}
		default:
			// Symlinks and device nodes are not needed by the melo package and
			// are the usual archive-escape vector, so they are dropped.
			continue
		}
	}
	if _, err := os.Stat(filepath.Join(staging, meloPackageDir, "api.py")); err != nil {
		return fmt.Errorf("melo package missing from %s: %w", meloArchiveName, err)
	}
	_ = os.RemoveAll(dest)
	if err := os.Rename(staging, dest); err != nil {
		return fmt.Errorf("install melo source: %w", err)
	}
	return nil
}

// meloRelativePath rebases a GitHub archive path (MeloTTS-<sha>/melo/api.py)
// onto the destination directory, returning "" for the archive's own root
// entry. It rejects absolute paths and any traversal.
func meloRelativePath(name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return "", fmt.Errorf("archive entry %q escapes the destination", name)
	}
	parts := strings.Split(clean, string(filepath.Separator))
	// Strip the single top-level directory GitHub wraps the tree in.
	if len(parts) > 1 {
		parts = parts[1:]
	} else {
		return "", nil
	}
	for _, part := range parts {
		if part == ".." || part == "" {
			return "", fmt.Errorf("archive entry %q escapes the destination", name)
		}
	}
	return filepath.Join(parts...), nil
}

func writeArchiveFile(target string, r io.Reader, mode os.FileMode) (err error) {
	if mode == 0 {
		mode = 0o644
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	// Bound the copy so a malformed archive cannot fill the disk.
	if _, err := io.Copy(f, io.LimitReader(r, 512<<20)); err != nil {
		return err
	}
	return nil
}

// meloOfflinePreamble is the network-free setup every MeloTTS python entry
// point needs before `import melo`.
//
// It exists because of two separate facts about the upstream package:
//
//  1. melo/text/cleaner.py imports every language backend, and six of them call
//     AutoTokenizer.from_pretrained at MODULE scope with a hardcoded Hub id — so
//     merely `import melo.api` reaches the network for tohoku-nlp/bert-base-japanese-v3,
//     kykim/bert-kor-base, bert-base-multilingual-uncased, the French and Spanish
//     tokenizers, and bert-base-uncased. English inference genuinely needs only
//     bert-base-uncased (english.py tokenizes, english_bert.py supplies the features),
//     so that one is redirected to the staged local files and every other id gets a stub
//     that raises on use. Pinning six more model repositories to satisfy imports this
//     engine never executes would mean ~25 extra checksummed artifacts for nothing.
//  2. Both the install-time import check and synthesis must do this. The check used to
//     run a bare `python -c "import melo.api"`, which hit the network and failed the
//     install; the preamble is shared so the two cannot drift.
//
// It reads the staged bert directory from sys.argv[1].
const meloOfflinePreamble = `import os
import sys

bert_dir = sys.argv[1]

import transformers

_BERT_ID = "bert-base-uncased"


class _UnbundledTokenizer:
    """Stands in for a tokenizer this engine does not ship.

    MeloTTS loads one at import time per language. Raising on first attribute
    access keeps the failure loud and named instead of silently producing wrong
    audio, and it is the signal to add that language's artifacts.
    """

    def __init__(self, name):
        self._name = name

    def __getattr__(self, attr):
        def _unavailable(*args, **kwargs):
            raise RuntimeError(
                "MeloTTS tokenizer %r is not bundled; this engine is English-only" % self._name
            )
        return _unavailable


# from_pretrained is a classmethod, so the class arrives as the first argument.
# The wrappers must therefore take cls first and re-pass it to the saved unbound
# function; naming that slot after the model id and then prepending the class
# shifts the arguments and leaves transformers resolving the class object as a
# Hub repo id.
_orig_tokenizer = transformers.AutoTokenizer.from_pretrained.__func__
_orig_model = transformers.AutoModelForMaskedLM.from_pretrained.__func__


def _resolve(orig, name):
    """Return the path to load, or None when the request must be stubbed.

    Only the one model this engine stages may be loaded, and only from the
    cache. Deciding this on "is it a bare Hub id" is not enough: MeloTTS asks
    for both namespaced ids (tohoku-nlp/bert-base-japanese-v3) and bare ones
    (bert-base-multilingual-uncased), so a slash test would let the latter
    through to the network. A staged filesystem path is the only other thing
    this driver ever legitimately passes.
    """
    if name == _BERT_ID:
        return bert_dir
    if isinstance(name, str) and os.path.isdir(name):
        return name
    return None


def _tokenizer(cls, name=None, *args, **kwargs):
    path = _resolve(_orig_tokenizer, name)
    if path is None:
        return _UnbundledTokenizer(name)
    return _orig_tokenizer(cls, path, *args, **kwargs)


def _model(cls, name=None, *args, **kwargs):
    path = _resolve(_orig_model, name)
    if path is None:
        return _UnbundledTokenizer(name)
    return _orig_model(cls, path, *args, **kwargs)


transformers.AutoTokenizer.from_pretrained = classmethod(_tokenizer)
transformers.AutoModelForMaskedLM.from_pretrained = classmethod(_model)
`

// meloImportCheckScript is what the installer's runtime verification runs. It is
// written to the cache as a file rather than passed with -c so the traceback names
// a real path, and it applies the same offline preamble synthesis will.
const meloImportCheckScript = meloOfflinePreamble + `
import melo.api
print("melo runtime import ok")
`

// meloSynthScript is the driver run inside the venv. It is written to the cache
// rather than passed with -c so the traceback names a real file.
//
// config_path and ckpt_path are always supplied, which is what keeps
// download_utils from calling hf_hub_download or cached_path.
const meloSynthScript = meloOfflinePreamble + `
config_path, ckpt_path, speaker, out = sys.argv[2:6]
text = sys.stdin.read().strip()
if not text:
    raise SystemExit("empty speech text")

from melo.api import TTS

model = TTS(
    language="EN",
    device="cpu",
    use_hf=False,
    config_path=config_path,
    ckpt_path=ckpt_path,
)
speakers = model.hps.data.spk2id
if speaker not in speakers:
    raise SystemExit("unknown speaker %r; available: %s" % (speaker, ", ".join(speakers)))
model.tts_to_file(text, speakers[speaker], out, speed=1.0, quiet=True)
`

// meloSynth runs one MeloTTS synthesis under the shared process supervisor.
//
// Unlike the other engines MeloTTS loads a ~200 MB checkpoint on every call, so
// the cost is dominated by process start and model load rather than by the
// audio itself. That is acceptable here because the supervisor serialises
// synthesis and discards superseded requests, but it is the reason the engine
// is not the default.
func meloSynth(ctx context.Context, sup *tool.ProcessSupervisor, root string, m Manifest, id string, text, outPath string, voice string) error {
	if voice == "" {
		voice = m.Voice
	}
	dir, err := CacheDirChecked(root, m.Engine, m.Voice, m.Version)
	if err != nil {
		return err
	}
	venv := filepath.Join(dir, "venv")
	scriptPath := filepath.Join(dir, "melo_synthesize.py")
	if err := os.WriteFile(scriptPath, []byte(meloSynthScript), 0o644); err != nil {
		return fmt.Errorf("write melo synth script: %w", err)
	}
	// Argument order matches the script: argv[1] is the staged bert directory
	// (read by the shared offline preamble), then config, checkpoint, speaker and
	// the output path.
	cmd := exec.CommandContext(ctx, venvPython(venv), scriptPath,
		filepath.Join(dir, bertDirName),
		filepath.Join(dir, "config.json"),
		filepath.Join(dir, "checkpoint.pth"),
		voice,
		outPath,
	)
	// Reuse the shared cwd/telemetry hardening: the child must not inherit the
	// app's working directory.
	applySynthProcessEnv(cmd, dir)
	cmd.Env = meloEnv(venv, dir)
	cmd.Stdin = strings.NewReader(text)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = io.Discard
	procID := "tts-melo-" + id
	if _, err := tool.StartSupervised(sup, cmd, tool.ProcessRegistration{
		ID:      procID,
		Name:    "MeloTTS synth",
		Command: "python melo synth",
		Kind:    tool.ProcessKindTTS,
	}); err != nil {
		return fmt.Errorf("start MeloTTS: %w", err)
	}
	err = cmd.Wait()
	code := 0
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	if err != nil {
		sup.MarkKilled(procID, code)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("MeloTTS exited: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	sup.MarkExited(procID, code)
	info, statErr := os.Stat(outPath)
	if statErr != nil || info.Size() == 0 {
		return fmt.Errorf("MeloTTS produced no audio: %s", strings.TrimSpace(stderr.String()))
	}
	return nil
}
