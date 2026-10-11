//go:build htr

// HTR release assets. The archive is produced by `make prepare-htr-assets`
// from the out-of-tree HTR extension/htrcli sources, so it is NOT committed
// (see .gitignore). Only builds that pass the `htr` tag — the install,
// desktop, and remote-binary targets in the Makefile — embed it.
//
// A source build without the tag compiles against htr_assets_stub.go, which
// leaves the archive empty; every HTR runtime path then reports the optional
// bundle as unavailable instead of failing to build.

package cdp

import _ "embed"

// The Makefile replaces this archive with the unpacked extension and the
// platform-matched htrcli binaries before install/desktop builds.
//
//go:embed htr-assets.zip
var embeddedHTRArchive []byte
