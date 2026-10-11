//go:build !models

// Source-build stub for the optional offline model-registry snapshot. Without
// the `models` tag the JSON is never embedded, so modelsSnapshotData is empty.
//
// That is the same state a shipped build is in whenever the embedded snapshot's
// generated_at stamp is older than modelsCacheTTL: loadRegistry already skips a
// stale snapshot, falls through to a live models.dev fetch, and on failure uses
// the on-disk cache before this empty snapshot. An empty slice is therefore
// handled, not a silent success — loadFromSnapshot reports ok=false and
// loadRegistry falls through. See internal/pricing/modelsdev.go, which keeps its
// own hardcoded fallback map precisely because the embedded snapshot may be
// absent or stale. Keep this an exact opposite of models_snapshot_embed.go.

package agent

// modelsSnapshotData is empty in builds without the `models` tag.
var modelsSnapshotData []byte
