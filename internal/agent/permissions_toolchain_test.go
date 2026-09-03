package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// toolchainTestEnvs lists every env var userWritableRoots consults, so tests
// start from a deterministic baseline regardless of the host environment.
var toolchainTestEnvs = []string{
	"XDG_CACHE_HOME", "CARGO_HOME", "GOBIN", "GOPATH", "GOCACHE",
	"NVM_DIR", "BUN_INSTALL", "DENO_INSTALL", "DENO_DIR", "PNPM_HOME",
	"VOLTA_HOME", "FNM_DIR", "UV_CACHE_DIR", "UV_PYTHON_INSTALL_DIR",
	"rvm_path", "RBENV_ROOT", "JENV_ROOT", "RUSTUP_HOME",
	"CONDA_ENVS_PATH", "CONDA_PKGS_DIRS", "CONDA_PREFIX",
	"NUGET_PACKAGES", "DOTNET_CLI_HOME", "DOTNET_ROOT",
	"SDKMAN_DIR", "GRADLE_USER_HOME", "COURSIER_CACHE",
	"WORKON_HOME", "POETRY_HOME", "JULIA_DEPOT_PATH", "TF_PLUGIN_CACHE_DIR",
	"ANDROID_HOME", "PUB_CACHE", "PIPX_HOME", "NPM_CONFIG_PREFIX",
	"R_LIBS", "R_LIBS_USER", "MIX_HOME", "HEX_HOME", "ASDF_DIR", "MISE_DATA_DIR",
	"VP_HOME", "VP_BIN_DIR", "VP_DATA_DIR", "VP_CACHE_DIR",
	"PIP_CACHE_DIR", "npm_config_cache", "YARN_CACHE_FOLDER", "COMPOSER_HOME",
	"GEM_HOME", "GEM_PATH", "GOMODCACHE",
}

func clearToolchainEnvs(t *testing.T) {
	t.Helper()
	for _, k := range toolchainTestEnvs {
		t.Setenv(k, "")
	}
}

func writableRootsSet(t *testing.T) map[string]struct{} {
	t.Helper()
	set := map[string]struct{}{}
	for _, r := range userWritableRoots() {
		set[r] = struct{}{}
	}
	return set
}

func TestUserWritableRoots_ToolchainManagerDefaults(t *testing.T) {
	setHomeForTest(t, "/home/testuser")
	clearToolchainEnvs(t)
	got := writableRootsSet(t)
	mustContain := []string{
		// Node / JS version managers and runtimes.
		"/home/testuser/.nvm",
		"/home/testuser/.vite-plus",
		"/home/testuser/.local/share/vite-plus",
		"/home/testuser/.bun",
		"/home/testuser/.deno",
		"/home/testuser/.volta",
		"/home/testuser/.fnm",
		// Ruby / Rust / JVM managers.
		"/home/testuser/.rvm",
		"/home/testuser/.rbenv",
		"/home/testuser/.jenv",
		"/home/testuser/.rustup",
		"/home/testuser/.sdkman",
		// Conda env+pkg state (install roots deliberately excluded).
		"/home/testuser/.conda",
		// npm / .NET / Mono.
		"/home/testuser/.npm",
		"/home/testuser/.nuget/packages",
		"/home/testuser/.dotnet",
		"/home/testuser/.mono",
		// JVM / Python / misc tool state.
		"/home/testuser/.sbt",
		"/home/testuser/.ivy2",
		"/home/testuser/.coursier",
		"/home/testuser/.m2",
		"/home/testuser/.gradle",
		"/home/testuser/.pyenv",
		"/home/testuser/.local/share/virtualenvs",
		"/home/testuser/.virtualenvs",
		"/home/testuser/.julia",
		"/home/testuser/.terraform.d",
		"/home/testuser/.ghcup",
		"/home/testuser/.cabal",
		"/home/testuser/.stack",
		// Mobile / misc ecosystems.
		"/home/testuser/.pub-cache",
		"/home/testuser/.local/share/pipx",
		"/home/testuser/.npm-global",
		"/home/testuser/.mix",
		"/home/testuser/.hex",
		"/home/testuser/.asdf",
		"/home/testuser/.local/share/mise",
		// Golang bin path (regression: `go install` must stay writable).
		"/home/testuser/go/bin",
	}
	// Platform-specific tool homes.
	if runtime.GOOS == "darwin" {
		mustContain = append(mustContain,
			"/home/testuser/Library/pnpm",
			"/home/testuser/Library/Application Support/uv/python",
			"/home/testuser/Library/Application Support/pypoetry",
			"/home/testuser/Library/Android/sdk",
		)
	} else {
		mustContain = append(mustContain,
			"/home/testuser/.local/share/pnpm",
			"/home/testuser/.local/share/uv/python",
			"/home/testuser/.local/share/pypoetry",
			"/home/testuser/Android/Sdk",
		)
	}
	for _, want := range mustContain {
		if _, ok := got[want]; !ok {
			t.Errorf("userWritableRoots missing %q; got %v", want, userWritableRoots())
		}
	}
	// Conda install roots must NOT be granted by default.
	for _, bad := range []string{
		"/home/testuser/miniconda3",
		"/home/testuser/anaconda3",
		"/home/testuser/miniforge3",
	} {
		if _, ok := got[bad]; ok {
			t.Errorf("userWritableRoots must not contain install root %q by default", bad)
		}
	}
}

func TestUserWritableRoots_ValidEnvOverrides(t *testing.T) {
	setHomeForTest(t, "/home/testuser")
	clearToolchainEnvs(t)
	t.Setenv("NVM_DIR", "/home/testuser/custom-nvm")
	t.Setenv("RUSTUP_HOME", "/home/testuser/custom-rustup")
	t.Setenv("CONDA_ENVS_PATH", "/home/testuser/e1:/home/testuser/e2")
	t.Setenv("GOCACHE", "/home/testuser/custom-gocache")
	t.Setenv("NUGET_PACKAGES", "/home/testuser/custom-nuget")
	got := writableRootsSet(t)
	for _, want := range []string{
		"/home/testuser/custom-nvm",
		"/home/testuser/custom-rustup",
		"/home/testuser/e1",
		"/home/testuser/e2",
		"/home/testuser/custom-gocache",
		"/home/testuser/custom-nuget",
	} {
		if _, ok := got[want]; !ok {
			t.Errorf("userWritableRoots missing valid override %q", want)
		}
	}
	// A set-but-valid override replaces the default (CARGO_HOME semantics).
	if _, ok := got["/home/testuser/.nvm"]; ok {
		t.Error("userWritableRoots should not contain default ~/.nvm when NVM_DIR is set")
	}
}

func TestUserWritableRoots_RejectsHomeEqualAndEscapes(t *testing.T) {
	setHomeForTest(t, "/home/testuser")
	clearToolchainEnvs(t)

	// $HOME itself must never be granted as a direct root.
	t.Setenv("NVM_DIR", "/home/testuser")
	t.Setenv("GOBIN", "/home/testuser")
	t.Setenv("XDG_CACHE_HOME", "/home/testuser")
	// System, outside-home, parent, relative, and root values rejected.
	t.Setenv("RUSTUP_HOME", "/usr/local/rustup")
	t.Setenv("BUN_INSTALL", "/tmp/evil-bun")
	t.Setenv("VOLTA_HOME", "/home")
	t.Setenv("FNM_DIR", "relative/fnm")
	t.Setenv("CONDA_ENVS_PATH", "/opt/conda/envs:/home/testuser/goode")
	got := writableRootsSet(t)
	for _, bad := range []string{
		"/home/testuser", // equal-home must not grant all of home
		"/usr/local/rustup",
		"/tmp/evil-bun",
		"/home",
		"relative/fnm",
		"/opt/conda/envs",
	} {
		if _, ok := got[bad]; ok {
			t.Errorf("userWritableRoots must reject %q", bad)
		}
	}
	// The valid entry in the mixed conda list is still granted.
	if _, ok := got["/home/testuser/goode"]; !ok {
		t.Error("userWritableRoots should grant the valid entry of a mixed CONDA_ENVS_PATH")
	}
	// Invalid env values grant nothing — not even a fallback.
	if _, ok := got["/home/testuser/.rustup"]; ok {
		t.Error("userWritableRoots should not fall back to ~/.rustup when RUSTUP_HOME is invalid")
	}
}

func TestUserWritableRoots_XDGEchoValidation(t *testing.T) {
	setHomeForTest(t, "/home/testuser")
	clearToolchainEnvs(t)
	// XDG value identical to what UserCacheDir returns (the Linux echo path):
	// granted after validation.
	t.Setenv("XDG_CACHE_HOME", "/home/testuser/Library/Caches")
	if _, ok := writableRootsSet(t)["/home/testuser/Library/Caches"]; !ok {
		t.Error("userWritableRoots should grant a valid XDG dir echoed by UserCacheDir")
	}
	// XDG=$HOME must never grant home, even on the echo path.
	t.Setenv("XDG_CACHE_HOME", "/home/testuser")
	if _, ok := writableRootsSet(t)["/home/testuser"]; ok {
		t.Error("userWritableRoots must reject XDG_CACHE_HOME=$HOME")
	}
}

func TestUserWritableRoots_RejectsSymlinkEscape(t *testing.T) {
	home := t.TempDir() // real dir, so EvalSymlinks resolves deterministically
	t.Setenv("HOME", home)
	clearToolchainEnvs(t)

	// Symlink lexically under home but pointing outside (os.TempDir): rejected.
	evil := filepath.Join(home, "evillink")
	if err := os.Symlink(os.TempDir(), evil); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	t.Setenv("NVM_DIR", evil)
	if _, ok := writableRootsSet(t)[evil]; ok {
		t.Errorf("userWritableRoots must reject symlink escape %q -> %q", evil, os.TempDir())
	}

	// Symlink staying inside home: granted as the canonical path.
	real := filepath.Join(home, "realnvm")
	if err := os.MkdirAll(real, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	link := filepath.Join(home, "nvm")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	t.Setenv("NVM_DIR", link)
	canonical, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if _, ok := writableRootsSet(t)[canonical]; !ok {
		t.Errorf("userWritableRoots should grant in-home symlink target %q", canonical)
	}
}

func TestLanguageDepRoots_RejectsInvalidOverrides(t *testing.T) {
	setHomeForTest(t, "/home/testuser")
	clearToolchainEnvs(t)
	inDep := func(want string) bool {
		for _, r := range languageDepRoots() {
			if r == want {
				return true
			}
		}
		return false
	}

	// Filesystem root and $HOME-equal overrides grant nothing.
	t.Setenv("PIP_CACHE_DIR", "/")
	t.Setenv("npm_config_cache", "/home/testuser")
	t.Setenv("GOMODCACHE", "/")
	if inDep("/") || inDep("/home/testuser") {
		t.Error("languageDepRoots must reject / and $HOME-equal overrides")
	}

	// Outside-home scratch caches stay usable (CI flexibility).
	t.Setenv("PIP_CACHE_DIR", "/tmp/piptest")
	t.Setenv("npm_config_cache", "/tmp/npmtest")
	if !inDep("/tmp/piptest") || !inDep("/tmp/npmtest") {
		t.Errorf("languageDepRoots should allow scratch-dir overrides; got %v", languageDepRoots())
	}
}
