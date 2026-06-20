package discovery

import "runtime"

// Artifact is one downloadable file with integrity check.
type Artifact struct {
	URL    string
	SHA256 string
	// Dest is the filename under the local cache dir (Task 18 resolves the dir).
	Dest string
	Exec bool // chmod +x after download (server binaries)
}

// ServerManifest describes how to obtain and launch the local embed server for one
// platform, and how to talk to it.
type ServerManifest struct {
	OS, Arch string
	ModelID  string // e.g. "local/lfm2-5-retriever"
	Dim      int
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
}

// localManifests are filled from Task 17 research. One entry per supported
// platform. URLs and SHAs are platform-specific release artifacts and are
// left as TODO placeholders — they are verified by the same
// "research-and-pin" workflow that produces the LFM2-5 model card itself,
// and the artifact downloader (EnsureArtifact) refuses to download when the
// URL is empty, so a half-filled manifest fails closed.
//
// Research notes (2026-06-21):
//
//  1. MODEL: LiquidAI/LFM2.5-Embedding-350M
//     - 354M parameter dense bi-encoder, multilingual retrieval
//     - 1024-d CLS-pooled embedding vector (Dim=1024)
//     - License: lfm1.0
//     - MLX build: sahilchachra/LFM2.5-Embedding-350M-fp16 (and -mxfp4)
//       — both ship a custom mlx_lfm2_encoder.py loader (causal LFM2 loaders
//       produce wrong embeddings; verified ≥0.999 cosine vs. transformers).
//     - GGUF build: pin a LiquidAI-published GGUF or a community quant.
//
//  2. SERVER: ggml-org/llama.cpp `llama-server`
//     - Cross-platform (macOS arm64/amd64, linux amd64) single binary.
//     - OpenAI-compatible POST /v1/embeddings route built in.
//     - Health probe: GET /v1/models.
//     - Launch args: `llama-server -m {model} --port {port} --embeddings`
//     - MLX-specific acceleration is NOT used: the model runs via llama.cpp
//       everywhere; on Apple Silicon, llama.cpp ships an MLX/Metal backend
//       (and CPU fallback). Using llama-server universally keeps the manifest
//       uniform and the artifacts small (no separate Python wheel + per-arch
//       wheel downloads).
//
//  3. ARTIFACT RESOLUTION: pinned at release time. The maintainer fills in
//     the URL + SHA from the chosen llama.cpp release tarball (per-arch
//     binary inside) and the chosen LFM2.5 GGUF before tagging a build.
//     The downloader (EnsureArtifact) verifies the SHA and writes atomically.
var localManifests = []ServerManifest{
	{
		OS: "darwin", Arch: "arm64",
		ModelID: "local/lfm2-5-retriever", Dim: 1024,
		Artifacts: []Artifact{
			// TODO: pin llama.cpp release tarball URL + SHA (extract llama-server
			// binary from `llama-<ver>-bin-macos-arm64.zip`).
			{URL: "", SHA256: "", Dest: "llama-server", Exec: true},
			// TODO: pin LFM2.5-Embedding-350M GGUF (e.g. a Q4_K_M quant) URL + SHA.
			{URL: "", SHA256: "", Dest: "lfm2-5-embedding-350m.gguf"},
		},
		LaunchArgv: []string{"{bin}/llama-server",
			"-m", "{bin}/lfm2-5-embedding-350m.gguf",
			"--port", "{port}",
			"--embeddings",
			"--host", "127.0.0.1"},
		HealthPath: "/v1/models",
		EmbedPath:  "/v1/embeddings",
	},
	{
		OS: "darwin", Arch: "amd64",
		ModelID: "local/lfm2-5-retriever", Dim: 1024,
		Artifacts: []Artifact{
			// TODO: pin llama.cpp darwin/amd64 release artifact.
			{URL: "", SHA256: "", Dest: "llama-server", Exec: true},
			// TODO: pin LFM2.5-Embedding-350M GGUF.
			{URL: "", SHA256: "", Dest: "lfm2-5-embedding-350m.gguf"},
		},
		LaunchArgv: []string{"{bin}/llama-server",
			"-m", "{bin}/lfm2-5-embedding-350m.gguf",
			"--port", "{port}",
			"--embeddings",
			"--host", "127.0.0.1"},
		HealthPath: "/v1/models",
		EmbedPath:  "/v1/embeddings",
	},
	{
		OS: "linux", Arch: "amd64",
		ModelID: "local/lfm2-5-retriever", Dim: 1024,
		Artifacts: []Artifact{
			// TODO: pin llama.cpp linux/amd64 release artifact.
			{URL: "", SHA256: "", Dest: "llama-server", Exec: true},
			// TODO: pin LFM2.5-Embedding-350M GGUF.
			{URL: "", SHA256: "", Dest: "lfm2-5-embedding-350m.gguf"},
		},
		LaunchArgv: []string{"{bin}/llama-server",
			"-m", "{bin}/lfm2-5-embedding-350m.gguf",
			"--port", "{port}",
			"--embeddings",
			"--host", "127.0.0.1"},
		HealthPath: "/v1/models",
		EmbedPath:  "/v1/embeddings",
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
func goos() string  { return runtime.GOOS }
func goarch() string { return runtime.GOARCH }
