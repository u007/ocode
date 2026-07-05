package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/ocr"
)

// OcrTool performs OCR on an image by delegating to the configured
// OCR backend (openai-compat, paddle, etc.).
type OcrTool struct {
	Config *config.Config
}

func (t *OcrTool) Name() string { return "ocr" }

func (t *OcrTool) Description() string {
	return "Extract text from an image using the configured OCR backend. " +
		"Provide an image_path to an image file. Supports multiple backends: " +
		"openai-compat (LM Studio, vLLM) and paddle (PaddleOCR native API)."
}

func (t *OcrTool) Parallel() bool { return false }

func (t *OcrTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "ocr",
		"description": "Extract text from an image file using the configured OCR backend. Provide image_path pointing to a screenshot or photo. The tool reads the file, sends it to the OCR backend, and returns the extracted text.",
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
	if t.Config == nil || !t.Config.Ocode.Ocr.Enabled {
		return "", &NoticedError{
			Err:    fmt.Errorf("OCR is not enabled"),
			Notice: "OCR is not enabled. Use /ocr enable to turn it on, then set an OCR model with /ocr model <name>.",
		}
	}

	ocrCfg := t.Config.Ocode.Ocr
	be := ocr.Get(ocrCfg.Backend)
	if be == nil {
		be = ocr.Get("openai-compat")
	}
	if be == nil {
		return "", &NoticedError{
			Err:    fmt.Errorf("no OCR backend available"),
			Notice: "No OCR backend available. Check your configuration.",
		}
	}

	// Build BackendConfig from the active backend's config sub-tree
	var beCfg ocr.BackendConfig
	switch ocrCfg.Backend {
	case "paddle":
		beCfg.BaseURL = ocrCfg.Paddle.Endpoint
		beCfg.Model = ocrCfg.Paddle.Variant
	default:
		beCfg.BaseURL = ocrCfg.OpenAI.BaseURL
		beCfg.Model = ocrCfg.OpenAI.Model
		beCfg.APIKey = auth.ResolveOpenAICompatKey(
			ocrCfg.OpenAI.APIKey,
			beCfg.BaseURL,
			ocrCfg.Backend == "lmstudio" || ocr.LooksLikeLMStudioBaseURL(beCfg.BaseURL),
		)
		beCfg.LMStudioNative = ocrCfg.Backend == "lmstudio" || ocr.LooksLikeLMStudioBaseURL(beCfg.BaseURL)
	}
	if beCfg.Model == "" {
		return "", &NoticedError{
			Err:    fmt.Errorf("no OCR model configured"),
			Notice: "No OCR model configured. Use /ocr model <name> to select a model.",
		}
	}

	// Check file exists before delegating
	if _, err := os.Stat(params.ImagePath); err != nil {
		return "", fmt.Errorf("image file not found: %w", err)
	}

	text, err := be.Execute(context.Background(), params.ImagePath, beCfg)
	if err != nil {
		return "", fmt.Errorf("OCR failed: %w", err)
	}
	return text, nil
}
