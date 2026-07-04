package ocr

// OcrConfig holds the persisted configuration for the OCR tool.
type OcrConfig struct {
	// Enabled toggles the OCR tool on/off.
	Enabled bool `json:"enabled"`
	// Backend selects the active OCR backend ("openai-compat" | "paddle" | "lmstudio").
	Backend string `json:"backend"`
	// OpenAI holds config for the openai-compat backend.
	OpenAI OpenAICfg `json:"openai,omitempty"`
	// Paddle holds config for the paddle backend.
	Paddle PaddleCfg `json:"paddle,omitempty"`
}

// OpenAICfg configures the openai-compat backend (LM Studio, vLLM, llama.cpp).
type OpenAICfg struct {
	// BaseURL is the API base URL (e.g. http://localhost:1234/v1).
	BaseURL string `json:"base_url"`
	// Model is the model ID used for OCR (e.g. "deepseek-ocr").
	Model string `json:"model"`
	// APIKey is the Bearer token sent to the endpoint. Required when the
	// server enforces auth (e.g. LM Studio with "Require API token" on).
	APIKey string `json:"api_key,omitempty"`
}

// PaddleCfg configures the PaddleOCR native REST API backend.
type PaddleCfg struct {
	// Endpoint is the PaddleOCR API URL (e.g. http://localhost:8100/ocr).
	Endpoint string `json:"endpoint"`
	// Variant selects the PaddleOCR pipeline: "standard" | "vl".
	Variant string `json:"variant"`
}

// DefaultOcrConfig returns a sensible default OCR configuration.
func DefaultOcrConfig() OcrConfig {
	return OcrConfig{
		Enabled: false,
		Backend: "openai-compat",
		OpenAI: OpenAICfg{
			BaseURL: "http://localhost:1234/v1",
		},
		Paddle: PaddleCfg{
			Endpoint: "http://localhost:8100/ocr",
			Variant:  "standard",
		},
	}
}
