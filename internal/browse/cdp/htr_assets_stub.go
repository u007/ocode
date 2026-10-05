//go:build !htr

// Source-build stub for the optional HTR bundle. Without the `htr` tag the
// archive is never embedded, so embeddedHTRArchive stays nil.
//
// Consumers treat a nil archive as "this build has no bundled HTR assets":
// openEmbeddedHTRArchive reports an invalid bundle, and ResolveHTRAssetsForHost
// then returns empty asset paths so the caller surfaces its documented fallback
// notice rather than launching a daemon that cannot exist. See
// docs/concepts/ for the HTR bundle lifecycle. Keep this declaration an exact
// opposite of htr_assets_embed.go.

package cdp

// embeddedHTRArchive is empty in builds without the `htr` tag.
var embeddedHTRArchive []byte
