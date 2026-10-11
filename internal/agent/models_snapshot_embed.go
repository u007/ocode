//go:build models

// Optional offline model-registry snapshot. The file is produced by
// `make models-snapshot` (which fetches models.dev) and is deliberately NOT
// committed — it is a regenerable, ~3 MB build input, so tracking it would bloat
// every clone for data that goes stale within days.
//
// It is embedded only under the `models` tag. A source build without the tag
// compiles against models_snapshot_stub.go and serves no embedded snapshot; the
// registry then resolves from the runtime cache and a live models.dev fetch,
// which is what it does anyway whenever the snapshot's generated_at stamp is
// older than modelsCacheTTL. Release targets that want offline model metadata
// pass the tag and depend on `make models-snapshot`.

package agent

import _ "embed"

//go:embed models-snapshot.json
var modelsSnapshotData []byte
