
## Updated: License → Pin → Download → Enable State Machine (v2)

### Per-Engine Install Flow
State machine for each local engine (Piper, Kokoro, Fish Audio, Breeze):

```
not-accepted → license-accepted → pinned → downloading → installed → enabled
```

- `not-accepted`: engine unavailable + license summary shown; user must open engine card.
- `license-accepted`: user clicked "Accept License" for exact engine + license text/hash; persisted.
- `pinned`: manifest pinned; download lock reserved.
- `downloading`: progress shown; retry available on failure.
- `installed`: checksum verified; atomically installed.
- `enabled`: supervisor selects engine; playback available.

### Design Rules
- Browser Native requires no license/pin/download/install.
- Selecting unavailable engine does NOT trigger download; explicit state progression required.
- License acceptance is per-engine/license-text-hash; manifest/license changes require re-acceptance.
- Download uses global advisory lock; progress via `GET /api/tts/status` and optional progress endpoint.
- Enable (`selectEngine`) rejects if not `installed`; no silent Browser Native fallback.
- Settings UI shows per-engine cards: license prompt, accept, pin status, download/progress, retry, enable.
