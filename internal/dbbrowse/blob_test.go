package dbbrowse

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// blobFixture is a table with a BLOB column, one row carrying "hello" and one
// carrying NULL, so the read path can be checked against both.
const blobFixture = `CREATE TABLE files(id INTEGER PRIMARY KEY, name TEXT, data BLOB);
	INSERT INTO files VALUES (1,'a.bin',X'68656c6c6f');   -- "hello"
	INSERT INTO files VALUES (2,'empty.bin',X'');         -- zero-length blob
	INSERT INTO files VALUES (3,'none.bin',NULL);         -- SQL NULL`

func TestReadBlobReturnsTheWholeValue(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "blob.db", blobFixture)

	got, err := ReadBlob(ctx, path, "files", "data", RowValues{"id": int64(1)})
	if err != nil {
		t.Fatalf("ReadBlob: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("got %q, want hello", got)
	}
}

// The grid only ever carries the first 8 KB of a blob. The whole point of this
// call is that it does not: a value larger than the preview cap must come back
// intact, byte for byte.
func TestReadBlobIsNotCappedLikeTheGridPreview(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "blob.db", `CREATE TABLE files(id INTEGER PRIMARY KEY, data BLOB);`)
	big := bytes.Repeat([]byte{0xAB}, blobDataMaxBytes*3+7)
	if _, err := RowInsert(ctx, path, "files", RowValues{"id": int64(1), "data": big}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := ReadBlob(ctx, path, "files", "data", RowValues{"id": int64(1)})
	if err != nil {
		t.Fatalf("ReadBlob: %v", err)
	}
	if !bytes.Equal(got, big) {
		t.Fatalf("got %d bytes, want the full %d", len(got), len(big))
	}
}

// A NULL cell and an empty blob are different values and must not be conflated:
// nil means "no blob stored", an empty slice means "a zero-length blob".
func TestReadBlobDistinguishesNullFromEmpty(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "blob.db", blobFixture)

	empty, err := ReadBlob(ctx, path, "files", "data", RowValues{"id": int64(2)})
	if err != nil {
		t.Fatalf("empty: %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty blob = %#v, want a non-nil zero-length slice", empty)
	}

	null, err := ReadBlob(ctx, path, "files", "data", RowValues{"id": int64(3)})
	if err != nil {
		t.Fatalf("null: %v", err)
	}
	if null != nil {
		t.Fatalf("null cell = %#v, want nil", null)
	}
}

// A non-blob value is text, not bytes; reading it as a blob would silently
// produce the encoding of the number rather than the user's data.
func TestReadBlobRejectsANonBlobColumn(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "blob.db", `CREATE TABLE t(id INTEGER PRIMARY KEY, n INTEGER, s TEXT);
		INSERT INTO t VALUES (1, 42, 'text');`)

	if _, err := ReadBlob(ctx, path, "t", "n", RowValues{"id": int64(1)}); err == nil {
		t.Fatal("expected reading an INTEGER column as a blob to fail")
	}
	if _, err := ReadBlob(ctx, path, "t", "s", RowValues{"id": int64(1)}); err == nil {
		t.Fatal("expected reading a TEXT column as a blob to fail")
	}
}

// The URI "BLOB affinity" trap: a column typed BLOB but holding TEXT is common
// (SQLite does not enforce types), so a file saved from it must not be silently
// mangled — it is refused with a reason instead.
func TestReadBlobRejectsTextStoredInABlobColumn(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "blob.db", `CREATE TABLE t(id INTEGER PRIMARY KEY, data BLOB);
		INSERT INTO t VALUES (1, 'plain text in a blob column');`)

	_, err := ReadBlob(ctx, path, "t", "data", RowValues{"id": int64(1)})
	if err == nil {
		t.Fatal("expected a TEXT value in a BLOB column to be refused")
	}
	if !strings.Contains(err.Error(), "not a BLOB") {
		t.Fatalf("err = %v, want a message naming the mismatch", err)
	}
}

func TestReadBlobRequiresAnExistingColumn(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "blob.db", blobFixture)
	if _, err := ReadBlob(ctx, path, "files", "nope", RowValues{"id": int64(1)}); err == nil {
		t.Fatal("expected an unknown column to be refused")
	}
}

func TestReadBlobRequiresAKey(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "blob.db", blobFixture)
	if _, err := ReadBlob(ctx, path, "files", "data", nil); err == nil {
		t.Fatal("expected a missing key to be refused")
	}
}

// A key matching no row must be an error, never an empty file: "download" of a
// row that is gone has to say so rather than saving 0 bytes.
func TestReadBlobMissingRowIsAnError(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "blob.db", blobFixture)
	_, err := ReadBlob(ctx, path, "files", "data", RowValues{"id": int64(999)})
	if !errors.Is(err, ErrNoRowsAffected) {
		t.Fatalf("err = %v, want ErrNoRowsAffected", err)
	}
}

// The ambiguity is the answer even when the FIRST match's column happens to
// hold TEXT: reporting "that column is not a BLOB" would name the wrong problem
// and send the user looking at the column instead of at their key.
func TestReadBlobPrefersAmbiguityOverTheColumnType(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "blob.db", `CREATE TABLE t(id INTEGER PRIMARY KEY, name TEXT, data BLOB);
		INSERT INTO t VALUES (1,'dup','text one'), (2,'dup',X'62');`)

	_, err := ReadBlob(ctx, path, "t", "data", RowValues{"name": "dup"})
	if !errors.Is(err, ErrMultipleRowsAffected) {
		t.Fatalf("err = %v, want ErrMultipleRowsAffected (not the TEXT complaint)", err)
	}
	if errors.Is(err, ErrNotABlob) {
		t.Fatal("a non-unique key must not be reported as a column-type problem")
	}
}

// A key that matches several rows makes "the blob" ambiguous. Reading the first
// match instead of refusing would hand the user a different row's data under
// the name they asked for — the worst possible failure for a download.
func TestReadBlobRefusesAnAmbiguousKey(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "blob.db", `CREATE TABLE t(id INTEGER PRIMARY KEY, name TEXT, data BLOB);
		INSERT INTO t VALUES (1,'same', X'61'), (2,'same', X'62');`)

	_, err := ReadBlob(ctx, path, "t", "data", RowValues{"name": "same"})
	if !errors.Is(err, ErrMultipleRowsAffected) {
		t.Fatalf("err = %v, want ErrMultipleRowsAffected", err)
	}
	// Narrowing to the unique column resolves the ambiguity.
	got, err := ReadBlob(ctx, path, "t", "data", RowValues{"id": int64(2)})
	if err != nil {
		t.Fatalf("narrowed key: %v", err)
	}
	if string(got) != "b" {
		t.Fatalf("got %q, want b", got)
	}
}

func TestReadBlobAddressesByRowID(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "blob.db", `CREATE TABLE t(x TEXT, data BLOB); INSERT INTO t VALUES ('a', X'61');`)
	got, err := ReadBlob(ctx, path, "t", "data", RowValues{RowIDColumn: int64(1)})
	if err != nil {
		t.Fatalf("ReadBlob: %v", err)
	}
	if string(got) != "a" {
		t.Fatalf("got %q, want a", got)
	}
}

// ── Write side ─────────────────────────────────────────────────────────────

func TestWriteBlobSetsTheCell(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "blob.db", blobFixture)

	res, err := WriteBlob(ctx, path, "files", "data", RowValues{"id": int64(3)}, []byte{0x01, 0x02, 0x03})
	if err != nil {
		t.Fatalf("WriteBlob: %v", err)
	}
	if res.RowsAffected != 1 {
		t.Fatalf("rows affected = %d, want 1", res.RowsAffected)
	}
	got, err := ReadBlob(ctx, path, "files", "data", RowValues{"id": int64(3)})
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Fatalf("got %#v, want 01 02 03", got)
	}
}

// A zero-length upload is a legitimate value (an empty file), not "clear it".
func TestWriteBlobAcceptsAnEmptyValue(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "blob.db", blobFixture)
	if _, err := WriteBlob(ctx, path, "files", "data", RowValues{"id": int64(1)}, []byte{}); err != nil {
		t.Fatalf("WriteBlob: %v", err)
	}
	got, err := ReadBlob(ctx, path, "files", "data", RowValues{"id": int64(1)})
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("got %#v, want a non-nil zero-length slice", got)
	}
}

// The exactly-one-row contract carries over from RowUpdate: a key matching
// nothing, or matching several rows, must not look like a successful upload.
func TestWriteBlobRequiresExactlyOneRow(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "blob.db", blobFixture)

	if _, err := WriteBlob(ctx, path, "files", "data", RowValues{"id": int64(999)}, []byte{1}); !errors.Is(err, ErrNoRowsAffected) {
		t.Fatalf("no match: err = %v, want ErrNoRowsAffected", err)
	}
	// A non-unique key that happens to match ONE row is legitimate...
	if _, err := WriteBlob(ctx, path, "files", "data", RowValues{"name": "a.bin"}, []byte{1}); err != nil {
		t.Fatalf("single match on name should succeed: %v", err)
	}
	// ...but one that matches several must be refused rather than writing all.
	dup := makeDB(t, "dup.db", `CREATE TABLE t(name TEXT, data BLOB);
		INSERT INTO t VALUES ('same', NULL), ('same', NULL);`)
	if _, err := WriteBlob(ctx, dup, "t", "data", RowValues{"name": "same"}, []byte{1}); !errors.Is(err, ErrMultipleRowsAffected) {
		t.Fatalf("multi match: err = %v, want ErrMultipleRowsAffected", err)
	}
	// And the refusal left both rows untouched.
	page, _, err := TablePageFiltered(ctx, dup, "t", PageOptions{})
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	for _, row := range page.Rows {
		if row[1] != nil {
			t.Fatalf("a refused multi-row write changed a cell: %#v", row)
		}
	}
}

func TestWriteBlobMissingFileIsAnError(t *testing.T) {
	ctx := context.Background()
	absent := filepath.Join(t.TempDir(), "absent.db")
	if _, err := WriteBlob(ctx, absent, "t", "data", RowValues{"id": int64(1)}, []byte{1}); err == nil {
		t.Fatal("expected writing to a missing file to fail")
	}
}

// A view cannot be written, and the refusal must come from SQLite rather than
// from this package guessing at the schema.
func TestWriteBlobRefusesAView(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "blob.db", blobFixture+` CREATE VIEW v AS SELECT * FROM files;`)
	if _, err := WriteBlob(ctx, path, "v", "data", RowValues{"id": int64(1)}, []byte{1}); err == nil {
		t.Fatal("expected writing through a view to fail")
	}
}

// ── Sniffing ───────────────────────────────────────────────────────────────

// The preview needs to know whether a blob is an image before it tries to
// render one; the check is by MAGIC BYTES, never by file extension or column
// name, because a blob has neither.
func TestSniffBlobMediaType(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0}
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"png", png, "image/png"},
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0}, "image/jpeg"},
		{"gif87", []byte("GIF87a"), "image/gif"},
		{"gif89", []byte("GIF89a"), "image/gif"},
		{"webp", append([]byte("RIFF"), append([]byte{0, 0, 0, 0}, []byte("WEBP")...)...), "image/webp"},
		{"pdf", []byte("%PDF-1.7"), "application/pdf"},
		{"gzip", []byte{0x1f, 0x8b, 0x08, 0x00}, "application/gzip"},
		{"zip", []byte{'P', 'K', 0x03, 0x04}, "application/zip"},
		{"plain", []byte("just some text"), "application/octet-stream"},
		{"empty", []byte{}, "application/octet-stream"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SniffBlobMediaType(c.data); got != c.want {
				t.Fatalf("SniffBlobMediaType = %q, want %q", got, c.want)
			}
		})
	}
}

// SVG is text, and a text/svg+xml response rendered inline is a same-origin
// script execution vector. It must be reported as a generic download.
func TestSniffBlobMediaTypeRefusesToGuessSVG(t *testing.T) {
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	if got := SniffBlobMediaType(svg); got == "image/svg+xml" {
		t.Fatal("SVG must not be sniffed as an inline image; it is a script vector")
	}
}

func TestIsInlineSafeMediaType(t *testing.T) {
	for _, safe := range []string{"image/png", "image/jpeg", "image/gif", "image/webp"} {
		if !IsInlineSafeMediaType(safe) {
			t.Fatalf("%s should be inline-safe", safe)
		}
	}
	for _, unsafe := range []string{
		"application/pdf", "application/octet-stream", "application/zip",
		"text/html", "image/svg+xml", "application/gzip",
	} {
		if IsInlineSafeMediaType(unsafe) {
			t.Fatalf("%s must NOT be inline-safe (script/markup or download-only)", unsafe)
		}
	}
}
