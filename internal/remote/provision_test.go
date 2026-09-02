package remote

import (
	"errors"
	"testing"
)

func TestUnameToGoEnv(t *testing.T) {
	cases := []struct {
		sys, mach    string
		goos, goarch string
		wantErr      bool
	}{
		{"Linux", "x86_64", "linux", "amd64", false},
		{"Linux", "aarch64", "linux", "arm64", false},
		{"Darwin", "arm64", "darwin", "arm64", false},
		{"Darwin", "x86_64", "darwin", "amd64", false},
		{"FreeBSD", "x86_64", "", "", true},
		{"Linux", "riscv64", "", "", true},
	}
	for _, c := range cases {
		goos, goarch, err := unameToGoEnv(c.sys, c.mach)
		if c.wantErr {
			if err == nil {
				t.Errorf("unameToGoEnv(%q,%q): expected error", c.sys, c.mach)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unameToGoEnv(%q,%q): unexpected error: %v", c.sys, c.mach, err)
		}
		if goos != c.goos || goarch != c.goarch {
			t.Errorf("unameToGoEnv(%q,%q) = %s/%s, want %s/%s", c.sys, c.mach, goos, goarch, c.goos, c.goarch)
		}
	}
}

func TestDetectPlatform(t *testing.T) {
	ft := newFakeTransport()
	ft.execResults["uname -sm"] = ExecResult{Stdout: "Linux aarch64\n"}
	goos, goarch, err := DetectPlatform(ft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if goos != "linux" || goarch != "arm64" {
		t.Errorf("got %s/%s, want linux/arm64", goos, goarch)
	}
}

func TestDetectPlatformMalformedOutput(t *testing.T) {
	ft := newFakeTransport()
	ft.execResults["uname -sm"] = ExecResult{Stdout: "garbage"}
	if _, _, err := DetectPlatform(ft); err == nil {
		t.Fatal("expected error for malformed uname output")
	}
}

func TestBinaryExists(t *testing.T) {
	ft := newFakeTransport()
	cmd := "test -x " + shellQuote(RemoteBinaryPath("1.2.3"))
	ft.execResults[cmd] = ExecResult{ExitCode: 0}
	if !BinaryExists(ft, "1.2.3") {
		t.Error("expected BinaryExists true")
	}

	ft2 := newFakeTransport()
	ft2.execErrs[cmd] = errors.New("not found")
	if BinaryExists(ft2, "1.2.3") {
		t.Error("expected BinaryExists false on exec error")
	}
}

func TestGCVersionsKeepsTwoNewest(t *testing.T) {
	ft := newFakeTransport()
	ft.execResults["ls -1 "+shellQuote(RemoteBinDir)+" 2>/dev/null || true"] = ExecResult{
		Stdout: "0.1.0\n0.2.0\n0.3.0\n0.9.9\n",
	}
	if err := GCVersions(ft); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// keep-two-newest lexicographically: 0.3.0 and 0.9.9 survive; 0.1.0 and
	// 0.2.0 are rm -rf'd.
	wantRemoved := map[string]bool{
		"rm -rf " + shellQuote(RemoteBinDir+"/0.1.0"): true,
		"rm -rf " + shellQuote(RemoteBinDir+"/0.2.0"): true,
	}
	seen := map[string]bool{}
	for _, c := range ft.execCalls {
		if wantRemoved[c] {
			seen[c] = true
		}
		if c == "rm -rf "+shellQuote(RemoteBinDir+"/0.3.0") || c == "rm -rf "+shellQuote(RemoteBinDir+"/0.9.9") {
			t.Errorf("GCVersions removed a newest-two dir: %s", c)
		}
	}
	if len(seen) != len(wantRemoved) {
		t.Errorf("GCVersions calls = %v, want removal of %v", ft.execCalls, wantRemoved)
	}
}

func TestGCVersionsNoopUnderTwo(t *testing.T) {
	ft := newFakeTransport()
	ft.execResults["ls -1 "+shellQuote(RemoteBinDir)+" 2>/dev/null || true"] = ExecResult{Stdout: "0.1.0\n"}
	if err := GCVersions(ft); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, c := range ft.execCalls {
		if len(c) > 6 && c[:6] == "rm -rf" {
			t.Errorf("GCVersions should not remove anything with only one version, got call %q", c)
		}
	}
}
