package server

import (
	"testing"
	"time"
)

// A failed funnel upgrade must leave the working tailnet-only mount alone: the
// old code tore it down first, so a funnel that proved nothing destroyed a
// share that was fine.
func TestFailedUpgradeKeepsWorkingServeMount(t *testing.T) {
	share := &tailscaleShare{}
	var removed []string
	share.removeFn = func(path string) { removed = append(removed, path) }

	share.exposeFn = func(kind string) (string, string) {
		return "https://host.ts.net/desktop", "serve"
	}
	if u, _ := share.ensureServe(1234); u == "" {
		t.Fatal("setup: serve should be live")
	}

	share.exposeFn = func(kind string) (string, string) { return "", "" } // funnel+serve both fail
	st := share.start("full", 1234)

	if !st.Running || st.Kind != "serve" || st.URL != "https://host.ts.net/desktop" {
		t.Fatalf("failed upgrade must keep the working serve share, got %+v", st)
	}
	if len(removed) != 0 {
		t.Fatalf("failed upgrade removed the live mount: %v", removed)
	}
}

// stop must not hold mu while the slow tailscale removal runs, or every status
// read (Settings/Share polling) freezes behind a hung CLI.
func TestStopDoesNotHoldStatusLockDuringRemoval(t *testing.T) {
	share := &tailscaleShare{}
	share.exposeFn = func(kind string) (string, string) { return "https://host.ts.net/desktop", "serve" }
	entered := make(chan struct{})
	release := make(chan struct{})
	share.removeFn = func(string) {
		close(entered)
		<-release
	}
	share.start("serve", 1234)

	done := make(chan struct{})
	go func() { share.stop(); close(done) }()
	<-entered

	statusDone := make(chan tailscaleShareStatus, 1)
	go func() { statusDone <- share.status() }()
	select {
	case st := <-statusDone:
		if st.Running {
			t.Fatalf("status during stop must already read not-running, got %+v", st)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("status() blocked behind a slow removeSetPath: stop holds mu across subprocesses")
	}
	close(release)
	<-done
}
