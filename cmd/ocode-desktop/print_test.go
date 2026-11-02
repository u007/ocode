package main

import (
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestPagePrintHTML(t *testing.T) {
	const host = "127.0.0.1:52100"
	const nonce = "9f2c4e1a7b3d5f60a1b2c3d4e5f60718"
	page := &application.OriginInfo{Origin: "https://127.0.0.1:52100/session/abc?token=t"}
	cases := []struct {
		name    string
		message string
		origin  *application.OriginInfo
		want    string
		wantOK  bool
	}{
		{"ocode page", msgPrintDocument + nonce + ":<html>a:b</html>", page, "<html>a:b</html>", true},
		{"page without IsMainFrame", msgPrintDocument + nonce + ":<p>x</p>", &application.OriginInfo{Origin: "https://127.0.0.1:52100/"}, "<p>x</p>", true},
		{"external iframe refused", msgPrintDocument + nonce + ":<p>x</p>", &application.OriginInfo{Origin: "https://evil.example/"}, "", false},
		{"other local port refused", msgPrintDocument + nonce + ":<p>x</p>", &application.OriginInfo{Origin: "http://127.0.0.1:5173/"}, "", false},
		{"empty origin refused", msgPrintDocument + nonce + ":<p>x</p>", &application.OriginInfo{}, "", false},
		{"no origin refused", msgPrintDocument + nonce + ":<p>x</p>", nil, "", false},
		{"wrong nonce refused", msgPrintDocument + "0000" + nonce[4:] + ":<p>x</p>", page, "", false},
		{"nonce prefix refused", msgPrintDocument + nonce[:16] + ":<p>x</p>", page, "", false},
		{"empty nonce refused", msgPrintDocument + ":<p>x</p>", page, "", false},
		{"missing nonce separator refused", msgPrintDocument + "<p>x</p>", page, "", false},
		{"empty html refused", msgPrintDocument + nonce + ":", page, "", false},
		{"clipboard write is not a print", msgClipboardWrite + nonce + ":hello", page, "", false},
		{"other message", msgFocusWindow, page, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := pagePrintHTML(tc.message, tc.origin, host, nonce)
			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("pagePrintHTML(%q) = %q, %v; want %q, %v", tc.message, got, ok, tc.want, tc.wantOK)
			}
		})
	}
	if _, ok := pagePrintHTML(msgPrintDocument+nonce+":<p>x</p>", page, "", nonce); ok {
		t.Fatal("pagePrintHTML accepted a print request before the window host was known")
	}
	if _, ok := pagePrintHTML(msgPrintDocument+":<p>x</p>", page, host, ""); ok {
		t.Fatal("pagePrintHTML accepted a print request from a window that has no nonce")
	}
}

func TestMsgPrintPrefixesAreNotWailsReserved(t *testing.T) {
	for _, msg := range []string{msgPrintDocument, msgPrintWindowReady} {
		if strings.HasPrefix(msg, "wails:") {
			t.Fatalf("message %q must not start with the reserved %q prefix", msg, "wails:")
		}
	}
}

func TestPrintWindowRegistry(t *testing.T) {
	const name = "ocode-print-registry-test"
	if isPrintWindowName(name) {
		t.Fatalf("window %q registered as a print window before it was marked", name)
	}
	markPrintWindow(name)
	t.Cleanup(func() { unmarkPrintWindow(name) })
	if !isPrintWindowName(name) {
		t.Fatalf("window %q not registered as a print window after markPrintWindow", name)
	}
	unmarkPrintWindow(name)
	if isPrintWindowName(name) {
		t.Fatalf("window %q still registered as a print window after unmarkPrintWindow", name)
	}
	if isPrintWindowName("ocode-main") {
		t.Fatal("unknown window name registered as a print window")
	}
}

func TestPrintWindowReadyIsOneShot(t *testing.T) {
	const name = "ocode-print-oneshot-test"
	markPrintWindow(name)
	t.Cleanup(func() { unmarkPrintWindow(name) })
	if !consumePrintWindowReady(name) {
		t.Fatal("first ready message from a registered print window was refused")
	}
	for i := range 3 {
		if consumePrintWindowReady(name) {
			t.Fatalf("ready message %d from the same print window started a second print", i+2)
		}
	}
	// A consumed window stays registered, so its other messages still take the
	// print-window path and are dropped there rather than reaching the ocode page.
	if !isPrintWindowName(name) {
		t.Fatal("print window lost its registration after its ready message was consumed")
	}
}

func TestPrintWindowReadyRefusedWhenUnregistered(t *testing.T) {
	const name = "ocode-print-unregistered-test"
	if consumePrintWindowReady(name) {
		t.Fatal("ready message from an unregistered window started a print")
	}
	markPrintWindow(name)
	unmarkPrintWindow(name)
	if consumePrintWindowReady(name) {
		t.Fatal("ready message from a closed print window started a print")
	}
}
