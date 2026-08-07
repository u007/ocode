package discovery

import (
	"runtime"
	"strings"
)

// ArchiveFormat tags a downloadable artifact as a compressed archive that must
// be extracted to a single file (Dest) after download. Empty means a single
// file download (no extraction).
type ArchiveFormat string

const (
	ArchiveNone ArchiveFormat = "" // raw file (e.g. .gguf, .bin)
	ArchiveGZ   ArchiveFormat = "tar.gz"
)

// Artifact is one downloadable file with integrity check. If Archive is set,
// the downloaded bytes are a tar.gz that is extracted to destDir preserving
// the archive's internal layout (e.g. "llama-b9777/llama-server" extracted
// from llama-b9777-bin-macos-arm64.tar.gz lands at
// destDir/llama-b9777/llama-server). Dest is used to find the entry inside
// the archive and to chmod +x the resulting file when Exec is true.
type Artifact struct {
	URL     string
	SHA256  string        // SHA256 of the downloaded bytes (the tarball when Archive is set)
	Dest    string        // Basename to look for inside the archive; or final filename for raw downloads
	Exec    bool          // chmod +x after download (server binaries)
	Archive ArchiveFormat // extraction format; "" = single file
}

// ServerManifest describes how to obtain and launch the local embed server for one
// platform, and how to talk to it.
type ServerManifest struct {
	OS, Arch string
	ModelID  string // e.g. "local/bge-m3" or "local/lfm2.5-embedding"
	// Kind distinguishes embedding manifests ("" or "embed", the existing
	// default) from chat/completion manifests ("chat"). Embedding-model
	// pickers (LocalManifestsForHost) only see "" / "embed"; the local-model
	// instance manager (ChatManifestsForHost) only sees "chat".
	Kind string
	Dim  int
	// Backend selects how the model is served. "" or "llamacpp" = bundled
	// llama-server with a GGUF; "mlx" = Apple-Silicon MLX Python server.
	Backend string
	// MLXRepo is the HuggingFace repo id the MLX server loads (Backend=="mlx").
	MLXRepo string
	// Artifacts lists the files to download. The first one is conventionally the
	// server binary (Exec=true) when the server is a single executable; the
	// second is the model.
	Artifacts []Artifact
	// LaunchArgv is the server command line, with placeholders {bin} and {port}
	// substituted at spawn time. argv[0] is the server binary path under the
	// cache dir.
	LaunchArgv []string
	HealthPath string // e.g. "/v1/models"
	EmbedPath  string // e.g. "/v1/embeddings"
	// CompletionPath is the OpenAI-compatible chat completions path (chat-kind
	// manifests only), e.g. "/v1/chat/completions".
	CompletionPath string
}

// localManifests pin concrete llama.cpp release tarballs (b9777) per supported
// platform and the BGE-M3 Q4_K_M GGUF. The tarball is verified against its
// SHA256, then extracted into binDir preserving the upstream "llama-b9777/"
// prefix — LaunchArgv references that subdir so the binary's bundled sibling
// libraries (libllama.*, libggml.*, …) resolve via DYLD_LIBRARY_PATH /
// LD_LIBRARY_PATH (set at spawn, see localserver.go).
//
// Updating: bump b9777 → newer llama.cpp release and recompute SHA256 of each
// new tarball. Run `shasum -a 256 <tarball>` against the URL below; pinning
// the SHA is the integrity guarantee that keeps the download safe.
//
// The model: BAAI/bge-m3 via bbvch-ai/bge-m3-GGUF:Q4_K_M. bge-m3 is the
// production-standard multilingual embedding (100+ languages, MTEB ~63 avg,
// 1024-d, MIT license) and has explicit llama.cpp support in its HF model
// card (`llama-server -hf bbvch-ai/bge-m3-GGUF:Q4_K_M`). It uses the BERT
// architecture, which upstream llama.cpp b9777 supports (LLM_ARCH_BERT).
//
// LFM2.5-Embedding-350M (Dim 1024, `lfm2-bidir` arch) is ALSO served via
// llama.cpp as of the b9777 bump: upstream added lfm2-bidir in PR #24913
// (merged 2026-06-24, first in release b9777), so `unknown model architecture:
// 'lfm2-bidir'` is fixed. We pin LiquidAI's official Q4_K_M GGUF, which carries
// `pooling_type=2` (CLS) in its metadata — llama.cpp auto-applies CLS pooling
// (no --pooling flag), the correct bidirectional pipeline this model was
// trained for. This replaces the earlier MLX (mlx_lm) backend, which ran the
// bidirectional model CAUSALLY with mean pooling: rankings were roughly right
// but absolute cosine scores collapsed (~0.31 for a near-paraphrase), so
// nothing ever cleared the attach threshold. See the git history for the
// investigation; the MLX server (mlx_embed_server.py) is retained for any
// future MLX-only model but is unused by the default local models now.
var localManifests = []ServerManifest{
	{
		OS: "darwin", Arch: "arm64",
		ModelID: "local/bge-m3", Dim: 1024, Backend: BackendLlamaCpp,
		Artifacts: []Artifact{
			// llama.cpp b9777 — macOS Apple Silicon.
			{
				URL:     "https://github.com/ggml-org/llama.cpp/releases/download/b9777/llama-b9777-bin-macos-arm64.tar.gz",
				SHA256:  "3784919bd0ebde85854d16efbf8b2240403358965d61ae8f7e2743cf4763a818",
				Dest:    "llama-server",
				Exec:    true,
				Archive: ArchiveGZ,
			},
			// BGE-M3, Q4_K_M quant (bbvch-ai/bge-m3-GGUF, MIT). 1024-d, 100+
			// languages, MTEB ~63. Auto-detects pooling from GGUF metadata
			// so no --pooling flag is needed.
			{
				URL:    "https://huggingface.co/bbvch-ai/bge-m3-GGUF/resolve/main/bge-m3-q4_k_m.gguf",
				SHA256: "d164fe641fe8aecc9da3592b5f1ca46e9c97923959661a5f815bbc8e72704fb2",
				Dest:   "bge-m3-q4_k_m.gguf",
			},
		},
		LaunchArgv: []string{"{bin}/llama-b9777/llama-server",
			"-m", "{bin}/bge-m3-q4_k_m.gguf",
			"--port", "{port}",
			"--embeddings",
			"--host", "127.0.0.1"},
		HealthPath: "/v1/models",
		EmbedPath:  "/v1/embeddings",
	},
	{
		OS: "darwin", Arch: "amd64",
		ModelID: "local/bge-m3", Dim: 1024, Backend: BackendLlamaCpp,
		Artifacts: []Artifact{
			// llama.cpp b9777 — macOS Intel.
			{
				URL:     "https://github.com/ggml-org/llama.cpp/releases/download/b9777/llama-b9777-bin-macos-x64.tar.gz",
				SHA256:  "6271bffb4aa142351f63fff1cb8e42bd16e7b9877f2b5bc5e49037f91f3f0897",
				Dest:    "llama-server",
				Exec:    true,
				Archive: ArchiveGZ,
			},
			{
				URL:    "https://huggingface.co/bbvch-ai/bge-m3-GGUF/resolve/main/bge-m3-q4_k_m.gguf",
				SHA256: "d164fe641fe8aecc9da3592b5f1ca46e9c97923959661a5f815bbc8e72704fb2",
				Dest:   "bge-m3-q4_k_m.gguf",
			},
		},
		LaunchArgv: []string{"{bin}/llama-b9777/llama-server",
			"-m", "{bin}/bge-m3-q4_k_m.gguf",
			"--port", "{port}",
			"--embeddings",
			"--host", "127.0.0.1"},
		HealthPath: "/v1/models",
		EmbedPath:  "/v1/embeddings",
	},
	{
		OS: "linux", Arch: "amd64",
		ModelID: "local/bge-m3", Dim: 1024, Backend: BackendLlamaCpp,
		Artifacts: []Artifact{
			// llama.cpp b9777 — Linux x86_64 (Ubuntu glibc build).
			{
				URL:     "https://github.com/ggml-org/llama.cpp/releases/download/b9777/llama-b9777-bin-ubuntu-x64.tar.gz",
				SHA256:  "f1994e1d9904f318c8347b000e7ef5dfd49fa4a24de044887da85d9bbfe84811",
				Dest:    "llama-server",
				Exec:    true,
				Archive: ArchiveGZ,
			},
			{
				URL:    "https://huggingface.co/bbvch-ai/bge-m3-GGUF/resolve/main/bge-m3-q4_k_m.gguf",
				SHA256: "d164fe641fe8aecc9da3592b5f1ca46e9c97923959661a5f815bbc8e72704fb2",
				Dest:   "bge-m3-q4_k_m.gguf",
			},
		},
		LaunchArgv: []string{"{bin}/llama-b9777/llama-server",
			"-m", "{bin}/bge-m3-q4_k_m.gguf",
			"--port", "{port}",
			"--embeddings",
			"--host", "127.0.0.1"},
		HealthPath: "/v1/models",
		EmbedPath:  "/v1/embeddings",
	},
	{
		// LFM2.5-Embedding-350M via llama.cpp (b9777+, which added the lfm2-bidir
		// arch — PR #24913). We pin LiquidAI's official Q4_K_M GGUF; its metadata
		// declares pooling_type=2 (CLS), so llama.cpp runs the model
		// bidirectionally and CLS-pools automatically — no --pooling flag, exactly
		// like the bge-m3 entry. Darwin/arm64 only (matches DefaultLocalModelID);
		// the llama.cpp binary is downloaded + SHA-verified, the GGUF likewise.
		OS: "darwin", Arch: "arm64",
		ModelID: "local/lfm2.5-embedding", Dim: 1024, Backend: BackendLlamaCpp,
		Artifacts: []Artifact{
			{
				URL:     "https://github.com/ggml-org/llama.cpp/releases/download/b9777/llama-b9777-bin-macos-arm64.tar.gz",
				SHA256:  "3784919bd0ebde85854d16efbf8b2240403358965d61ae8f7e2743cf4763a818",
				Dest:    "llama-server",
				Exec:    true,
				Archive: ArchiveGZ,
			},
			// LFM2.5-Embedding-350M, official Q4_K_M GGUF (LiquidAI). 1024-d,
			// lfm2-bidir arch, CLS pooling baked into the GGUF metadata.
			{
				URL:    "https://huggingface.co/LiquidAI/LFM2.5-Embedding-350M-GGUF/resolve/main/LFM2.5-Embedding-350M-Q4_K_M.gguf",
				SHA256: "4d7aa9dc6406a10fc3dec2c11f8f06781af063bf49211b8e4132e9b876d3f32a",
				Dest:   "lfm2.5-embedding-350m-q4_k_m.gguf",
			},
		},
		LaunchArgv: []string{"{bin}/llama-b9777/llama-server",
			"-m", "{bin}/lfm2.5-embedding-350m-q4_k_m.gguf",
			"--port", "{port}",
			"--embeddings",
			"--host", "127.0.0.1"},
		HealthPath: "/v1/models",
		EmbedPath:  "/v1/embeddings",
	},
	{
		// Bonsai 8B, 1-bit (ternary/BitNet-style quant). MLX build on Apple
		// Silicon: prism-ml/Bonsai-8B-mlx-1bit, run via mlx_lm's own
		// OpenAI-compatible server (mlx_lm.server).
		//
		// NO concurrency flag: earlier revisions of this manifest passed
		// --decode-concurrency {parallel}, assuming mlx_lm.server had a
		// concurrent-request-slot flag like llama-server's --parallel. It
		// does not — confirmed from this server's own `--help` usage output
		// (only -h/--model/--adapter-path/--host/--port/--draft-model/
		// --num-draft-tokens/--trust-remote-code/--log-level/--chat-template/
		// --use-default-chat-template/--temp/--top-p/--top-k/--min-p/
		// --max-tokens/--chat-template-args exist), which is why passing it
		// crashed the spawn with "unrecognized arguments: --decode-concurrency"
		// even with spawnMLXChatServer's flag-probe/drop safety net working
		// correctly. MaxParallel (/localmodel limit) is therefore a no-op for
		// this backend — accepted and stored, but nothing in the launch
		// command reflects it. Only the llama.cpp GGUF entries below actually
		// honor --parallel.
		//
		// NOTE: 1-bit kernels require the PrismML mlx fork (the model card
		// instructs `pip install mlx @ git+https://github.com/PrismML-Eng/mlx.git@prism`);
		// stock mlx rejects bits=1 at load ("requested number of bits 1 is not
		// supported").
		OS: "darwin", Arch: "arm64",
		ModelID: "local/bonsai-8b-1bit", Kind: "chat", Backend: BackendMLX,
		MLXRepo: "prism-ml/Bonsai-8B-mlx-1bit",
		LaunchArgv: []string{"python3", "-m", "mlx_lm.server",
			"--model", "{repo}",
			"--host", "127.0.0.1",
			"--port", "{port}"},
		HealthPath:     "/v1/models",
		CompletionPath: "/v1/chat/completions",
	},
	{
		// Qwen3-4B-Instruct-2507, 4-bit MLX quant (mlx-community). Evaluated
		// as a replacement auto-permission judge for local/bonsai-8b-1bit: the
		// 1-bit Bonsai quant fabricated banned-prefix matches that were not
		// present in the command (e.g. claiming "sed -i" on a plain git push).
		// Qwen3-4B-Instruct-4bit reproduced none of those hallucinations
		// across a 7-case suite built from real auto-permission session
		// history, at ~1.2s/judgment (~19x faster than the -Thinking variant,
		// which spends its budget on <think> reasoning before the verdict
		// line) and ~3GB peak VRAM.
		OS: "darwin", Arch: "arm64",
		ModelID: "local/qwen3-4b-instruct-4bit", Kind: "chat", Backend: BackendMLX,
		MLXRepo: "mlx-community/Qwen3-4B-Instruct-2507-4bit",
		LaunchArgv: []string{"python3", "-m", "mlx_lm.server",
			"--model", "{repo}",
			"--host", "127.0.0.1",
			"--port", "{port}"},
		HealthPath:     "/v1/models",
		CompletionPath: "/v1/chat/completions",
	},
	{
		// Bonsai 8B, 1-bit — macOS Intel, via llama.cpp GGUF
		// (prism-ml/Bonsai-8B-gguf). --parallel is llama-server's
		// concurrent-request-slot flag (aka -np), substituted from
		// {parallel} by the instance manager.
		// NOTE: the model card ships Q1_0 kernels via the PrismML llama.cpp
		// fork (github.com/PrismML-Eng/llama.cpp, "includes Q1_0 kernels"); if
		// upstream b9777 llama-server fails to load the Q1_0 GGUF
		// ("unsupported quantization"), switch the server artifact to the
		// fork's release tarball (llama-prism-b9599-9ca265a-bin-macos-x64.tar.gz,
		// sha256 7c6179a43265c0f24c7336c976b5aae81589b33cb979543421d538ae25175d88).
		OS: "darwin", Arch: "amd64",
		ModelID: "local/bonsai-8b-1bit", Kind: "chat", Backend: BackendLlamaCpp,
		Artifacts: []Artifact{
			{
				URL:     "https://github.com/ggml-org/llama.cpp/releases/download/b9777/llama-b9777-bin-macos-x64.tar.gz",
				SHA256:  "6271bffb4aa142351f63fff1cb8e42bd16e7b9877f2b5bc5e49037f91f3f0897",
				Dest:    "llama-server",
				Exec:    true,
				Archive: ArchiveGZ,
			},
			{
				URL:    "https://huggingface.co/prism-ml/Bonsai-8B-gguf/resolve/main/Bonsai-8B-Q1_0.gguf",
				SHA256: "284a335aa3fb2ced3b1b01fcb40b08aa783e3b70832767f0dd2e3fdfa134bd54",
				Dest:   "bonsai-8b-1bit.gguf",
			},
		},
		LaunchArgv: []string{"{bin}/llama-b9777/llama-server",
			"-m", "{bin}/bonsai-8b-1bit.gguf",
			"--port", "{port}",
			"--parallel", "{parallel}",
			"--host", "127.0.0.1"},
		HealthPath:     "/v1/models",
		CompletionPath: "/v1/chat/completions",
	},
	{
		// Bonsai 8B, 1-bit — Linux x86_64, via llama.cpp GGUF (same artifact
		// as the darwin/amd64 entry, different llama-server tarball).
		// NOTE: same Q1_0-kernel caveat as the darwin/amd64 entry — if
		// upstream b9777 cannot load the Q1_0 GGUF, use the PrismML fork
		// tarball (llama-prism-b9599-9ca265a-bin-ubuntu-x64.tar.gz,
		// sha256 35e935c828f58f9837d3da357a86846deb11d4feffc835d9e94e7d8fc1bbd55e).
		OS: "linux", Arch: "amd64",
		ModelID: "local/bonsai-8b-1bit", Kind: "chat", Backend: BackendLlamaCpp,
		Artifacts: []Artifact{
			{
				URL:     "https://github.com/ggml-org/llama.cpp/releases/download/b9777/llama-b9777-bin-ubuntu-x64.tar.gz",
				SHA256:  "f1994e1d9904f318c8347b000e7ef5dfd49fa4a24de044887da85d9bbfe84811",
				Dest:    "llama-server",
				Exec:    true,
				Archive: ArchiveGZ,
			},
			{
				URL:    "https://huggingface.co/prism-ml/Bonsai-8B-gguf/resolve/main/Bonsai-8B-Q1_0.gguf",
				SHA256: "284a335aa3fb2ced3b1b01fcb40b08aa783e3b70832767f0dd2e3fdfa134bd54",
				Dest:   "bonsai-8b-1bit.gguf",
			},
		},
		LaunchArgv: []string{"{bin}/llama-b9777/llama-server",
			"-m", "{bin}/bonsai-8b-1bit.gguf",
			"--port", "{port}",
			"--parallel", "{parallel}",
			"--host", "127.0.0.1"},
		HealthPath:     "/v1/models",
		CompletionPath: "/v1/chat/completions",
	},
}

// CurrentManifest returns the manifest matching the host, if supported.
func CurrentManifest() (ServerManifest, bool) {
	for _, m := range localManifests {
		if m.OS == runtime.GOOS && m.Arch == runtime.GOARCH {
			return m, true
		}
	}
	return ServerManifest{}, false
}

// goos / goarch wrap runtime constants so localserver.go doesn't have to
// import runtime directly.
func goos() string   { return runtime.GOOS }
func goarch() string { return runtime.GOARCH }

// DefaultLocalModelID returns the local embedding model we recommend for this
// host: bge-m3 on every platform. Its wide cosine band (strong matches ~0.6+) is
// what SelectMin (0.40) was tuned for, so semantic attachment works out of the
// box. LFM2.5-Embedding (darwin/arm64, opt-in via `/discover model
// local/lfm2.5-embedding`) is smaller/faster but has a compressed cosine band —
// a strong match tops out ~0.3, below the attach floor, however it is pooled or
// prefixed (measured against the llama.cpp CLS GGUF; see the Bug C
// investigation). It still RANKS correctly, so it's fine for a user who lowers
// the threshold, but it is not the default.
func DefaultLocalModelID() string {
	return "local/bge-m3"
}

// ManifestForModel returns the manifest whose ModelID matches, or false.
func ManifestForModel(modelID string) (ServerManifest, bool) {
	for _, m := range localManifests {
		if m.ModelID == modelID {
			return m, true
		}
	}
	return ServerManifest{}, false
}

// LocalManifestsForHost returns every embedding manifest that can run on this
// host (used by the embedding-model picker to list selectable local models).
// Chat-kind manifests (see ChatManifestsForHost) are excluded.
func LocalManifestsForHost() []ServerManifest {
	var out []ServerManifest
	for _, m := range localManifests {
		if m.OS == runtime.GOOS && m.Arch == runtime.GOARCH && m.Kind != "chat" {
			out = append(out, m)
		}
	}
	return out
}

// ChatManifestsForHost returns every chat/completion manifest that can run on
// this host (used by /localmodel to list catalog entries available to add).
func ChatManifestsForHost() []ServerManifest {
	var out []ServerManifest
	for _, m := range localManifests {
		if m.OS == runtime.GOOS && m.Arch == runtime.GOARCH && m.Kind == "chat" {
			out = append(out, m)
		}
	}
	return out
}

// ChatManifestForHost returns the chat-kind manifest for modelID that matches
// THIS host's OS/Arch, or false. Unlike ManifestForModel (a bare ModelID
// match), this is safe to use for a specific model id that has multiple
// per-platform entries with DIFFERENT backends — e.g. local/bonsai-8b-1bit is
// MLX on darwin/arm64 but llama.cpp GGUF everywhere else. ManifestForModel
// would silently return whichever entry for that ModelID happens to be first
// in localManifests, regardless of the current host, which is only harmless
// for embedding manifests (all platform rows share one backend). All chat
// call sites (StartModelInstance, /localmodel add) must use this function,
// not ManifestForModel.
func ChatManifestForHost(modelID string) (ServerManifest, bool) {
	for _, m := range localManifests {
		if m.ModelID == modelID && m.Kind == "chat" && m.OS == runtime.GOOS && m.Arch == runtime.GOARCH {
			return m, true
		}
	}
	return ServerManifest{}, false
}

// ExpectedServeID is the model id the running server must report in
// /v1/models for us to adopt it. llama.cpp reports the GGUF path, so we match
// on the GGUF basename; the MLX server reports the served model id — the HF
// repo id (mlx_lm.server, chat manifests) or the discovery ModelID verbatim
// (the bundled mlx_embed_server.py, which is handed the ModelID via --model-id).
func (m ServerManifest) ExpectedServeID() string {
	if m.Backend == BackendMLX {
		if m.MLXRepo != "" {
			return m.MLXRepo
		}
		return m.ModelID
	}
	for _, a := range m.Artifacts {
		if strings.HasSuffix(a.Dest, ".gguf") {
			return a.Dest
		}
	}
	return m.ModelID
}
