package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/u007/ocode/internal/secretfile"
)

func newSecretHandler(t *testing.T) (*Handler, string) {
	t.Helper()
	tmpDir := t.TempDir()
	h := NewHandler()
	h.SetWorkDir(tmpDir)
	return h, tmpDir
}

func doJSON(t *testing.T, h *Handler, handler func(http.ResponseWriter, *http.Request), method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, path, reader)
	handler(w, r)
	return w
}

func TestHandleSecretInit_CreatesKey(t *testing.T) {
	h, dir := newSecretHandler(t)

	w := doJSON(t, h, h.HandleSecretInit, "POST", "/api/secret/init", secretInitRequest{
		Passphrase: "pw", ConfirmPassphrase: "pw",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(secretfile.ProjectKeyPath(dir)); err != nil {
		t.Fatalf("expected key file: %v", err)
	}
}

func TestHandleSecretInit_PassphraseMismatchFails(t *testing.T) {
	h, _ := newSecretHandler(t)

	w := doJSON(t, h, h.HandleSecretInit, "POST", "/api/secret/init", secretInitRequest{
		Passphrase: "pw1", ConfirmPassphrase: "pw2",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleSecretEncryptDecrypt_SingleFile_RoundTrip(t *testing.T) {
	h, dir := newSecretHandler(t)
	doJSON(t, h, h.HandleSecretInit, "POST", "/api/secret/init", secretInitRequest{
		Passphrase: "pw", ConfirmPassphrase: "pw",
	})

	file := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(file, []byte("top secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	w := doJSON(t, h, h.HandleSecretEncrypt, "POST", "/api/secret/encrypt", secretTransformRequest{
		Path: file, Passphrase: "pw", ConfirmPassphrase: "pw",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("encrypt: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !secretfile.IsEncrypted(data) {
		t.Fatal("expected file to be encrypted")
	}

	w = doJSON(t, h, h.HandleSecretDecrypt, "POST", "/api/secret/decrypt", secretTransformRequest{
		Path: file, Passphrase: "pw",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("decrypt: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "top secret" {
		t.Fatalf("content mismatch: %q", got)
	}
}

func TestHandleSecretEncrypt_PassphraseRetypeMismatchFails(t *testing.T) {
	h, dir := newSecretHandler(t)
	doJSON(t, h, h.HandleSecretInit, "POST", "/api/secret/init", secretInitRequest{
		Passphrase: "pw", ConfirmPassphrase: "pw",
	})
	file := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(file, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}

	w := doJSON(t, h, h.HandleSecretEncrypt, "POST", "/api/secret/encrypt", secretTransformRequest{
		Path: file, Passphrase: "pw", ConfirmPassphrase: "different",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if secretfile.IsEncrypted(data) {
		t.Fatal("file must remain plaintext on retype mismatch")
	}
}

func TestHandleSecretEncrypt_PathOutsideWorkDirRejected(t *testing.T) {
	h, _ := newSecretHandler(t)
	doJSON(t, h, h.HandleSecretInit, "POST", "/api/secret/init", secretInitRequest{
		Passphrase: "pw", ConfirmPassphrase: "pw",
	})

	outside := t.TempDir()
	file := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(file, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}

	w := doJSON(t, h, h.HandleSecretEncrypt, "POST", "/api/secret/encrypt", secretTransformRequest{
		Path: file, Passphrase: "pw", ConfirmPassphrase: "pw",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path outside workdir, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleSecretScan_ReportsFileCount(t *testing.T) {
	h, dir := newSecretHandler(t)
	doJSON(t, h, h.HandleSecretInit, "POST", "/api/secret/init", secretInitRequest{
		Passphrase: "pw", ConfirmPassphrase: "pw",
	})
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o600); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/secret/scan?path="+dir+"&mode=encrypt", nil)
	h.HandleSecretScan(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp["is_dir"].(bool) {
		t.Fatal("expected is_dir true")
	}
	if resp["file_count"].(float64) != 2 {
		t.Fatalf("expected file_count 2, got %v", resp["file_count"])
	}
}

func TestHandleSecretEncrypt_Dir_StartsJobAndPublishesProgress(t *testing.T) {
	h, dir := newSecretHandler(t)
	doJSON(t, h, h.HandleSecretInit, "POST", "/api/secret/init", secretInitRequest{
		Passphrase: "pw", ConfirmPassphrase: "pw",
	})
	fileA := filepath.Join(dir, "a.txt")
	fileB := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(fileA, []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileB, []byte("b"), 0o600); err != nil {
		t.Fatal(err)
	}

	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	w := doJSON(t, h, h.HandleSecretEncrypt, "POST", "/api/secret/encrypt", secretTransformRequest{
		Path: dir, Passphrase: "pw", ConfirmPassphrase: "pw",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp secretTransformResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != "started" || resp.JobID == "" || resp.Total != 2 {
		t.Fatalf("unexpected response: %+v", resp)
	}

	sawDone := false
	deadline := time.After(5 * time.Second)
	for !sawDone {
		select {
		case env := <-sub:
			if env.Event == "secret_done" {
				sawDone = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for secret_done event")
		}
	}

	for _, f := range []string{fileA, fileB} {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if !secretfile.IsEncrypted(data) {
			t.Fatalf("%s was not encrypted by the job", f)
		}
	}
}

func TestHandleSecretRekey_RoundTrip(t *testing.T) {
	h, dir := newSecretHandler(t)
	doJSON(t, h, h.HandleSecretInit, "POST", "/api/secret/init", secretInitRequest{
		Passphrase: "old-pw", ConfirmPassphrase: "old-pw",
	})
	file := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(file, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	doJSON(t, h, h.HandleSecretEncrypt, "POST", "/api/secret/encrypt", secretTransformRequest{
		Path: file, Passphrase: "old-pw", ConfirmPassphrase: "old-pw",
	})

	w := doJSON(t, h, h.HandleSecretRekey, "POST", "/api/secret/rekey", secretRekeyRequest{
		OldPassphrase: "old-pw", NewPassphrase: "new-pw", ConfirmNewPassphrase: "new-pw",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("rekey: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w = doJSON(t, h, h.HandleSecretDecrypt, "POST", "/api/secret/decrypt", secretTransformRequest{
		Path: file, Passphrase: "new-pw",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("decrypt with new passphrase: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleSecretRekey_WrongOldPassphraseFails(t *testing.T) {
	h, _ := newSecretHandler(t)
	doJSON(t, h, h.HandleSecretInit, "POST", "/api/secret/init", secretInitRequest{
		Passphrase: "old-pw", ConfirmPassphrase: "old-pw",
	})

	w := doJSON(t, h, h.HandleSecretRekey, "POST", "/api/secret/rekey", secretRekeyRequest{
		OldPassphrase: "wrong", NewPassphrase: "new-pw", ConfirmNewPassphrase: "new-pw",
	})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleSecretCancel_StopsRunningJob(t *testing.T) {
	h, dir := newSecretHandler(t)
	doJSON(t, h, h.HandleSecretInit, "POST", "/api/secret/init", secretInitRequest{
		Passphrase: "pw", ConfirmPassphrase: "pw",
	})
	for i := 0; i < 5; i++ {
		f := filepath.Join(dir, string(rune('a'+i))+".txt")
		if err := os.WriteFile(f, []byte("data"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	w := doJSON(t, h, h.HandleSecretEncrypt, "POST", "/api/secret/encrypt", secretTransformRequest{
		Path: dir, Passphrase: "pw", ConfirmPassphrase: "pw",
	})
	var resp secretTransformResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	// Wait for at least one progress event before cancelling, so the job has
	// actually started (avoids a Cancel racing Start's registration).
	deadline := time.After(5 * time.Second)
	for {
		select {
		case env := <-sub:
			if env.Event == "secret_progress" {
				goto cancel
			}
		case <-deadline:
			t.Fatal("timed out waiting for first progress event")
		}
	}
cancel:
	cw := doJSON(t, h, h.HandleSecretCancel, "POST", "/api/secret/cancel", map[string]string{"job_id": resp.JobID})
	if cw.Code != http.StatusOK {
		t.Fatalf("cancel: expected 200, got %d: %s", cw.Code, cw.Body.String())
	}
	var cancelResp map[string]bool
	if err := json.Unmarshal(cw.Body.Bytes(), &cancelResp); err != nil {
		t.Fatal(err)
	}
	if !cancelResp["cancelled"] {
		t.Fatal("expected cancelled: true")
	}

	deadline = time.After(5 * time.Second)
	for {
		select {
		case env := <-sub:
			if env.Event == "secret_cancelled" {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for secret_cancelled event")
		}
	}
}
