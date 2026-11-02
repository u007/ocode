package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/u007/ocode/internal/crashguard"
)

// msgSaveFile is the page → shell message asking the shell to save a file the
// page built in memory. WKWebView drops a blob <a download> click without any
// error (Wails v3 installs no WKDownload delegate), so the page hands the bytes
// here and the shell shows the native save panel instead. The wire format is
// msgSaveFile + nonce + ":" + JSON saveFileRequest.
const msgSaveFile = "ocode:save-file:"

// desktopSaveMaxBytes caps the decoded size of one save. web/src/lib/wails.ts
// (MAX_DESKTOP_SAVE_BYTES) enforces the same limit before sending; keep them equal.
const desktopSaveMaxBytes = 64 << 20

// saveFileRequest is the JSON body of a save-file message. The name is a
// filename only: the shell keeps just its base name.
type saveFileRequest struct {
	Name string `json:"name"`
	Data string `json:"data"`
}

// desktopSave is a validated save request ready for the save panel.
type desktopSave struct {
	name string
	data []byte
}

// pageSaveFileRequest extracts a save request from a page → shell message. It
// applies the same nonce and origin checks as pageClipboardText, then refuses
// an empty name, a name with no usable base, bad base64, or a file over
// desktopSaveMaxBytes. Refusals are logged without the file's contents.
func pageSaveFileRequest(message string, origin *application.OriginInfo, appHost, nonce string) (desktopSave, bool) {
	body, ok := pageBridgePayload(message, msgSaveFile, "save file", origin, appHost, nonce)
	if !ok {
		return desktopSave{}, false
	}
	var req saveFileRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		log.Printf("ocode-desktop: refused save file: request is not valid JSON: %v", err)
		return desktopSave{}, false
	}
	name := saveFileBaseName(req.Name)
	if name == "" {
		log.Printf("ocode-desktop: refused save file: no usable file name")
		return desktopSave{}, false
	}
	// Check the encoded length before decoding, so an oversized request is not
	// expanded in memory first.
	if len(req.Data) > base64.StdEncoding.EncodedLen(desktopSaveMaxBytes) {
		log.Printf("ocode-desktop: refused save file %q: over the %d-byte limit", name, desktopSaveMaxBytes)
		return desktopSave{}, false
	}
	data, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil {
		log.Printf("ocode-desktop: refused save file %q: data is not base64: %v", name, err)
		return desktopSave{}, false
	}
	return desktopSave{name: name, data: data}, true
}

// saveFileBaseName reduces a page-supplied name to a safe file name. It treats
// both separators as path breaks, and returns "" for names that would leave
// the chosen folder or name nothing.
func saveFileBaseName(raw string) string {
	base := filepath.Base(strings.ReplaceAll(raw, `\`, "/"))
	if base == "." || base == ".." || base == "/" {
		return ""
	}
	return base
}

// openSaveFile asks the user where to save req, then writes it. The save panel
// blocks on the Wails main thread and this handler may already be running on
// it, so the work runs in its own goroutine (the same rule as the print ready
// path). The panel is attached to window as a sheet.
func openSaveFile(window application.Window, req desktopSave) {
	app := application.Get()
	if app == nil {
		log.Printf("ocode-desktop: refused save file %q: application not ready", req.name)
		return
	}
	crashguard.Go(func() {
		dialog := app.Dialog.SaveFile().SetFilename(req.name).AttachToWindow(window)
		// intentionally not logged: without a home directory the panel opens in its own default folder
		if home, err := os.UserHomeDir(); err == nil {
			dialog.SetDirectory(filepath.Join(home, "Downloads"))
		}
		path, err := dialog.PromptForSingleSelection()
		if err != nil {
			log.Printf("ocode-desktop: save panel for %q failed: %v", req.name, err)
			showSaveError(app, req.name, err)
			return
		}
		if path == "" {
			log.Printf("ocode-desktop: save of %q cancelled by user", req.name)
			return
		}
		if err := os.WriteFile(path, req.data, 0o644); err != nil {
			log.Printf("ocode-desktop: save of %q to %q failed: %v", req.name, path, err)
			showSaveError(app, req.name, err)
			return
		}
		log.Printf("ocode-desktop: saved %q (%d bytes)", req.name, len(req.data))
	})
}

// showSaveError surfaces a failed save as a native alert. The bridge has no
// acknowledgement, so a log line alone would look like the download did nothing.
func showSaveError(app *application.App, name string, err error) {
	app.Dialog.Error().
		SetTitle("Could not save file").
		SetMessage(fmt.Sprintf("%s: %v", name, err)).
		Show()
}
