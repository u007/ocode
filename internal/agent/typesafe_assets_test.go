package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// TestTypesafeDecideWithAssetsSendsHeaderOnlyWithAssets: a request with no assets is
// the same request DecideCtx sends, with no images header and no assets key. A
// request with assets carries both.
func TestTypesafeDecideWithAssetsSendsHeaderOnlyWithAssets(t *testing.T) {
	var headers []string
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = append(headers, r.Header.Get(TypesafeImagesHeader))
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"jev-latest","answers":{"q":{"type":"noul","noul":0.9,"confidence":0.9}},"usage":{}}`))
	}))
	defer srv.Close()

	c := newTypesafeClient("k", "jev-latest", srv.URL)
	ctx := context.Background()
	questions := map[string]TypesafeQuestion{"q": {Type: "noul", Instructions: "done?"}}
	if _, err := c.DecideCtx(ctx, "state", questions); err != nil {
		t.Fatalf("DecideCtx: %v", err)
	}
	if _, err := c.DecideWithAssetsCtx(ctx, "state", questions, nil); err != nil {
		t.Fatalf("DecideWithAssetsCtx without assets: %v", err)
	}
	assets := map[string]TypesafeAsset{"shot": {Type: "image", Format: "png", DataBase64: "AAAA"}}
	if _, err := c.DecideWithAssetsCtx(ctx, "state", questions, assets); err != nil {
		t.Fatalf("DecideWithAssetsCtx with assets: %v", err)
	}

	if len(headers) != 3 {
		t.Fatalf("requests = %d, want 3", len(headers))
	}
	if headers[0] != "" || headers[1] != "" {
		t.Fatalf("images header sent without assets: %q, %q", headers[0], headers[1])
	}
	if headers[2] != "true" {
		t.Fatalf("images header with assets = %q, want true", headers[2])
	}
	for i := 0; i < 2; i++ {
		if _, ok := bodies[i]["assets"]; ok {
			t.Fatalf("request %d carries an assets key without assets", i)
		}
	}
	got, ok := bodies[2]["assets"].(map[string]any)
	if !ok {
		t.Fatalf("request with assets lacks the assets object: %v", bodies[2])
	}
	shot, _ := got["shot"].(map[string]any)
	if shot["type"] != "image" || shot["format"] != "png" || shot["data_base64"] != "AAAA" {
		t.Fatalf("asset = %v, want the image as sent", shot)
	}
}

// TestTypesafeLiveImageAsset asks Jev which number is on a blue box in a synthetic
// image, through a real image asset. Gated on TYPESAFE_API_KEY. It logs the input
// tokens the image request cost.
func TestTypesafeLiveImageAsset(t *testing.T) {
	key := os.Getenv("TYPESAFE_API_KEY")
	if key == "" {
		t.Skip("TYPESAFE_API_KEY not set; skipping live TypeSafe image test")
	}
	raw := syntheticTwoBoxPNG(t)
	const placeholder = "image-placeholder-0001"
	state := map[string]any{"screenshot": placeholder}
	questions := map[string]TypesafeQuestion{
		"which": {
			Type:         "choice",
			Instructions: "The image in the state shows two boxes, each with one number drawn on it. Which number is on the blue box?",
			Criteria:     map[string]string{"3": "the number three", "8": "the number eight"},
		},
	}
	assets := map[string]TypesafeAsset{placeholder: {
		Type:       "image",
		Format:     "png",
		DataBase64: base64.StdEncoding.EncodeToString(raw),
	}}
	c := newTypesafeClient(key, "jev-latest", "https://api.typesafe.ai/v1")
	resp, err := c.DecideWithAssetsCtx(context.Background(), state, questions, assets)
	if err != nil {
		t.Fatalf("DecideWithAssetsCtx: %v", err)
	}
	ans, ok := resp.Answers["which"]
	if !ok {
		t.Fatalf("no answer to the question: %+v", resp.Answers)
	}
	t.Logf("image request input_tokens=%d choice=%s probability=%.2f", resp.Usage.InputTokens, ans.Choice, ans.Probabilities[ans.Choice])
	if ans.Choice != "3" {
		t.Fatalf("choice = %q, want 3 (the number on the blue box)", ans.Choice)
	}
}

// syntheticTwoBoxPNG draws a 640x400 image with a blue box numbered 3 and a red box
// numbered 8. Each number is drawn at a size the model can read.
func syntheticTwoBoxPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 640, 400))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	blue := image.Rect(60, 120, 260, 300)
	red := image.Rect(380, 120, 580, 300)
	draw.Draw(img, blue, &image.Uniform{C: color.RGBA{0, 0, 255, 255}}, image.Point{}, draw.Src)
	draw.Draw(img, red, &image.Uniform{C: color.RGBA{255, 0, 0, 255}}, image.Point{}, draw.Src)
	drawBigDigit(img, "3", blue)
	drawBigDigit(img, "8", red)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode synthetic image: %v", err)
	}
	return buf.Bytes()
}

// drawBigDigit draws one digit, centred in r, at a size several times the basic font's
// 7x13 pixels: the glyph is drawn small, then each of its pixels is scaled up.
func drawBigDigit(dst *image.RGBA, digit string, r image.Rectangle) {
	const scale = 10
	face := basicfont.Face7x13
	glyph := image.NewRGBA(image.Rect(0, 0, 7, 13))
	d := &font.Drawer{Dst: glyph, Src: image.White, Face: face, Dot: fixed.P(0, 11)}
	d.DrawString(digit)
	w, h := 7*scale, 13*scale
	x0 := r.Min.X + (r.Dx()-w)/2
	y0 := r.Min.Y + (r.Dy()-h)/2
	for gy := 0; gy < 13; gy++ {
		for gx := 0; gx < 7; gx++ {
			if _, _, _, a := glyph.At(gx, gy).RGBA(); a == 0 {
				continue
			}
			block := image.Rect(x0+gx*scale, y0+gy*scale, x0+(gx+1)*scale, y0+(gy+1)*scale)
			draw.Draw(dst, block, &image.Uniform{C: color.White}, image.Point{}, draw.Src)
		}
	}
}
