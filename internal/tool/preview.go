package tool

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// PreviewOpenSentinel prefixes the preview_open tool result. The web
// PreviewHost's usePreviewActivation hook scans chat tool results for this
// prefix and auto-opens the referenced file in the sidebar — no extra SSE
// plumbing needed; the chat stream already carries tool results to the SPA.
const PreviewOpenSentinel = "PREVIEW_OPEN:"

// previewOpenKinds maps extensions that have a SPECIALIZED sidebar renderer
// (pdf.js, docx-preview, jszip slide parser, mermaid.js, images, native
// audio/video) plus the markdown family. Everything else is text by default —
// see previewNonTextExts and Execute. The authoritative binary gate is
// content-based: GET /api/files/content returns is_binary from a NUL sniff.
var previewOpenKinds = map[string]string{
	".pdf":  "pdf",
	".docx": "docx",
	".pptx": "pptx",
	".xlsx": "excel",
	".xls":  "excel",
	".csv":  "excel",
	".png":  "image",
	".jpg":  "image",
	".jpeg": "image",
	".gif":  "image",
	".webp": "image",
	".svg":  "image",
	".mmd":  "mermaid",
	".md":   "text",
	// Markdown family (the client resolves the preview kind from the extension;
	// .mdx renders through MarkdownViewer and highlights as Monaco `mdx`).
	".markdown": "text",
	".mdx":      "text",
	// Audio/video: browser-playable containers only (mirrors
	// HandleFileRaw.previewRawTypes; .mkv/.avi stay out).
	".mp3":  "audio",
	".m4a":  "audio",
	".aac":  "audio",
	".wav":  "audio",
	".ogg":  "audio",
	".oga":  "audio",
	".opus": "audio",
	".flac": "audio",
	".mp4":  "video",
	".m4v":  "video",
	".webm": "video",
	".ogv":  "video",
	".mov":  "video",
}

// previewNonTextExts lists known binary formats with no in-browser renderer.
// They keep the OS-open fallback (or stay un-previewable) instead of being
// opened as text — a huge archive or disk image read through
// /api/files/content would be JSON-decoded into a string for nothing.
//
// This is a UX/perf denylist, NOT the binary gate: an extension missing from
// it still degrades safely to the content endpoint's NUL-byte check. Keep it
// roughly in sync with NON_PREVIEWABLE_EXTS in web/src/lib/previewKind.ts.
var previewNonTextExts = map[string]bool{
	// Archives, packages, disk images.
	".zip": true, ".tar": true, ".gz": true, ".tgz": true, ".bz2": true,
	".tbz": true, ".tbz2": true, ".xz": true, ".txz": true, ".lz": true,
	".lzma": true, ".zst": true, ".zstd": true, ".7z": true, ".rar": true,
	".cab": true, ".arj": true, ".lzh": true, ".jar": true, ".war": true,
	".ear": true, ".apk": true, ".aab": true, ".ipa": true, ".dmg": true,
	".iso": true, ".img": true, ".vhd": true, ".vhdx": true, ".vmdk": true,
	".qcow2": true, ".deb": true, ".rpm": true, ".pkg": true, ".mpkg": true,
	".msi": true, ".msp": true, ".snap": true, ".flatpak": true, ".crx": true,
	".xpi": true, ".whl": true, ".gem": true, ".nupkg": true, ".vsix": true,
	".zpaq": true, ".lzo": true,
	// Executables, libraries, object/debug artifacts.
	".exe": true, ".dll": true, ".dylib": true, ".so": true, ".o": true,
	".obj": true, ".a": true, ".lib": true, ".bin": true, ".class": true,
	".pyc": true, ".pyo": true, ".pyd": true, ".wasm": true, ".elf": true,
	".com": true, ".sys": true, ".ko": true, ".app": true, ".msix": true,
	".appx": true, ".node": true, ".out": true, ".pdb": true, ".dsym": true,
	// Fonts.
	".ttf": true, ".otf": true, ".woff": true, ".woff2": true, ".eot": true,
	".ttc": true, ".pfb": true, ".pfm": true, ".dfont": true,
	// Databases and binary indexes/dumps.
	".sqlite": true, ".sqlite3": true, ".db": true, ".db3": true, ".mdb": true,
	".accdb": true, ".idx": true, ".pack": true, ".lmdb": true, ".mdbx": true,
	".frm": true, ".ibd": true, ".myi": true, ".myd": true, ".rdb": true,
	".ldb": true, ".sst": true,
	// Audio/video containers with no reliable browser renderer.
	".mkv": true, ".avi": true, ".wmv": true, ".flv": true, ".mpg": true,
	".mpeg": true, ".m2ts": true, ".mts": true, ".vob": true, ".rm": true,
	".rmvb": true, ".3gp": true, ".3g2": true, ".ogm": true, ".divx": true,
	".asf": true, ".f4v": true, ".mxf": true, ".dv": true, ".wtv": true,
	".wma": true, ".aiff": true, ".aif": true, ".aifc": true, ".au": true,
	".snd": true, ".mid": true, ".midi": true, ".ra": true, ".ram": true,
	".mka": true, ".ape": true, ".wv": true, ".amr": true, ".ac3": true,
	".dts": true, ".caf": true, ".aax": true,
	// Images with no ImageViewer renderer (png/jpg/gif/webp/svg only).
	".tiff": true, ".tif": true, ".bmp": true, ".heic": true, ".heif": true,
	".avif": true, ".ico": true, ".cur": true, ".psd": true, ".psb": true,
	".xcf": true, ".raw": true, ".cr2": true, ".cr3": true, ".nef": true,
	".arw": true, ".dng": true, ".orf": true, ".rw2": true, ".svgz": true,
	".jp2": true, ".j2k": true,
	// Legacy/binary Office and document containers.
	".doc": true, ".ppt": true, ".docm": true, ".dot": true, ".dotm": true,
	".dotx": true, ".xlsm": true, ".xlt": true, ".xltx": true, ".xltm": true,
	".pptm": true, ".pot": true, ".potx": true, ".potm": true, ".pps": true,
	".ppsx": true, ".ppsm": true, ".vsd": true, ".vsdx": true, ".one": true,
	".msg": true, ".pub": true,
	// Other opaque binary containers.
	".swf": true, ".ai": true, ".eps": true, ".indd": true, ".sketch": true,
	".fig": true, ".blend": true, ".stl": true, ".fbx": true, ".3ds": true,
	".glb": true, ".dwg": true, ".der": true, ".p12": true, ".pfx": true,
	".jks": true, ".keystore": true, ".nib": true, ".car": true, ".icns": true,
	".pak": true, ".bundle": true, ".parquet": true, ".avro": true, ".orc": true,
	".arrow": true, ".feather": true, ".npy": true, ".npz": true, ".pkl": true,
	".pickle": true, ".pt": true, ".pth": true, ".onnx": true, ".h5": true,
	".hdf5": true, ".safetensors": true, ".ckpt": true, ".gguf": true,
	".msgpack": true, ".bson": true, ".cbor": true,
}

// PreviewOpenTool lets the LLM activate the sidebar preview on a file:
// "show this deck in the sidebar", "open the report for a look".
// Office documents (Word, Excel, PowerPoint), PDFs, images, and audio/video
// are preview-only; only text/code opens editable. It validates the
// path only (workspace-relative + allowlist); the actual bytes flow
// through GET /api/files/raw or /api/files/content so confinement stays
// in the server handlers, not the tool.
type PreviewOpenTool struct{}

func (t *PreviewOpenTool) Name() string { return "preview_open" }

func (t *PreviewOpenTool) Description() string {
	return "Open a file in the sidebar preview (browser tab stays available). Use when the user asks to preview or present a file: PDFs (multi-page), Word docs (preview + select), Excel sheets (preview + select), PowerPoint decks (slides + present mode), mermaid diagrams (clickable nodes), images, audio, or video (native player), or text/code (editable). Returns a PREVIEW_OPEN directive the web UI auto-opens."
}

// Definition returns the canonical FLAT tool descriptor
// ({name, description, parameters}) that every other built-in uses. The
// OpenAI transport wraps it into the nested {type:function, function:{...}}
// shape (openAITools); the Anthropic transport reads name/parameters directly
// and has no nested-form normalization, so returning the nested object here
// produced malformed Anthropic tool entries (empty name, nil input_schema) and
// a 400 from every Anthropic-protocol route.
func (t *PreviewOpenTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        t.Name(),
		"description": t.Description(),
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Workspace-relative file path to preview (e.g. docs/deck.pptx). Must stay inside the project root.",
				},
				"page": map[string]interface{}{
					"type":        "integer",
					"description": "Optional 1-based initial page/slide to show (PDF, PPTX). Defaults to 1.",
				},
			},
			"required": []string{"path"},
		},
	}
}

func (t *PreviewOpenTool) Parallel() bool { return true }

func (t *PreviewOpenTool) Execute(args json.RawMessage) (string, error) {
	var a struct {
		Path string `json:"path"`
		Page int    `json:"page"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	p := strings.TrimSpace(a.Path)
	if p == "" {
		return "", fmt.Errorf("path is required")
	}
	clean := filepath.Clean(p)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path must be workspace-relative and stay inside the project root")
	}
	ext := strings.ToLower(filepath.Ext(clean))
	kind, ok := previewOpenKinds[ext]
	if !ok {
		// Text is the default (code, config, markup, extensionless files);
		// only known binary/no-renderer formats are refused.
		if previewNonTextExts[ext] {
			return "", fmt.Errorf("file type is not sidebar-previewable (binary or no renderer)")
		}
		kind = "text"
	}
	page := a.Page
	if page < 0 {
		return "", fmt.Errorf("page must be >= 1")
	}
	if page == 0 {
		page = 1
	}
	return fmt.Sprintf("%s%s|kind=%s|page=%d", PreviewOpenSentinel, clean, kind, page), nil
}
