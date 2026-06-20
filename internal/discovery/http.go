package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"
)

// HTTPModel describes a curated embeddings model. Endpoint is the full URL of an
// OpenAI-compatible /v1/embeddings route. EnvVar names the API key variable.
type HTTPModel struct {
	ID        string
	Provider  string
	Endpoint  string
	EnvVar    string
	Dimension int
}

// HTTPModels is the curated list. NOT sourced from the models.dev chat registry
// (which has no embedding models). Keep sorted by ID.
var HTTPModels = func() []HTTPModel {
	m := []HTTPModel{
		{ID: "openai/text-embedding-3-small", Provider: "openai", Endpoint: "https://api.openai.com/v1/embeddings", EnvVar: "OPENAI_API_KEY", Dimension: 1536},
		{ID: "openai/text-embedding-3-large", Provider: "openai", Endpoint: "https://api.openai.com/v1/embeddings", EnvVar: "OPENAI_API_KEY", Dimension: 3072},
		{ID: "voyage/voyage-3", Provider: "voyage", Endpoint: "https://api.voyageai.com/v1/embeddings", EnvVar: "VOYAGE_API_KEY", Dimension: 1024},
		{ID: "voyage/voyage-3-lite", Provider: "voyage", Endpoint: "https://api.voyageai.com/v1/embeddings", EnvVar: "VOYAGE_API_KEY", Dimension: 512},
	}
	sort.Slice(m, func(i, j int) bool { return m[i].ID < m[j].ID })
	return m
}()

// HTTPModelByID looks up a curated model.
func HTTPModelByID(id string) (HTTPModel, bool) {
	for _, m := range HTTPModels {
		if m.ID == id {
			return m, true
		}
	}
	return HTTPModel{}, false
}

type httpEmbedder struct {
	model  HTTPModel
	apiKey string
	client *http.Client
}

// NewHTTPEmbedder builds an OpenAI-compatible embeddings client.
func NewHTTPEmbedder(m HTTPModel, apiKey string) *httpEmbedder {
	return &httpEmbedder{model: m, apiKey: apiKey, client: &http.Client{Timeout: 20 * time.Second}}
}

func (e *httpEmbedder) ID() string { return e.model.ID }
func (e *httpEmbedder) Dim() int   { return e.model.Dimension }

func (e *httpEmbedder) Embed(ctx context.Context, texts []string, _ EmbedKind) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	// model field carries the bare model name after the provider/ prefix.
	bareModel := e.model.ID
	if i := indexByte(bareModel, '/'); i >= 0 {
		bareModel = bareModel[i+1:]
	}
	body, _ := json.Marshal(map[string]interface{}{"model": bareModel, "input": texts})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.model.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build embeddings request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embeddings request to %s: %w", e.model.Endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embeddings %s returned status %d", e.model.ID, resp.StatusCode)
	}
	var parsed struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode embeddings response: %w", err)
	}
	out := make([][]float32, len(parsed.Data))
	for i, d := range parsed.Data {
		out[i] = d.Embedding
	}
	return out, nil
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
