package agent

import (
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/ocr"
	"github.com/u007/ocode/internal/tool"
)

func TestGetToolDefinitions_OCRSilentWhenDisabled(t *testing.T) {
	// Capture global debug sink.
	var got []string
	old := DebugAppend
	DebugAppend = func(kind, msg string) { got = append(got, kind+": "+msg) }
	defer func() { DebugAppend = old }()

	cfg := &config.Config{Ocode: config.OcodeConfig{Ocr: ocr.OcrConfig{Enabled: false}}}
	// Agent without ocr in a.tools (e.g. an explore sub-agent).
	a := NewAgent(nil, nil, cfg, nil)
	// Ensure ocr is absent to trigger the "missing from a.tools" branch.
	delete(a.tools, "ocr")

	got = nil
	_ = a.GetToolDefinitions()
	for _, m := range got {
		if strings.Contains(m, "ocr not exposed") {
			t.Fatalf("expected no ocr diagnostic when disabled, got %q (all=%v)", m, got)
		}
	}
}

func TestGetToolDefinitions_OCREmitsWhenEnabledAndMissing(t *testing.T) {
	var got []string
	old := DebugAppend
	DebugAppend = func(kind, msg string) { got = append(got, kind+": "+msg) }
	defer func() { DebugAppend = old }()

	cfg := &config.Config{Ocode: config.OcodeConfig{Ocr: ocr.OcrConfig{Enabled: true}}}
	a := NewAgent(nil, nil, cfg, nil)
	delete(a.tools, "ocr")

	got = nil
	_ = a.GetToolDefinitions()
	found := false
	for _, m := range got {
		if strings.Contains(m, "ocr not exposed: missing from a.tools") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected ocr missing diagnostic when enabled, got %v", got)
	}
}

func TestGetToolDefinitions_OCREmitsWhenEnabledButFiltered(t *testing.T) {
	var got []string
	old := DebugAppend
	DebugAppend = func(kind, msg string) { got = append(got, kind+": "+msg) }
	defer func() { DebugAppend = old }()

	cfg := &config.Config{Ocode: config.OcodeConfig{Ocr: ocr.OcrConfig{Enabled: true}}}
	tools := []tool.Tool{&tool.OcrTool{Config: cfg}, &tool.ReadTool{}}
	a := NewAgent(nil, tools, cfg, nil)
	// Deny ocr via spec so it exists but is not exposed.
	a.spec = &AgentSpec{DeniedTools: []string{"ocr"}}

	got = nil
	_ = a.GetToolDefinitions()
	found := false
	for _, m := range got {
		if strings.Contains(m, "ocr not exposed:") && strings.Contains(m, "exists=true") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected filtered ocr diagnostic when enabled, got %v", got)
	}
}
