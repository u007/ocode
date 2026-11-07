package tailscale

import "testing"

// TestArtifactUpstreamPathReachesCLIArgv pins the exact argv for the artifact
// mount. The server passes target "localhost:<port>/p" (see
// internal/server newArtifactTailscaleShare); the path must survive
// normalization so `--set-path /p` proxies to localhost:<port>/p, not the root.
func TestArtifactUpstreamPathReachesCLIArgv(t *testing.T) {
	t.Run("serve", func(t *testing.T) {
		calls := installFakeTailscaleCLI(t, "https://host.ts.net/p\n")
		url, _, _ := StartServeExpose("localhost:1234/p", "p")
		expose := exposeCommands(calls())
		if len(expose) != 1 {
			t.Fatalf("want exactly one exposure attempt, got %v", expose)
		}
		want := "serve --bg --set-path /p http://localhost:1234/p"
		if expose[0] != want {
			t.Fatalf("serve argv = %q, want %q", expose[0], want)
		}
		if url != "https://host.ts.net/p" {
			t.Fatalf("url = %q", url)
		}
	})

	t.Run("funnel", func(t *testing.T) {
		calls := installFakeTailscaleCLI(t, "https://host.ts.net:8443/p\n")
		_, _, _, kind := StartExposeWithKind("localhost:1234/p", "p")
		if kind != "funnel" {
			t.Fatalf("kind = %q, want funnel", kind)
		}
		expose := exposeCommands(calls())
		if len(expose) == 0 {
			t.Fatal("no exposure attempt recorded")
		}
		want := "funnel --bg --https=8443 --set-path /p http://localhost:1234/p"
		if expose[0] != want {
			t.Fatalf("funnel argv = %q, want %q", expose[0], want)
		}
	})
}
