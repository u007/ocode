//go:build darwin && cgo

package main

/*
#cgo LDFLAGS: -framework Foundation -framework WebKit -framework Security
int ocode_pinLocalCert(const void *der, int len);
*/
import "C"

import (
	"fmt"
	"unsafe"

	"github.com/u007/ocode/internal/server"
)

// pinLocalCert makes WKWebView accept the desktop server's per-launch TLS
// certificate (see localcert_darwin.m). Must run before any window exists.
func pinLocalCert(cert *server.LocalCert) error {
	switch rc := C.ocode_pinLocalCert(unsafe.Pointer(&cert.DER[0]), C.int(len(cert.DER))); rc {
	case 0:
		return nil
	case -1:
		return fmt.Errorf("pin local cert: Wails WebviewWindowDelegate class not found")
	case -2:
		return fmt.Errorf("pin local cert: WebviewWindowDelegate already implements the auth-challenge method (Wails upgrade?)")
	default:
		return fmt.Errorf("pin local cert: unexpected result %d", int(rc))
	}
}
