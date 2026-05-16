package agent

import (
	"encoding/base64"
	"mime"
	"os"
	"path/filepath"
	"strings"
)

var visionModels = map[string]bool{
	"gpt-4o":            true,
	"gpt-4o-mini":       true,
	"gpt-4-vision":      true,
	"claude-3-opus":     true,
	"claude-3-sonnet":   true,
	"claude-3-haiku":    true,
	"claude-3-5-sonnet": true,
	"gemini-1.5-pro":    true,
	"gemini-1.5-flash":  true,
	"gemini-2.0-flash":  true,
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

func EncodeImage(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}
