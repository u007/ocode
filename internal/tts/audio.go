package tts

import (
	"fmt"
	"net/http"
	"os"
)

// ServeAudio serves a completed seekable audio file with HTTP range support.
// Synthesis is intentionally separate from serving; an engine adapter must
// produce a fully verified file before calling this helper.
func ServeAudio(w http.ResponseWriter, r *http.Request, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open audio: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat audio: %w", err)
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
	return nil
}
