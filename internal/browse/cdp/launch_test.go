package cdp

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

func TestFindChrome_ConfiguredExists(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "chrome")
	if err := os.WriteFile(p, []byte("#!/bin/sh\necho hi"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := FindChrome(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != p {
		t.Fatalf("got %q want %q", got, p)
	}
}

func TestFindChrome_ConfiguredMissing(t *testing.T) {
	_, err := FindChrome("/nonexistent/chrome-xyz-123")
	if !errors.Is(err, ErrChromeNotFound) {
		t.Fatalf("want ErrChromeNotFound, got %v", err)
	}
}

func TestFindChrome_EnvVar(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "chrome-env")
	if err := os.WriteFile(p, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OCODE_CHROME_PATH", p)
	got, err := FindChrome("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != p {
		t.Fatalf("got %q want %q", got, p)
	}
}

func TestFindChrome_ProbeOrder(t *testing.T) {
	origGOOS := chromeGOOS
	origStat := chromeStat
	origLookPath := chromeLookPath
	defer func() {
		chromeGOOS = origGOOS
		chromeStat = origStat
		chromeLookPath = origLookPath
	}()

	// darwin order: stat probed in candidate order
	t.Run("darwin", func(t *testing.T) {
		chromeGOOS = "darwin"
		var probed []string
		chromeStat = func(p string) (os.FileInfo, error) {
			probed = append(probed, p)
			if p == macOSCandidates[2] {
				return nil, nil // third candidate exists
			}
			return nil, os.ErrNotExist
		}
		chromeLookPath = func(string) (string, error) {
			t.Fatal("lookPath should not be called on darwin")
			return "", errors.New("no")
		}
		t.Setenv("OCODE_CHROME_PATH", "")
		got, err := FindChrome("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != macOSCandidates[2] {
			t.Fatalf("got %q want %q", got, macOSCandidates[2])
		}
		if len(probed) != 3 {
			t.Fatalf("probed %v want first 3", probed)
		}
		for i := 0; i < 3; i++ {
			if probed[i] != macOSCandidates[i] {
				t.Fatalf("probe %d: got %q want %q", i, probed[i], macOSCandidates[i])
			}
		}
	})

	// linux order
	t.Run("linux", func(t *testing.T) {
		chromeGOOS = "linux"
		var probed []string
		chromeStat = func(p string) (os.FileInfo, error) { return nil, os.ErrNotExist }
		chromeLookPath = func(name string) (string, error) {
			probed = append(probed, name)
			if name == linuxCandidates[1] {
				return "/usr/bin/" + name, nil
			}
			return "", errors.New("not found")
		}
		t.Setenv("OCODE_CHROME_PATH", "")
		got, err := FindChrome("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "/usr/bin/"+linuxCandidates[1] {
			t.Fatalf("got %q", got)
		}
		if len(probed) != 2 || probed[0] != linuxCandidates[0] || probed[1] != linuxCandidates[1] {
			t.Fatalf("probed %v", probed)
		}
	})
}

func TestFindChrome_Windows(t *testing.T) {
	origGOOS := chromeGOOS
	defer func() { chromeGOOS = origGOOS }()
	chromeGOOS = "windows"
	t.Setenv("OCODE_CHROME_PATH", "")
	_, err := FindChrome("")
	if !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("want ErrUnsupportedPlatform, got %v", err)
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "chrome.exe")
	_ = os.WriteFile(p, []byte("x"), 0o755)
	_, err = FindChrome(p)
	if !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("want ErrUnsupportedPlatform even with configured path, got %v", err)
	}
}

func TestFindChrome_NoSandboxNoPort(t *testing.T) {
	// grep package for forbidden flags
	data, err := os.ReadFile("launch.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(s, "--no-sandbox") {
		t.Error("launch.go must not contain --no-sandbox")
	}
	if strings.Contains(s, "--remote-debugging-port") {
		t.Error("launch.go must not contain --remote-debugging-port")
	}
}

func TestLaunchChrome_Gated(t *testing.T) {
	chromePath := os.Getenv("OCODE_CHROME_PATH")
	if chromePath == "" {
		t.Skip("OCODE_CHROME_PATH not set — gated test")
	}
	if _, err := os.Stat(chromePath); err != nil {
		t.Skipf("chrome not found at %s: %v", chromePath, err)
	}
	ctx := t.Context()
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	lg := log.New(os.Stderr, "", 0)
	conn, exited, cleanup, err := launchChrome(ctx, chromePath, sup, lg)
	if err != nil {
		t.Fatalf("launchChrome failed: %v", err)
	}
	defer cleanup()
	var ver struct {
		Product string `json:"product"`
	}
	if err := conn.Call(ctx, "", "Browser.getVersion", nil, &ver); err != nil {
		t.Fatalf("Browser.getVersion: %v", err)
	}
	if ver.Product == "" {
		t.Error("expected product string")
	}
	found := false
	for _, rec := range sup.Snapshot() {
		if rec.ID == "browse-chrome" {
			found = true
			if rec.Kind != "browser" {
				t.Errorf("kind %q want browser", rec.Kind)
			}
		}
	}
	if !found {
		t.Error("supervisor missing browse-chrome record")
	}
	cleanup()
	select {
	case <-exited:
	case <-ctx.Done():
		t.Fatal("timeout waiting for exit")
	}
}
