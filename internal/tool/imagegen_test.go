package tool

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tiny 1x1 transparent PNG
const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

func TestNormalizeProvider(t *testing.T) {
	cases := map[string]string{
		"gemini":    "google",
		"GEMINI":    "google",
		"google":    "google",
		"openai":    "openai",
		"novita":    "novita-ai",
		"novita-ai": "novita-ai",
		"deepinfra": "deepinfra",
	}
	for in, want := range cases {
		p, ok := normalizeProvider(in)
		if !ok {
			t.Errorf("normalizeProvider(%q) not found", in)
			continue
		}
		if p.id != want {
			t.Errorf("normalizeProvider(%q).id = %q, want %q", in, p.id, want)
		}
	}
	if _, ok := normalizeProvider("totally-unknown"); ok {
		t.Errorf("normalizeProvider(unknown) should not be found")
	}
}

func TestExtForMIME(t *testing.T) {
	cases := map[string]string{
		"image/png":  "png",
		"image/jpeg": "jpg",
		"image/webp": "webp",
		"image/gif":  "gif",
		"":           "png",
	}
	for mime, want := range cases {
		if got := extForMIME(mime); got != want {
			t.Errorf("extForMIME(%q) = %q, want %q", mime, got, want)
		}
	}
}

func TestWithExtAndInsertIndex(t *testing.T) {
	if got := withExt("/tmp/out", "png"); got != "/tmp/out.png" {
		t.Errorf("withExt = %q", got)
	}
	if got := withExt("/tmp/out.png", "png"); got != "/tmp/out.png" {
		t.Errorf("withExt existing ext = %q", got)
	}
	if got := insertIndex("/tmp/out.png", 2, "png"); got != "/tmp/out_2.png" {
		t.Errorf("insertIndex = %q", got)
	}
}

func TestParseGeminiResponse(t *testing.T) {
	body := `{
		"candidates": [{
			"content": {
				"parts": [
					{"text": "Here is your image."},
					{"inlineData": {"mimeType": "image/png", "data": "` + tinyPNG + `"}}
				]
			}
		}]
	}`
	imgs, text, err := parseGeminiResponse([]byte(body))
	if err != nil {
		t.Fatalf("parseGeminiResponse error: %v", err)
	}
	if len(imgs) != 1 {
		t.Fatalf("expected 1 image, got %d", len(imgs))
	}
	if imgs[0].mimeType != "image/png" {
		t.Errorf("mimeType = %q", imgs[0].mimeType)
	}
	if len(imgs[0].data) == 0 {
		t.Errorf("image data empty")
	}
	if text != "Here is your image." {
		t.Errorf("text = %q", text)
	}
}

func TestParseOpenAIResponse(t *testing.T) {
	body := `{
		"data": [
			{"b64_json": "` + tinyPNG + `", "revised_prompt": "a cat"},
			{"b64_json": "` + tinyPNG + `"}
		]
	}`
	imgs, revised, err := parseOpenAIResponse([]byte(body))
	if err != nil {
		t.Fatalf("parseOpenAIResponse error: %v", err)
	}
	if len(imgs) != 2 {
		t.Fatalf("expected 2 images, got %d", len(imgs))
	}
	if imgs[0].revised != "a cat" {
		t.Errorf("revised = %q", imgs[0].revised)
	}
	if revised != "a cat" {
		t.Errorf("returned revised = %q", revised)
	}
}

func TestExecuteGeminiViaTestServer(t *testing.T) {
	var gotURL, gotKey, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.Path
		gotKey = r.Header.Get("x-goog-api-key")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"`+tinyPNG+`"}}]}}]}`)
	}))
	defer srv.Close()

	outDir := t.TempDir()
	tool := &ImageGenTool{}
	args, _ := json.Marshal(map[string]interface{}{
		"prompt":      "a cyberpunk city",
		"provider":    "gemini",
		"model":       "gemini-3.1-flash-image",
		"base_url":    srv.URL,
		"api_key":     "test-key",
		"output_path": outDir,
	})
	res, err := tool.Execute(args)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if gotURL != "/v1/models/gemini-3.1-flash-image:generateContent" {
		t.Errorf("url = %q", gotURL)
	}
	if gotKey != "test-key" {
		t.Errorf("api key header = %q", gotKey)
	}
	if !strings.Contains(gotBody, "responseModalities") || !strings.Contains(gotBody, "IMAGE") {
		t.Errorf("request body missing responseModalities: %q", gotBody)
	}

	var summary struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Count    int    `json:"count"`
		Images   []struct {
			Path   string `json:"path"`
			Format string `json:"format"`
			Bytes  int    `json:"bytes"`
		} `json:"images"`
	}
	if err := json.Unmarshal([]byte(res), &summary); err != nil {
		t.Fatalf("unmarshal summary: %v", err)
	}
	if summary.Count != 1 || len(summary.Images) != 1 {
		t.Fatalf("expected 1 image in summary, got %+v", summary)
	}
	if summary.Images[0].Format != "png" {
		t.Errorf("format = %q", summary.Images[0].Format)
	}
	if _, err := os.Stat(summary.Images[0].Path); err != nil {
		t.Errorf("saved file missing: %v", err)
	}
}

func TestExecuteOpenAIViaTestServer(t *testing.T) {
	var gotURL, gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data":[{"b64_json":"`+tinyPNG+`","revised_prompt":"an orange cat"}]}`)
	}))
	defer srv.Close()

	outDir := t.TempDir()
	tool := &ImageGenTool{}
	args, _ := json.Marshal(map[string]interface{}{
		"prompt":      "an orange cat",
		"provider":    "openai",
		"model":       "gpt-image-1",
		"size":        "1024x1024",
		"base_url":    srv.URL,
		"api_key":     "sk-test",
		"output_path": outDir,
	})
	res, err := tool.Execute(args)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if gotURL != "/images/generations" {
		t.Errorf("url = %q", gotURL)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("auth = %q", gotAuth)
	}
	if !strings.Contains(gotBody, `"model":"gpt-image-1"`) {
		t.Errorf("body missing model: %q", gotBody)
	}
	if !strings.Contains(gotBody, `"size":"1024x1024"`) {
		t.Errorf("body missing size: %q", gotBody)
	}
	if !strings.Contains(gotBody, `"response_format":"b64_json"`) {
		t.Errorf("body missing response_format: %q", gotBody)
	}
	_ = filepath.Base(res) // ensure summary is returned
}

func TestExecuteMissingKeyReturnsNotice(t *testing.T) {
	// Unknown provider without base_url and no key -> NoticedError.
	tool := &ImageGenTool{}
	args, _ := json.Marshal(map[string]interface{}{
		"prompt":   "x",
		"provider": "does-not-exist",
	})
	_, err := tool.Execute(args)
	if err == nil {
		t.Fatalf("expected error for unknown provider without base_url")
	}
	ne, ok := err.(*NoticedError)
	if !ok {
		t.Fatalf("expected NoticedError, got %T: %v", err, err)
	}
	if ne.Notice == "" {
		t.Errorf("NoticedError missing Notice")
	}
}

func TestDefinitionSchema(t *testing.T) {
	tool := &ImageGenTool{}
	def := tool.Definition()
	if def["name"] != "imagegen" {
		t.Errorf("name = %v", def["name"])
	}
	props, ok := def["parameters"].(map[string]interface{})["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("parameters.properties missing")
	}
	if _, ok := props["prompt"]; !ok {
		t.Errorf("prompt param missing")
	}
	if _, ok := props["provider"]; !ok {
		t.Errorf("provider param missing")
	}
}
