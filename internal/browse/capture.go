package browse

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"net/http"
	"strings"
)

//go:embed capture.js
var captureJS []byte

// injectCapture inserts the bootstrap + capture <script> as the first children
// of <head> so wrapping happens before any page script runs. Falls back to
// before </body>, </html>, or append so capture is never silently skipped.
func injectCapture(html []byte, stateKey, spaOrigin string) []byte {
	// json.Marshal of a plain string cannot fail; err intentionally not logged.
	sk, _ := json.Marshal(stateKey)
	so, _ := json.Marshal(spaOrigin)
	var b strings.Builder
	b.WriteString("<script>window.__OCODE_BROWSE__={stateKey:")
	b.Write(sk)
	b.WriteString(",spaOrigin:")
	b.Write(so)
	b.WriteString("};</script>")
	b.WriteString(`<script src="/__ocode_capture.js"></script>`)
	snippet := []byte(b.String())

	lower := bytes.ToLower(html)
	if i := bytes.Index(lower, []byte("<head>")); i != -1 {
		at := i + len("<head>")
		return concat(html[:at], snippet, html[at:])
	}
	// Head with attributes: <head ...>
	if i := bytes.Index(lower, []byte("<head")); i != -1 {
		if end := bytes.IndexByte(html[i:], '>'); end != -1 {
			at := i + end + 1
			return concat(html[:at], snippet, html[at:])
		}
	}
	if i := bytes.Index(lower, []byte("</head>")); i != -1 {
		return concat(html[:i], snippet, html[i:])
	}
	if i := bytes.Index(lower, []byte("<body")); i != -1 {
		return concat(html[:i], snippet, html[i:])
	}
	return concat(html, snippet, nil)
}

func concat(a, b, c []byte) []byte {
	out := make([]byte, 0, len(a)+len(b)+len(c))
	out = append(out, a...)
	out = append(out, b...)
	out = append(out, c...)
	return out
}

// serveCapture returns the embedded capture bundle from the browse origin.
// It carries no secrets — the per-panel identity lives in the inline bootstrap
// and the HttpOnly cookie — so it is served unauthenticated for cacheability.
func (s *Server) serveCapture(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	if _, err := w.Write(captureJS); err != nil {
		s.log.Printf("browse: serve capture bundle: %v", err)
	}
}
