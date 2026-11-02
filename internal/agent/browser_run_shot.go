package agent

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"strings"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// The screenshot view of browser_run draws each offered control on the page's
// screenshot with its number, so the decision model answers by number. A
// text-entry control is painted solid first, before any outline or number, so
// the pixels of a typed value never reach the decision model. The image is the
// only page content the view sends: the state carries no page text.

const (
	// browserRunShotMaxWidth and browserRunShotMaxHeight bound the image the view
	// sends. The page is scaled to fit inside them, keeping its aspect ratio.
	browserRunShotMaxWidth  = 1280
	browserRunShotMaxHeight = 800
	// browserRunShotMinSide is the smallest viewport side the screenshot view takes.
	// The decision model rejects smaller images ("Invalid image dimensions"), and a
	// tiny viewport is refused before the screenshot is taken.
	browserRunShotMinSide = 64
	// browserRunShotStroke is the outline thickness of a drawn control, in pixels.
	browserRunShotStroke = 2
	// browserRunShotNote is added to every question the screenshot view asks.
	browserRunShotNote = "The screenshot shows the page. Each element is numbered, and its number is drawn at its top-left corner. Text-entry fields are drawn solid black, so their contents are not shown. Judge only what the screenshot shows."
)

var (
	// browserRunShotMask paints over a text-entry control.
	browserRunShotMask = color.RGBA{A: 0xff}
	// browserRunShotMark is the outline and number colour.
	browserRunShotMark = color.RGBA{R: 0xe0, G: 0x20, B: 0x80, A: 0xff}
)

// browserRunShot describes one screenshot view: the image size and the controls
// drawn and masked on it.
type browserRunShot struct {
	width, height int
	drawn, masked int
}

// browserRunElement is one drawn control as the screenshot state lists it. It
// carries no label and no value.
type browserRunElement struct {
	Ref  string `json:"ref"`
	Role string `json:"role"`
}

// browserRunTextEntry reports whether a control takes typed text: its role is a
// text entry, or it already holds a value.
func browserRunTextEntry(c browserRunControl) bool {
	switch c.Role {
	case "textbox", "searchbox", "combobox", "spinbutton":
		return true
	}
	return c.Value != ""
}

// browserRunShotControls picks what the screenshot view draws and masks. Drawn
// controls are the ones in the viewport, capped at browserRunMaxControls, in
// listing order. Masked controls are every text-entry control in the viewport
// from the whole listing, so a field past the cap still has its pixels covered.
func browserRunShotControls(page browserRunPage) (drawn, masked []browserRunControl, err error) {
	for _, c := range page.Controls {
		if !c.InViewport {
			continue
		}
		if c.Box == nil {
			return nil, nil, fmt.Errorf("browser_run: control %s is in the viewport but has no box", c.Ref)
		}
		if browserRunTextEntry(c) {
			masked = append(masked, c)
		}
		if len(drawn) < browserRunMaxControls {
			drawn = append(drawn, c)
		}
	}
	return drawn, masked, nil
}

// browserRunDrawShot decodes the screenshot, scales it to fit the shot size, paints
// every masked control solid, then outlines each drawn control and writes its
// number at the control's top-left corner. It returns the PNG and its size.
func browserRunDrawShot(raw []byte, page browserRunPage, drawn, masked []browserRunControl) ([]byte, int, int, error) {
	src, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("browser_run: decode screenshot: %w", err)
	}
	if page.ViewportWidth <= 0 || page.ViewportHeight <= 0 {
		return nil, 0, 0, fmt.Errorf("browser_run: viewport %dx%d has no size to place controls in", page.ViewportWidth, page.ViewportHeight)
	}
	b := src.Bounds()
	fit := math.Min(1, math.Min(float64(browserRunShotMaxWidth)/float64(b.Dx()), float64(browserRunShotMaxHeight)/float64(b.Dy())))
	dw := int(math.Floor(float64(b.Dx()) * fit))
	dh := int(math.Floor(float64(b.Dy()) * fit))
	if dw < 1 || dh < 1 {
		return nil, 0, 0, fmt.Errorf("browser_run: screenshot of %dx%d cannot be scaled to fit %dx%d", b.Dx(), b.Dy(), browserRunShotMaxWidth, browserRunShotMaxHeight)
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)

	// The screenshot covers the viewport, so viewport pixels map to image pixels by
	// the ratio of the two sizes, per axis.
	sx := float64(dw) / float64(page.ViewportWidth)
	sy := float64(dh) / float64(page.ViewportHeight)
	for _, c := range masked {
		draw.Draw(dst, browserRunShotRect(*c.Box, sx, sy, dst.Bounds()), &image.Uniform{C: browserRunShotMask}, image.Point{}, draw.Src)
	}
	for _, c := range drawn {
		r := browserRunShotRect(*c.Box, sx, sy, dst.Bounds())
		browserRunStroke(dst, r, browserRunShotMark)
		browserRunTag(dst, r, browserRunShotNumber(c.Ref))
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, 0, 0, fmt.Errorf("browser_run: encode screenshot: %w", err)
	}
	return buf.Bytes(), dw, dh, nil
}

// browserRunShotRect maps a control's box from viewport pixels to image pixels and
// clips it to the image. The edges round outward, so a mask covers every pixel the
// box touches, including a fringe of a value that a rounded edge would miss.
func browserRunShotRect(b browserRunBox, sx, sy float64, bounds image.Rectangle) image.Rectangle {
	r := image.Rect(
		int(math.Floor(b.X*sx)),
		int(math.Floor(b.Y*sy)),
		int(math.Ceil((b.X+b.W)*sx)),
		int(math.Ceil((b.Y+b.H)*sy)),
	)
	return r.Intersect(bounds)
}

// browserRunStroke draws the outline of r, browserRunShotStroke pixels thick.
func browserRunStroke(dst *image.RGBA, r image.Rectangle, c color.Color) {
	u := &image.Uniform{C: c}
	s := browserRunShotStroke
	draw.Draw(dst, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+s), u, image.Point{}, draw.Src)
	draw.Draw(dst, image.Rect(r.Min.X, r.Max.Y-s, r.Max.X, r.Max.Y), u, image.Point{}, draw.Src)
	draw.Draw(dst, image.Rect(r.Min.X, r.Min.Y, r.Min.X+s, r.Max.Y), u, image.Point{}, draw.Src)
	draw.Draw(dst, image.Rect(r.Max.X-s, r.Min.Y, r.Max.X, r.Max.Y), u, image.Point{}, draw.Src)
}

// browserRunTag writes text on a filled tag at the top-left corner of r.
func browserRunTag(dst *image.RGBA, r image.Rectangle, text string) {
	face := basicfont.Face7x13
	w := font.MeasureString(face, text).Ceil() + 4
	h := face.Metrics().Height.Ceil() + 2
	tag := image.Rect(r.Min.X, r.Min.Y, r.Min.X+w, r.Min.Y+h).Intersect(dst.Bounds())
	draw.Draw(dst, tag, &image.Uniform{C: browserRunShotMark}, image.Point{}, draw.Src)
	d := &font.Drawer{
		Dst:  dst,
		Src:  image.White,
		Face: face,
		Dot:  fixed.P(tag.Min.X+2, tag.Min.Y+2+face.Metrics().Ascent.Ceil()),
	}
	d.DrawString(text)
}

// browserRunShotNumber is the number a control is drawn with: its ref without the
// "@e" prefix.
func browserRunShotNumber(ref string) string {
	return strings.TrimPrefix(ref, "@e")
}

// browserRunDescribeShotControl names a control the way the screenshot view shows
// it: by role and number. Its label is never sent in this view.
func browserRunDescribeShotControl(c browserRunControl) string {
	return c.Role + " numbered " + browserRunShotNumber(c.Ref)
}

// browserRunShotElements lists the drawn controls for the screenshot state.
func browserRunShotElements(drawn []browserRunControl) []browserRunElement {
	out := make([]browserRunElement, 0, len(drawn))
	for _, c := range drawn {
		out = append(out, browserRunElement{Ref: c.Ref, Role: c.Role})
	}
	return out
}

// browserRunPlaceholder names the screenshot inside the state. It is random for
// each request, so no text on the page can match it.
func browserRunPlaceholder() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("browser_run: make the screenshot placeholder: %w", err)
	}
	return "screenshot-" + hex.EncodeToString(b[:]), nil
}

// browserRunHasBox reports whether any control carries a box.
func browserRunHasBox(controls []browserRunControl) bool {
	for _, c := range controls {
		if c.Box != nil {
			return true
		}
	}
	return false
}
