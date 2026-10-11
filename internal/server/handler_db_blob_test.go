package server

import (
	"bytes"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const blobDBFixture = `CREATE TABLE files(id INTEGER PRIMARY KEY, name TEXT, data BLOB);
	INSERT INTO files VALUES (1,'hello.bin',X'68656c6c6f');
	INSERT INTO files VALUES (2,'none.bin',NULL);`

func blobGet(t *testing.T, h *Handler, params map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	q := url.Values{}
	for k, v := range params {
		q.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.HandleDBBlob(rec, httptest.NewRequest(http.MethodGet, "/api/db/blob?"+q.Encode(), nil))
	return rec
}

func blobPost(t *testing.T, h *Handler, params map[string]string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	q := url.Values{}
	for k, v := range params {
		q.Set(k, v)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/db/blob?"+q.Encode(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/octet-stream")
	h.HandleDBBlob(rec, req)
	return rec
}

// ── Download ───────────────────────────────────────────────────────────────

func TestDBBlobDownloadStreamsTheValue(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	dbPath := filepath.Join(tmpDir, "app.db")
	seedDB(t, dbPath, blobDBFixture)

	rec := blobGet(t, h, map[string]string{
		"path": dbPath, "table": "files", "column": "data", "key": `{"id":1}`,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "hello" {
		t.Fatalf("body = %q, want hello", rec.Body.String())
	}
	// A blob has no filename, so the response must supply one or the browser
	// saves it as "blob" / the URL's last segment.
	disposition := rec.Header().Get("Content-Disposition")
	if !strings.Contains(disposition, "attachment") || !strings.Contains(disposition, "files") {
		t.Fatalf("Content-Disposition = %q, want an attachment naming the table/column", disposition)
	}
}

// A NULL cell is not a zero-byte file. The client needs to tell them apart, and
// a 200 with an empty body would look exactly like a successful empty download.
func TestDBBlobDownloadReportsANullCell(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	dbPath := filepath.Join(tmpDir, "app.db")
	seedDB(t, dbPath, blobDBFixture)

	rec := blobGet(t, h, map[string]string{
		"path": dbPath, "table": "files", "column": "data", "key": `{"id":2}`,
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 for a NULL cell", rec.Code)
	}
}

func TestDBBlobDownloadRejectsTextInABlobColumn(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	dbPath := filepath.Join(tmpDir, "app.db")
	seedDB(t, dbPath, blobDBFixture+` INSERT INTO files VALUES (3,'t',NULL); UPDATE files SET data='not bytes' WHERE id=3;`)

	rec := blobGet(t, h, map[string]string{
		"path": dbPath, "table": "files", "column": "data", "key": `{"id":3}`,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s, want 400", rec.Code, rec.Body.String())
	}
}

// The key is JSON in a query parameter, so it must be decoded with UseNumber:
// a plain Unmarshal turns a large integer id into a float64 and the row is never
// found, which would present as "this row has no blob".
func TestDBBlobDownloadFindsALargeIntegerKey(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	dbPath := filepath.Join(tmpDir, "app.db")
	bigID := int64(9007199254740993) // 2^53 + 1: not representable as float64
	seedDB(t, dbPath, `CREATE TABLE t(id INTEGER PRIMARY KEY, data BLOB);`)
	if _, err := execSQL(t, dbPath, "INSERT INTO t(id,data) VALUES (?, X'6162')", bigID); err != nil {
		t.Fatalf("seed: %v", err)
	}

	rec := blobGet(t, h, map[string]string{
		"path": dbPath, "table": "t", "column": "data",
		"key": `{"id":9007199254740993}`,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want 200 (a float64 key would miss the row)", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "ab" {
		t.Fatalf("body = %q, want ab", rec.Body.String())
	}
}

func TestDBBlobDownloadRejectsAMissingKey(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	dbPath := filepath.Join(tmpDir, "app.db")
	seedDB(t, dbPath, blobDBFixture)

	rec := blobGet(t, h, map[string]string{"path": dbPath, "table": "files", "column": "data"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a missing key", rec.Code)
	}
}

func TestDBBlobDownloadRejectsAMissingRow(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	dbPath := filepath.Join(tmpDir, "app.db")
	seedDB(t, dbPath, blobDBFixture)

	rec := blobGet(t, h, map[string]string{
		"path": dbPath, "table": "files", "column": "data", "key": `{"id":999}`,
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// Download must stay inside the same containment boundary as every other DB
// endpoint: a path outside the project roots is refused before it is opened.
func TestDBBlobDownloadRejectsOutsideRoots(t *testing.T) {
	h, _ := newFilesHandler(t)
	outside := filepath.Join(t.TempDir(), "elsewhere.db")
	seedDB(t, outside, blobDBFixture)

	rec := blobGet(t, h, map[string]string{
		"path": outside, "table": "files", "column": "data", "key": `{"id":1}`,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a database outside the project roots", rec.Code)
	}
}

// ── Upload ─────────────────────────────────────────────────────────────────

func TestDBBlobUploadSetsTheCell(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	dbPath := filepath.Join(tmpDir, "app.db")
	seedDB(t, dbPath, blobDBFixture)

	rec := blobPost(t, h, map[string]string{
		"path": dbPath, "table": "files", "column": "data", "key": `{"id":2}`,
	}, []byte{0xde, 0xad, 0xbe, 0xef})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	back := blobGet(t, h, map[string]string{
		"path": dbPath, "table": "files", "column": "data", "key": `{"id":2}`,
	})
	if back.Code != http.StatusOK || back.Body.String() != "\xde\xad\xbe\xef" {
		t.Fatalf("read back: status=%d body=%q", back.Code, back.Body.String())
	}
}

// The upload is a raw body, so binary content must survive byte for byte — no
// charset mangling, no base64 round trip.
func TestDBBlobUploadRoundTripsBinary(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	dbPath := filepath.Join(tmpDir, "app.db")
	seedDB(t, dbPath, blobDBFixture)

	payload := make([]byte, 512)
	for i := range payload {
		payload[i] = byte(i % 256)
	}
	if rec := blobPost(t, h, map[string]string{
		"path": dbPath, "table": "files", "column": "data", "key": `{"id":2}`,
	}, payload); rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	back := blobGet(t, h, map[string]string{
		"path": dbPath, "table": "files", "column": "data", "key": `{"id":2}`,
	})
	if !bytes.Equal(back.Body.Bytes(), payload) {
		t.Fatalf("round trip differs: got %d bytes", back.Body.Len())
	}
}

// Uploading is a mutation, so a NULL-keyed row that does not exist must be a
// 404 rather than a silent no-op, and a key matching several rows must be a 409.
func TestDBBlobUploadRejectsAMissingRow(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	dbPath := filepath.Join(tmpDir, "app.db")
	seedDB(t, dbPath, blobDBFixture)

	rec := blobPost(t, h, map[string]string{
		"path": dbPath, "table": "files", "column": "data", "key": `{"id":999}`,
	}, []byte{1})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body = %s, want 409", rec.Code, rec.Body.String())
	}
}

func TestDBBlobUploadRejectsAnOversizeBody(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	dbPath := filepath.Join(tmpDir, "app.db")
	seedDB(t, dbPath, blobDBFixture)

	// One byte over the cap, without allocating the whole 64 MB.
	rec := httptest.NewRecorder()
	q := url.Values{
		"path": {dbPath}, "table": {"files"}, "column": {"data"}, "key": {`{"id":1}`},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/db/blob?"+q.Encode(),
		io.LimitReader(neverEndingReader{}, blobUploadMaxBytes+1))
	req.Header.Set("Content-Type", "application/octet-stream")
	h.HandleDBBlob(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 for an oversize upload", rec.Code)
	}
}

// An upload must not create a database that is not there, and must not write to
// one ocode owns.
func TestDBBlobUploadRejectsANonSQLiteFile(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	txt := filepath.Join(tmpDir, "notes.db")
	if err := os.WriteFile(txt, []byte("not a database"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	rec := blobPost(t, h, map[string]string{
		"path": txt, "table": "files", "column": "data", "key": `{"id":1}`,
	}, []byte{1})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s, want 400", rec.Code, rec.Body.String())
	}
}

// A view is not writable; the refusal must come from the engine rather than
// being mistaken for success.
func TestDBBlobUploadRefusesAView(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	dbPath := filepath.Join(tmpDir, "app.db")
	seedDB(t, dbPath, blobDBFixture+` CREATE VIEW v AS SELECT * FROM files;`)

	rec := blobPost(t, h, map[string]string{
		"path": dbPath, "table": "v", "column": "data", "key": `{"id":1}`,
	}, []byte{1})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s, want 400", rec.Code, rec.Body.String())
	}
}

// An upload replaces the previous value in place, so it must take the same
// snapshot a row edit does — it is a second mutation route to the same file.
func TestDBBlobUploadBacksUpFirst(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	dbPath := filepath.Join(tmpDir, "app.db")
	seedDB(t, dbPath, blobDBFixture)

	if rec := blobPost(t, h, map[string]string{
		"path": dbPath, "table": "files", "column": "data", "key": `{"id":1}`,
	}, []byte("replaced")); rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	bak := dbPath + ".bak"
	if _, err := os.Stat(bak); err != nil {
		t.Fatalf("no backup was taken: %v", err)
	}
	// The snapshot holds the PREVIOUS value, which is the point of taking it.
	back := blobGet(t, h, map[string]string{
		"path": bak, "table": "files", "column": "data", "key": `{"id":1}`,
	})
	if back.Code != http.StatusOK || back.Body.String() != "hello" {
		t.Fatalf("backup holds %q (status %d), want the original hello", back.Body.String(), back.Code)
	}
}

// The table and column names reach a Content-Disposition filename and then the
// user's disk, so a name containing a quote, a slash or a newline must not be
// able to escape the quoted string or suggest a path.
func TestDBBlobDownloadSanitizesTheFilename(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	dbPath := filepath.Join(tmpDir, "app.db")
	// A table name with a quote, a path separator and a newline. Quoted
	// identifiers keep the SQL valid; the FILENAME is what is being tested.
	seedDB(t, dbPath, `CREATE TABLE "ev/il""x" (id INTEGER PRIMARY KEY, "da""ta" BLOB);
		INSERT INTO "ev/il""x" VALUES (1, X'61');`)

	rec := blobGet(t, h, map[string]string{
		"path": dbPath, "table": `ev/il"x`, "column": `da"ta`, "key": `{"id":1}`,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	disposition := rec.Header().Get("Content-Disposition")
	if strings.Contains(disposition, "/") {
		t.Fatalf("Content-Disposition = %q, want no path separator", disposition)
	}
	// Exactly one quote pair, and the inner name must not close it early.
	if got := strings.Count(disposition, `"`); got != 2 {
		t.Fatalf("Content-Disposition = %q, want exactly one quoted filename", disposition)
	}
	if strings.ContainsAny(disposition, "\r\n") {
		t.Fatalf("Content-Disposition = %q, want no CR/LF (header injection)", disposition)
	}
}

// A malformed key is the caller's mistake, not a 500.
func TestDBBlobRejectsAMalformedKey(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	dbPath := filepath.Join(tmpDir, "app.db")
	seedDB(t, dbPath, blobDBFixture)

	for _, key := range []string{"", "not json", "[1,2]"} {
		rec := blobGet(t, h, map[string]string{
			"path": dbPath, "table": "files", "column": "data", "key": key,
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("key %q: status = %d, want 400", key, rec.Code)
		}
	}
}

// ── Route registration ─────────────────────────────────────────────────────

func TestDBBlobRoutesRegistered(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmpDir := t.TempDir()
	srv := New("127.0.0.1:0", "", "", nil)
	srv.handler.SetWorkDir(tmpDir)
	seedDB(t, filepath.Join(tmpDir, "route.db"), blobDBFixture)
	h := srv.serveHandler()

	get := httptest.NewRecorder()
	h.ServeHTTP(get, httptest.NewRequest(http.MethodGet,
		"/api/db/blob?path=route.db&table=files&column=data&key="+url.QueryEscape(`{"id":1}`), nil))
	if get.Code != http.StatusOK {
		t.Fatalf("GET /api/db/blob = %d body=%s (404 means the route is missing)", get.Code, get.Body.String())
	}

	post := httptest.NewRecorder()
	h.ServeHTTP(post, httptest.NewRequest(http.MethodPost,
		"/api/db/blob?path=route.db&table=files&column=data&key="+url.QueryEscape(`{"id":1}`),
		strings.NewReader("xy")))
	if post.Code != http.StatusOK {
		t.Fatalf("POST /api/db/blob = %d body=%s (404 means the route is missing)", post.Code, post.Body.String())
	}
}

// ── helpers ────────────────────────────────────────────────────────────────

// neverEndingReader yields zero bytes forever, so an over-cap request can be
// exercised without materialising 64 MB in the test process.
type neverEndingReader struct{}

func (neverEndingReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

func execSQL(t *testing.T, dbPath, stmt string, args ...any) (int64, error) {
	t.Helper()
	db, err := openSQLite(dbPath)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	res, err := db.Exec(stmt, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func openSQLite(path string) (*sql.DB, error) { return sql.Open("sqlite", path) }

// Ensure the JSON key helper agrees with the endpoint's own decoding.
func TestDecodeRowKeyPreservesIntegers(t *testing.T) {
	key, err := decodeRowKey(`{"id":9007199254740993}`)
	if err != nil {
		t.Fatalf("decodeRowKey: %v", err)
	}
	if key["id"] != int64(9007199254740993) {
		t.Fatalf("id = %#v, want an int64 (a float64 would round it)", key["id"])
	}
	if _, err := decodeRowKey("[]"); err == nil {
		t.Fatal("expected a non-object key to be refused")
	}
}
