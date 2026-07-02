package agent

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"testing"
)

func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// The default provider is Anthropic; a tool result carrying an image must
// serialize as a tool_result whose content is a block array with an image
// source, and the media_type must be a provider-supported format.
func TestBuildAnthropicMessages_ToolImageBlock(t *testing.T) {
	c := &GenericClient{Provider: "anthropic", Model: "claude-opus-4-8"}
	msgs := []Message{
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Type: "function"}}},
		{Role: "tool", ToolID: "c1", Content: "[image file: pic.png — shown below]",
			Images: []Image{{MIMEType: "image/png", Data: "AAAA"}}},
	}
	out, err := c.buildAnthropicMessages(msgs)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	// Last message is the tool result (mapped to role=user).
	last := out[len(out)-1]
	content, ok := last["content"].([]interface{})
	if !ok || len(content) != 1 {
		t.Fatalf("expected one tool_result block, got %T %v", last["content"], last["content"])
	}
	tr := content[0].(map[string]interface{})
	blocks, ok := tr["content"].([]interface{})
	if !ok {
		t.Fatalf("tool_result content should be a block array for an image, got %T", tr["content"])
	}
	sawImage := false
	for _, b := range blocks {
		bm := b.(map[string]interface{})
		if bm["type"] == "image" {
			sawImage = true
			src := bm["source"].(map[string]interface{})
			if mt := src["media_type"]; mt != "image/png" {
				t.Errorf("media_type = %v, want a provider-supported type", mt)
			}
		}
	}
	if !sawImage {
		t.Errorf("no image block in tool_result: %+v", blocks)
	}
}

// A model with no vision capability must never receive image blocks, even if
// images were baked into history by an earlier vision model (mid-session
// switch) — the tool_result content stays a plain string.
func TestBuildAnthropicMessages_NonVisionDropsImage(t *testing.T) {
	c := &GenericClient{Provider: "", Model: "some-text-only-model-xyz"}
	msgs := []Message{
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Type: "function"}}},
		{Role: "tool", ToolID: "c1", Content: "[image file: pic.png]",
			Images: []Image{{MIMEType: "image/png", Data: "AAAA"}}},
	}
	out, err := c.buildAnthropicMessages(msgs)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	tr := out[len(out)-1]["content"].([]interface{})[0].(map[string]interface{})
	if _, isStr := tr["content"].(string); !isStr {
		t.Errorf("non-vision model must get string tool_result content, got %T", tr["content"])
	}
}

// The user's explicit ask: oversized images are downscaled to the cap with
// aspect ratio preserved before embedding.
func TestNewImageFromBytes_DownscalesPreservingRatio(t *testing.T) {
	enc, err := NewImageFromBytes(makePNG(t, 3000, 1000), "image/png", 1000)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(enc.Data)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 1000 || cfg.Height != 333 {
		t.Errorf("dims = %dx%d, want 1000x333 (capped longest edge, ratio preserved)", cfg.Width, cfg.Height)
	}
	// Original size must be reported so the tool result can surface it.
	if !enc.Scaled || enc.OrigWidth != 3000 || enc.OrigHeight != 1000 {
		t.Errorf("meta = {scaled:%v orig:%dx%d}, want scaled with original 3000x1000", enc.Scaled, enc.OrigWidth, enc.OrigHeight)
	}
	if enc.Width != 1000 || enc.Height != 333 {
		t.Errorf("embedded dims meta = %dx%d, want 1000x333", enc.Width, enc.Height)
	}
}

// A within-bounds image is not marked scaled (nothing to report).
func TestNewImageFromBytes_WithinBoundsNotScaled(t *testing.T) {
	enc, err := NewImageFromBytes(makePNG(t, 800, 600), "image/png", 2000)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if enc.Scaled {
		t.Errorf("800x600 under a 2000 cap must not be marked scaled")
	}
}

// A decodable-but-unsupported source format (bmp/tiff) must be re-encoded to a
// provider-supported type even when within bounds — otherwise the API rejects
// the turn. Simulated via an unsupported mimeHint over valid pixels.
func TestNewImageFromBytes_ReencodesUnsupportedMime(t *testing.T) {
	img, err := NewImageFromBytes(makePNG(t, 10, 10), "image/bmp", 2000)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !providerSafeImageMime(img.MIMEType) {
		t.Errorf("MIME = %q, must be re-encoded to a provider-supported type", img.MIMEType)
	}
}

// A tool result carrying an image cannot ride the string-only OpenAI tool
// message; it must be flushed as a following user message with an image_url
// block, and the tool messages must stay contiguous.
func TestConvertToOpenAIMessages_ToolImageBecomesTrailingUserMessage(t *testing.T) {
	c := &GenericClient{Provider: "openai", Model: "gpt-4o"}
	msgs := []Message{
		{Role: "user", Content: "look at this"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "call_1", Type: "function"}}},
		{Role: "tool", ToolID: "call_1", Content: "[image file: pic.png — shown below]",
			Images: []Image{{MIMEType: "image/png", Data: "AAAA"}}},
	}

	out, err := c.convertToOpenAIMessages(msgs)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	// Find the tool message and assert the next message is a user image message.
	toolIdx := -1
	for i, m := range out {
		if m["role"] == "tool" {
			toolIdx = i
		}
	}
	if toolIdx == -1 {
		t.Fatalf("no tool message produced: %+v", out)
	}
	if s, _ := out[toolIdx]["content"].(string); s == "" {
		t.Errorf("tool message content must stay a string, got %T", out[toolIdx]["content"])
	}
	if toolIdx+1 >= len(out) {
		t.Fatalf("expected a user image message after the tool message")
	}
	next := out[toolIdx+1]
	if next["role"] != "user" {
		t.Fatalf("message after tool result should be role=user, got %v", next["role"])
	}
	blocks, ok := next["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("user image message content should be a block array, got %T", next["content"])
	}
	sawImage := false
	for _, b := range blocks {
		if b["type"] == "image_url" {
			sawImage = true
		}
	}
	if !sawImage {
		t.Errorf("no image_url block in flushed user message: %+v", blocks)
	}
}

func TestModelSupportsVision_FallbackFamilies(t *testing.T) {
	// These families are all multimodal; the offline heuristic must accept them
	// even when the registry has no entry (so images are never wrongly stubbed
	// for a capable default). Uses a made-up point release to force the prefix
	// fallback rather than an exact registry hit.
	for _, m := range []string{
		"anthropic/claude-opus-4-8-99999999",
		"openai/gpt-4o",
		"google/gemini-3-pro-latest",
	} {
		if !ModelSupportsVision(m) {
			t.Errorf("ModelSupportsVision(%q) = false, want true", m)
		}
	}
}
