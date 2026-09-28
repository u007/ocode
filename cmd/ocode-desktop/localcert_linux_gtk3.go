//go:build linux && cgo && gtk3

package main

/*
#cgo linux pkg-config: webkit2gtk-4.1
#include <stdlib.h>
#include <webkit2/webkit2.h>

static int ocode_allow_local_cert(const char *pem, char **errOut) {
	GError *err = NULL;
	GTlsCertificate *cert = g_tls_certificate_new_from_pem(pem, -1, &err);
	if (cert == NULL) {
		*errOut = g_strdup(err != NULL ? err->message : "unknown error");
		if (err != NULL) g_error_free(err);
		return -1;
	}
	WebKitWebContext *ctx = webkit_web_context_get_default();
	webkit_web_context_allow_tls_certificate_for_host(ctx, cert, "127.0.0.1");
	webkit_web_context_allow_tls_certificate_for_host(ctx, cert, "localhost");
	g_object_unref(cert);
	return 0;
}
*/
import "C"

import (
	"fmt"
	"unsafe"

	"github.com/u007/ocode/internal/server"
)

// pinLocalCert lets WebKitGTK accept the desktop server's per-launch TLS
// certificate for the loopback host only. Wails creates its webviews on the
// default web context, so allowing it there covers them. Must run before the
// first window navigates.
func pinLocalCert(cert *server.LocalCert) error {
	pem := C.CString(string(cert.PEM))
	defer C.free(unsafe.Pointer(pem))
	var cerr *C.char
	if C.ocode_allow_local_cert(pem, &cerr) != 0 {
		defer C.g_free(C.gpointer(cerr))
		return fmt.Errorf("pin local cert: %s", C.GoString(cerr))
	}
	return nil
}
