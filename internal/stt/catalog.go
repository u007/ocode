// Package stt is the speech-to-text boundary shared by the TUI, the web UI and
// the desktop shell (which embeds the web UI).
//
// Two engines are offered, mirroring how Handy (github.com/cjpais/Handy)
// exposes its model list: local models that run on this machine, and hosted
// models that need a provider key. Local models are advertised as ready only
// when the host has the runtime they need; the catalog never guesses.
package stt

import (
	"errors"
)

// Engine says where a model runs.
type Engine string

const (
	// EngineLocal runs an ONNX model on this machine through onnx-asr
	// (Python). The model weights download from Hugging Face on first use.
	EngineLocal Engine = "local"
	// EngineOpenAI calls the OpenAI audio transcription endpoint.
	EngineOpenAI Engine = "openai"
)

// DefaultModel is used when the config names no model or a stale one.
const DefaultModel = "parakeet-tdt-0.6b-v3"

// spec is one catalog entry. Upstream is the onnx-asr model name for local
// engines and the API model name for hosted ones.
type spec struct {
	ID       string
	Label    string
	Engine   Engine
	Upstream string
	// Repo is the Hugging Face repo holding the ONNX export (local engines).
	Repo        string
	Languages   string
	SizeMB      int
	Description string
}

var catalog = []spec{
	{
		ID:          "parakeet-tdt-0.6b-v3",
		Label:       "Parakeet TDT 0.6B v3",
		Engine:      EngineLocal,
		Upstream:    "nemo-parakeet-tdt-0.6b-v3",
		Repo:        "istupakov/parakeet-tdt-0.6b-v3-onnx",
		Languages:   "25 European languages",
		SizeMB:      478,
		Description: "Offline and fast on CPU. Recommended default.",
	},
	{
		ID:          "parakeet-tdt-0.6b-v2",
		Label:       "Parakeet TDT 0.6B v2",
		Engine:      EngineLocal,
		Upstream:    "nemo-parakeet-tdt-0.6b-v2",
		Repo:        "istupakov/parakeet-tdt-0.6b-v2-onnx",
		Languages:   "English",
		SizeMB:      473,
		Description: "Offline English-only model. Slightly better English accuracy than v3.",
	},
	{
		ID:          "gpt-4o-transcribe",
		Label:       "OpenAI gpt-4o-transcribe",
		Engine:      EngineOpenAI,
		Upstream:    "gpt-4o-transcribe",
		Languages:   "Many languages",
		Description: "Hosted by OpenAI. Needs an OpenAI API key; no local install.",
	},
	{
		ID:          "whisper-1",
		Label:       "OpenAI Whisper",
		Engine:      EngineOpenAI,
		Upstream:    "whisper-1",
		Languages:   "99+ languages",
		Description: "Hosted by OpenAI. Needs an OpenAI API key; no local install.",
	},
}

// ModelInfo is one catalog row as the UI sees it, with availability resolved
// for the current host.
type ModelInfo struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Engine      Engine `json:"engine"`
	Languages   string `json:"languages"`
	SizeMB      int    `json:"size_mb,omitempty"`
	Description string `json:"description"`
	Available   bool   `json:"available"`
	Reason      string `json:"reason,omitempty"`
}

// lookup returns the catalog entry for id.
func lookup(id string) (spec, bool) {
	for _, s := range catalog {
		if s.ID == id {
			return s, true
		}
	}
	return spec{}, false
}

// ValidModel reports whether id names a catalog entry.
func ValidModel(id string) bool {
	_, ok := lookup(id)
	return ok
}

// ResolveModel maps a configured id to a catalog id, falling back to the
// default for an empty or unknown value so a stale config never breaks voice.
func ResolveModel(id string) string {
	if ValidModel(id) {
		return id
	}
	return DefaultModel
}

// ErrUnknownModel is returned when a caller names a model outside the catalog.
var ErrUnknownModel = errors.New("unknown speech-to-text model")
