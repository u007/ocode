package discovery

import (
	"runtime"
	"strings"
	"testing"
)

func TestManifestForModel(t *testing.T) {
	if m, ok := ManifestForModel("local/bge-m3"); !ok || m.ModelID != "local/bge-m3" || m.Backend != BackendLlamaCpp {
		t.Fatalf("bge-m3 manifest not found/incorrect: %+v ok=%v", m, ok)
	}
	// LFM2.5 only has a manifest on darwin/arm64, but ManifestForModel is
	// platform-independent — it must still resolve regardless of host. It is now
	// served via llama.cpp (b9777+ added the lfm2-bidir arch), not MLX.
	if m, ok := ManifestForModel("local/lfm2.5-embedding"); !ok {
		t.Fatalf("lfm2.5-embedding manifest not found")
	} else if m.Backend != BackendLlamaCpp {
		t.Fatalf("lfm2.5 manifest wrong backend: %+v", m)
	}
	if _, ok := ManifestForModel("local/does-not-exist"); ok {
		t.Fatalf("expected unknown model to be absent")
	}
}

// TestEmbedManifestForHost guards the remote-host "Exec format error": the
// embed spawn path must pick the archive for the HOST platform, not the first
// row carrying the model id (darwin/arm64 for bge-m3).
func TestEmbedManifestForHost(t *testing.T) {
	m, ok := embedManifestFor("local/bge-m3", "linux", "amd64")
	if !ok {
		t.Fatalf("bge-m3 has no linux/amd64 manifest")
	}
	if m.OS != "linux" || m.Arch != "amd64" {
		t.Fatalf("linux/amd64 lookup returned %s/%s manifest", m.OS, m.Arch)
	}
	for _, a := range m.Artifacts {
		if strings.Contains(a.URL, "macos") {
			t.Fatalf("linux/amd64 bge-m3 manifest references a macOS archive: %s", a.URL)
		}
	}
	// lfm2.5 is darwin/arm64 only: a Linux host must get "unsupported", not
	// the darwin archive.
	if _, ok := embedManifestFor("local/lfm2.5-embedding", "linux", "amd64"); ok {
		t.Fatalf("lfm2.5-embedding must not resolve on linux/amd64")
	}
	// Chat rows never satisfy an embedding lookup.
	if _, ok := embedManifestFor("local/bonsai-8b-1bit", "linux", "amd64"); ok {
		t.Fatalf("chat manifest resolved as an embedding manifest")
	}
	if m, ok := EmbedManifestForHost("local/bge-m3"); ok && (m.OS != runtime.GOOS || m.Arch != runtime.GOARCH) {
		t.Fatalf("EmbedManifestForHost returned %s/%s on %s/%s", m.OS, m.Arch, runtime.GOOS, runtime.GOARCH)
	}
}

func TestDefaultLocalModelID(t *testing.T) {
	// The default depends on the host arch; assert it is one of the two known
	// local models and that the manifest for it resolves.
	def := DefaultLocalModelID()
	if def != "local/lfm2.5-embedding" && def != "local/bge-m3" {
		t.Fatalf("unexpected default model id %q", def)
	}
	if _, ok := ManifestForModel(def); !ok {
		t.Fatalf("default model %q has no manifest", def)
	}
}

func TestLocalManifestsForHost(t *testing.T) {
	mans := LocalManifestsForHost()
	if len(mans) == 0 {
		t.Fatalf("expected at least one local manifest for this host")
	}
	for _, m := range mans {
		if m.ModelID == "" || m.Backend == "" {
			t.Fatalf("host manifest missing model id or backend: %+v", m)
		}
	}
}

func TestExpectedServeID(t *testing.T) {
	bge, _ := ManifestForModel("local/bge-m3")
	if got := bge.ExpectedServeID(); got != "bge-m3-q4_k_m.gguf" {
		t.Fatalf("bge ExpectedServeID = %q, want gguf basename", got)
	}
	lfm, _ := ManifestForModel("local/lfm2.5-embedding")
	if got := lfm.ExpectedServeID(); got != "lfm2.5-embedding-350m-q4_k_m.gguf" {
		t.Fatalf("lfm ExpectedServeID = %q, want gguf basename", got)
	}
	// The chat MLX manifest serves via mlx_lm.server, which reports the HF repo
	// id in /v1/models — not the discovery ModelID.
	bonsaiMLX, _ := ManifestForModel("local/bonsai-8b-1bit")
	if got := bonsaiMLX.ExpectedServeID(); got != "prism-ml/Bonsai-8B-mlx-1bit" {
		t.Fatalf("bonsai MLX ExpectedServeID = %q, want HF repo id", got)
	}
}

func TestLocalManifestsForHostExcludesChatKind(t *testing.T) {
	for _, m := range LocalManifestsForHost() {
		if m.Kind == "chat" {
			t.Fatalf("LocalManifestsForHost returned a chat-kind manifest: %s", m.ModelID)
		}
	}
}

func TestChatManifestsForHostOnlyChatKind(t *testing.T) {
	for _, m := range ChatManifestsForHost() {
		if m.Kind != "chat" {
			t.Fatalf("ChatManifestsForHost returned a non-chat manifest: %s (kind=%q)", m.ModelID, m.Kind)
		}
	}
}

func TestBonsaiManifestResolvesOnEveryDeclaredPlatform(t *testing.T) {
	cases := []struct {
		os, arch, wantBackend string
	}{
		{"darwin", "arm64", BackendMLX},
		{"darwin", "amd64", BackendLlamaCpp},
		{"linux", "amd64", BackendLlamaCpp},
	}
	for _, c := range cases {
		found := false
		for _, m := range localManifests {
			if m.ModelID == "local/bonsai-8b-1bit" && m.OS == c.os && m.Arch == c.arch {
				found = true
				if m.Backend != c.wantBackend {
					t.Errorf("%s/%s: backend = %q, want %q", c.os, c.arch, m.Backend, c.wantBackend)
				}
				if m.Kind != "chat" {
					t.Errorf("%s/%s: Kind = %q, want \"chat\"", c.os, c.arch, m.Kind)
				}
				if m.CompletionPath != "/v1/chat/completions" {
					t.Errorf("%s/%s: CompletionPath = %q, want /v1/chat/completions", c.os, c.arch, m.CompletionPath)
				}
			}
		}
		if !found {
			t.Errorf("no local/bonsai-8b-1bit manifest for %s/%s", c.os, c.arch)
		}
	}
}

// TestQwen06BQ8ManifestResolvesOnEveryDeclaredPlatform pins the bundled
// Qwen3-0.6B catalog entry: three llama.cpp rows, the HF LFS-pinned GGUF SHA,
// and the llama-server argv referencing the same Dest the downloader writes.
func TestQwen06BQ8ManifestResolvesOnEveryDeclaredPlatform(t *testing.T) {
	const wantSHA = "9465e63a22add5354d9bb4b99e90117043c7124007664907259bd16d043bb031"
	cases := []struct{ os, arch string }{
		{"darwin", "arm64"},
		{"darwin", "amd64"},
		{"linux", "amd64"},
	}
	for _, c := range cases {
		found := false
		for _, m := range localManifests {
			if m.ModelID != "local/qwen3-0.6b-q8" || m.OS != c.os || m.Arch != c.arch {
				continue
			}
			found = true
			if m.Kind != "chat" {
				t.Errorf("%s/%s: Kind = %q, want \"chat\"", c.os, c.arch, m.Kind)
			}
			if m.Backend != BackendLlamaCpp {
				t.Errorf("%s/%s: Backend = %q, want %q", c.os, c.arch, m.Backend, BackendLlamaCpp)
			}
			if m.ExpectedServeID() != "qwen3-0.6b-q8_0.gguf" {
				t.Errorf("%s/%s: ExpectedServeID = %q, want qwen3-0.6b-q8_0.gguf", c.os, c.arch, m.ExpectedServeID())
			}
			var modelOK bool
			for _, a := range m.Artifacts {
				if a.Dest == "qwen3-0.6b-q8_0.gguf" {
					modelOK = true
					if a.SHA256 != wantSHA {
						t.Errorf("%s/%s: model SHA256 = %q, want HF LFS oid %q", c.os, c.arch, a.SHA256, wantSHA)
					}
				}
			}
			if !modelOK {
				t.Errorf("%s/%s: no qwen3-0.6b-q8_0.gguf model artifact", c.os, c.arch)
			}
			argv := strings.Join(m.LaunchArgv, " ")
			if !strings.Contains(argv, "qwen3-0.6b-q8_0.gguf") || !strings.Contains(argv, "{ctx}") {
				t.Errorf("%s/%s: LaunchArgv missing model filename or {ctx}: %v", c.os, c.arch, m.LaunchArgv)
			}
		}
		if !found {
			t.Errorf("no local/qwen3-0.6b-q8 manifest for %s/%s", c.os, c.arch)
		}
	}
}

// TestManifestForModelIgnoresHostForDivergentBackends documents the exact bug
// ChatManifestForHost was added to fix: ManifestForModel does a bare ModelID
// match with no OS/Arch filter, so for a model id with per-platform entries
// that use DIFFERENT backends (local/bonsai-8b-1bit: MLX on darwin/arm64,
// llama.cpp everywhere else), it returns whichever entry happens to be first
// in localManifests — regardless of this host's actual platform. Chat call
// sites (StartModelInstance, /localmodel add) must use ChatManifestForHost
// instead, never this function, for exactly this reason.
func TestManifestForModelIgnoresHostForDivergentBackends(t *testing.T) {
	got, ok := ManifestForModel("local/bonsai-8b-1bit")
	if !ok {
		t.Fatal("expected a local/bonsai-8b-1bit manifest to exist")
	}
	if got.OS != "darwin" || got.Arch != "arm64" || got.Backend != BackendMLX {
		t.Fatalf("test assumption changed (first declared bonsai entry is no longer darwin/arm64 MLX): got %s/%s backend=%q — update this test's expectation and re-verify the bug this documents still applies to whichever entry is now first", got.OS, got.Arch, got.Backend)
	}
}

// TestChatManifestForHostMatchesRuntimeHost is the regression guard for the
// same bug: unlike ManifestForModel, ChatManifestForHost must only ever
// return the manifest whose OS/Arch equal the actual running host's.
func TestChatManifestForHostMatchesRuntimeHost(t *testing.T) {
	man, ok := ChatManifestForHost("local/bonsai-8b-1bit")
	if !ok {
		t.Skip("no local/bonsai-8b-1bit manifest declared for this test host's OS/Arch")
	}
	if man.OS != runtime.GOOS || man.Arch != runtime.GOARCH {
		t.Fatalf("ChatManifestForHost returned %s/%s, want the current host %s/%s", man.OS, man.Arch, runtime.GOOS, runtime.GOARCH)
	}
	if man.Kind != "chat" {
		t.Fatalf("ChatManifestForHost returned a non-chat manifest: Kind=%q", man.Kind)
	}
}

func TestChatManifestForHostUnknownModelReturnsFalse(t *testing.T) {
	if _, ok := ChatManifestForHost("local/does-not-exist-anywhere"); ok {
		t.Fatal("expected false for an unregistered model id")
	}
}

// TestBonsaiMLXManifestHasNoConcurrencyFlag guards a real crash: an earlier
// revision passed --decode-concurrency {parallel} in the MLX LaunchArgv,
// assuming mlx_lm.server supported a concurrent-request-slot flag. It
// doesn't (confirmed from the server's own --help usage output), so the
// spawn crashed with "unrecognized arguments: --decode-concurrency" — this
// happened even with the flag-probe/drop safety net working correctly,
// because the flag never existed on any version of this server to probe
// for. The fix is to never pass it in the first place, not to rely on
// runtime detection for a flag we now know is always absent.
func TestBonsaiMLXManifestHasNoConcurrencyFlag(t *testing.T) {
	for _, m := range localManifests {
		if m.ModelID != "local/bonsai-8b-1bit" || m.Backend != BackendMLX {
			continue
		}
		for _, a := range m.LaunchArgv {
			if a == "--decode-concurrency" || a == "--max-sequences" || a == "{parallel}" {
				t.Fatalf("MLX bonsai manifest LaunchArgv must not reference a concurrency flag/placeholder this server doesn't support: %v", m.LaunchArgv)
			}
		}
	}
}

func TestModelMatches(t *testing.T) {
	// llama.cpp reports the full GGUF path; substring match on basename.
	if !modelMatches([]string{"/Users/x/discovery/local-darwin-arm64/bge-m3-q4_k_m.gguf"}, "bge-m3-q4_k_m.gguf") {
		t.Fatalf("should match llama.cpp gguf path")
	}
	// MLX server reports the discovery model id verbatim.
	if !modelMatches([]string{"local/lfm2.5-embedding"}, "local/lfm2.5-embedding") {
		t.Fatalf("should match mlx model id")
	}
	// Wrong model must not match.
	if modelMatches([]string{"local/bge-m3"}, "local/lfm2.5-embedding") {
		t.Fatalf("bge server must not match lfm request")
	}
	if modelMatches(nil, "anything") {
		t.Fatalf("empty served list must not match")
	}
}

// The llama-server artifact is shared by every llama.cpp chat model on a given
// platform: one b9777 tarball and SHA per (os, arch). A copy/paste slip in a row
// (an arm64 URL on the amd64 row) is otherwise caught only at download time by
// the SHA check, so pin the platform pairing and cross-check it against the
// sibling model that already ships that build.
func TestQwen06BQ8ManifestPinsLlamaServerArtifact(t *testing.T) {
	wantTarball := map[string]string{
		"darwin/arm64": "llama-b9777-bin-macos-arm64.tar.gz",
		"darwin/amd64": "llama-b9777-bin-macos-x64.tar.gz",
		"linux/amd64":  "llama-b9777-bin-ubuntu-x64.tar.gz",
	}
	sibling := map[string]Artifact{}
	for _, m := range localManifests {
		if m.ModelID != "local/qwen3.8-4b-distill" {
			continue
		}
		if a, ok := llamaServerArtifact(m); ok {
			sibling[m.OS+"/"+m.Arch] = a
		}
	}
	if len(sibling) == 0 {
		t.Fatal("no local/qwen3.8-4b-distill llama-server artifact to compare against")
	}
	seen := map[string]bool{}
	for _, m := range localManifests {
		if m.ModelID != "local/qwen3-0.6b-q8" {
			continue
		}
		plat := m.OS + "/" + m.Arch
		seen[plat] = true
		want, known := wantTarball[plat]
		if !known {
			t.Errorf("%s: unexpected platform", plat)
			continue
		}
		a, ok := llamaServerArtifact(m)
		if !ok {
			t.Errorf("%s: no llama-server artifact", plat)
			continue
		}
		if !strings.HasSuffix(a.URL, "/"+want) {
			t.Errorf("%s: llama-server URL = %q, want the %s tarball", plat, a.URL, want)
		}
		if !a.Exec || a.Archive != ArchiveGZ {
			t.Errorf("%s: llama-server artifact must be an executable gz archive (Exec=%v, Archive=%q)", plat, a.Exec, a.Archive)
		}
		if len(a.SHA256) != 64 || strings.Trim(a.SHA256, "0123456789abcdef") != "" {
			t.Errorf("%s: llama-server SHA256 = %q, want 64 lowercase hex chars", plat, a.SHA256)
		}
		if sib, ok := sibling[plat]; ok && sib != a {
			t.Errorf("%s: llama-server artifact %+v differs from the qwen3.8 row %+v; the same b9777 build must be shared", plat, a, sib)
		}
		if argv := strings.Join(m.LaunchArgv, " "); !strings.Contains(argv, "--parallel {parallel}") {
			t.Errorf("%s: LaunchArgv must pass --parallel {parallel}: %v", plat, m.LaunchArgv)
		}
	}
	for plat := range wantTarball {
		if !seen[plat] {
			t.Errorf("no local/qwen3-0.6b-q8 row for %s", plat)
		}
	}
}

// llamaServerArtifact returns the manifest's llama-server binary artifact.
func llamaServerArtifact(m ServerManifest) (Artifact, bool) {
	for _, a := range m.Artifacts {
		if a.Dest == "llama-server" {
			return a, true
		}
	}
	return Artifact{}, false
}
