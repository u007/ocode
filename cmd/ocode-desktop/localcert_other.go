//go:build !(darwin && cgo) && !(linux && cgo)

package main

import "github.com/u007/ocode/internal/server"

// pinLocalCert is a no-op here: on Windows the pin is the WebView2
// --ignore-certificate-errors-spki-list browser argument set on
// application.Options (it must be known before the browser environment
// starts), and cgo-less builds have no native webview to configure.
func pinLocalCert(*server.LocalCert) error { return nil }
