package ocr

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Local vision models can take minutes: cold model load (~16s observed) plus
// large-image inference (~36s warm for a phone-photo receipt).
const openaiTimeout = 600 * time.Second
const maxImageBytes = 20 * 1024 * 1024 // 20 MB

type openaiCompatBackend struct{}

func init() {
	Register(&openaiCompatBackend{})
}

func (b *openaiCompatBackend) Name() string { return "openai-compat" }

func (b *openaiCompatBackend) Execute(ctx context.Context, imagePath string, cfg BackendConfig) (string, error) {
	// Read image file
	imageData, err := os.ReadFile(imagePath)
	if err != nil {
		return "", fmt.Errorf("cannot read image file %q: %w", imagePath, err)
	}
	if len(imageData) > maxImageBytes {
		return "", fmt.Errorf("image too large (%d bytes, max %d)", len(imageData), maxImageBytes)
	}

	// Detect content type from extension
	contentType := detectContentType(imagePath)

	// Base64 encode
	b64Data := base64.StdEncoding.EncodeToString(imageData)
	dataURL := fmt.Sprintf("data:%s;base64,%s", contentType, b64Data)

	// Build the API URL
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:1234/v1"
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/v1"
	}

	model := cfg.Model
	if model == "" {
		return "", fmt.Errorf("no OCR model configured")
	}
	if strings.HasPrefix(model, "lmstudio/") {
		model = strings.TrimPrefix(model, "lmstudio/")
	}

	if cfg.LMStudioNative || LooksLikeLMStudioBaseURL(baseURL) {
		return b.executeLMStudioNative(ctx, imagePath, baseURL, model, cfg.APIKey)
	}

	apiURL := baseURL + "/chat/completions"

	payload := map[string]interface{}{
		"model": model,
		"messages": []map[string]interface{}{
			{
				"role": "system",
				"content": "You are an OCR engine. Extract all text from the image exactly as seen. " +
					"Return only the extracted text, no commentary. Preserve the original formatting, " +
					"line breaks, and structure as closely as possible.",
			},
			{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type": "image_url",
						"image_url": map[string]string{
							"url": dataURL,
						},
					},
					{
						"type": "text",
						"text": "Extract all text from this image.",
					},
				},
			},
		},
		"max_tokens":  4096,
		"temperature": 0.0,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}

	client := &http.Client{Timeout: openaiTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024))
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("API returned no choices")
	}

	text := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	if text == "" {
		return "No text was extracted from the image.", nil
	}
	return text, nil
}

func (b *openaiCompatBackend) executeLMStudioNative(ctx context.Context, imagePath, baseURL, model, apiKey string) (string, error) {
	imageData, err := os.ReadFile(imagePath)
	if err != nil {
		return "", fmt.Errorf("cannot read image file %q: %w", imagePath, err)
	}
	if len(imageData) > maxImageBytes {
		return "", fmt.Errorf("image too large (%d bytes, max %d)", len(imageData), maxImageBytes)
	}

	contentType := detectContentType(imagePath)
	b64Data := base64.StdEncoding.EncodeToString(imageData)
	dataURL := fmt.Sprintf("data:%s;base64,%s", contentType, b64Data)

	baseURL = normalizeLMStudioBaseURL(baseURL)
	apiURL := baseURL + "/chat"

	// The instruction rides in the text input item instead of system_prompt:
	// some vision-model jinja templates (e.g. paddleocr-vl) iterate over
	// message content parts and fail to render LM Studio's string-typed
	// system message ("Expected iterable or object type in for loop").
	payload := map[string]interface{}{
		"model": model,
		"input": []map[string]interface{}{
			{
				"type": "text",
				"content": "You are an OCR engine. Extract all text from the image exactly as seen. " +
					"Return only the extracted text, no commentary. Preserve the original formatting, " +
					"line breaks, and structure as closely as possible.\n\n" +
					"Extract all text from this image.",
			},
			{
				"type":     "image",
				"data_url": dataURL,
			},
		},
		"context_length": 4096,
		"temperature":    0.0,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: openaiTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024))
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp struct {
		Output []struct {
			Type    string `json:"type"`
			Content string `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	var parts []string
	for _, item := range chatResp.Output {
		if item.Type == "message" && strings.TrimSpace(item.Content) != "" {
			parts = append(parts, item.Content)
		}
	}
	text := strings.TrimSpace(strings.Join(parts, "\n"))
	if text == "" {
		return "", fmt.Errorf("API returned no output text")
	}
	return text, nil
}

func normalizeLMStudioBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return "http://localhost:1234/api/v1"
	}
	switch {
	case strings.HasSuffix(baseURL, "/api/v1"):
		return baseURL
	case strings.HasSuffix(baseURL, "/v1"):
		return strings.TrimSuffix(baseURL, "/v1") + "/api/v1"
	default:
		return baseURL + "/api/v1"
	}
}

func (b *openaiCompatBackend) ListModels(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	if baseURL == "" {
		baseURL = "http://localhost:1234/v1"
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/v1"
	}

	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned HTTP %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	all := make([]string, 0, len(result.Data))
	for _, m := range result.Data {
		if m.ID != "" {
			all = append(all, m.ID)
		}
	}

	// Filter for OCR/vision models by keyword matching
	var filtered []string
	for _, m := range all {
		lower := strings.ToLower(m)
		if ocrKeywordMatch(lower) {
			filtered = append(filtered, m)
		}
	}

	// If no matches, return all models so the user can still pick one.
	if len(filtered) == 0 {
		return all, nil
	}
	return filtered, nil
}

// ocrKeywordMatch returns true if the lower-cased string matches known
// OCR or vision model name patterns.
func ocrKeywordMatch(lower string) bool {
	keywords := []string{
		"ocr", "paddle", "deepseek", "vision", "caption",
		"moondream", "florence", "cogvlm", "pixtral", "paligemma",
		"minicpm", "internvl", "llava", "clip", "phi",
		"gemma", "qwen",
	}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	// Check for "vl" suffix/prefix patterns (e.g. "qwen2-vl", "internvl")
	if strings.Contains(lower, "vl") {
		return true
	}
	// Check for "vlm" (vision language model)
	if strings.Contains(lower, "vlm") {
		return true
	}
	return false
}

func detectContentType(path string) string {
	ext := strings.ToLower(fileExt(path))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	default:
		return "image/png"
	}
}

func fileExt(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '.' {
			return path[i:]
		}
		if path[i] == '/' || path[i] == '\\' {
			break
		}
	}
	return ""
}
