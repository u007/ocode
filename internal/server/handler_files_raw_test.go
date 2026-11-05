package server

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newRawSaveRequest(t *testing.T, path string, body []byte, ct string, q url.Values) *http.Request {
	t.Helper()
	u := "/api/files/raw?path=" + url.QueryEscape(path)
	if q != nil {
		u += "&" + q.Encode()
	}
	r := httptest.NewRequest("PUT", u, bytes.NewReader(body))
	if ct != "" {
		r.Header.Set("Content-Type", ct)
	}
	return r
}

func TestSaveFileRawHappyPathWritesBytesAndCreatesBak(t *testing.T) {
	h, tmpDir := newSaveHandler(t)
	path := filepath.Join(tmpDir, "book.xlsx")
	if err := os.WriteFile(path, []byte("PK\x03\x04old"), 0644); err != nil {
		t.Fatal(err)
	}
	// expected_hash is the SHA-256 hex of the exact bytes on disk.
	expected := rawContentHash([]byte("PK\x03\x04old"))
	body := []byte("PK\x03\x04new-bytes")
	r := newRawSaveRequest(t, path, body, "application/octet-stream", url.Values{"expected_hash": {expected}})
	w := httptest.NewRecorder()
	h.HandleSaveFileRaw(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, body) {
		t.Fatalf("file content = %q, want %q", data, body)
	}
	bak, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("expected .bak of existing file: %v", err)
	}
	if string(bak) != "PK\x03\x04old" {
		t.Fatalf(".bak = %q, want old bytes", bak)
	}
	// Second save must not overwrite the existing .bak.
	if err := os.WriteFile(path, []byte("second-prev"), 0644); err != nil {
		t.Fatal(err)
	}
	expected2 := rawContentHash([]byte("second-prev"))
	r2 := newRawSaveRequest(t, path, []byte("second"), "application/octet-stream", url.Values{"expected_hash": {expected2}})
	w2 := httptest.NewRecorder()
	h.HandleSaveFileRaw(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("second save expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	bak2, _ := os.ReadFile(path + ".bak")
	if string(bak2) != "PK\x03\x04old" {
		t.Fatalf(".bak was overwritten: %q", bak2)
	}
}

func TestSaveFileRawNewFileNoBak(t *testing.T) {
	h, tmpDir := newSaveHandler(t)
	path := filepath.Join(tmpDir, "new.XLSX")                // case-insensitive gate
	r := newRawSaveRequest(t, path, []byte("data"), "", nil) // missing CT treated as octet-stream
	w := httptest.NewRecorder()
	h.HandleSaveFileRaw(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("no .bak expected for new file, stat err = %v", err)
	}
}

func TestSaveFileRawHashMismatch409(t *testing.T) {
	h, tmpDir := newSaveHandler(t)
	path := filepath.Join(tmpDir, "book.xlsx")
	if err := os.WriteFile(path, []byte("on-disk-now"), 0644); err != nil {
		t.Fatal(err)
	}
	stale := rawContentHash([]byte("opened-earlier"))
	r := newRawSaveRequest(t, path, []byte("new"), "application/octet-stream", url.Values{"expected_hash": {stale}})
	w := httptest.NewRecorder()
	h.HandleSaveFileRaw(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	data, _ := os.ReadFile(path)
	if string(data) != "on-disk-now" {
		t.Fatalf("file should not be overwritten, got %q", data)
	}
}

func TestSaveFileRawForceOneOverwrites(t *testing.T) {
	h, tmpDir := newSaveHandler(t)
	path := filepath.Join(tmpDir, "book.xlsx")
	if err := os.WriteFile(path, []byte("on-disk-now"), 0644); err != nil {
		t.Fatal(err)
	}
	stale := rawContentHash([]byte("opened-earlier"))
	r := newRawSaveRequest(t, path, []byte("forced"), "application/octet-stream", url.Values{"expected_hash": {stale}, "force": {"1"}})
	w := httptest.NewRecorder()
	h.HandleSaveFileRaw(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with force=1, got %d: %s", w.Code, w.Body.String())
	}
	data, _ := os.ReadFile(path)
	if string(data) != "forced" {
		t.Fatalf("expected forced content, got %q", data)
	}
}

func TestSaveFileRawExistingFileRequiresExpectedHash(t *testing.T) {
	h, tmpDir := newSaveHandler(t)
	path := filepath.Join(tmpDir, "book.xlsx")
	if err := os.WriteFile(path, []byte("on-disk-now"), 0644); err != nil {
		t.Fatal(err)
	}
	r := newRawSaveRequest(t, path, []byte("new"), "application/octet-stream", nil)
	w := httptest.NewRecorder()
	h.HandleSaveFileRaw(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without expected_hash, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "expected_hash required unless force=1") {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
	if data, _ := os.ReadFile(path); string(data) != "on-disk-now" {
		t.Fatalf("file must not be overwritten without expected_hash, got %q", data)
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("no .bak may be written on a refused save, stat err = %v", err)
	}

	// force=1 is the explicit opt-out and needs no hash.
	r2 := newRawSaveRequest(t, path, []byte("forced"), "application/octet-stream", url.Values{"force": {"1"}})
	w2 := httptest.NewRecorder()
	h.HandleSaveFileRaw(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 with force=1 and no hash, got %d: %s", w2.Code, w2.Body.String())
	}
	if data, _ := os.ReadFile(path); string(data) != "forced" {
		t.Fatalf("expected forced content, got %q", data)
	}
}

func TestSaveFileRawKeepsPermissionBits(t *testing.T) {
	h, tmpDir := newSaveHandler(t)
	for _, tc := range []struct {
		name string
		mode os.FileMode
	}{{"0644", 0644}, {"0640", 0640}} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(tmpDir, "book-"+tc.name+".xlsx")
			if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			// Chmod explicitly: WriteFile's mode is filtered by the umask.
			if err := os.Chmod(path, tc.mode); err != nil {
				t.Fatal(err)
			}
			r := newRawSaveRequest(t, path, []byte("new"), "application/octet-stream", url.Values{"expected_hash": {rawContentHash([]byte("old"))}})
			w := httptest.NewRecorder()
			h.HandleSaveFileRaw(w, r)
			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != tc.mode {
				t.Fatalf("saved file has mode %v, want %v", info.Mode().Perm(), tc.mode)
			}
		})
	}
}

func TestSaveFileRawNewFileGetsOwnerOnlyMode(t *testing.T) {
	h, tmpDir := newSaveHandler(t)
	path := filepath.Join(tmpDir, "new.xlsx")
	r := newRawSaveRequest(t, path, []byte("data"), "application/octet-stream", nil)
	w := httptest.NewRecorder()
	h.HandleSaveFileRaw(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Same mode HandleSaveFileContent creates new files with.
	if info.Mode().Perm() != 0600 {
		t.Fatalf("new file mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestSaveFileRawNonXlsxGate400(t *testing.T) {
	h, tmpDir := newSaveHandler(t)
	for _, name := range []string{"a.xls", "a.xlsm", "a.csv", "a.txt", "a"} {
		path := filepath.Join(tmpDir, name)
		r := newRawSaveRequest(t, path, []byte("x"), "application/octet-stream", nil)
		w := httptest.NewRecorder()
		h.HandleSaveFileRaw(w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d: %s", name, w.Code, w.Body.String())
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s: file must not be created", name)
		}
	}
}

func TestSaveFileRawWrongContentType400(t *testing.T) {
	h, tmpDir := newSaveHandler(t)
	path := filepath.Join(tmpDir, "book.xlsx")
	r := newRawSaveRequest(t, path, []byte("x"), "application/json", nil)
	w := httptest.NewRecorder()
	h.HandleSaveFileRaw(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file must not be created on wrong content-type")
	}
}

func TestSaveFileRawSizeCap413(t *testing.T) {
	h, tmpDir := newSaveHandler(t)
	path := filepath.Join(tmpDir, "big.xlsx")
	big := make([]byte, previewRawMaxBytes+1)
	r := newRawSaveRequest(t, path, big, "application/octet-stream", nil)
	w := httptest.NewRecorder()
	h.HandleSaveFileRaw(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file must not be created on over-cap body")
	}
}

func TestSaveFileRawPathTraversal400(t *testing.T) {
	h, tmpDir := newSaveHandler(t)
	outside := filepath.Join(filepath.Dir(tmpDir), "evil.xlsx")
	// Absolute path outside the scope root.
	r := newRawSaveRequest(t, outside, []byte("x"), "application/octet-stream", nil)
	w := httptest.NewRecorder()
	h.HandleSaveFileRaw(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	// Relative traversal escape.
	r2 := newRawSaveRequest(t, "../evil.xlsx", []byte("x"), "application/octet-stream", nil)
	w2 := httptest.NewRecorder()
	h.HandleSaveFileRaw(w2, r2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("relative traversal: expected 400, got %d: %s", w2.Code, w2.Body.String())
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(tmpDir), "evil.xlsx")); !os.IsNotExist(err) {
		t.Fatalf("outside file must not be created")
	}
}

func TestSaveFileRawSymlinkFinalComponent400(t *testing.T) {
	h, tmpDir := newSaveHandler(t)
	target := filepath.Join(tmpDir, "real.xlsx")
	if err := os.WriteFile(target, []byte("real"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(tmpDir, "link.xlsx")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	r := newRawSaveRequest(t, link, []byte("evil"), "application/octet-stream", nil)
	w := httptest.NewRecorder()
	h.HandleSaveFileRaw(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	data, _ := os.ReadFile(target)
	if string(data) != "real" {
		t.Fatalf("symlink target must not be modified, got %q", data)
	}
	// The refusal must precede the .bak: under the old ordering a backup of
	// the link's target was written to link.xlsx.bak before the 400.
	for _, p := range []string{link + ".bak", target + ".bak"} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Fatalf("no backup may be written before a symlink refusal: %s (err %v)", p, err)
		}
	}
}

func TestRawContentHashIsSHA256Hex(t *testing.T) {
	// Vectors are mirrored in web/src/components/Preview/univerSerialize.test.ts
	// (describe "rawContentHash"). Change both or neither.
	cases := []struct {
		name string
		in   []byte
		want string
	}{
		{"empty", []byte{}, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{"abc (FIPS 180-4)", []byte("abc"), "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"},
		{"invalid UTF-8 0xff", []byte{0xff, 0xfe, 0x00}, "ba778c0261008c8f71ae4061ad0162ffcbe63b52c91f89f236738131d1217ec7"},
		{"invalid UTF-8 0xfd", []byte{0xfd, 0xfe, 0x00}, "7af5f0e495c9b5a16918176e41bec43c582443617f9daf3c35d5730cf4c4d6ab"},
	}
	for _, tc := range cases {
		if got := rawContentHash(tc.in); got != tc.want {
			t.Errorf("rawContentHash(%s) = %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestSaveFileRawHashIsByteExact(t *testing.T) {
	h, tmpDir := newSaveHandler(t)
	onDisk := []byte{0xff, 0x01}
	other := []byte{0xfe, 0x01}
	// Precondition: the rune-based hashContent cannot tell these apart, which
	// is exactly why the raw guard must not use it.
	if hashContent(string(onDisk)) != hashContent(string(other)) {
		t.Fatalf("precondition failed: hashContent should collide on these bytes")
	}
	path := filepath.Join(tmpDir, "book.xlsx")
	if err := os.WriteFile(path, onDisk, 0644); err != nil {
		t.Fatal(err)
	}
	r := newRawSaveRequest(t, path, []byte("new"), "application/octet-stream", url.Values{"expected_hash": {rawContentHash(other)}})
	w := httptest.NewRecorder()
	h.HandleSaveFileRaw(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 for a different byte string, got %d: %s", w.Code, w.Body.String())
	}
	data, _ := os.ReadFile(path)
	if !bytes.Equal(data, onDisk) {
		t.Fatalf("file should not be overwritten, got %q", data)
	}
}

func TestSaveFileRawBackupWriteFailureAbortsSave(t *testing.T) {
	h, tmpDir := newSaveHandler(t)
	path := filepath.Join(tmpDir, "book.xlsx")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	orig := writeRawBackup
	writeRawBackup = func(string, []byte, os.FileMode) error { return errors.New("disk full") }
	t.Cleanup(func() { writeRawBackup = orig })

	r := newRawSaveRequest(t, path, []byte("new"), "application/octet-stream", url.Values{"expected_hash": {rawContentHash([]byte("old"))}})
	w := httptest.NewRecorder()
	h.HandleSaveFileRaw(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when the backup cannot be written, got %d: %s", w.Code, w.Body.String())
	}
	data, _ := os.ReadFile(path)
	if string(data) != "old" {
		t.Fatalf("target must not be overwritten without a backup, got %q", data)
	}
	leftovers, _ := filepath.Glob(filepath.Join(tmpDir, ".raw-save-*.tmp"))
	if len(leftovers) != 0 {
		t.Fatalf("temp file left behind after aborted save: %v", leftovers)
	}
}

func TestSaveFileRawBackupStatErrorAbortsSave(t *testing.T) {
	h, tmpDir := newSaveHandler(t)
	// 252-byte name: the target fits NAME_MAX (255) but "<name>.bak" does not,
	// so Lstat(bak) fails with ENAMETOOLONG rather than reporting not-exist.
	path := filepath.Join(tmpDir, strings.Repeat("a", 247)+".xlsx")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path + ".bak"); err == nil || os.IsNotExist(err) {
		t.Fatalf("precondition failed: want a non-not-exist Lstat error, got %v", err)
	}
	r := newRawSaveRequest(t, path, []byte("new"), "application/octet-stream", url.Values{"expected_hash": {rawContentHash([]byte("old"))}})
	w := httptest.NewRecorder()
	h.HandleSaveFileRaw(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when the backup cannot be inspected, got %d: %s", w.Code, w.Body.String())
	}
	data, _ := os.ReadFile(path)
	if string(data) != "old" {
		t.Fatalf("target must not be overwritten when the backup state is unknown, got %q", data)
	}
}

func TestSaveFileRawSymlinkBackupRefused(t *testing.T) {
	h, tmpDir := newSaveHandler(t)
	path := filepath.Join(tmpDir, "book.xlsx")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "untouched.bin")
	if err := os.WriteFile(outside, []byte("untouched"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path+".bak"); err != nil {
		t.Fatal(err)
	}
	r := newRawSaveRequest(t, path, []byte("new"), "application/octet-stream", url.Values{"expected_hash": {rawContentHash([]byte("old"))}})
	w := httptest.NewRecorder()
	h.HandleSaveFileRaw(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a symlinked .bak, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "back up through a symlink") {
		t.Fatalf("expected the backup-symlink refusal, got %s", w.Body.String())
	}
	if data, _ := os.ReadFile(outside); string(data) != "untouched" {
		t.Fatalf("symlinked .bak target must not be written, got %q", data)
	}
	if data, _ := os.ReadFile(path); string(data) != "old" {
		t.Fatalf("target must not be saved when the backup is refused, got %q", data)
	}
}

// failingBody is a request body whose read fails with a non-size error.
type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, errors.New("connection reset by peer") }
func (failingBody) Close() error             { return nil }

func TestSaveFileRawBodyReadErrorIs400(t *testing.T) {
	h, tmpDir := newSaveHandler(t)
	path := filepath.Join(tmpDir, "book.xlsx")
	r := newRawSaveRequest(t, path, nil, "application/octet-stream", nil)
	r.Body = failingBody{}
	w := httptest.NewRecorder()
	h.HandleSaveFileRaw(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a broken body (not a size error), got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "failed to read request body") {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file must not be created on a failed body read")
	}
}

// newRemoteRawSaveRequest builds a raw save for a remote project: ?host=
// routes it to the transport and project_root names the registered root.
func newRemoteRawSaveRequest(t *testing.T, host, root, path string, body []byte, q url.Values) *http.Request {
	t.Helper()
	if q == nil {
		q = url.Values{}
	}
	q.Set("host", host)
	q.Set("project_root", root)
	return newRawSaveRequest(t, path, body, "application/octet-stream", q)
}

func TestSaveFileRawRemoteBacksUpOnceAndSaves(t *testing.T) {
	installFakeSSH(t)
	root := t.TempDir()
	path := filepath.Join(root, "book.xlsx")
	if err := os.WriteFile(path, []byte("PK old"), 0644); err != nil {
		t.Fatal(err)
	}
	h := newTestHandlerWithRemote(t, "ci.local", root)
	w := httptest.NewRecorder()
	h.HandleSaveFileRaw(w, newRemoteRawSaveRequest(t, "ci.local", root, "book.xlsx", []byte("PK new"),
		url.Values{"expected_hash": {rawContentHash([]byte("PK old"))}}))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if data, _ := os.ReadFile(path); string(data) != "PK new" {
		t.Fatalf("remote file = %q, want new bytes", data)
	}
	if bak, _ := os.ReadFile(path + ".bak"); string(bak) != "PK old" {
		t.Fatalf("remote .bak = %q, want old bytes", bak)
	}

	// A second save must keep the first backup.
	if err := os.WriteFile(path, []byte("PK mid"), 0644); err != nil {
		t.Fatal(err)
	}
	w2 := httptest.NewRecorder()
	h.HandleSaveFileRaw(w2, newRemoteRawSaveRequest(t, "ci.local", root, "book.xlsx", []byte("PK newer"),
		url.Values{"expected_hash": {rawContentHash([]byte("PK mid"))}}))
	if w2.Code != http.StatusOK {
		t.Fatalf("second save expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	if bak, _ := os.ReadFile(path + ".bak"); string(bak) != "PK old" {
		t.Fatalf("remote .bak was overwritten: %q", bak)
	}
}

// A remote save replaces the file by rename, like the local save: the inode
// changes, the existing mode is kept, and no temp file is left in the folder.
func TestSaveFileRawRemoteReplacesByRenameKeepsModeAndNoTemp(t *testing.T) {
	installFakeSSH(t)
	root := t.TempDir()
	path := filepath.Join(root, "book.xlsx")
	if err := os.WriteFile(path, []byte("PK old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	h := newTestHandlerWithRemote(t, "ci.local", root)
	w := httptest.NewRecorder()
	h.HandleSaveFileRaw(w, newRemoteRawSaveRequest(t, "ci.local", root, "book.xlsx", []byte("PK new"),
		url.Values{"expected_hash": {rawContentHash([]byte("PK old"))}}))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Fatal("remote save wrote in place; want a temp file renamed over the target")
	}
	if after.Mode().Perm() != 0644 {
		t.Fatalf("remote save mode = %v, want 0644 kept from the target", after.Mode().Perm())
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".raw-save-") {
			t.Fatalf("temp file left behind after remote save: %s", e.Name())
		}
	}
}

func TestSaveFileRawRemoteHashMismatch409(t *testing.T) {
	installFakeSSH(t)
	root := t.TempDir()
	path := filepath.Join(root, "book.xlsx")
	if err := os.WriteFile(path, []byte("on-disk-now"), 0644); err != nil {
		t.Fatal(err)
	}
	h := newTestHandlerWithRemote(t, "ci.local", root)
	w := httptest.NewRecorder()
	h.HandleSaveFileRaw(w, newRemoteRawSaveRequest(t, "ci.local", root, "book.xlsx", []byte("new"),
		url.Values{"expected_hash": {rawContentHash([]byte("opened-earlier"))}}))
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if data, _ := os.ReadFile(path); string(data) != "on-disk-now" {
		t.Fatalf("remote file should not be overwritten, got %q", data)
	}
}

func TestSaveFileRawRemoteExistingFileRequiresExpectedHash(t *testing.T) {
	installFakeSSH(t)
	root := t.TempDir()
	path := filepath.Join(root, "book.xlsx")
	if err := os.WriteFile(path, []byte("on-disk-now"), 0644); err != nil {
		t.Fatal(err)
	}
	h := newTestHandlerWithRemote(t, "ci.local", root)
	w := httptest.NewRecorder()
	h.HandleSaveFileRaw(w, newRemoteRawSaveRequest(t, "ci.local", root, "book.xlsx", []byte("new"), nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without expected_hash, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "expected_hash required unless force=1") {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
	if data, _ := os.ReadFile(path); string(data) != "on-disk-now" {
		t.Fatalf("remote file must not be overwritten without expected_hash, got %q", data)
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("no remote .bak may be written on a refused save, stat err = %v", err)
	}
}

// A directory at the target makes the remote read fail with a non-missing
// error. That must be a 502, never the misleading 409 a missing file gets.
func TestSaveFileRawRemoteReadErrorIs502NotConflict(t *testing.T) {
	installFakeSSH(t)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "book.xlsx"), 0755); err != nil {
		t.Fatal(err)
	}
	h := newTestHandlerWithRemote(t, "ci.local", root)
	expected := rawContentHash([]byte("anything"))
	for _, q := range []url.Values{
		{"expected_hash": {expected}},
		{"expected_hash": {expected}, "force": {"1"}},
	} {
		w := httptest.NewRecorder()
		h.HandleSaveFileRaw(w, newRemoteRawSaveRequest(t, "ci.local", root, "book.xlsx", []byte("new"), q))
		if w.Code != http.StatusBadGateway {
			t.Fatalf("query %v: expected 502 for an unreadable remote target, got %d: %s", q, w.Code, w.Body.String())
		}
	}
}

func TestSaveFileRawRemoteSymlinkFinalComponentRefused(t *testing.T) {
	installFakeSSH(t)
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "real.xlsx")
	if err := os.WriteFile(target, []byte("real"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link.xlsx")); err != nil {
		t.Fatal(err)
	}
	h := newTestHandlerWithRemote(t, "ci.local", root)
	w := httptest.NewRecorder()
	h.HandleSaveFileRaw(w, newRemoteRawSaveRequest(t, "ci.local", root, "link.xlsx", []byte("evil"), nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if data, _ := os.ReadFile(target); string(data) != "real" {
		t.Fatalf("symlink target must not be modified, got %q", data)
	}
	for _, p := range []string{filepath.Join(root, "link.xlsx.bak"), target + ".bak"} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Fatalf("no backup may be written before a symlink refusal: %s (err %v)", p, err)
		}
	}
}

func TestSaveFileRawRemoteSymlinkedParentEscapeRefused(t *testing.T) {
	installFakeSSH(t)
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "linkdir")); err != nil {
		t.Fatal(err)
	}
	h := newTestHandlerWithRemote(t, "ci.local", root)
	w := httptest.NewRecorder()
	h.HandleSaveFileRaw(w, newRemoteRawSaveRequest(t, "ci.local", root, "linkdir/book.xlsx", []byte("evil"), nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a parent symlink escaping the root, got %d: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(outside, "book.xlsx")); !os.IsNotExist(err) {
		t.Fatalf("file must not be written outside the project root")
	}
}

func TestSaveFileRawRemoteBackupSymlinkRefused(t *testing.T) {
	installFakeSSH(t)
	root := t.TempDir()
	path := filepath.Join(root, "book.xlsx")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "untouched.bin")
	if err := os.WriteFile(outside, []byte("untouched"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path+".bak"); err != nil {
		t.Fatal(err)
	}
	h := newTestHandlerWithRemote(t, "ci.local", root)
	w := httptest.NewRecorder()
	h.HandleSaveFileRaw(w, newRemoteRawSaveRequest(t, "ci.local", root, "book.xlsx", []byte("new"),
		url.Values{"expected_hash": {rawContentHash([]byte("old"))}}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a symlinked remote .bak, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "back up through a symlink") {
		t.Fatalf("expected the backup-symlink refusal, got %s", w.Body.String())
	}
	if data, _ := os.ReadFile(outside); string(data) != "untouched" {
		t.Fatalf("symlinked .bak target must not be written, got %q", data)
	}
	if data, _ := os.ReadFile(path); string(data) != "old" {
		t.Fatalf("target must not be saved when the backup is refused, got %q", data)
	}
}
