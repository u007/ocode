package agent

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func writeImageFile(t *testing.T, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func encodePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func encodeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func decodedDims(t *testing.T, b64 string) (int, int) {
	t.Helper()
	dec, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(dec))
	if err != nil {
		t.Fatalf("embedded image not decodable: %v", err)
	}
	return cfg.Width, cfg.Height
}

// Oversized images are downscaled so the longest edge is maxImageDim, aspect
// ratio preserved.
func TestNewImageDownscalesOversized(t *testing.T) {
	p := writeImageFile(t, "big.png", encodePNG(t, 4000, 3000))
	img, err := NewImage(p)
	if err != nil {
		t.Fatal(err)
	}
	if w, h := decodedDims(t, img.Data); w != 2000 || h != 1500 {
		t.Fatalf("want 2000x1500, got %dx%d", w, h)
	}
	if img.MIMEType != "image/png" {
		t.Fatalf("mime=%s", img.MIMEType)
	}
}

func TestNewImageDownscalesPortraitJPEG(t *testing.T) {
	p := writeImageFile(t, "tall.jpg", encodeJPEG(t, 3000, 6000))
	img, err := NewImage(p)
	if err != nil {
		t.Fatal(err)
	}
	if w, h := decodedDims(t, img.Data); w != 1000 || h != 2000 {
		t.Fatalf("want 1000x2000, got %dx%d", w, h)
	}
	if img.MIMEType != "image/jpeg" {
		t.Fatalf("mime=%s", img.MIMEType)
	}
}

// A valid, in-bounds image is embedded byte-for-byte (no re-encode).
func TestNewImageWithinBoundsPassesThrough(t *testing.T) {
	orig := encodePNG(t, 100, 80)
	p := writeImageFile(t, "small.png", orig)
	img, err := NewImage(p)
	if err != nil {
		t.Fatal(err)
	}
	dec, _ := base64.StdEncoding.DecodeString(img.Data)
	if !bytes.Equal(dec, orig) {
		t.Fatal("valid in-bounds image must pass through unchanged")
	}
}

// A corrupt/truncated decodable image must fail loudly rather than embed
// garbage bytes the provider would reject.
func TestNewImageCorruptFailsLoudly(t *testing.T) {
	bad := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0x41}, 64)...)
	p := writeImageFile(t, "corrupt.png", bad)
	if _, err := NewImage(p); err == nil {
		t.Fatal("expected error for corrupt image")
	}
}

func TestNewImageTruncatedJPEGFailsLoudly(t *testing.T) {
	full := encodeJPEG(t, 1200, 900)
	trunc := full[:len(full)-len(full)/5] // drop the tail
	p := writeImageFile(t, "trunc.jpg", trunc)
	if _, err := NewImage(p); err == nil {
		t.Fatal("expected error for truncated jpeg")
	}
}

// NewImageWithMaxDim respects the explicit cap parameter and does not rely on
// any package-global mutable image size state.
func TestNewImageWithMaxDimOverride(t *testing.T) {
	p := writeImageFile(t, "mid.png", encodePNG(t, 1500, 1200))
	img, err := NewImageWithMaxDim(p, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if w, h := decodedDims(t, img.Data); w != 1000 || h != 800 {
		t.Fatalf("want 1000x800 under 1000 cap, got %dx%d", w, h)
	}

	img, err = NewImageWithMaxDim(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if w, h := decodedDims(t, img.Data); w != 1500 || h != 1200 {
		t.Fatalf("want default pass-through under zero cap, got %dx%d", w, h)
	}
}

// Formats we cannot decode (svg, ico) are accepted and embedded verbatim.
func TestNewImageUndecodablePassesThrough(t *testing.T) {
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10"/></svg>`)
	p := writeImageFile(t, "vec.svg", svg)
	img, err := NewImage(p)
	if err != nil {
		t.Fatal(err)
	}
	dec, _ := base64.StdEncoding.DecodeString(img.Data)
	if !bytes.Equal(dec, svg) {
		t.Fatal("svg must pass through unchanged")
	}
	if img.MIMEType != "image/svg+xml" {
		t.Fatalf("mime=%s", img.MIMEType)
	}
}
