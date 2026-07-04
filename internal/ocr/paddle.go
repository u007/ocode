package ocr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const paddleTimeout = 120 * time.Second

type paddleBackend struct{}

func init() {
	Register(&paddleBackend{})
}

func (b *paddleBackend) Name() string { return "paddle" }

func (b *paddleBackend) Execute(ctx context.Context, imagePath string, cfg BackendConfig) (string, error) {
	endpoint := cfg.BaseURL
	if endpoint == "" {
		endpoint = "http://localhost:8100/ocr"
	}
	endpoint = strings.TrimRight(endpoint, "/")

	variant := cfg.Model
	if variant == "" {
		variant = "standard"
	}

	// Open the image file for multipart upload
	file, err := os.Open(imagePath)
	if err != nil {
		return "", fmt.Errorf("cannot open image %q: %w", imagePath, err)
	}
	defer file.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	// Add the image file to the multipart form
	part, err := w.CreateFormFile("file", filepath.Base(imagePath))
	if err != nil {
		return "", fmt.Errorf("multipart create form file: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", fmt.Errorf("multipart copy file: %w", err)
	}

	// Add variant field
	if err := w.WriteField("variant", variant); err != nil {
		return "", fmt.Errorf("multipart write variant: %w", err)
	}
	w.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &buf)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	client := &http.Client{Timeout: paddleTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("PaddleOCR request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("PaddleOCR returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	// PaddleOCR REST APIs typically return JSON with a "text" field or array
	// of recognized text lines. Try to extract structured text.
	var result struct {
		Text string `json:"text"`
	}
	if err := jsonUnmarshalStrict(respBody, &result); err == nil && result.Text != "" {
		return strings.TrimSpace(result.Text), nil
	}

	// Fallback: return raw response as text
	return strings.TrimSpace(string(respBody)), nil
}

func (b *paddleBackend) ListModels(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	// PaddleOCR has no model enumeration API and no auth; return static variants.
	return []string{"standard", "vl"}, nil
}

// jsonUnmarshalStrict unmarshals data into v with strict field checking.
func jsonUnmarshalStrict(data []byte, v interface{}) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	return d.Decode(v)
}
