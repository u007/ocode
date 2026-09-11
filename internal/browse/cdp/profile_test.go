package cdp

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

func TestPrepareProfileDir_EmptyUsesTemp(t *testing.T) {
	dir, cleanup, err := prepareProfileDir("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(filepath.Base(dir), "ocode-browse-") {
		t.Fatalf("expected temp dir, got %s", dir)
	}
	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("temp dir should be removed after cleanup")
	}
}

func TestPrepareProfileDir_PersistentCreatedAndKept(t *testing.T) {
	want := filepath.Join(t.TempDir(), "nested", "chrome-profile")
	dir, cleanup, err := prepareProfileDir(want, nil)
	if err != nil {
		t.Fatal(err)
	}
	if dir != want {
		t.Fatalf("dir = %s, want %s", dir, want)
	}
	if err := os.WriteFile(filepath.Join(dir, "Cookies"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cleanup()
	if _, err := os.Stat(filepath.Join(dir, "Cookies")); err != nil {
		t.Fatalf("persistent profile must survive cleanup: %v", err)
	}
}

func TestPrepareProfileDir_StaleLockRemoved(t *testing.T) {
	want := t.TempDir()
	// Chrome's posix singleton: SingletonLock -> "<host>-<pid>". Use a pid
	// that cannot be alive.
	lock := filepath.Join(want, "SingletonLock")
	if err := os.Symlink("somehost-2147483000", lock); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"SingletonSocket", "SingletonCookie"} {
		if err := os.Symlink("dangling", filepath.Join(want, n)); err != nil {
			t.Fatal(err)
		}
	}
	dir, cleanup, err := prepareProfileDir(want, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if dir != want {
		t.Fatalf("stale lock should not force fallback; got %s", dir)
	}
	for _, n := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie"} {
		if _, err := os.Lstat(filepath.Join(want, n)); !os.IsNotExist(err) {
			t.Fatalf("%s should be removed", n)
		}
	}
}

func TestPrepareProfileDir_LiveLockFallsBackToTemp(t *testing.T) {
	want := t.TempDir()
	lock := filepath.Join(want, "SingletonLock")
	// Our own pid is certainly alive.
	if err := os.Symlink("somehost-"+strconv.Itoa(os.Getpid()), lock); err != nil {
		t.Fatal(err)
	}
	dir, cleanup, err := prepareProfileDir(want, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if dir == want {
		t.Fatalf("live lock must fall back to a temp profile")
	}
	if _, err := os.Lstat(lock); err != nil {
		t.Fatalf("live lock must be left intact: %v", err)
	}
}

// Gated: real Chrome, persistent profile, cookie survives a full relaunch.
func TestPersistentProfile_CookieSurvivesRelaunch_Gated(t *testing.T) {
	chromePath := os.Getenv("OCODE_CHROME_PATH")
	if chromePath == "" {
		t.Skip("OCODE_CHROME_PATH not set — gated test")
	}
	profile := filepath.Join(t.TempDir(), "chrome-profile")
	ctx := t.Context()
	lg := log.New(os.Stderr, "", 0)

	launch := func() (*Conn, func()) {
		sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
		conn, exited, cleanup, err := launchChromeWithOptions(ctx, chromePath, sup, lg, "", "", "", profile, true)
		if err != nil {
			t.Fatalf("launch: %v", err)
		}
		return conn, func() {
			// Graceful close so Chrome flushes the Cookies db.
			_ = conn.Call(ctx, "", "Browser.close", nil, nil)
			select {
			case <-exited:
			case <-time.After(10 * time.Second):
				t.Fatal("chrome did not exit")
			}
			cleanup()
		}
	}

	conn, closeFn := launch()
	if err := conn.Call(ctx, "", "Storage.setCookies", map[string]any{
		"cookies": []map[string]any{{
			"name": "ocode_persist", "value": "yes", "domain": "example.com", "path": "/",
			"expires": float64(time.Now().Add(24 * time.Hour).Unix()),
		}},
	}, nil); err != nil {
		t.Fatalf("setCookies: %v", err)
	}
	closeFn()

	conn, closeFn = launch()
	defer closeFn()
	var res struct {
		Cookies []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"cookies"`
	}
	if err := conn.Call(ctx, "", "Storage.getCookies", nil, &res); err != nil {
		t.Fatalf("getCookies: %v", err)
	}
	for _, c := range res.Cookies {
		if c.Name == "ocode_persist" && c.Value == "yes" {
			return
		}
	}
	t.Fatalf("cookie did not survive relaunch; got %+v", res.Cookies)
}
