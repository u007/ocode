// Package ocr provides the OCR backend abstraction for the ocode OCR tool.
//
// Multiple backends can be registered at init time. The OCR tool resolves
// the active backend from config at runtime and delegates Execute() and
// ListModels() to it.
package ocr

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// OcrBackend is the interface that every OCR backend must implement.
type OcrBackend interface {
	// Name returns the backend identifier ("openai-compat", "paddle").
	Name() string

	// Execute performs OCR on the image at imagePath and returns
	// the extracted text. Backends re-read the file so they can
	// stream it or access it directly from disk.
	Execute(ctx context.Context, imagePath string, cfg BackendConfig) (string, error)

	// ListModels returns available model identifiers for this
	// backend. May query a remote API or return a static list.
	// apiKey is sent as a Bearer token when the endpoint enforces auth
	// (backends that don't need it ignore the argument). Returns nil if
	// the backend cannot enumerate models (callers should show a
	// manual-entry prompt in that case).
	ListModels(ctx context.Context, baseURL, apiKey string) ([]string, error)
}

// BackendConfig holds the runtime configuration passed to Execute.
// The backend reads the fields relevant to it.
type BackendConfig struct {
	BaseURL string // API base URL (default varies by backend)
	Model   string // model ID, variant, or endpoint path
	APIKey  string // Bearer token; empty means no auth header
	// LMStudioNative switches the openai-compat backend over to LM Studio's
	// native /api/v1/chat endpoint, which avoids the OpenAI chat template path
	// that can fail on some vision models.
	LMStudioNative bool
}

var (
	mu       sync.RWMutex
	backends = map[string]OcrBackend{}
)

// Register registers an OCR backend. Called from init() in each backend
// implementation file. Panics if a backend with the same name is registered
// more than once.
func Register(b OcrBackend) {
	mu.Lock()
	defer mu.Unlock()
	name := b.Name()
	if _, ok := backends[name]; ok {
		panic(fmt.Sprintf("ocr: backend %q already registered", name))
	}
	backends[name] = b
}

// Get returns the backend with the given name, or nil if not registered.
func Get(name string) OcrBackend {
	mu.RLock()
	defer mu.RUnlock()
	return backends[name]
}

// List returns the names of all registered backends.
func List() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(backends))
	for name := range backends {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
