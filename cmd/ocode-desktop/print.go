package main

import (
	"fmt"
	"log"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/u007/ocode/internal/crashguard"
)

// msgPrintDocument is the page → shell message asking for a rendered document to
// be printed. The wire format is msgPrintDocument + nonce + ":" + html. WKWebView
// ignores window.print() and Wails' whole-webview print would print the SPA
// itself, so the document is opened in its own window and printed from there
// (see openPrintWindow).
const msgPrintDocument = "ocode:print-document:"

// msgPrintWindowReady is sent by the bootstrap script of a print window once its
// document has loaded. It is the only message honoured from a print window.
const msgPrintWindowReady = "ocode:print-window-ready"

// pagePrintHTML extracts the document of a print request. It applies the same
// checks as pageClipboardText, so only the ocode page can open print windows.
func pagePrintHTML(message string, origin *application.OriginInfo, appHost, nonce string) (string, bool) {
	return pageBridgePayload(message, msgPrintDocument, "print request", origin, appHost, nonce)
}

// printWindows holds the names of the windows opened by openPrintWindow, each
// with whether its ready message is still expected (true until the first one
// arrives). The bridge trusts only these windows; see isPrintWindow.
//
// It is keyed by name because the name is fixed before the window exists, so a
// window can be registered before its document loads. Keying by id would need
// the window to exist first, which is the race openPrintWindow guards against.
var printWindows sync.Map // window name (string) -> ready expected (bool)

func markPrintWindow(name string) {
	printWindows.Store(name, true)
}

func unmarkPrintWindow(name string) {
	printWindows.Delete(name)
}

func isPrintWindowName(name string) bool {
	_, ok := printWindows.Load(name)
	return ok
}

// consumePrintWindowReady reports whether a ready message from the named print
// window may start printing. Only the first call after markPrintWindow returns
// true. The window stays registered either way, so its other messages are still
// dropped by handlePrintWindowMessage.
func consumePrintWindowReady(name string) bool {
	return printWindows.CompareAndSwap(name, true, false)
}

// isPrintWindow reports whether w was opened by openPrintWindow.
func isPrintWindow(w application.Window) bool {
	if w == nil {
		return false
	}
	return isPrintWindowName(w.Name())
}

// printWindowSeq makes each print window's name unique, which Wails requires.
var printWindowSeq atomic.Uint64

// openPrintWindow opens html in a new window and registers it as a print window.
// The window's own bootstrap script reports readiness with msgPrintWindowReady,
// and handlePrintWindowMessage then prints it. The window stays open as a print
// preview, so the native print sheet offers Save as PDF; the user closes it.
//
// The HTML is the user's document and is deliberately never logged.
//
// The document may be an untrusted .html file and its scripts run here, as they
// do in the SPA's sandboxed HtmlViewer frame. The window grants them nothing
// beyond a plain webview: Wails itself honours only this window's own drag,
// resize and runtime-ready messages, and every other bridge message is dropped by
// handlePrintWindowMessage before it can reach the clipboard, external-URL or
// quit-guard handling meant for the ocode page.
func openPrintWindow(html string) {
	app := application.Get()
	if app == nil {
		log.Printf("ocode-desktop: refused print: application not ready")
		return
	}
	// The window is registered before NewWithOptions loads its document. Wails
	// runs each window message on its own goroutine (handleWindowMessage), so the
	// ready message can be handled as soon as the page loads, which can be before
	// NewWithOptions returns on this goroutine.
	name := fmt.Sprintf("ocode-print-%d", printWindowSeq.Add(1))
	markPrintWindow(name)
	// NewWithOptions has no error return and always returns a window, so there is
	// no failure branch to unmark on. The only teardown is the WindowClosing
	// listener below. The entry must stay registered until the window closes:
	// unmarking it early would let an unmarked window holding untrusted HTML fall
	// through to quitGuard/openExternalURL. Only a panic in NewWithOptions can leak
	// one string key.
	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      name,
		Title:     "Print",
		Width:     900,
		Height:    1000,
		MinWidth:  400,
		MinHeight: 300,
		HTML:      html,
	})
	win.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		unmarkPrintWindow(name)
	})
}

// handlePrintWindowMessage applies the bridge rules for a print window. Its
// document is user content, so everything except the ready message is dropped.
// The message text is not logged because it may carry document content.
func handlePrintWindowMessage(window application.Window, message string) {
	if message != msgPrintWindowReady {
		log.Printf("ocode-desktop: ignored bridge message from print window %d", window.ID())
		return
	}
	// The document can post ready again, so only the first one prints.
	if !consumePrintWindowReady(window.Name()) {
		log.Printf("ocode-desktop: ignored repeated ready message from print window %d", window.ID())
		return
	}
	// Print() runs InvokeSyncWithError on the Wails main thread, and this handler
	// may itself be running there. Calling it inline could block the thread on
	// itself, so the call runs in its own goroutine.
	crashguard.Go(func() {
		if err := window.Print(); err != nil {
			log.Printf("ocode-desktop: print window %d failed to print: %v", window.ID(), err)
		}
	})
}
