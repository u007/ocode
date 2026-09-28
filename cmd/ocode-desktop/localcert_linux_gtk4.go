//go:build linux && cgo && !gtk3

package main

/*
#cgo linux pkg-config: webkitgtk-6.0
#include <stdlib.h>
#include <webkit/webkit.h>

static int ocode_allow_local_cert(const char *pem, char **errOut) {
	GError *err = NULL;
	GTlsCertificate *cert = g_tls_certificate_new_from_pem(pem, -1, &err);
	if (cert == NULL) {
		*errOut = g_strdup(err != NULL ? err->message : "unknown error");
		if (err != NULL) g_error_free(err);
		return -1;
	}
	WebKitNetworkSession *session = webkit_network_session_get_default();
	webkit_network_session_allow_tls_certificate_for_host(session, cert, "127.0.0.1");
	webkit_network_session_allow_tls_certificate_for_host(session, cert, "localhost");
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
// default network session, so allowing it there covers them. Must run before
// the first window navigates.
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
