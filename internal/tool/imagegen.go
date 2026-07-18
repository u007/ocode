package tool

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/paths"
)

// defaultImageGenTimeout is the built-in per-request timeout for image
// generation when no explicit timeout is configured or passed. It was raised
// from 10 minutes to 15 minutes because image generation (especially
// high-quality or multi-image requests) can take longer than the previous
// ceiling. The timeout is overridable per-call via the tool's `timeout`
// argument and globally via the persisted imagegen config `Timeout` field.
const defaultImageGenTimeout = 15 * time.Minute

// resolveImageGenTimeout converts a per-call timeout (seconds) into a
// time.Duration, falling back to defaultImageGenTimeout when unset or invalid.
func resolveImageGenTimeout(seconds int) time.Duration {
	if seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return defaultImageGenTimeout
}

// ImageGenTool generates images from a text prompt (and optionally an input
// image for editing) using a pluggable provider backend.
//
// Supported providers (selected via the `provider` argument):
//   - gemini   : Google Gemini image models (a.k.a. "Nano Banana"). Default
//     model is the latest Nano Banana release. Uses the Gemini
//     generateContent API and returns inline base64 image data.
//   - openai   : OpenAI image models (gpt-image-1, gpt-image-2, dall-e-3, ...).
//     Uses the OpenAI-compatible /images/generations endpoint.
//   - novita   : Novita AI open-weight image models (FLUX, SDXL, SD3, ...).
//     OpenAI-compatible.
//   - deepinfra: DeepInfra open-weight image models (FLUX Schnell by default).
//     OpenAI-compatible.
//   - <generic>: Any OpenAI-compatible image endpoint. Pass an explicit
//     `base_url` and `model`; the API key is resolved from the
//     provider id (or `api_key` argument).
//
// API keys are resolved through the same machinery as the rest of ocode:
// OPENCODE_AUTH_TOKEN > provider env var (e.g. GOOGLE_API_KEY) > opencode
// config provider.<id>.options.apiKey > stored credential in auth.json.
type ImageGenTool struct {
	Config *config.Config
}

// imgCostRecord is a single logged image-generation cost event.
type imgCostRecord struct {
	Timestamp time.Time `json:"t"`
	Provider  string    `json:"provider"`
	Model     string    `json:"model"`
	Count     int       `json:"count"`
	UnitPrice float64   `json:"unit_price"`
	Cost      float64   `json:"cost"`
	Size      string    `json:"size,omitempty"`
	Quality   string    `json:"quality,omitempty"`
}

// imgPerImagePriceUSD returns the representative list price (USD) for a single
// generated image of the given model. For OpenAI image models size/quality
// refine the price. Prices are editable constants and may drift from provider
// list pricing — update them as needed.
func imgPerImagePriceUSD(provider, model, size, quality string) float64 {
	switch strings.ToLower(provider) {
	case "openai":
		switch strings.ToLower(model) {
		case "dall-e-3":
			if strings.EqualFold(quality, "hd") {
				return 0.080
			}
			return 0.040
		case "dall-e-2":
			return 0.020
		default: // gpt-image-1 and friends: size/quality tiers
			switch strings.ToLower(quality) {
			case "high":
				return 0.083
			case "low":
				return 0.011
			default: // medium / auto / empty
				return 0.016
			}
		}
	case "gemini", "google":
		if strings.Contains(strings.ToLower(model), "pro") {
			return 0.10
		}
		return 0.039
	case "novita", "novita-ai":
		if strings.Contains(strings.ToLower(model), "flux") {
			return 0.012
		}
		return 0.040
	case "deepinfra":
		return 0.003
	default:
		return 0
	}
}

// logImageGenCost writes the cost event to a JSONL file under the global data
// dir and emits a debug log line. Writing to the log (not stdout) keeps the
// TUI alt-screen safe.
func logImageGenCost(rec imgCostRecord) {
	dir, err := paths.GlobalDataDir()
	if err != nil {
		log.Printf("[IMAGEGEN] cost not logged (no data dir): %v", err)
		return
	}
	path := filepath.Join(dir, "imagegen_costs.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("[IMAGEGEN] cost log write failed: %v", err)
		return
	}
	defer f.Close()
	line, err := json.Marshal(rec)
	if err != nil {
		log.Printf("[IMAGEGEN] cost log marshal failed: %v", err)
		return
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		log.Printf("[IMAGEGEN] cost log write failed: %v", err)
		return
	}
	log.Printf("[IMAGEGEN] cost=$%.4f provider=%s model=%s images=%d", rec.Cost, rec.Provider, rec.Model, rec.Count)
}

// recordCost computes the estimated generation cost, logs it to a file, and
// returns a display-only notice describing it. The notice travels with the
// return value (rather than being stored on the tool instance, which is
// shared across concurrently-running agents/sub-agents) so it can never be
// clobbered by or leaked to a concurrent call.
func (t *ImageGenTool) recordCost(p imgGenParams, model string, count int) string {
	if count <= 0 {
		return ""
	}
	unit := imgPerImagePriceUSD(p.Provider, model, p.Size, p.Quality)
	var notice string
	if unit > 0 {
		total := unit * float64(count)
		notice = fmt.Sprintf("Image generation cost: $%.4f (provider=%s model=%s images=%d @ $%.4f/image)",
			total, p.Provider, model, count, unit)
	} else {
		notice = fmt.Sprintf("Image generation cost: unknown (no price configured for provider=%s model=%s)",
			p.Provider, model)
	}
	logImageGenCost(imgCostRecord{
		Timestamp: time.Now(),
		Provider:  p.Provider,
		Model:     model,
		Count:     count,
		UnitPrice: unit,
		Cost:      unit * float64(count),
		Size:      p.Size,
		Quality:   p.Quality,
	})
	return notice
}

// imgProvider describes how to talk to a backend.
type imgProvider struct {
	// id is the auth provider id used for key/base-URL resolution
	// (see internal/auth/providers.go).
	id string
	// style is "gemini" or "openai".
	style string
	// defBaseURL is the default API base (no trailing slash).
	defBaseURL string
	// defModel is the default model when the caller does not specify one.
	defModel string
}

// imgRegistry maps a normalized provider alias to its descriptor.
var imgRegistry = map[string]imgProvider{
	"gemini":    {id: "google", style: "gemini", defBaseURL: "https://generativelanguage.googleapis.com", defModel: "gemini-3.1-flash-image"},
	"google":    {id: "google", style: "gemini", defBaseURL: "https://generativelanguage.googleapis.com", defModel: "gemini-3.1-flash-image"},
	"openai":    {id: "openai", style: "openai", defBaseURL: "https://api.openai.com/v1", defModel: "gpt-image-1"},
	"novita":    {id: "novita-ai", style: "openai", defBaseURL: "https://api.novita.ai/v3/openai", defModel: "dall-e-3"},
	"novita-ai": {id: "novita-ai", style: "openai", defBaseURL: "https://api.novita.ai/v3/openai", defModel: "dall-e-3"},
	"deepinfra": {id: "deepinfra", style: "openai", defBaseURL: "https://api.deepinfra.com/v1/openai", defModel: ""},
}

// normalizeProvider resolves a user-supplied provider alias to a descriptor.
// It also handles a generic OpenAI-compatible provider when an explicit
// base_url is supplied.
func normalizeProvider(provider string) (imgProvider, bool) {
	if p, ok := imgRegistry[strings.ToLower(strings.TrimSpace(provider))]; ok {
		return p, true
	}
	return imgProvider{}, false
}

func (t *ImageGenTool) Name() string { return "imagegen" }

func (t *ImageGenTool) Description() string {
	return "Generate images from a text prompt using an image-generation model. " +
		"Supports Google Gemini (Nano Banana, the default), OpenAI image models (gpt-image-1/gpt-image-2/dall-e-3), " +
		"and open-weight models hosted on Novita AI and DeepInfra (FLUX, SDXL, SD3, ...). " +
		"For Gemini you may also pass an input image to edit it with natural language. " +
		"Returns the saved image file path(s), format, and size. " +
		"Set the provider via `provider`; the API key is resolved from the environment, " +
		"opencode config, or the auth store (e.g. GOOGLE_API_KEY, OPENAI_API_KEY, NOVITA_API_KEY, DEEPINFRA_API_KEY)."
}

func (t *ImageGenTool) Parallel() bool { return false }

func (t *ImageGenTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "imagegen",
		"description": t.Description(),
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prompt": map[string]interface{}{
					"type":        "string",
					"description": "Text description of the image to generate (or the edit instruction when image_path is supplied).",
				},
				"provider": map[string]interface{}{
					"type":        "string",
					"description": "Backend provider. One of: gemini (default), openai, novita, deepinfra, or any OpenAI-compatible endpoint (pass an explicit base_url).",
					"enum":        []string{"gemini", "openai", "novita", "deepinfra"},
					"default":     "gemini",
				},
				"model": map[string]interface{}{
					"type":        "string",
					"description": "Model id. Defaults per provider: gemini -> gemini-3.1-flash-image (Nano Banana 2 / latest Nano Banana); openai -> gpt-image-1; novita -> dall-e-3; deepinfra -> server default (FLUX Schnell). Newer Gemini models (gemini-2.5-flash-image, gemini-3-pro-image) also work.",
				},
				"image_path": map[string]interface{}{
					"type":        "string",
					"description": "Optional input image (absolute or relative path) for editing. Supported natively by the gemini provider; ignored by OpenAI-compatible providers in this version.",
				},
				"size": map[string]interface{}{
					"type":        "string",
					"description": "Image size for OpenAI-compatible providers (e.g. \"1024x1024\", \"1792x1024\", \"1536x1024\"). Ignored by gemini.",
				},
				"aspect_ratio": map[string]interface{}{
					"type":        "string",
					"description": "Aspect ratio for the gemini provider (e.g. \"1:1\", \"16:9\", \"9:16\", \"3:4\", \"4:3\"). Ignored by OpenAI-compatible providers.",
				},
				"quality": map[string]interface{}{
					"type":        "string",
					"description": "Quality hint for OpenAI gpt-image models (e.g. \"standard\", \"hd\", \"auto\"). Optional.",
				},
				"style": map[string]interface{}{
					"type":        "string",
					"description": "Style hint for OpenAI gpt-image models (e.g. \"vivid\", \"natural\"). Optional.",
				},
				"n": map[string]interface{}{
					"type":        "integer",
					"description": "Number of images to generate (OpenAI-compatible providers only). Default 1.",
					"default":     1,
				},
				"output_path": map[string]interface{}{
					"type":        "string",
					"description": "Where to save the image(s). A directory, a file path, or omitted to use the working directory with an auto-generated name. For n>1 an index is appended.",
				},
				"base_url": map[string]interface{}{
					"type":        "string",
					"description": "Override the API base URL. Required for generic OpenAI-compatible providers; optional override for known providers.",
				},
				"api_key": map[string]interface{}{
					"type":        "string",
					"description": "Optional API key override. If omitted, resolved from environment, opencode config, or the auth store.",
				},
				"timeout": map[string]interface{}{
					"type":        "integer",
					"description": "Per-call request timeout in seconds. Overrides the configured imagegen timeout (default 900). Use a larger value for slow or high-quality generations.",
				},
			},
			"required": []string{"prompt"},
		},
	}
}

// imgGenParams is the decoded tool arguments.
type imgGenParams struct {
	Prompt      string `json:"prompt"`
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	ImagePath   string `json:"image_path"`
	Size        string `json:"size"`
	AspectRatio string `json:"aspect_ratio"`
	Quality     string `json:"quality"`
	Style       string `json:"style"`
	N           int    `json:"n"`
	OutputPath  string `json:"output_path"`
	BaseURL     string `json:"base_url"`
	APIKey      string `json:"api_key"`
	Timeout     int    `json:"timeout"`
}

// Execute performs the image generation and returns a JSON summary.
func (t *ImageGenTool) Execute(args json.RawMessage) (string, error) {
	var p imgGenParams
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(p.Prompt) == "" {
		return "", fmt.Errorf("prompt is required")
	}
	if p.N <= 0 {
		p.N = 1
	}
	// Respect the persisted enabled flag. A nil config (e.g. some unit-test
	// callers) is treated as enabled so the tool still works.
	if t.Config != nil && !t.Config.Ocode.ImageGen.Enabled {
		return "", &NoticedError{
			Err:    fmt.Errorf("image generation is disabled"),
			Notice: "Image generation is disabled. Use /image enable to turn it on.",
		}
	}

	// Apply persisted defaults (explicit tool args still win).
	if t.Config != nil {
		if p.Provider == "" {
			p.Provider = t.Config.Ocode.ImageGen.Provider
		}
		if p.Model == "" {
			p.Model = t.Config.Ocode.ImageGen.Model
		}
		if p.OutputPath == "" {
			p.OutputPath = t.Config.Ocode.ImageGen.OutputPath
		}
		if p.Timeout <= 0 && t.Config.Ocode.ImageGen.Timeout > 0 {
			p.Timeout = t.Config.Ocode.ImageGen.Timeout
		}
	}
	if p.Provider == "" {
		p.Provider = "gemini"
	}

	// Ensure stored credentials are loaded before resolving keys.
	_ = auth.LoadStore()

	prov, known := normalizeProvider(p.Provider)
	// For a generic OpenAI-compatible provider the caller must give a base_url.
	if !known {
		if strings.TrimSpace(p.BaseURL) == "" {
			return "", &NoticedError{
				Err:    fmt.Errorf("unknown provider %q and no base_url supplied", p.Provider),
				Notice: "Unknown provider. Use one of gemini, openai, novita, deepinfra, or supply an explicit base_url for any OpenAI-compatible image endpoint.",
			}
		}
		// Treat as OpenAI-compatible using the raw provider string as the auth id.
		prov = imgProvider{id: strings.ToLower(p.Provider), style: "openai", defBaseURL: strings.TrimRight(p.BaseURL, "/")}
	}

	// Resolve API key.
	apiKey := p.APIKey
	if apiKey == "" {
		apiKey = auth.ResolveKey(prov.id)
	}
	if apiKey == "" {
		return "", &NoticedError{
			Err:    fmt.Errorf("no API key for provider %q", p.Provider),
			Notice: fmt.Sprintf("No API key found for provider %q. Set %s (or OPENCODE_AUTH_TOKEN), configure provider.%s.options.apiKey, or pass api_key.", p.Provider, envVarForProvider(prov.id), prov.id),
		}
	}

	// Resolve base URL: explicit param > stored credential base URL > default.
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		if credBase := auth.GetBaseURL(prov.id); credBase != "" {
			baseURL = strings.TrimRight(credBase, "/")
		}
	}
	if baseURL == "" {
		baseURL = prov.defBaseURL
	}
	if baseURL == "" {
		return "", &NoticedError{
			Err:    fmt.Errorf("no base URL for provider %q", p.Provider),
			Notice: "No base URL available. Pass base_url explicitly for this provider.",
		}
	}

	// Resolve model.
	model := p.Model
	if model == "" {
		model = prov.defModel
	}

	ctx, cancel := context.WithTimeout(context.Background(), resolveImageGenTimeout(p.Timeout))
	defer cancel()

	var (
		images []generatedImage
		text   string
		err    error
	)
	switch prov.style {
	case "gemini":
		images, text, err = t.generateGemini(ctx, baseURL, apiKey, model, p)
	default:
		images, text, err = t.generateOpenAI(ctx, baseURL, apiKey, model, p)
	}
	if err != nil {
		return "", err
	}

	// Save images to disk.
	saved, err := t.saveImages(p, images)
	if err != nil {
		return "", err
	}

	// Compute, display, and log the estimated generation cost. This is
	// display-only: prefixed onto the result behind SuccessNoticeSeparator so
	// the agent can split it into Message.Notice (shown in the transcript,
	// never sent to the LLM) without the tool storing any per-call state on
	// itself. It is intentionally NOT in the JSON result.
	notice := t.recordCost(p, model, len(saved))

	summary := map[string]interface{}{
		"provider": p.Provider,
		"model":    model,
		"count":    len(saved),
		"images":   saved,
	}
	if text != "" {
		summary["text"] = text
	}
	out, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal result: %w", err)
	}
	if notice != "" {
		return notice + SuccessNoticeSeparator + string(out), nil
	}
	return string(out), nil
}

// generatedImage is a decoded image before it is written to disk.
type generatedImage struct {
	data     []byte
	mimeType string
	revised  string
}

// generateGemini calls the Gemini generateContent image API.
func (t *ImageGenTool) generateGemini(ctx context.Context, baseURL, apiKey, model string, p imgGenParams) ([]generatedImage, string, error) {
	parts := []map[string]interface{}{
		{"text": p.Prompt},
	}
	if p.ImagePath != "" {
		raw, mime, err := readImageFile(p.ImagePath)
		if err != nil {
			return nil, "", err
		}
		parts = append(parts, map[string]interface{}{
			"inline_data": map[string]interface{}{
				"mime_type": mime,
				"data":      base64.StdEncoding.EncodeToString(raw),
			},
		})
	}

	genConfig := map[string]interface{}{
		"responseModalities": []string{"IMAGE", "TEXT"},
	}
	if p.AspectRatio != "" {
		genConfig["imageConfig"] = map[string]interface{}{
			"aspectRatio": p.AspectRatio,
		}
	}

	body := map[string]interface{}{
		"contents": []map[string]interface{}{
			{"parts": parts},
		},
		"generationConfig": genConfig,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, "", fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent", baseURL, model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("gemini request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("gemini API error (%d): %s", resp.StatusCode, truncateErr(respBody))
	}

	return parseGeminiResponse(respBody)
}

// parseGeminiResponse extracts inline image data and text from a generateContent response.
func parseGeminiResponse(body []byte) ([]generatedImage, string, error) {
	var parsed struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text       string `json:"text"`
					InlineData struct {
						MimeType string `json:"mimeType"`
						Data     string `json:"data"`
					} `json:"inlineData"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, "", fmt.Errorf("parse gemini response: %w", err)
	}
	if parsed.Error.Message != "" {
		return nil, "", fmt.Errorf("gemini API error: %s", parsed.Error.Message)
	}

	var imgs []generatedImage
	var texts []string
	for _, c := range parsed.Candidates {
		for _, part := range c.Content.Parts {
			if part.InlineData.Data != "" {
				data, derr := base64.StdEncoding.DecodeString(part.InlineData.Data)
				if derr != nil {
					return nil, "", fmt.Errorf("decode gemini image: %w", derr)
				}
				imgs = append(imgs, generatedImage{data: data, mimeType: part.InlineData.MimeType})
			}
			if part.Text != "" {
				texts = append(texts, part.Text)
			}
		}
	}
	if len(imgs) == 0 {
		return nil, "", fmt.Errorf("gemini returned no image (text only: %q)", strings.Join(texts, " "))
	}
	return imgs, strings.Join(texts, "\n"), nil
}

// generateOpenAI calls an OpenAI-compatible /images/generations endpoint.
func (t *ImageGenTool) generateOpenAI(ctx context.Context, baseURL, apiKey, model string, p imgGenParams) ([]generatedImage, string, error) {
	body := map[string]interface{}{
		"prompt":          p.Prompt,
		"response_format": "b64_json",
		"n":               p.N,
	}
	if model != "" {
		body["model"] = model
	}
	if p.Size != "" {
		body["size"] = p.Size
	}
	if p.Quality != "" {
		body["quality"] = p.Quality
	}
	if p.Style != "" {
		body["style"] = p.Style
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, "", fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/images/generations", baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("image API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("image API error (%d): %s", resp.StatusCode, truncateErr(respBody))
	}

	return parseOpenAIResponse(respBody)
}

// parseOpenAIResponse extracts base64 images and revised prompts.
func parseOpenAIResponse(body []byte) ([]generatedImage, string, error) {
	var parsed struct {
		Data []struct {
			B64JSON       string `json:"b64_json"`
			URL           string `json:"url"`
			RevisedPrompt string `json:"revised_prompt"`
		} `json:"data"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, "", fmt.Errorf("parse image response: %w", err)
	}
	if parsed.Error.Message != "" {
		return nil, "", fmt.Errorf("image API error: %s", parsed.Error.Message)
	}

	var imgs []generatedImage
	var revised []string
	for _, d := range parsed.Data {
		if d.B64JSON != "" {
			data, derr := base64.StdEncoding.DecodeString(d.B64JSON)
			if derr != nil {
				return nil, "", fmt.Errorf("decode image: %w", derr)
			}
			imgs = append(imgs, generatedImage{data: data, mimeType: "image/png", revised: d.RevisedPrompt})
			if d.RevisedPrompt != "" {
				revised = append(revised, d.RevisedPrompt)
			}
			continue
		}
		if d.URL != "" {
			// Some providers return a URL instead of base64; fetch it.
			data, ferr := fetchBytes(d.URL)
			if ferr != nil {
				return nil, "", fmt.Errorf("download image from url: %w", ferr)
			}
			imgs = append(imgs, generatedImage{data: data, mimeType: sniffImageMIME(data), revised: d.RevisedPrompt})
		}
	}
	if len(imgs) == 0 {
		return nil, "", fmt.Errorf("image API returned no images")
	}
	return imgs, strings.Join(revised, "\n"), nil
}

// savedImage is the on-disk result reported back to the caller.
type savedImage struct {
	Path   string `json:"path"`
	Format string `json:"format"`
	Bytes  int    `json:"bytes"`
}

// saveImages writes the generated images to disk and returns their metadata.
func (t *ImageGenTool) saveImages(p imgGenParams, imgs []generatedImage) ([]savedImage, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("working directory: %w", err)
	}

	base := p.OutputPath
	if base == "" {
		base = filepath.Join(wd, fmt.Sprintf("%s_%s", strings.ReplaceAll(p.Provider, "-", "_"), time.Now().Format("20060102_150405")))
	} else {
		// Confine the output path to the working directory / allowed roots.
		confined, cerr := confinedPath(base)
		if cerr != nil {
			// Fall back to treating it as a name under the working directory.
			base = filepath.Join(wd, filepath.Base(base))
		} else {
			base = confined
		}
	}

	info, statErr := os.Stat(base)
	isDir := statErr == nil && info.IsDir()

	var saved []savedImage
	for i, img := range imgs {
		ext := extForMIME(img.mimeType)
		var path string
		switch {
		case isDir:
			path = filepath.Join(base, fmt.Sprintf("%s_%d.%s", defaultBaseName(p), i+1, ext))
		case len(imgs) > 1:
			path = insertIndex(base, i+1, ext)
		default:
			path = withExt(base, ext)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create output dir: %w", err)
		}
		if err := os.WriteFile(path, img.data, 0o644); err != nil {
			return nil, fmt.Errorf("write image %s: %w", path, err)
		}
		saved = append(saved, savedImage{Path: path, Format: ext, Bytes: len(img.data)})
	}
	return saved, nil
}

// ---- helpers ----

var extRe = regexp.MustCompile(`\.[a-zA-Z0-9]+$`)

func extForMIME(mime string) string {
	switch strings.ToLower(mime) {
	case "image/jpeg", "image/jpg":
		return "jpg"
	case "image/webp":
		return "webp"
	case "image/gif":
		return "gif"
	case "image/png":
		return "png"
	default:
		return "png"
	}
}

func withExt(path, ext string) string {
	if extRe.MatchString(path) {
		return path
	}
	return path + "." + ext
}

// insertIndex inserts an index before the extension, e.g.
// out.png + i=2 -> out_2.png.
func insertIndex(path string, i int, ext string) string {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	if m := extRe.FindString(base); m != "" {
		base = strings.TrimSuffix(base, m)
	}
	return filepath.Join(dir, fmt.Sprintf("%s_%d.%s", base, i, ext))
}

func defaultBaseName(p imgGenParams) string {
	model := p.Model
	if model == "" {
		model = p.Provider
	}
	return strings.ReplaceAll(model, "/", "_")
}

func readImageFile(path string) ([]byte, string, error) {
	safe, err := confinedPath(path)
	if err != nil {
		return nil, "", fmt.Errorf("invalid image_path: %w", err)
	}
	raw, err := os.ReadFile(safe)
	if err != nil {
		return nil, "", fmt.Errorf("read image_path: %w", err)
	}
	mime := sniffImageMIME(raw)
	if mime == "" {
		mime = "image/png"
	}
	return raw, mime, nil
}

func fetchBytes(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed (%d)", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func truncateErr(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) > 500 {
		s = s[:500] + "..."
	}
	return s
}

// envVarForProvider returns the conventional env var name for a provider id,
// used only for user-facing notices.
func envVarForProvider(id string) string {
	switch id {
	case "google":
		return "GOOGLE_API_KEY"
	case "openai":
		return "OPENAI_API_KEY"
	case "novita-ai":
		return "NOVITA_API_KEY"
	case "deepinfra":
		return "DEEPINFRA_API_KEY"
	default:
		return strings.ToUpper(id) + "_API_KEY"
	}
}
