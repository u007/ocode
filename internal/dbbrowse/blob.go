package dbbrowse

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// This file is the BLOB side of the browser: reading one cell's full bytes out
// (the grid preview is capped at 8 KB and is a *prefix*, not the value) and
// writing one cell's bytes in.
//
// Both operations address a single cell by its row key, so they inherit the same
// exactly-one-row contract as RowUpdate/RowDelete. The read runs on the
// read-only connection; the write is a parameterized UPDATE, never string
// concatenation, so a blob's content can never be parsed as SQL.

// ErrNotABlob reports that the addressed cell holds a value that is not a BLOB.
// SQLite does not enforce column types, so a BLOB column routinely holds TEXT —
// and silently saving that as a file would produce the bytes of a string the
// user never uploaded.
var ErrNotABlob = errors.New("cell is not a BLOB")

// ReadBlob returns the full bytes of one BLOB cell.
//
// A SQL NULL comes back as nil (there is no value); a zero-length BLOB comes
// back as a non-nil empty slice. The caller needs that distinction to tell
// "nothing stored" from "an empty file", so it must not be flattened.
func ReadBlob(ctx context.Context, path, table, column string, key RowValues) ([]byte, error) {
	if strings.TrimSpace(table) == "" {
		return nil, errors.New("table is required")
	}
	if strings.TrimSpace(column) == "" {
		return nil, errors.New("column is required")
	}
	if len(key) == 0 {
		return nil, errors.New("no key to identify the row")
	}

	keyCols := sortedKeys(key)
	var sb strings.Builder
	sb.WriteString("SELECT ")
	sb.WriteString(QuoteIdent(column))
	sb.WriteString(" FROM ")
	sb.WriteString(QuoteIdent(table))
	sb.WriteString(" WHERE ")
	args := make([]any, 0, len(keyCols))
	for i, c := range keyCols {
		if i > 0 {
			sb.WriteString(" AND ")
		}
		sb.WriteString(QuoteIdent(c))
		appendKeyPredicate(&sb, &args, key[c])
	}

	// Collect the matches FIRST and only judge the value once the key is known
	// to be unambiguous. Checking the value as we go would let the first match's
	// column type mask an ambiguous key: the user would be told "that column is
	// not a BLOB" when the real answer is "I cannot tell which row you meant".
	var values []any
	err := withReadOnly(ctx, path, func(db *sql.DB) error {
		rs, err := db.QueryContext(ctx, sb.String(), args...)
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			var v any
			if err := rs.Scan(&v); err != nil {
				return err
			}
			values = append(values, v)
			// Two is enough to know the key is not unique; stop reading rather
			// than materialising every matching row's blob.
			if len(values) > 1 {
				break
			}
		}
		return rs.Err()
	})
	if err != nil {
		return nil, err
	}
	switch len(values) {
	case 0:
		return nil, ErrNoRowsAffected
	case 1:
		// fall through to the type check below
	default:
		return nil, ErrMultipleRowsAffected
	}

	var out []byte
	switch t := values[0].(type) {
	case nil:
		out = nil
	case []byte:
		// A zero-length blob must stay non-nil after the copy, so it is not
		// confused with the nil a SQL NULL produces above.
		out = append([]byte{}, t...)
	case string:
		return nil, fmt.Errorf("%w: column %q holds TEXT", ErrNotABlob, column)
	default:
		return nil, fmt.Errorf("%w: column %q holds %T", ErrNotABlob, column, t)
	}
	return out, nil
}

// WriteBlob sets one BLOB cell to data.
//
// It delegates to RowUpdate so the exactly-one-row transaction, the quoted
// identifiers, the parameterized value and the refusal to write a view are all
// the ones already proven for every other row mutation — a blob upload is not a
// second, weaker write path.
func WriteBlob(ctx context.Context, path, table, column string, key RowValues, data []byte) (ExecResult, error) {
	if strings.TrimSpace(column) == "" {
		return ExecResult{}, errors.New("column is required")
	}
	if data == nil {
		// nil would become SQL NULL, which is a different value from an empty
		// file. Treat a nil upload as the empty blob the user selected.
		data = []byte{}
	}
	return RowUpdate(ctx, path, table, key, RowValues{column: data})
}

// ── Media sniffing ─────────────────────────────────────────────────────────

// SniffBlobMediaType identifies a BLOB by its leading magic bytes. A blob has no
// name and no extension, so the bytes are the only evidence available — guessing
// from the column name would be wrong as often as it is right.
//
// SVG is deliberately NOT detected. It is XML that browsers execute, so
// reporting it as an image would turn "preview this blob" into a same-origin
// script sink; it falls through to the generic binary type and is download-only.
func SniffBlobMediaType(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}):
		return "image/png"
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}):
		return "image/jpeg"
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return "image/gif"
	case len(data) >= 12 && bytes.HasPrefix(data, []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return "image/webp"
	case bytes.HasPrefix(data, []byte("%PDF-")):
		return "application/pdf"
	case bytes.HasPrefix(data, []byte{0x1f, 0x8b}):
		return "application/gzip"
	case bytes.HasPrefix(data, []byte{'P', 'K', 0x03, 0x04}):
		return "application/zip"
	default:
		return "application/octet-stream"
	}
}

// IsInlineSafeMediaType reports whether a media type may be rendered DIRECTLY in
// the page (an <img> src or a blob URL) rather than downloaded.
//
// The list is an allowlist of raster images on purpose. `image/svg+xml` and
// `text/html` execute script, and PDF can script too, so they are download-only
// even though a browser can display them.
func IsInlineSafeMediaType(mediaType string) bool {
	switch mediaType {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

// BlobExtension returns a file extension for a sniffed media type, so a
// downloaded blob lands with a name the OS can open.
func BlobExtension(mediaType string) string {
	switch mediaType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "application/pdf":
		return ".pdf"
	case "application/gzip":
		return ".gz"
	case "application/zip":
		return ".zip"
	default:
		return ".bin"
	}
}
