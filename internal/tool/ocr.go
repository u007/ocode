package tool

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/u007/ocode/internal/config"
)

const ocrHTTPTimeout = 60 * time.Second
const maxImageBytes = 20 * 1024 * 1024 // 20 MB

var ocrHTTPClient = &http.Client{
	Timeout: ocrHTTPTimeout,
}

// OcrTool performs OCR on an image by sending it to LM Studio's
// OpenAI-compatible /v1/chat/completions endpoint using the configured
// OCR vision model.
type OcrTool struct {
	Config *config.Config
}

func (t *OcrTool) Name() string { return "ocr" }

func (t *OcrTool) Description() string {
	return "Extract text from an image using the configured OCR model (LM Studio). " +
		"Provide an image_path to an image file. The image is sent to LM Studio " +
		"and the extracted text is returned."
}

func (t *OcrTool) Parallel() bool { return false }

func (t *OcrTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "ocr",
		"description": "Extract text from an image file using OCR (LM Studio vision model). Provide image_path pointing to a screenshot or photo. The tool reads the file, sends it to the OCR model, and returns the extracted text.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"image_path": map[string]interface{}{
					"type":        "string",
					"description": "Absolute or relative path to the image file to OCR (PNG, JPG, etc.)",
				},
			},
			"required": []string{"image_path"},
		},
	}
}

func (t *OcrTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		ImagePath string `json:"image_path"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if params.ImagePath == "" {
		return "", fmt.Errorf("image_path is required")
	}

	// Check if OCR is enabled
	if t.Config == nil || !t.Config.Ocode.OcrEnabled {
		return "", &NoticedError{
			Err:    fmt.Errorf("OCR is not enabled"),
			Notice: "OCR is not enabled. Use /ocr enable to turn it on, then set an OCR model with /ocr model <name>.",
		}
	}

	model := t.Config.Ocode.OcrModel
	if model == "" {
		return "", &NoticedError{
			Err:    fmt.Errorf("no OCR model configured"),
			Notice: "No OCR model configured. Use /ocr model <name> to select a model from LM Studio.",
		}
	}

	// Read the image file
	imageData, err := os.ReadFile(params.ImagePath)
	if err != nil {
		return "", fmt.Errorf("cannot read image file %q: %w", params.ImagePath, err)
	}
	if len(imageData) > maxImageBytes {
		return "", fmt.Errorf("image too large (%d bytes, max %d)", len(imageData), maxImageBytes)
	}

	// Detect content type from extension
	ext := strings.ToLower(fileExt(params.ImagePath))
	contentType := "image/png"
	switch ext {
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".gif":
		contentType = "image/gif"
	case ".webp":
		contentType = "image/webp"
	case ".bmp":
		contentType = "image/bmp"
	}

	// Base64 encode the image
	b64Data := base64.StdEncoding.EncodeToString(imageData)
	dataURL := fmt.Sprintf("data:%s;base64,%s", contentType, b64Data)

	// Build the LM Studio /v1/chat/completions request
	baseURL := os.Getenv("LMSTUDIO_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:1234/v1"
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/v1"
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
		"max_tokens": 4096,
		"temperature": 0.0,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, apiURL, strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ocrHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("LM Studio request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024))
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("LM Studio returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", fmt.Errorf("failed to parse LM Studio response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("LM Studio returned no choices")
	}

	text := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	if text == "" {
		return "No text was extracted from the image.", nil
	}
	return text, nil
}

// fileExt returns the file extension (lowercase, including the dot).
func fileExt(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '.' {
			return strings.ToLower(path[i:])
		}
		if path[i] == '/' || path[i] == '\\' {
			break
		}
	}
	return ""
}
