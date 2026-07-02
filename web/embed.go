// Package web embeds the built SPA (dist/) so any binary in this module can
// serve it. The embed lives here because go:embed cannot reach ../web/dist
// from a second main package (e.g. cmd/ocode-desktop).
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the built SPA rooted so index.html is at the FS root, or nil
// when the dist directory is missing (e.g. web build not yet run).
func FS() fs.FS {
	f, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil
	}
	return f
}
