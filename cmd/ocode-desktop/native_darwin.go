//go:build darwin && cgo

package main

/*
#cgo LDFLAGS: -framework Foundation -framework WebKit
void ocode_disablePressAndHold(void);
int ocode_printWindow(void *nativeWindow);
*/
import "C"

import (
	"errors"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// disablePressAndHold turns off macOS's "press and hold for accent characters"
// behaviour for this application so held keys repeat instead of opening the
// accent chooser. Implemented in native_darwin.m; see that file for why this
// cannot be done from the web layer. No-op via native_stub.go on every other
// platform (and on darwin builds without cgo).
func disablePressAndHold() {
	C.ocode_disablePressAndHold()
}

// printWindow shows the macOS print panel for w's webview, using the user's saved
// print settings. Wails' Window.Print forces landscape; see ocode_printWindow in
// native_darwin.m. An error means nothing was printed.
func printWindow(w application.Window) error {
	native := w.NativeWindow()
	if native == nil {
		return errors.New("print window has no native window")
	}
	if C.ocode_printWindow(native) != 0 {
		return errors.New("print window has no visible WKWebView to print")
	}
	return nil
}
