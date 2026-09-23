package server

import (
	"bytes"
	"strings"
	"unicode/utf16"
)

// editorTextContent prepares raw file bytes for the text editor / preview:
// BOM-marked UTF-16/UTF-32 content is transcoded to UTF-8, and everything else
// is classified by a NUL-byte sniff.
//
// A UTF-16 file is full of NUL bytes across its ASCII range, so a raw NUL sniff
// alone reported it as binary and the editor showed the "Binary File" dead end
// for what is really text — common for `.sql`, `.mdx`, and other files written
// by Windows tools. Transcoding makes them editable instead.
//
// A file with no recognized Unicode BOM is returned unchanged with the usual
// NUL-based verdict. Saving a transcoded file writes UTF-8; the editor already
// warns that byte-for-byte fidelity is not guaranteed for non-text content.
func editorTextContent(data []byte) ([]byte, bool) {
	if decoded, ok := decodeUnicodeBOM(data); ok {
		return decoded, false
	}
	return data, bytes.IndexByte(data, 0) >= 0
}

// decodeUnicodeBOM transcodes UTF-16/UTF-32 text (identified by its BOM) to
// UTF-8. UTF-8 BOMs are deliberately left in place: they contain no NUL bytes,
// so they were never misclassified, and stripping one would silently change a
// file that already round-trips.
func decodeUnicodeBOM(data []byte) ([]byte, bool) {
	switch {
	// UTF-32 LE (FF FE 00 00) must be tested before UTF-16 LE (FF FE).
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE, 0x00, 0x00}):
		return decodeUTF32(data[4:], true), true
	case bytes.HasPrefix(data, []byte{0x00, 0x00, 0xFE, 0xFF}):
		return decodeUTF32(data[4:], false), true
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}):
		return decodeUTF16(data[2:], true), true
	case bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		return decodeUTF16(data[2:], false), true
	}
	return nil, false
}

// decodeUTF16 converts UTF-16 code units to UTF-8. utf16.Decode collapses valid
// surrogate pairs and substitutes U+FFFD for unpaired surrogates; an odd
// trailing byte is dropped (it cannot form a unit).
func decodeUTF16(b []byte, littleEndian bool) []byte {
	units := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		if littleEndian {
			units = append(units, uint16(b[i])|uint16(b[i+1])<<8)
		} else {
			units = append(units, uint16(b[i])<<8|uint16(b[i+1]))
		}
	}
	return []byte(string(utf16.Decode(units)))
}

// decodeUTF32 converts UTF-32 code points to UTF-8. An out-of-range code point
// becomes U+FFFD via utf8.EncodeRune inside strings.Builder.WriteRune; a
// trailing partial unit is dropped.
func decodeUTF32(b []byte, littleEndian bool) []byte {
	var sb strings.Builder
	sb.Grow(len(b))
	for i := 0; i+3 < len(b); i += 4 {
		var v uint32
		if littleEndian {
			v = uint32(b[i]) | uint32(b[i+1])<<8 | uint32(b[i+2])<<16 | uint32(b[i+3])<<24
		} else {
			v = uint32(b[i])<<24 | uint32(b[i+1])<<16 | uint32(b[i+2])<<8 | uint32(b[i+3])
		}
		sb.WriteRune(rune(v))
	}
	return []byte(sb.String())
}
