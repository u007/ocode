package sync

import (
	"bytes"
	"log"
	"strings"
	"sync"
	"testing"
)

func withTempHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
}

func TestSnapshotPathDiffersPerBlobType(t *testing.T) {
	withTempHome(t)

	configPath, err := SnapshotPath(BlobTypeConfig)
	if err != nil {
		t.Fatalf("SnapshotPath(config): %v", err)
	}
	authPath, err := SnapshotPath(BlobTypeAuth)
	if err != nil {
		t.Fatalf("SnapshotPath(auth): %v", err)
	}
	if configPath == authPath {
		t.Fatalf("expected distinct paths, got %q for both", configPath)
	}
}

func TestTokenPathIsNonEmpty(t *testing.T) {
	withTempHome(t)

	path, err := TokenPath()
	if err != nil {
		t.Fatalf("TokenPath: %v", err)
	}
	if path == "" {
		t.Fatal("expected non-empty path")
	}
}

func TestSaveLoadClearTokenRoundTrip(t *testing.T) {
	withTempHome(t)

	_, ok, err := LoadToken()
	if err != nil {
		t.Fatalf("LoadToken on empty: %v", err)
	}
	if ok {
		t.Fatal("expected no token initially")
	}

	if err := SaveToken("test-token-abc"); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}
	token, ok, err := LoadToken()
	if err != nil {
		t.Fatalf("LoadToken after save: %v", err)
	}
	if !ok {
		t.Fatal("expected token to be present")
	}
	if token != "test-token-abc" {
		t.Fatalf("expected 'test-token-abc', got %q", token)
	}

	if err := ClearToken(); err != nil {
		t.Fatalf("ClearToken: %v", err)
	}
	_, ok, err = LoadToken()
	if err != nil {
		t.Fatalf("LoadToken after clear: %v", err)
	}
	if ok {
		t.Fatal("expected token to be cleared")
	}
}

// captureLog redirects log output to a buffer for the duration of fn and
// returns what was written. Used to assert the one-time base-URL diagnostic
// without polluting stderr.
func captureLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	old := log.Writer()
	flags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(old)
		log.SetFlags(flags)
	}()
	fn()
	return buf.String()
}

// TestResolveBaseURLWithSource verifies the URL + source are resolved
// consistently: config.SyncURL wins, then OCODE_SYNC_URL env, then the
// production default.
func TestResolveBaseURLWithSource(t *testing.T) {
	resetBaseURLNoticeTestOnly()
	t.Run("config wins", func(t *testing.T) {
		t.Setenv("OCODE_SYNC_URL", "https://env.example.com")
		got, src := ResolveBaseURLWithSource("https://cfg.example.com")
		if got != "https://cfg.example.com" || src != BaseURLSourceConfig {
			t.Fatalf("got %q src=%v, want https://cfg.example.com src=Config", got, src)
		}
	})
	t.Run("env when no config", func(t *testing.T) {
		t.Setenv("OCODE_SYNC_URL", "https://env.example.com")
		got, src := ResolveBaseURLWithSource("")
		if got != "https://env.example.com" || src != BaseURLSourceEnv {
			t.Fatalf("got %q src=%v, want https://env.example.com src=Env", got, src)
		}
	})
	t.Run("default when nothing set", func(t *testing.T) {
		t.Setenv("OCODE_SYNC_URL", "")
		got, src := ResolveBaseURLWithSource("")
		if got != "https://hub.mercstudio.com" || src != BaseURLSourceDefault {
			t.Fatalf("got %q src=%v, want https://hub.mercstudio.com src=Default", got, src)
		}
	})
}

// TestLogBaseURLNoticeOnceOnly verifies the diagnostic fires exactly once per
// process (guarded by sync.Once) and that the warning branch is used for the
// default source while config/env sources emit a shorter info line.
func TestLogBaseURLNoticeOnceOnly(t *testing.T) {
	resetBaseURLTestOnly := func() { baseURLNoticeOnce = sync.Once{} }

	t.Run("default emits warning", func(t *testing.T) {
		resetBaseURLTestOnly()
		out := captureLog(t, func() { LogBaseURLNotice("https://hub.mercstudio.com", BaseURLSourceDefault) })
		if !strings.Contains(out, "WARNING") {
			t.Fatalf("expected WARNING in default notice, got %q", out)
		}
		if !strings.Contains(out, "sync_url") {
			t.Fatalf("expected sync_url hint in default notice, got %q", out)
		}
	})
	t.Run("config emits info not warning", func(t *testing.T) {
		resetBaseURLTestOnly()
		out := captureLog(t, func() { LogBaseURLNotice("https://cfg.example.com", BaseURLSourceConfig) })
		if strings.Contains(out, "WARNING") {
			t.Fatalf("config notice should not be a warning, got %q", out)
		}
		if !strings.Contains(out, "configured") {
			t.Fatalf("expected 'configured' in config notice, got %q", out)
		}
	})
	t.Run("env emits info not warning", func(t *testing.T) {
		resetBaseURLTestOnly()
		out := captureLog(t, func() { LogBaseURLNotice("https://env.example.com", BaseURLSourceEnv) })
		if strings.Contains(out, "WARNING") {
			t.Fatalf("env notice should not be a warning, got %q", out)
		}
		if !strings.Contains(out, "OCODE_SYNC_URL") {
			t.Fatalf("expected OCODE_SYNC_URL in env notice, got %q", out)
		}
	})
	t.Run("fires only once", func(t *testing.T) {
		resetBaseURLTestOnly()
		out1 := captureLog(t, func() { LogBaseURLNotice("https://hub.mercstudio.com", BaseURLSourceDefault) })
		out2 := captureLog(t, func() { LogBaseURLNotice("https://hub.mercstudio.com", BaseURLSourceDefault) })
		if out1 == "" {
			t.Fatal("expected first call to log")
		}
		if out2 != "" {
			t.Fatalf("expected second call to be suppressed by once-guard, got %q", out2)
		}
	})
}

func TestClearTokenOnMissingFileIsNotError(t *testing.T) {
	withTempHome(t)

	if err := ClearToken(); err != nil {
		t.Fatalf("ClearToken on missing file: %v", err)
	}
}

func TestLocalConfigPathForReturnsPaths(t *testing.T) {
	withTempHome(t)

	configPath, err := localConfigPathFor(BlobTypeConfig)
	if err != nil {
		t.Logf("config path error (expected when no config exists yet): %v", err)
	} else if configPath == "" {
		t.Error("expected non-empty config path")
	}
	authPath, err := localConfigPathFor(BlobTypeAuth)
	if err != nil {
		t.Errorf("auth path: %v", err)
	} else if authPath == "" {
		t.Error("expected non-empty auth path")
	}
}
