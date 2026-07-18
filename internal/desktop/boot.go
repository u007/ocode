// Package desktop provides the server boot helper and run-state watcher for
// the ocode desktop shell. It is pure Go and MUST NOT import Wails, keeping
// unit tests cgo-free and the boundary clean.
package desktop

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log"

	"github.com/u007/ocode/internal/server"
)

// Handle is the result of a successful server boot.
type Handle struct {
	URL   string // e.g. "http://127.0.0.1:52341" (no trailing slash)
	Token string // hex-encoded 16-byte random token (32 hex chars)
	Srv   *server.Server
}

// StartServer boots an ocode HTTP/SSE API server on 127.0.0.1:0 (random port)
// with a fresh auth token, and returns the handle the desktop shell needs to
// open its webview window. The server runs in a background goroutine and dies
// with the process by design — internal/server has no graceful-shutdown API
// and window close = app quit = process exit (see the desktop shell spec).
//
// webFS is the embedded SPA (web.FS()). workDir is the project root the
// server resolves relative paths from.
func StartServer(webFS fs.FS, workDir string) (*Handle, error) {
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("desktop: generate token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)

	srv := server.New("127.0.0.1:0", "ocode", token, webFS)
	srv.SetWorkDir(workDir)

	ln, err := srv.Listen()
	if err != nil {
		return nil, fmt.Errorf("desktop: listen: %w", err)
	}

	// Read the actual bound address (Listen writes the *requested* address
	// back to s.addr, so we must use ln.Addr()).
	addr := ln.Addr().String()
	url := fmt.Sprintf("http://%s", addr)

	go func() {
		log.Printf("desktop: serving on %s", url)
		if err := srv.Serve(ln); err != nil {
			log.Printf("desktop: serve error: %v", err)
		}
	}()

	return &Handle{
		URL:   url,
		Token: token,
		Srv:   srv,
	}, nil
}
