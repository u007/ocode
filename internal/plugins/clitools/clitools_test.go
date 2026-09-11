package clitools

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCatalogHasRequiredTools(t *testing.T) {
	want := map[string]bool{"fd": false, "rg": false, "fzf": false, "eza": false, "bat": false, "grep": false}
	tools := Catalog()
	if len(tools) == 0 {
		t.Fatal("Catalog() returned no tools")
	}
	for _, tool := range tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
		if tool.Description == "" {
			t.Errorf("tool %q has empty description", tool.Name)
		}
		if tool.Project == "" {
			t.Errorf("tool %q has empty project URL", tool.Name)
		}
		if len(tool.install) == 0 {
			t.Errorf("tool %q has no install recipes", tool.Name)
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("catalog missing required tool %q", name)
		}
	}
}

func TestCatalogCoversAllPlatforms(t *testing.T) {
	// Every tool must have at least one recipe per common desktop platform
	// family (or a documented reason not to — grep on Windows comes from
	// Git-for-Windows).
	platformManagers := map[string][]PackageManager{
		"darwin":  {PMBrew},
		"linux":   {PMApt, PMDnf, PMPacman},
		"windows": {PMWinget, PMChoco, PMScoop},
	}
	for _, tool := range Catalog() {
		for plat, managers := range platformManagers {
			var any bool
			for _, pm := range managers {
				if tool.installRecipes(pm) != nil {
					any = true
					break
				}
			}
			if !any && !(tool.Name == "grep" && plat == "windows") {
				t.Errorf("tool %q has no %s install recipe (managers %v)", tool.Name, plat, managers)
			}
		}
	}
}

func TestFindToolByCanonicalNameAndAlias(t *testing.T) {
	if tool, ok := FindTool("rg"); !ok || tool.Name != "rg" {
		t.Fatalf("FindTool(\"rg\") = %v, %v; want rg", tool, ok)
	}
	// Alias lookup: fd ↔ fdfind.
	if tool, ok := FindTool("fdfind"); !ok || tool.Name != "fd" {
		t.Fatalf("FindTool(\"fdfind\") = %v, %v; want fd", tool, ok)
	}
	if _, ok := FindTool("nonexistent-tool"); ok {
		t.Fatal("FindTool(\"nonexistent-tool\") should not resolve")
	}
}

func TestDetectUsesAlias(t *testing.T) {
	dir := t.TempDir()
	// Create a fake "fdfind" binary on PATH; Detect for tool "fd" should
	// resolve through the alias.
	bin := filepath.Join(dir, "fdfind")
	if runtime.GOOS == "windows" {
		bin += ".bat"
	}
	script := "#!/bin/sh\nexit 0\n"
	if runtime.GOOS == "windows" {
		script = "@echo off\r\nexit /b 0\r\n"
	}
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	for _, tool := range Catalog() {
		if tool.Name != "fd" {
			continue
		}
		cmd, ok := tool.Detect()
		if !ok {
			t.Fatal("Detect() did not find fake fdfind on PATH")
		}
		if cmd != "fdfind" {
			t.Fatalf("Detect() resolved %q, want fdfind", cmd)
		}
	}
}

func TestDetectMissingTool(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir) // empty dir: nothing findable
	for _, tool := range Catalog() {
		if _, ok := tool.Detect(); ok {
			t.Errorf("Detect() reported tool %q found with empty PATH", tool.Name)
		}
	}
}

func TestDetectRealGrepOnHost(t *testing.T) {
	// Every Unix-like dev host ships grep; if the test binary can find it,
	// Detect must too.
	if _, err := exec.LookPath("grep"); err != nil {
		t.Skip("no grep on host PATH")
	}
	tool, ok := FindTool("grep")
	if !ok {
		t.Fatal("grep missing from catalog")
	}
	if _, ok := tool.Detect(); !ok {
		t.Fatal("Detect() could not find host grep")
	}
}

func TestInstallUnknownTool(t *testing.T) {
	res := Install("definitely-not-a-tool-xyz")
	if res.Err == nil {
		t.Fatal("Install(unknown) should error")
	}
	if !strings.Contains(res.Err.Error(), "unknown tool") {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if res.NoManager {
		t.Fatal("unknown-tool failure must not set NoManager (hint is for missing managers only)")
	}
}

func TestMissingManagerHintNonEmpty(t *testing.T) {
	hint := MissingManagerHint()
	if hint == "" {
		t.Fatal("MissingManagerHint() must not be empty")
	}
	if !strings.Contains(hint, "No supported package manager found on PATH") {
		t.Fatalf("hint missing expected prefix, got: %q", hint)
	}
}

func TestManagerCommandInstallForms(t *testing.T) {
	cases := []struct {
		pm       PackageManager
		wantBin  string
		wantArgs []string
	}{
		{PMBrew, "brew", []string{"install", "ripgrep"}},
		{PMApt, "sudo", []string{"apt-get", "install", "-y", "ripgrep"}},
		{PMDnf, "dnf", []string{"install", "ripgrep"}},
		{PMPacman, "sudo", []string{"pacman", "-S", "--noconfirm", "ripgrep"}},
		{PMApk, "apk", []string{"add", "ripgrep"}},
		{PMWinget, "winget", []string{"install", "-e", "--accept-source-agreements", "--accept-package-agreements", "ripgrep"}},
		{PMChoco, "choco", []string{"install", "-y", "ripgrep"}},
		{PMScoop, "scoop", []string{"install", "ripgrep"}},
		{PMCargo, "cargo", []string{"install", "--locked", "ripgrep"}},
		{PMFreeBSD, "sudo", []string{"pkg", "install", "-y", "ripgrep"}},
	}
	for _, tc := range cases {
		bin, args := managerCommand(tc.pm, "install", []string{"ripgrep"})
		if bin != tc.wantBin {
			t.Errorf("%s: bin = %q, want %q", tc.pm, bin, tc.wantBin)
		}
		if strings.Join(args, " ") != strings.Join(tc.wantArgs, " ") {
			t.Errorf("%s: args = %v, want %v", tc.pm, args, tc.wantArgs)
		}
	}
}

func TestManagerCommandProbeForms(t *testing.T) {
	// Probe must not use sudo (apt/pacman/pkg probe paths).
	if bin, _ := managerCommand(PMApt, "probe", []string{"fd-find"}); bin == "sudo" {
		t.Error("apt probe should not use sudo")
	}
	if bin, _ := managerCommand(PMPacman, "probe", []string{"fd"}); bin == "sudo" {
		t.Error("pacman probe should not use sudo")
	}
	if bin, _ := managerCommand(PMFreeBSD, "probe", []string{"fd"}); bin == "sudo" {
		t.Error("pkg probe should not use sudo")
	}
}

func TestInstallOrderIncludesGOOS(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "windows", "freebsd"} {
		if _, ok := installOrder[goos]; !ok {
			t.Errorf("installOrder missing GOOS %q", goos)
		}
	}
}
