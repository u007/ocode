package discovery

import "runtime"

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
// the archive's internal layout (e.g. "llama-b9747/llama-server" extracted
// from llama-b9747-bin-macos-arm64.tar.gz lands at
// destDir/llama-b9747/llama-server). Dest is used to find the entry inside
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
	ModelID  string // e.g. "local/bge-m3"
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

// localManifests pin concrete llama.cpp release tarballs (b9747) per supported
// platform and the BGE-M3 Q4_K_M GGUF. The tarball is verified against its
// SHA256, then extracted into binDir preserving the upstream "llama-b9747/"
// prefix — LaunchArgv references that subdir so the binary's bundled sibling
// libraries (libllama.*, libggml.*, …) resolve via DYLD_LIBRARY_PATH /
// LD_LIBRARY_PATH (set at spawn, see localserver.go).
//
// Updating: bump b9747 → newer llama.cpp release and recompute SHA256 of each
// new tarball. Run `shasum -a 256 <tarball>` against the URL below; pinning
// the SHA is the integrity guarantee that keeps the download safe.
//
// The model: BAAI/bge-m3 via bbvch-ai/bge-m3-GGUF:Q4_K_M. bge-m3 is the
// production-standard multilingual embedding (100+ languages, MTEB ~63 avg,
// 1024-d, MIT license) and has explicit llama.cpp support in its HF model
// card (`llama-server -hf bbvch-ai/bge-m3-GGUF:Q4_K_M`). It uses the BERT
// architecture, which upstream llama.cpp b9747 supports (LLM_ARCH_BERT).
//
// We previously pinned LFM2.5-Embedding-350M (Dim 1024) but its
// `lfm2-bidir` architecture is not in upstream llama.cpp — only a community
// fork has the support, so the bundled server would fail to load the model.
// bge-m3 was chosen as the swap because it has comparable MTEB quality, more
// languages, an MIT license, and a first-class llama.cpp path.
var localManifests = []ServerManifest{
	{
		OS: "darwin", Arch: "arm64",
		ModelID: "local/bge-m3", Dim: 1024,
		Artifacts: []Artifact{
			// llama.cpp b9747 — macOS Apple Silicon.
			{
				URL:     "https://github.com/ggml-org/llama.cpp/releases/download/b9747/llama-b9747-bin-macos-arm64.tar.gz",
				SHA256:  "15e1a57690addafa48309760df81c31457e2112dbfa05d02a5e2580381850641",
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
		LaunchArgv: []string{"{bin}/llama-b9747/llama-server",
			"-m", "{bin}/bge-m3-q4_k_m.gguf",
			"--port", "{port}",
			"--embeddings",
			"--host", "127.0.0.1"},
		HealthPath: "/v1/models",
		EmbedPath:  "/v1/embeddings",
	},
	{
		OS: "darwin", Arch: "amd64",
		ModelID: "local/bge-m3", Dim: 1024,
		Artifacts: []Artifact{
			// llama.cpp b9747 — macOS Intel.
			{
				URL:     "https://github.com/ggml-org/llama.cpp/releases/download/b9747/llama-b9747-bin-macos-x64.tar.gz",
				SHA256:  "7a465af113733e130a2905572dd9a4596d158a9ee7bd2f4b31c219d70c31b13e",
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
		LaunchArgv: []string{"{bin}/llama-b9747/llama-server",
			"-m", "{bin}/bge-m3-q4_k_m.gguf",
			"--port", "{port}",
			"--embeddings",
			"--host", "127.0.0.1"},
		HealthPath: "/v1/models",
		EmbedPath:  "/v1/embeddings",
	},
	{
		OS: "linux", Arch: "amd64",
		ModelID: "local/bge-m3", Dim: 1024,
		Artifacts: []Artifact{
			// llama.cpp b9747 — Linux x86_64 (Ubuntu glibc build).
			{
				URL:     "https://github.com/ggml-org/llama.cpp/releases/download/b9747/llama-b9747-bin-ubuntu-x64.tar.gz",
				SHA256:  "b865de21024c91432b6b1f29e2e2f8c3797204315b2914d43fa86d1999c8ef8c",
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
		LaunchArgv: []string{"{bin}/llama-b9747/llama-server",
			"-m", "{bin}/bge-m3-q4_k_m.gguf",
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
