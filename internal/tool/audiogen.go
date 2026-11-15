package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/u007/ocode/internal/config"
)

const (
	// defaultAudioGenModel is the current Gemini TTS model.
	defaultAudioGenModel   = "gemini-3.8-flash-tts"
	defaultAudioGenVoice   = "Kore"
	defaultAudioGenTimeout = 5 * time.Minute
)

// AudioGenTool turns text into speech with Gemini TTS, single-speaker or
// multi-speaker, and saves the result as a WAV file.
type AudioGenTool struct {
	Config *config.Config
}

func (t *AudioGenTool) Name() string { return "audiogen" }

func (t *AudioGenTool) Description() string {
	return "Generate speech audio (WAV) from text using Gemini TTS. " +
		"Single-speaker: pass text plus an optional voice and style. " +
		"Multi-speaker: pass turns (speaker, text, style) plus speakers (name, voice). " +
		"Returns the saved audio file path and size. " +
		"Configure defaults with /audio; the Gemini API key is resolved like imagegen " +
		"(GOOGLE_API_KEY, opencode config, or the auth store)."
}

func (t *AudioGenTool) Parallel() bool { return false }

func (t *AudioGenTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "audiogen",
		"description": t.Description(),
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"text": map[string]interface{}{
					"type":        "string",
					"description": "Verbatim transcript to speak (single-speaker). Required unless turns is supplied.",
				},
				"voice": map[string]interface{}{
					"type":        "string",
					"description": "Prebuilt voice name for single-speaker output (e.g. Kore, Puck). Default Kore.",
				},
				"style": map[string]interface{}{
					"type":        "string",
					"description": "Optional delivery style for single-speaker output (e.g. \"cheerful and friendly\", \"whispered urgently\").",
				},
				"turns": map[string]interface{}{
					"type":        "array",
					"description": "Multi-speaker dialogue turns. Each turn names one of the speakers. When set, text and voice are ignored.",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"speaker": map[string]interface{}{"type": "string", "description": "Speaker name, matching an entry in speakers."},
							"text":    map[string]interface{}{"type": "string", "description": "Verbatim words for this turn."},
							"style":   map[string]interface{}{"type": "string", "description": "Optional delivery style for this turn."},
						},
						"required": []string{"speaker", "text"},
					},
				},
				"speakers": map[string]interface{}{
					"type":        "array",
					"description": "Multi-speaker voice assignments. Required when turns is supplied.",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"name":  map[string]interface{}{"type": "string", "description": "Speaker name used by turns."},
							"voice": map[string]interface{}{"type": "string", "description": "Prebuilt voice name for this speaker."},
						},
						"required": []string{"name", "voice"},
					},
				},
				"model": map[string]interface{}{
					"type":        "string",
					"description": "TTS model id. Default gemini-3.8-flash-tts.",
				},
				"output_path": map[string]interface{}{
					"type":        "string",
					"description": "Where to save the audio. A directory, a file path, or omitted to use the working directory with an auto-generated name.",
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
					"description": "Per-call request timeout in seconds. Overrides the configured audiogen timeout (default 300).",
				},
			},
			"required": []string{},
		},
	}
}

// audioTurn is one multi-speaker dialogue turn.
type audioTurn struct {
	Speaker string `json:"speaker"`
	Text    string `json:"text"`
	Style   string `json:"style"`
}

// audioSpeaker assigns a voice to one speaker name.
type audioSpeaker struct {
	Name  string `json:"name"`
	Voice string `json:"voice"`
}

// audioGenParams is the decoded tool arguments.
type audioGenParams struct {
	Text       string         `json:"text"`
	Voice      string         `json:"voice"`
	Style      string         `json:"style"`
	Turns      []audioTurn    `json:"turns"`
	Speakers   []audioSpeaker `json:"speakers"`
	Model      string         `json:"model"`
	OutputPath string         `json:"output_path"`
	BaseURL    string         `json:"base_url"`
	APIKey     string         `json:"api_key"`
	Timeout    int            `json:"timeout"`
}

func (t *AudioGenTool) Execute(args json.RawMessage) (string, error) {
	var p audioGenParams
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if t.Config != nil && !t.Config.Ocode.AudioGen.Enabled {
		return "", &NoticedError{
			Err:    fmt.Errorf("audio generation is disabled"),
			Notice: "Audio generation is disabled. Use /audio enable to turn it on.",
		}
	}

	// Apply persisted defaults (explicit tool args still win).
	if t.Config != nil {
		cfg := t.Config.Ocode.AudioGen
		if p.Model == "" {
			p.Model = cfg.Model
		}
		if p.OutputPath == "" {
			p.OutputPath = cfg.OutputPath
		}
		if p.Timeout <= 0 && cfg.Timeout > 0 {
			p.Timeout = cfg.Timeout
		}
		if p.Voice == "" {
			p.Voice = cfg.Voice
		}
	}
	if p.Model == "" {
		p.Model = defaultAudioGenModel
	}
	if p.Voice == "" {
		p.Voice = defaultAudioGenVoice
	}

	input, speechConfig, spokenTurns, err := buildAudioInteractionInput(p)
	if err != nil {
		return "", err
	}

	apiKey, baseURL, err := geminiGenerativeCredentials(p.APIKey, p.BaseURL)
	if err != nil {
		return "", err
	}

	timeout := defaultAudioGenTimeout
	if p.Timeout > 0 {
		timeout = time.Duration(p.Timeout) * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	body := map[string]any{
		"model":           p.Model,
		"input":           input,
		"response_format": map[string]any{"type": "audio"},
		"generation_config": map[string]any{
			"speech_config": speechConfig,
		},
	}
	parts, status, err := callGeminiInteraction(ctx, baseURL, apiKey, body)
	if err != nil {
		return "", err
	}
	data, mime, err := lastInteractionData(parts, "audio")
	if err != nil {
		return "", err
	}
	if mime == "" {
		mime = "audio/wav"
	}
	defaultName := fmt.Sprintf("audiogen_%s", time.Now().Format("20060102_150405"))
	saved, err := saveMediaFile(ctx, p.OutputPath, defaultName, "wav", data)
	if err != nil {
		return "", err
	}

	summary := map[string]any{
		"provider":  "gemini",
		"model":     p.Model,
		"status":    status,
		"path":      saved,
		"format":    "wav",
		"mime_type": mime,
		"bytes":     len(data),
	}
	if len(p.Turns) > 0 {
		summary["speakers"] = spokenTurns
	} else {
		summary["voice"] = p.Voice
		summary["style"] = p.Style
	}
	out, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal result: %w", err)
	}
	return string(out), nil
}

// buildAudioInteractionInput validates the single/multi-speaker arguments and
// returns the Interactions API input, the speech_config value, and (for
// multi-speaker output) the sorted speaker names.
func buildAudioInteractionInput(p audioGenParams) (input any, speechConfig any, speakerNames []string, err error) {
	if len(p.Turns) == 0 {
		if strings.TrimSpace(p.Text) == "" {
			return nil, nil, nil, fmt.Errorf("text is required for single-speaker audio (or supply turns and speakers)")
		}
		textPart := map[string]any{"type": "text", "text": p.Text}
		if p.Style != "" {
			textPart["annotations"] = []any{map[string]any{"type": "speech_metadata", "style": p.Style}}
		}
		return []any{map[string]any{
			"type":    "user_input",
			"content": []any{textPart},
		}}, []any{map[string]any{"voice": p.Voice}}, nil, nil
	}

	if len(p.Speakers) == 0 {
		return nil, nil, nil, fmt.Errorf("speakers is required when turns is supplied")
	}
	voiceByName := make(map[string]string, len(p.Speakers))
	for _, sp := range p.Speakers {
		name := strings.TrimSpace(sp.Name)
		if name == "" || strings.TrimSpace(sp.Voice) == "" {
			return nil, nil, nil, fmt.Errorf("each speaker needs a name and a voice")
		}
		voiceByName[name] = sp.Voice
	}

	content := make([]any, 0, len(p.Turns))
	seen := make(map[string]bool, len(voiceByName))
	for _, turn := range p.Turns {
		text := strings.TrimSpace(turn.Text)
		if text == "" {
			return nil, nil, nil, fmt.Errorf("turn for speaker %q has no text", turn.Speaker)
		}
		if turn.Speaker == "" {
			return nil, nil, nil, fmt.Errorf("each turn needs a speaker")
		}
		if _, ok := voiceByName[turn.Speaker]; !ok {
			return nil, nil, nil, fmt.Errorf("turn speaker %q has no matching entry in speakers", turn.Speaker)
		}
		annotations := map[string]any{"type": "speech_metadata", "speaker": turn.Speaker}
		if turn.Style != "" {
			annotations["style"] = turn.Style
		}
		content = append(content, map[string]any{
			"type":        "text",
			"text":        text,
			"annotations": []any{annotations},
		})
		seen[turn.Speaker] = true
	}
	for name := range voiceByName {
		if !seen[name] {
			return nil, nil, nil, fmt.Errorf("speaker %q is configured but never used by any turn", name)
		}
	}

	names := make([]string, 0, len(voiceByName))
	for name := range voiceByName {
		names = append(names, name)
	}
	sort.Strings(names)
	speakers := make([]any, 0, len(names))
	for _, name := range names {
		speakers = append(speakers, map[string]any{"speaker": name, "voice": voiceByName[name]})
	}

	return []any{map[string]any{
		"type":    "user_input",
		"content": content,
	}}, map[string]any{"speakers": speakers}, names, nil
}
