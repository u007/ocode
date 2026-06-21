package tui

import (
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/discovery"
)

// TestEmbeddingPickerUsesManifestModelID locks in the contract that the
// "Local (downloaded on first use)" entry in the embedding-model picker shows
// the model id from the current platform's discovery manifest — not a
// hardcoded string that would silently drift from the manifest when artifacts
// are bumped (e.g. when the model is swapped from LFM2.5-Embedding to bge-m3).
func TestEmbeddingPickerUsesManifestModelID(t *testing.T) {
	man, ok := discovery.CurrentManifest()
	if !ok {
		t.Skip("no local manifest for this platform; the picker should hide the section entirely")
	}

	m := model{}
	m.config = &config.Config{}
	m.config.Ocode.Discovery.LocalModelStatus = "ready"

	m.openEmbeddingModelPicker()

	if !m.showPicker || m.pickerKind != "embedding-model" {
		t.Fatalf("expected embedding-model picker to be open, got showPicker=%v kind=%q",
			m.showPicker, m.pickerKind)
	}

	// Find the entry whose value equals the manifest's ModelID. The label
	// may include a status suffix like "(ready)" — match on the value
	// (the part that /discover model will set), not the label.
	var found bool
	for i, v := range m.pickerValues {
		if v == "" {
			continue // header row
		}
		if v == man.ModelID {
			found = true
			if !strings.Contains(m.pickerItems[i], man.ModelID) {
				t.Fatalf("local model label %q must contain id %q", m.pickerItems[i], man.ModelID)
			}
			if !strings.Contains(m.pickerItems[i], "(ready)") {
				t.Fatalf("expected label to include the LocalModelStatus suffix, got %q", m.pickerItems[i])
			}
		}
	}
	if !found {
		t.Fatalf("local model id %q not found in picker; items=%v values=%v",
			man.ModelID, m.pickerItems, m.pickerValues)
	}
}

// TestEmbeddingPickerIncludesHTTPModels verifies the HTTP section still shows
// the curated OpenAI/Voyage models alongside the local section.
func TestEmbeddingPickerIncludesHTTPModels(t *testing.T) {
	m := model{}
	m.config = &config.Config{}

	m.openEmbeddingModelPicker()

	if !m.showPicker {
		t.Fatal("expected picker to be open")
	}
	// The HTTP section is hardcoded with at least one curated model in
	// internal/discovery.HTTPModels. Verify a known one shows up.
	const want = "openai/text-embedding-3-small"
	var found bool
	for _, v := range m.pickerValues {
		if v == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected %q in HTTP section; values=%v", want, m.pickerValues)
	}
}
