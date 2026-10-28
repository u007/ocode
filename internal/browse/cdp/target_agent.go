package cdp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Page operations for the browser agent tool (internal/server/browser_tool.go).
// Each runs on the tab's own CDP session with the caller's context, which the
// tool bounds per action. Navigate, Back, Forward, Reload and InsertText are
// already on Target and are reused as-is.

// pageTextMaxChars caps what PageText returns, in UTF-16 code units (the unit
// String.length counts). The slice runs inside the page so a multi-megabyte
// document never crosses the CDP socket.
const pageTextMaxChars = 20000

// maxScreenshotBytes guards one PNG. The capture is the visible viewport at
// device resolution, so this is a safety bound rather than an expected size.
const maxScreenshotBytes = 16 << 20

// ErrUnknownKey is returned by PressKey for a name outside namedKeys.
var ErrUnknownKey = errors.New("unknown key name")

// pageTextExpr returns the visible text (innerText, so hidden elements and
// scripts are excluded) cut to pageTextMaxChars, plus the full length.
const pageTextExpr = `(() => {
	const s = document.body ? document.body.innerText : "";
	return {text: s.slice(0, %d), total: s.length};
})()`

// viewportSizeExpr reads the window's inner size in CSS pixels. innerWidth and
// innerHeight include any scrollbar, which matches what captureScreenshot
// returns, and they are the space mouse events are dispatched in (the panel
// sends canvas-relative CSS pixels, and its canvas is the viewport).
const viewportSizeExpr = `({w: window.innerWidth, h: window.innerHeight})`

// Screenshot captures the visible viewport as PNG at device resolution. It
// also returns the viewport size in CSS pixels, so the caller can map image
// pixels back to CSS pixels for mouse events.
func (t *Target) Screenshot(ctx context.Context) (png []byte, cssW, cssH int, err error) {
	var size struct {
		Result struct {
			Value struct {
				W int `json:"w"`
				H int `json:"h"`
			} `json:"value"`
		} `json:"result"`
	}
	if err := t.conn.Call(ctx, t.sessionID, "Runtime.evaluate",
		map[string]any{"expression": viewportSizeExpr, "returnByValue": true}, &size); err != nil {
		return nil, 0, 0, err
	}
	var shot struct {
		Data string `json:"data"`
	}
	if err := t.conn.Call(ctx, t.sessionID, "Page.captureScreenshot", map[string]string{"format": "png"}, &shot); err != nil {
		return nil, 0, 0, err
	}
	raw, err := base64.StdEncoding.DecodeString(shot.Data)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("decode screenshot: %w", err)
	}
	if len(raw) > maxScreenshotBytes {
		return nil, 0, 0, fmt.Errorf("screenshot is %d bytes, over the %d byte limit", len(raw), maxScreenshotBytes)
	}
	return raw, size.Result.Value.W, size.Result.Value.H, nil
}

// PageText returns the page's visible text, cut to pageTextMaxChars. truncated
// reports whether the page had more.
func (t *Target) PageText(ctx context.Context) (text string, truncated bool, err error) {
	var res struct {
		Result struct {
			Value struct {
				Text  string `json:"text"`
				Total int    `json:"total"`
			} `json:"value"`
		} `json:"result"`
		ExceptionDetails json.RawMessage `json:"exceptionDetails"`
	}
	expr := fmt.Sprintf(pageTextExpr, pageTextMaxChars)
	if err := t.conn.Call(ctx, t.sessionID, "Runtime.evaluate",
		map[string]any{"expression": expr, "returnByValue": true}, &res); err != nil {
		return "", false, err
	}
	if len(res.ExceptionDetails) > 0 {
		return "", false, fmt.Errorf("page text evaluation threw: %s", res.ExceptionDetails)
	}
	v := res.Result.Value
	return v.Text, v.Total > pageTextMaxChars, nil
}

// Click clicks at (x, y) in CSS pixels of the viewport. The pointer moves there
// first so hover state matches a real click, then one left press and release.
// The move carries button "none" and no buttons held, as the panel's own moves
// do; Chrome rejects an empty button name.
func (t *Target) Click(ctx context.Context, x, y float64) error {
	if err := t.Mouse(ctx, MouseEvent{Kind: "move", X: x, Y: y, Button: "none"}); err != nil {
		return err
	}
	if err := t.Mouse(ctx, MouseEvent{Kind: "down", X: x, Y: y, Button: "left", Buttons: 1, ClickCount: 1}); err != nil {
		return err
	}
	return t.Mouse(ctx, MouseEvent{Kind: "up", X: x, Y: y, Button: "left", ClickCount: 1})
}

// ScrollPage scrolls the window by most of one viewport height: dir is 1 for
// down and -1 for up. Scrollable containers inside the page are not targeted.
func (t *Target) ScrollPage(ctx context.Context, dir int) error {
	if dir != 1 && dir != -1 {
		return fmt.Errorf("scroll direction must be 1 (down) or -1 (up), got %d", dir)
	}
	expr := fmt.Sprintf("window.scrollBy(0, %d * window.innerHeight * 0.8)", dir)
	return t.conn.Call(ctx, t.sessionID, "Runtime.evaluate",
		map[string]any{"expression": expr, "returnByValue": true}, nil)
}

// PressKey presses one named key (see NamedKeys) as a key-down and key-up pair.
func (t *Target) PressKey(ctx context.Context, name string) error {
	ev, ok := namedKeys[name]
	if !ok {
		return fmt.Errorf("%w %q (allowed: %s)", ErrUnknownKey, name, strings.Join(NamedKeys(), ", "))
	}
	down := ev
	down.Kind = "down"
	if err := t.Key(ctx, down); err != nil {
		return err
	}
	up := ev
	up.Kind = "up"
	up.Text = ""
	return t.Key(ctx, up)
}

// ReadyState returns the top document's document.readyState: "loading",
// "interactive" or "complete".
func (t *Target) ReadyState(ctx context.Context) (string, error) {
	var res struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
		ExceptionDetails json.RawMessage `json:"exceptionDetails"`
	}
	if err := t.conn.Call(ctx, t.sessionID, "Runtime.evaluate",
		map[string]any{"expression": "document.readyState", "returnByValue": true}, &res); err != nil {
		return "", err
	}
	if len(res.ExceptionDetails) > 0 {
		return "", fmt.Errorf("reading document.readyState threw: %s", res.ExceptionDetails)
	}
	return res.Result.Value, nil
}
