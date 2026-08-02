package discovery

import (
	"runtime"
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
