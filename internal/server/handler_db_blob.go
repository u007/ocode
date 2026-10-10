package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/u007/ocode/internal/dbbrowse"
)

// blobUploadMaxBytes bounds an uploaded BLOB. The body is a raw octet stream
// (not base64 JSON), so this is the real byte size of the file; 64 MB covers
// icons, thumbnails, audio clips and small documents while keeping the server
// from buffering an unbounded upload in memory. It is deliberately generous and
// explicitly enforced — a missing cap is indistinguishable from a memory leak
// triggered by a stranger's curl.
const blobUploadMaxBytes = 64 << 20

// HandleDBBlob reads or writes ONE BLOB cell.
//
// GET  — streams the cell's full bytes. The grid's blob chip only carries the
//
//	first 8 KB of a value, so this is the only way to obtain the real one;
//	a NULL cell answers 204 (not 200-with-empty-body), because "nothing
//	stored" and "an empty file" are different and the client must not save
//	a zero-byte file for the former.
//
// POST — sets the cell from a raw body. This is a mutation, so it runs the same
//
//	containment and write guard as every other write path, and delegates the
//	statement to dbbrowse.WriteBlob so the exactly-one-row transaction is
//	the proven one.
//
// The row key arrives as a JSON object in the `key` query parameter (the same
// shape the grid already builds for row edits). It is decoded with
// json.Decoder.UseNumber so a large integer id stays an int64 — a plain
// Unmarshal would round it to a float64 and the row would never be found.
func (h *Handler) HandleDBBlob(w http.ResponseWriter, r *http.Request) {
	path, ok := h.resolveDBRequest(w, r)
	if !ok {
		return
	}
	table := r.URL.Query().Get("table")
	column := r.URL.Query().Get("column")
	if strings.TrimSpace(table) == "" || strings.TrimSpace(column) == "" {
		writeError(w, http.StatusBadRequest, "table and column are required")
		return
	}
	key, err := decodeRowKey(r.URL.Query().Get("key"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbQueryTimeout)
	defer cancel()

	if r.Method == http.MethodPost {
		// The body is the value; cap it BEFORE reading, so an oversize upload is
		// refused by the reader rather than after the whole thing is in memory.
		r.Body = http.MaxBytesReader(w, r.Body, blobUploadMaxBytes)
		if status, msg := h.dbWriteGuard(path); msg != "" {
			writeError(w, status, msg)
			return
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				writeError(w, http.StatusRequestEntityTooLarge,
					fmt.Sprintf("Blob is larger than the %d MB limit.", blobUploadMaxBytes>>20))
				return
			}
			writeError(w, http.StatusBadRequest, "could not read the upload: "+err.Error())
			return
		}
		// Back up before mutating, exactly like a row edit: an upload replaces
		// the previous value in place and there is no undo otherwise.
		if _, err := dbbrowse.Backup(ctx, path); err != nil {
			logDBError("backup before blob upload", path, err)
			writeError(w, http.StatusInternalServerError, "could not back up the database before writing: "+err.Error())
			return
		}
		res, err := dbbrowse.WriteBlob(ctx, path, table, column, key, data)
		if err != nil {
			writeBlobError(w, err, true)
			return
		}
		writeJSON(w, http.StatusOK, res)
		return
	}

	data, err := dbbrowse.ReadBlob(ctx, path, table, column, key)
	if err != nil {
		writeBlobError(w, err, false)
		return
	}
	if data == nil {
		// SQL NULL: no value at all, so there is nothing to stream. 204 tells the
		// client "the cell is empty" without pretending an empty file was read.
		w.WriteHeader(http.StatusNoContent)
		return
	}

	mediaType := dbbrowse.SniffBlobMediaType(data)
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Content-Length", fmt.Sprint(len(data)))
	// Always an attachment: even an image is being SAVED here, and the inline
	// preview path uses fetch + an object URL rather than this response's
	// disposition. Names are sanitized because table/column come from the client.
	filename := fmt.Sprintf("%s-%s%s", safeFilenamePart(table), safeFilenamePart(column), dbbrowse.BlobExtension(mediaType))
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	// A blob can be any bytes; forbid the browser from re-interpreting the type.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	// Best-effort: the bytes are already fully in memory, so a write error here
	// can only mean the client went away.
	_, _ = w.Write(data)
}

// writeBlobError maps a dbbrowse blob failure to a status. Branching on the
// sentinel (not the message) keeps the mapping stable if the wording changes.
//
// writing changes what "no row matched" means: for a READ it is a 404 (the
// thing you asked for is gone), but for a WRITE it is a 409 — the same
// optimistic-concurrency signal HandleDBRow returns, so the UI treats "the row
// moved under me" identically whether the change was a cell or a blob.
func writeBlobError(w http.ResponseWriter, err error, writing bool) {
	switch {
	case errors.Is(err, dbbrowse.ErrNoRowsAffected):
		if writing {
			writeError(w, http.StatusConflict, "No row matched — it may have changed or been deleted.")
			return
		}
		writeError(w, http.StatusNotFound, "No row matched — it may have changed or been deleted.")
	case errors.Is(err, dbbrowse.ErrMultipleRowsAffected):
		writeError(w, http.StatusConflict, "The key matched more than one row; refusing to guess which blob you meant.")
	case errors.Is(err, dbbrowse.ErrNotABlob):
		writeError(w, http.StatusBadRequest, "This cell does not hold a BLOB: "+err.Error())
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

// decodeRowKey parses the row key query parameter into driver-ready values.
//
// UseNumber is load-bearing: without it a JSON integer id beyond 2^53 becomes a
// float64 and the WHERE clause matches nothing, which the user experiences as
// "this row has no blob".
func decodeRowKey(raw string) (dbbrowse.RowValues, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("key is required")
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, errors.New("key must be a JSON object")
	}
	// Reject trailing content so `{"id":1}{"id":2}` is not silently half-read.
	if dec.More() {
		return nil, errors.New("key must be a single JSON object")
	}
	if len(m) == 0 {
		return nil, errors.New("key must name at least one column")
	}
	values, err := dbbrowse.NormalizeRow(m)
	if err != nil {
		return nil, err
	}
	return values, nil
}

// safeFilenamePart strips anything that could break out of the quoted filename
// or traverse a directory. A table name is user-controlled, and it ends up in a
// Content-Disposition header and on the user's disk.
func safeFilenamePart(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "._")
	if out == "" {
		return "blob"
	}
	return out
}
