package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/u007/ocode/internal/config"
)

const (
	// defaultVideoGenModel is the current Gemini video model. Veo 3.1 Preview
	// was shut down in the Gemini API on 2026-10-22; video generation now runs
	// through Gemini Omni Flash on the Interactions API.
	defaultVideoGenModel   = "gemini-omni-1.2-flash"
	defaultVideoGenTimeout = 15 * time.Minute
)

// VideoGenTool generates a short video from a text prompt using Gemini Omni
// Flash (the Interactions API).
type VideoGenTool struct {
	Config *config.Config
}

func (t *VideoGenTool) Name() string { return "videogen" }

func (t *VideoGenTool) Description() string {
	return "Generate a short video (3-15 seconds, native audio) from a text prompt using Gemini Omni Flash. " +
		"Returns the saved video file path, format and size. " +
		"Configure defaults with /video; the Gemini API key is resolved like imagegen " +
		"(GOOGLE_API_KEY, opencode config, or the auth store)."
}

func (t *VideoGenTool) Parallel() bool { return false }

func (t *VideoGenTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "videogen",
		"description": t.Description(),
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prompt": map[string]interface{}{
					"type":        "string",
					"description": "Text description of the video: scene, camera movement, lighting and mood.",
				},
				"model": map[string]interface{}{
					"type":        "string",
					"description": "Video model id. Default gemini-omni-1.2-flash.",
				},
				"aspect_ratio": map[string]interface{}{
					"type":        "string",
					"description": "Output aspect ratio: \"16:9\" (default, landscape) or \"9:16\" (portrait).",
					"enum":        []string{"16:9", "9:16"},
				},
				"resolution": map[string]interface{}{
					"type":        "string",
					"description": "Output resolution: 360p, 720p (default), 1080p, or 4k (upscaled).",
					"enum":        []string{"360p", "720p", "1080p", "4k"},
				},
				"output_path": map[string]interface{}{
					"type":        "string",
					"description": "Where to save the video. A directory, a file path, or omitted to use the working directory with an auto-generated name.",
				},
				"base_url": map[string]interface{}{
					"type":        "string",
					"description": "Override the Gemini API base URL (default https://generativelanguage.googleapis.com).",
				},
				"api_key": map[string]interface{}{
					"type":        "string",
					"description": "Optional API key override. If omitted, resolved from the environment, opencode config, or the auth store.",
				},
				"timeout": map[string]interface{}{
					"type":        "integer",
					"description": "Per-call request timeout in seconds. Overrides the configured videogen timeout (default 900).",
				},
			},
			"required": []string{"prompt"},
		},
	}
}

// videoGenParams is the decoded tool arguments.
type videoGenParams struct {
	Prompt      string `json:"prompt"`
	Model       string `json:"model"`
	AspectRatio string `json:"aspect_ratio"`
	Resolution  string `json:"resolution"`
	OutputPath  string `json:"output_path"`
	BaseURL     string `json:"base_url"`
	APIKey      string `json:"api_key"`
	Timeout     int    `json:"timeout"`
}

func (t *VideoGenTool) Execute(args json.RawMessage) (string, error) {
	var p videoGenParams
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(p.Prompt) == "" {
		return "", fmt.Errorf("prompt is required")
	}
	if t.Config != nil && !t.Config.Ocode.VideoGen.Enabled {
		return "", &NoticedError{
			Err:    fmt.Errorf("video generation is disabled"),
			Notice: "Video generation is disabled. Use /video enable to turn it on.",
		}
	}

	// Apply persisted defaults (explicit tool args still win).
	if t.Config != nil {
		cfg := t.Config.Ocode.VideoGen
		if p.Model == "" {
			p.Model = cfg.Model
		}
		if p.OutputPath == "" {
			p.OutputPath = cfg.OutputPath
		}
		if p.Timeout <= 0 && cfg.Timeout > 0 {
			p.Timeout = cfg.Timeout
		}
		if p.AspectRatio == "" {
			p.AspectRatio = cfg.AspectRatio
		}
		if p.Resolution == "" {
			p.Resolution = cfg.Resolution
		}
	}
	if p.Model == "" {
		p.Model = defaultVideoGenModel
	}
	if p.AspectRatio == "" {
		p.AspectRatio = "16:9"
	}
	if p.Resolution == "" {
		p.Resolution = "720p"
	}
	switch p.AspectRatio {
	case "16:9", "9:16":
	default:
		return "", fmt.Errorf("invalid aspect_ratio %q (use \"16:9\" or \"9:16\")", p.AspectRatio)
	}
	switch p.Resolution {
	case "360p", "720p", "1080p", "4k":
	default:
		return "", fmt.Errorf("invalid resolution %q (use 360p, 720p, 1080p, or 4k)", p.Resolution)
	}

	apiKey, baseURL, err := geminiGenerativeCredentials(p.APIKey, p.BaseURL)
	if err != nil {
		return "", err
	}

	timeout := defaultVideoGenTimeout
	if p.Timeout > 0 {
		timeout = time.Duration(p.Timeout) * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	body := map[string]any{
		"model": p.Model,
		"input": p.Prompt,
		"response_format": map[string]any{
			"type":         "video",
			"aspect_ratio": p.AspectRatio,
			"resolution":   p.Resolution,
		},
	}
	parts, status, err := callGeminiInteraction(ctx, baseURL, apiKey, body)
	if err != nil {
		return "", err
	}
	data, mime, err := lastInteractionData(parts, "video")
	if err != nil {
		return "", err
	}
	if mime == "" {
		mime = "video/mp4"
	}
	defaultName := fmt.Sprintf("videogen_%s", time.Now().Format("20060102_150405"))
	saved, err := saveMediaFile(ctx, p.OutputPath, defaultName, "mp4", data)
	if err != nil {
		return "", err
	}

	summary := map[string]any{
		"provider":     "gemini",
		"model":        p.Model,
		"status":       status,
		"path":         saved,
		"format":       "mp4",
		"mime_type":    mime,
		"bytes":        len(data),
		"aspect_ratio": p.AspectRatio,
		"resolution":   p.Resolution,
	}
	out, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal result: %w", err)
	}
	return string(out), nil
}
