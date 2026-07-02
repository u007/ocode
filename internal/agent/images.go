package agent

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"log"
	"mime"
	"os"
	"path/filepath"
	"strings"

	// Register decoders so image.DecodeConfig / image.Decode can handle the
	// formats we accept. gif is decode-only here (re-encoded as png).
	_ "image/gif"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"

	xdraw "golang.org/x/image/draw"
)

// defaultMaxImageDim is the longest edge (px) an embedded image may have unless
// overridden via config (ocode `image_max_dim`). Larger images are downscaled
// to fit a square box of this size, preserving aspect ratio, before base64
// encoding. This caps the token/byte cost of attachments and keeps requests
// under provider image-size limits.
const defaultMaxImageDim = 2000

// ResolveImageMaxDim normalizes a configured longest-edge cap and falls back
// to the package default when unset or invalid.
func ResolveImageMaxDim(n int) int {
	if n > 0 {
		return n
	}
	return defaultMaxImageDim
}

// visionModels is the OFFLINE FALLBACK for ModelSupportsVision — used only when
// the models.dev registry has no entry for a model. Prefix-matched via
// IsVisionModel, so family prefixes (e.g. "claude-opus") cover every point
// release. All current Claude/GPT/Gemini frontier families are multimodal, so
// listing the family is safe and fails open for new point releases.
var visionModels = map[string]bool{
	// OpenAI
	"gpt-4o":       true,
	"gpt-4-vision": true,
	"gpt-4.1":      true,
	"gpt-5":        true,
	"o3":           true,
	"o4":           true,
	// Anthropic (all Claude 3+ families accept images)
	"claude-3":      true,
	"claude-opus":   true,
	"claude-sonnet": true,
	"claude-haiku":  true,
	// Google
	"gemini-1.5": true,
	"gemini-2":   true,
	"gemini-3":   true,
}

func IsVisionModel(model string) bool {
	base := model
	if idx := strings.LastIndex(model, "/"); idx != -1 {
		base = model[idx+1:]
	}
	if visionModels[base] {
		return true
	}
	for vm := range visionModels {
		if strings.HasPrefix(base, vm) {
			return true
		}
	}
	return false
}

var imageExtensions = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".gif":  true,
	".webp": true,
	".bmp":  true,
	".svg":  true,
	".tiff": true,
	".ico":  true,
}

func IsImageFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return imageExtensions[ext]
}

func DetectImage(path string) (bool, string, error) {
	if !IsImageFile(path) {
		return false, "", nil
	}
	ext := strings.ToLower(filepath.Ext(path))
	mimeType := mime.TypeByExtension(ext)
	if mimeType == "" {
		switch ext {
		case ".png":
			mimeType = "image/png"
		case ".jpg", ".jpeg":
			mimeType = "image/jpeg"
		case ".gif":
			mimeType = "image/gif"
		case ".webp":
			mimeType = "image/webp"
		case ".bmp":
			mimeType = "image/bmp"
		case ".svg":
			mimeType = "image/svg+xml"
		case ".tiff":
			mimeType = "image/tiff"
		case ".ico":
			mimeType = "image/x-icon"
		default:
			mimeType = "application/octet-stream"
		}
	}
	return true, mimeType, nil
}

// decodableImageExt lists the raster formats we can decode (and therefore
// verify/resize). svg and ico are valid image types we accept but cannot
// decode here, so they are embedded verbatim.
var decodableImageExt = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".gif":  true,
	".webp": true,
	".bmp":  true,
	".tiff": true,
}

func isDecodableImage(path string) bool {
	return decodableImageExt[strings.ToLower(filepath.Ext(path))]
}

// providerSafeImageMime reports whether an image MIME type can be embedded as a
// vision block as-is. Anthropic and OpenAI both accept only these four; other
// decodable formats (bmp, tiff) must be re-encoded to png before embedding.
func providerSafeImageMime(mime string) bool {
	switch mime {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

func NewImage(path string) (Image, error) {
	return NewImageWithMaxDim(path, defaultMaxImageDim)
}

// NewImageWithMaxDim embeds an image using the supplied longest-edge cap.
// maxDim values <= 0 fall back to the default cap.
func NewImageWithMaxDim(path string, maxDim int) (Image, error) {
	isImage, mimeType, err := DetectImage(path)
	if err != nil {
		return Image{}, err
	}
	if !isImage {
		return Image{}, fmt.Errorf("not an image file: %s", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Image{}, err
	}
	out, outMime, err := processImage(raw, mimeType, isDecodableImage(path), ResolveImageMaxDim(maxDim))
	if err != nil {
		return Image{}, fmt.Errorf("process image %s: %w", path, err)
	}
	return Image{Path: path, MIMEType: outMime, Data: base64.StdEncoding.EncodeToString(out)}, nil
}

// EncodedImage is an embeddable Image plus the source and embedded pixel
// dimensions, so callers can report when (and by how much) an image was
// downscaled to fit the provider size cap.
type EncodedImage struct {
	Image
	OrigWidth  int
	OrigHeight int
	Width      int
	Height     int
	Scaled     bool // embedded dimensions differ from the source
}

// NewImageFromBytes builds an embeddable image from already-read raw bytes
// (e.g. bytes a confined tool read from disk), applying the same
// decode/salvage/resize pipeline as NewImage. mimeHint should be the source
// content type; the final MIME may differ if the image is re-encoded during
// resize/salvage. The caller must ensure the bytes are a decodable raster
// format — svg/ico have no decoder and are not supported here.
func NewImageFromBytes(raw []byte, mimeHint string, maxDim int) (EncodedImage, error) {
	out, outMime, err := processImage(raw, mimeHint, true, ResolveImageMaxDim(maxDim))
	if err != nil {
		return EncodedImage{}, err
	}
	enc := EncodedImage{
		Image: Image{MIMEType: outMime, Data: base64.StdEncoding.EncodeToString(out)},
	}
	// DecodeConfig reads only the header, so these are cheap. Best-effort:
	// dimensions are informational, so a decode miss just leaves them zero.
	if cfg, _, e := image.DecodeConfig(bytes.NewReader(raw)); e == nil {
		enc.OrigWidth, enc.OrigHeight = cfg.Width, cfg.Height
		enc.Width, enc.Height = cfg.Width, cfg.Height
	}
	if cfg, _, e := image.DecodeConfig(bytes.NewReader(out)); e == nil {
		enc.Width, enc.Height = cfg.Width, cfg.Height
	}
	enc.Scaled = enc.OrigWidth > 0 && (enc.Width != enc.OrigWidth || enc.Height != enc.OrigHeight)
	return enc, nil
}

// IsDecodableImage reports whether the file at path is a raster image format we
// can decode (and therefore resize and embed as a vision block). svg/ico are
// accepted image types but not decodable here.
func IsDecodableImage(path string) bool {
	return isDecodableImage(path)
}

// processImage validates, salvages, and right-sizes raw image bytes before
// embedding:
//
//   - Undecodable-but-accepted formats (svg, ico) pass through verbatim.
//   - A decodable image that fully fails to decode is treated as corrupt and an
//     error is returned, so the caller surfaces it instead of shipping garbage
//     bytes to the provider.
//   - A partially corrupt image that still yields pixels (e.g. a truncated jpeg)
//     is salvaged by re-encoding the decoded portion to clean, valid bytes.
//   - An oversized image is downscaled so its longest edge ≤ maxDim, preserving
//     aspect ratio.
//   - A valid, in-bounds image passes through verbatim (no re-encode).
func processImage(raw []byte, mimeType string, decodable bool, maxDim int) ([]byte, string, error) {
	if !decodable {
		// No decoder (svg/ico) — cannot inspect or resize; embed as-is and let
		// the provider validate. // intentionally not logged: expected for vector/unsupported formats
		return raw, mimeType, nil
	}

	img, format, derr := image.Decode(bytes.NewReader(raw))
	if img == nil {
		// Unrecoverable: no pixels could be decoded at all.
		return nil, "", fmt.Errorf("image is corrupt or truncated: %w", derr)
	}

	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	oversized := w > maxDim || h > maxDim
	partial := derr != nil // decoded some pixels but hit an error → salvageable

	// Providers accept only jpeg/png/gif/webp image blocks. A decodable-but-
	// unsupported format (bmp, tiff) must be re-encoded to png even when it is
	// within bounds and otherwise valid, or the API rejects the whole turn.
	unsafeMime := !providerSafeImageMime(mimeType)

	if !oversized && !partial && !unsafeMime {
		return raw, mimeType, nil // valid, supported, and within bounds — embed original bytes
	}
	if partial {
		log.Printf("processImage: salvaging partially corrupt image by re-encoding decoded pixels: %v", derr)
	}

	// Scale the longest edge down to maxDim, preserving aspect ratio. When the
	// image fits but is being re-encoded to salvage corruption, keep its size.
	newW, newH := w, h
	if oversized {
		scale := float64(maxDim) / float64(w)
		if s := float64(maxDim) / float64(h); s < scale {
			scale = s
		}
		newW = max(1, int(float64(w)*scale))
		newH = max(1, int(float64(h)*scale))
	}

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), xdraw.Over, nil)

	var buf bytes.Buffer
	outMime := "image/png"
	if format == "jpeg" {
		if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 90}); err != nil {
			return nil, "", fmt.Errorf("re-encode jpeg: %w", err)
		}
		outMime = "image/jpeg"
	} else {
		// png preserves transparency; used for png/gif/webp/tiff/bmp sources.
		if err := png.Encode(&buf, dst); err != nil {
			return nil, "", fmt.Errorf("re-encode png: %w", err)
		}
	}
	return buf.Bytes(), outMime, nil
}
