package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func saveMessage(t *testing.T, nonce string, req saveFileRequest) string {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	return msgSaveFile + nonce + ":" + string(body)
}

func TestPageSaveFileRequest(t *testing.T) {
	const host = "127.0.0.1:52100"
	const nonce = "9f2c4e1a7b3d5f60a1b2c3d4e5f60718"
	page := &application.OriginInfo{Origin: "https://127.0.0.1:52100/session/abc?token=t"}
	data := []byte("%PDF-1.7\nbody: with colons\n")
	enc := base64.StdEncoding.EncodeToString(data)

	cases := []struct {
		name     string
		message  string
		origin   *application.OriginInfo
		wantName string
		wantOK   bool
	}{
		{"ocode page", saveMessage(t, nonce, saveFileRequest{Name: "resume.pdf", Data: enc}), page, "resume.pdf", true},
		{"path separators stripped", saveMessage(t, nonce, saveFileRequest{Name: "../../etc/x.pdf", Data: enc}), page, "x.pdf", true},
		{"backslash separators stripped", saveMessage(t, nonce, saveFileRequest{Name: `dir\a.pdf`, Data: enc}), page, "a.pdf", true},
		{"newline and colon kept in name", saveMessage(t, nonce, saveFileRequest{Name: "a:b\nc.pdf", Data: enc}), page, "a:b\nc.pdf", true},
		{"empty data is a valid empty file", saveMessage(t, nonce, saveFileRequest{Name: "empty.txt", Data: ""}), page, "empty.txt", true},
		{"empty name refused", saveMessage(t, nonce, saveFileRequest{Name: "", Data: enc}), page, "", false},
		{"dot name refused", saveMessage(t, nonce, saveFileRequest{Name: "..", Data: enc}), page, "", false},
		{"slash only refused", saveMessage(t, nonce, saveFileRequest{Name: "/", Data: enc}), page, "", false},
		{"bad base64 refused", saveMessage(t, nonce, saveFileRequest{Name: "a.pdf", Data: "%%%not-base64"}), page, "", false},
		{"not json refused", msgSaveFile + nonce + ":{nope", page, "", false},
		{"wrong nonce refused", saveMessage(t, "0000"+nonce[4:], saveFileRequest{Name: "a.pdf", Data: enc}), page, "", false},
		{"empty nonce refused", msgSaveFile + ":" + `{"name":"a.pdf","data":""}`, page, "", false},
		{"external frame refused", saveMessage(t, nonce, saveFileRequest{Name: "a.pdf", Data: enc}), &application.OriginInfo{Origin: "https://evil.example/"}, "", false},
		{"no origin refused", saveMessage(t, nonce, saveFileRequest{Name: "a.pdf", Data: enc}), nil, "", false},
		{"empty body refused", msgSaveFile + nonce + ":", page, "", false},
		{"other message", msgClipboardWrite + nonce + ":x", page, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := pageSaveFileRequest(tc.message, tc.origin, host, nonce)
			if ok != tc.wantOK || got.name != tc.wantName {
				t.Fatalf("pageSaveFileRequest() name=%q ok=%v; want name=%q ok=%v", got.name, ok, tc.wantName, tc.wantOK)
			}
		})
	}
}

func TestPageSaveFileRequestDecodesData(t *testing.T) {
	const host = "127.0.0.1:52100"
	const nonce = "9f2c4e1a7b3d5f60a1b2c3d4e5f60718"
	page := &application.OriginInfo{Origin: "https://127.0.0.1:52100/"}
	data := []byte("%PDF-1.7\nbody: with colons\n")
	msg := saveMessage(t, nonce, saveFileRequest{Name: "a.pdf", Data: base64.StdEncoding.EncodeToString(data)})
	got, ok := pageSaveFileRequest(msg, page, host, nonce)
	if !ok {
		t.Fatal("pageSaveFileRequest refused a valid request")
	}
	if !bytes.Equal(got.data, data) {
		t.Fatalf("decoded data = %q, want %q", got.data, data)
	}
}

func TestPageSaveFileRequestRefusesOverCap(t *testing.T) {
	const host = "127.0.0.1:52100"
	const nonce = "9f2c4e1a7b3d5f60a1b2c3d4e5f60718"
	page := &application.OriginInfo{Origin: "https://127.0.0.1:52100/"}

	// Longer than the base64 form of the cap: the encoded length check must refuse
	// it before any decoding happens.
	over := strings.Repeat("A", base64.StdEncoding.EncodedLen(desktopSaveMaxBytes)+4)
	if _, ok := pageSaveFileRequest(saveMessage(t, nonce, saveFileRequest{Name: "big.bin", Data: over}), page, host, nonce); ok {
		t.Fatal("pageSaveFileRequest accepted a file over desktopSaveMaxBytes")
	}
}
