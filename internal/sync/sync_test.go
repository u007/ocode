package sync

import (
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
		t.Fatal("expected no token after clear")
	}
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
