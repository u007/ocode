package server

import (
	"bytes"
	"testing"
	"unicode/utf16"
)

// utf16Bytes encodes s as UTF-16 (with a BOM when withBOM), in the requested
// endianness. Test-only helper mirroring what Windows editors emit.
func utf16Bytes(s string, littleEndian, withBOM bool) []byte {
	units := utf16.Encode([]rune(s))
	var out []byte
	if withBOM {
		if littleEndian {
			out = append(out, 0xFF, 0xFE)
		} else {
			out = append(out, 0xFE, 0xFF)
		}
	}
	for _, u := range units {
		if littleEndian {
			out = append(out, byte(u), byte(u>>8))
		} else {
			out = append(out, byte(u>>8), byte(u))
		}
	}
	return out
}

// utf32Bytes encodes s as UTF-32 with a BOM.
func utf32Bytes(s string, littleEndian bool) []byte {
	var out []byte
	if littleEndian {
		out = append(out, 0xFF, 0xFE, 0x00, 0x00)
	} else {
		out = append(out, 0x00, 0x00, 0xFE, 0xFF)
	}
	for _, r := range s {
		v := uint32(r)
		if littleEndian {
			out = append(out, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
		} else {
			out = append(out, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
		}
	}
	return out
}

func TestEditorTextContentPlainUTF8(t *testing.T) {
	in := []byte("SELECT * FROM users;\n-- 中文注释\n")
	out, isBinary := editorTextContent(in)
	if isBinary {
		t.Fatalf("plain UTF-8 must not be binary")
	}
	if !bytes.Equal(out, in) {
		t.Fatalf("plain UTF-8 must pass through unchanged, got %q", out)
	}
}

func TestEditorTextContentUTF8BOMKept(t *testing.T) {
	in := append([]byte{0xEF, 0xBB, 0xBF}, []byte("hello\n")...)
	out, isBinary := editorTextContent(in)
	if isBinary {
		t.Fatalf("UTF-8 BOM must not be binary")
	}
	if !bytes.Equal(out, in) {
		t.Fatalf("UTF-8 BOM must be preserved (no NUL, already round-trips), got %q", out)
	}
}

func TestEditorTextContentUTF16BOMTranscoded(t *testing.T) {
	const want = "CREATE TABLE t (id INT);\n-- emoji: \U0001F600\n"
	for _, le := range []bool{true, false} {
		in := utf16Bytes(want, le, true)
		if bytes.IndexByte(in, 0) < 0 {
			t.Fatalf("test fixture must contain NUL bytes (le=%v)", le)
		}
		out, isBinary := editorTextContent(in)
		if isBinary {
			t.Errorf("UTF-16 (le=%v) text must not be binary", le)
		}
		if string(out) != want {
			t.Errorf("UTF-16 (le=%v) decode mismatch:\n got %q\nwant %q", le, out, want)
		}
	}
}

func TestEditorTextContentUTF32BOMTranscoded(t *testing.T) {
	const want = "hello \U0001F642 world\n"
	for _, le := range []bool{true, false} {
		out, isBinary := editorTextContent(utf32Bytes(want, le))
		if isBinary {
			t.Errorf("UTF-32 (le=%v) text must not be binary", le)
		}
		if string(out) != want {
			t.Errorf("UTF-32 (le=%v) decode mismatch:\n got %q\nwant %q", le, out, want)
		}
	}
}

func TestEditorTextContentBinaryWithoutBOMStaysBinary(t *testing.T) {
	in := []byte{0x89, 'P', 'N', 'G', 0x00, 0x01, 0x02, 0x00}
	out, isBinary := editorTextContent(in)
	if !isBinary {
		t.Fatalf("NUL-bearing bytes without a Unicode BOM must stay binary")
	}
	if !bytes.Equal(out, in) {
		t.Fatalf("binary bytes must pass through unchanged, got %q", out)
	}
}

func TestDecodeUTF16UnpairedSurrogateReplaced(t *testing.T) {
	// A lone high surrogate (D800) with no low surrogate must decode to U+FFFD,
	// not panic or produce invalid UTF-8.
	in := []byte{0xFF, 0xFE, 0x00, 0xD8, 'a', 0x00}
	out, ok := decodeUnicodeBOM(in)
	if !ok {
		t.Fatal("expected UTF-16 LE BOM to be recognized")
	}
	if !bytes.Equal(out, []byte("\uFFFDa")) {
		t.Fatalf("got %q, want %q", out, "\uFFFDa")
	}
}
