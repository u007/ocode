# OCR Backend Architecture Design

**Date:** 2026-07-03
**Status:** Draft

## Problem

The current OCR tool (`internal/tool/ocr.go`) is hardcoded to LM Studio's
`/v1/chat/completions` endpoint. The `/ocr model` picker prints a plain text
list in the chat instead of using the interactive picker. The model filter
(`OcrModelsFromLMStudio`) only matches a narrow set of keywords and has no
concept of multiple OCR backends.

## Goals

1. Support multiple OCR backends: `openai-compat` (LM Studio, vLLM, llama.cpp)
   and `paddle` (PaddleOCR native REST API).
2. `/ocr model` opens an interactive picker (same infrastructure as `/model`)
   showing backend categories with their available models.
3. Better keyword filtering for OCR/vision models.
4. Web UI parity: CoworkSidebar gets a backend/model selector.
5. Backward-compatible config migration from flat `OcrModel`/`OcrEnabled` to
   structured `OcrConfig`.

## Architecture

### Package layout

```
internal/
  ocr/
    backend.go           # OcrBackend interface + registry
    openai_compat.go     # openai-compat backend
    paddle.go            # PaddleOCR backend
    config.go            # OcrConfig types (shared with config package)
  config/
    ocodeconfig.go       # imports ocr.OcrConfig, removes OcrModel/OcrEnabled
  tool/
    ocr.go               # thin adapter: resolves backend from config, delegates
  agent/
    models_registry.go   # OcrModelsFromLMStudio stays, used by openai-compat
  tui/
    picker.go            # new pickerKind "ocr-model"
    model.go             # /ocr command uses picker
  server/
    server.go            # new API endpoints: /api/ocr/config, /api/ocr/models
```

### OcrBackend interface

```go
package ocr

type OcrBackend interface {
    // Name returns the backend identifier ("openai-compat", "paddle").
    Name() string

    // Execute performs OCR on an image file and returns extracted text.
    Execute(ctx context.Context, imagePath string) (string, error)

    // ListModels returns available model IDs for this backend.
    // May query a remote API or return a static list.
    // Returns nil if the backend cannot enumerate models.
    ListModels(ctx context.Context) ([]string, error)
}
```

`Execute` takes an image path (not raw bytes) so backends can re-read the file
for streaming upload or direct filesystem access. The tool layer resolves the
path before calling Execute.

### Config

```go
package ocr

type OcrConfig struct {
    Enabled bool          `json:"enabled"`
    Backend string        `json:"backend"`          // "openai-compat" | "paddle"
    OpenAI  OpenAICfg     `json:"openai,omitempty"`
    Paddle  PaddleCfg     `json:"paddle,omitempty"`
}

type OpenAICfg struct {
    BaseURL string `json:"base_url"` // default: http://localhost:1234/v1
    Model   string `json:"model"`
}

type PaddleCfg struct {
    Endpoint string `json:"endpoint"` // e.g. http://localhost:8100/ocr
    Variant  string `json:"variant"`  // "standard" | "vl"
}
```

In `OcodeConfig`:
```go
Ocr ocr.OcrConfig `json:"ocr"`
```

### Backend registration

Backends register themselves at init time:

```go
var backends map[string]OcrBackend

func Register(b OcrBackend) { backends[b.Name()] = b }
func Get(name string) OcrBackend { return backends[name] }
func List() []string { names... }
```

## Backend Implementations

### openai-compat

- **ListModels**: Queries `{BaseURL}/v1/models`, filters by OCR/vision keywords.
  Same logic as current `OcrModelsFromLMStudio` but accepts a base URL parameter.
- **Execute**: Sends image via `/v1/chat/completions` with `image_url` content
  (same as current LM Studio code, extracted into backend).
- **Keywords expanded to**: `ocr`, `paddle`, `deepseek`, `vision`, `caption`,
  `moondream`, `qwen.*vl`, `florence`, `cogvlm`, `pixtral`, `paligemma`,
  `minicpm`, `internvl`, `llava`, `clip`, `phi.*vision`, `gemma.*vision`.

### paddle

- **ListModels**: Returns static list `["standard", "vl"]`.
- **Execute**: Sends image via `POST {Endpoint}` as multipart form data.
  Variant `"standard"` → PaddleOCR pipeline (detection + recognition).
  Variant `"vl"` → PaddleOCR-VL model.
- **Note**: This assumes a running PaddleOCR REST API server (e.g.
  `paddleocr-pdf-api` Docker container). Not an LM Studio model.

## TUI Picker

### New picker kind: "ocr-model"

The existing `openModelPicker()` is designed for cloud-provider model selection
and is not reused. Instead, `openOcrModelPicker()` builds the picker directly:

1. Sets `pickerKind = "ocr-model"`.
2. Queries each registered backend's `ListModels()` (async).
3. Builds picker items:
   ```
   › openai-compat  (section header)
     deepseek-ocr
     llama-3.2-11b-vision
     ...
   › paddle  (section header)
     standard
     vl
   ```

### selectPickerIndex branch

Adds to `selectPickerIndex`:
```go
if kind == "ocr-model" {
    parts := strings.SplitN(selected, "/", 2)
    if len(parts) == 2 {
        // "openai-compat/deepseek-ocr" → backend + model
        backend = parts[0]
        model = parts[1]
    } else {
        // paddle/variant or bare model name
        backend = m.config.Ocode.Ocr.Backend
        model = selected
    }
    return m.handleCommand(fmt.Sprintf("/ocr model %s/%s", backend, model))
}
```

### Picker title and hints

```go
if m.pickerKind == "ocr-model" {
    title = "Select OCR backend + model"
}
```

## Config Save

Replace `SaveOcrEnabled(bool)` and `SaveOcrModel(string)` with:

```go
func SaveOcrConfig(cfg ocr.OcrConfig) error
```

Implementation: load existing config, replace `Ocr` sub-tree, write back.
Must NOT snapshot the full in-memory config (prevents concurrent write erasure
of other fields).

## Web API

### New endpoints

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/api/ocr/config` | Returns full `OcrConfig` JSON |
| `PUT` | `/api/ocr/config` | Updates `OcrConfig` (partial merge on server) |
| `GET` | `/api/ocr/models` | Returns `{ backends: { name: ..., models: [...] } }` |

### Deprecated endpoints (keep for backward compat, remove in next major)

- `GET /api/config/ocr-enabled` → kept, reads from new config
- `PUT /api/config/ocr-enabled` → kept, writes to new config
- `PUT /api/config/ocr-model` → kept, writes to new config (sets current backend)

### Web UI (CoworkSidebar)

The OCR section UI:
1. Shows current backend + model name
2. Click opens a small modal with:
   - Backend tabs/selector (`openai-compat` | `paddle`)
   - Model list fetched from `GET /api/ocr/models`
   - Base URL / Endpoint URL input field
3. Saves via `PUT /api/ocr/config`

The chat `/ocr` command (`commands.ts`) gets backend support:
```
/ocr model openai-compat/deepseek-ocr
/ocr model paddle/vl
```

## Migration

Existing configs have `ocr_model` and `ocr_enabled` at the top level. On load:

```go
if cfg.OcrModel != "" || cfg.OcrEnabled {
    // Migrate to new structure
    cfg.Ocr = ocr.OcrConfig{
        Enabled: cfg.OcrEnabled,
        Backend: "openai-compat",
        OpenAI:  ocr.OpenAICfg{Model: cfg.OcrModel},
    }
    cfg.OcrModel = ""
    // ocr_enabled remains at old location too; cfg.Ocr.Enabled is source of truth
}
```

## Files Changed

| File | Change |
|------|--------|
| `internal/ocr/backend.go` | New — `OcrBackend` interface + registry |
| `internal/ocr/openai_compat.go` | New — openai-compat backend |
| `internal/ocr/paddle.go` | New — PaddleOCR backend |
| `internal/ocr/config.go` | New — `OcrConfig`, `OpenAICfg`, `PaddleCfg` types |
| `internal/config/ocodeconfig.go` | Replace `OcrModel`/`OcrEnabled` with `Ocr ocr.OcrConfig` |
| `internal/config/config.go` | Add `SaveOcrConfig()` helper |
| `internal/tool/ocr.go` | Refactor to use backend registry + config |
| `internal/tui/picker.go` | Add `pickerKind = "ocr-model"` support |
| `internal/tui/model.go` | Update `/ocr` command handlers |
| `internal/server/server.go` | Add new OCR API endpoints |
| `internal/agent/models_registry.go` | Refine `OcrModelsFromLMStudio` keywords |
| `web/src/api/client.ts` | Add new OCR API methods |
| `web/src/api/types.ts` | Add OCR config types |
| `web/src/stores/chatStore.tsx` | Add backend to OCR state |
| `web/src/components/Layout/CoworkSidebar.tsx` | Add backend/model selector modal |
| `web/src/components/Chat/commands.ts` | Update `/ocr model` handler for backends |

## Future Work (not in scope)

- Adding more backends (e.g., `tesseract` for local Tesseract OCR).
- Auto-detecting PaddleOCR server health in the picker.
- Streaming OCR output for very large documents.
