//go:build darwin

package keychain

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeDarwinRoundTrip(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, "store")
	if err := os.MkdirAll(storeDir, 0700); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "security")
	content := `#!/usr/bin/env python3
import sys, os
store = os.environ["FAKE_STORE_DIR"]
args = sys.argv[1:]
cmd = args[0]
svc = acct = secret = None
i = 1
while i < len(args):
    if args[i] == "-s":
        svc = args[i+1]; i += 2; continue
    if args[i] == "-a":
        acct = args[i+1]; i += 2; continue
    if args[i] == "-w":
        if i+1 < len(args) and not args[i+1].startswith("-"):
            secret = args[i+1]; i += 2; continue
        i += 1; continue
    if args[i] == "-U":
        i += 1; continue
    i += 1
path = os.path.join(store, svc + "\x1f" + acct)
if cmd == "find-generic-password":
    if os.path.exists(path):
        sys.stdout.write(open(path).read())
        sys.exit(0)
    sys.stderr.write("could not be found\n")
    sys.exit(44)
elif cmd == "add-generic-password":
    with open(path, "w") as f:
        f.write(secret if secret is not None else "")
    sys.exit(0)
elif cmd == "delete-generic-password":
    if os.path.exists(path):
        os.remove(path)
        sys.exit(0)
    sys.stderr.write("could not be found\n")
    sys.exit(44)
else:
    sys.stderr.write("unknown cmd\n")
    sys.exit(2)
`
	if err := os.WriteFile(script, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_STORE_DIR", storeDir)

	oldBin := darwinSecurityBin
	darwinSecurityBin = script
	defer func() { darwinSecurityBin = oldBin }()

	s, ok := newPlatformStore(Options{})
	if !ok || s == nil {
		t.Fatal("native store not available")
	}
	if err := s.Set("svc", "acct", "secret123"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("svc", "acct")
	if err != nil || got != "secret123" {
		t.Fatalf("got %q err %v", got, err)
	}
	if err := s.Delete("svc", "acct"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("svc", "acct"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound got %v", err)
	}
	// missing delete
	if err := s.Delete("svc", "acct"); err != ErrNotFound {
		t.Fatalf("delete missing: expected ErrNotFound got %v", err)
	}
	_ = fmt.Sprintf
}
