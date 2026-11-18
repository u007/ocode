package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"slices"
	"strings"
	"time"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// The frames and then_text arguments let one browser call show what an action
// causes. frames takes screenshots at millisecond offsets after the action and
// composes them into one view-only contact sheet. The argument rules are in
// checkViews; the call flow is performAndSee in browser_tool.go.

const (
	// browserFramesMax is the most offsets one frames argument may list.
	browserFramesMax = 4
	// browserFrameMaxMS is the latest offset a frame may be taken at.
	browserFrameMaxMS = 5000
	// browserSheetSep is the gap in pixels between contact-sheet cells.
	browserSheetSep = 2
)

var (
	// browserBurstActions may take frames: the actions that change the page, and screenshot.
	browserBurstActions = []string{"click", "fill", "key", "screenshot", "type"}
	// browserThenTextActions may take then_text.
	browserThenTextActions = []string{"click", "fill", "key", "type"}
)

// sheetSeparator is the colour of the gaps between cells and of an unused cell.
var sheetSeparator = color.RGBA{R: 0x40, G: 0x40, B: 0x40, A: 0xff}

// checkViews refuses frames and then_text arguments the action cannot use. It
// runs before any Chrome call, like the rest of checkShape.
func (p browserArgs) checkViews() error {
	if p.Frames != nil && p.ThenText != nil {
		return errors.New("browser: frames and then_text cannot be combined: use frames for timed views, or then_text for one")
	}
	if p.Frames != nil {
		if !slices.Contains(browserBurstActions, p.Action) {
			return fmt.Errorf("browser: action %s does not take frames", p.Action)
		}
		if n := len(p.Frames); n < 1 || n > browserFramesMax {
			return fmt.Errorf("browser: frames needs 1 to %d offsets in milliseconds, got %d", browserFramesMax, n)
		}
		for i, off := range p.Frames {
			if off < 0 || off > browserFrameMaxMS {
				return fmt.Errorf("browser: frame offset %d ms is outside 0 to %d ms", off, browserFrameMaxMS)
			}
			if i > 0 && off <= p.Frames[i-1] {
				return fmt.Errorf("browser: frame offsets must be strictly ascending, got %d ms after %d ms", off, p.Frames[i-1])
			}
		}
	}
	if p.ThenText != nil {
		if !slices.Contains(browserThenTextActions, p.Action) {
			return fmt.Errorf("browser: action %s does not take then_text", p.Action)
		}
		if *p.ThenText == "" {
			return errors.New("browser: then_text needs text: the text to wait for")
		}
		if p.Timeout != nil && (*p.Timeout < 1 || *p.Timeout > browserWaitMaxSeconds) {
			return fmt.Errorf("browser: timeout must be 1 to %d seconds, got %d", browserWaitMaxSeconds, *p.Timeout)
		}
	}
	return nil
}

// sheetCell is one frame of a contact sheet and the label drawn in its corner.
type sheetCell struct {
	img   image.Image
	label string
}

// sheetLayout returns the columns and rows for n frames: one row for a portrait
// viewport and for one or two frames, otherwise two columns.
func sheetLayout(n int, portrait bool) (cols, rows int) {
	if portrait || n <= 2 {
		return n, 1
	}
	return 2, (n + 1) / 2
}

// composeFrameSheet lays the cells out in reading order and returns the sheet as
// a PNG whose longest side is at most maxSide. The cells are scaled, not the
// finished sheet, so the separators and labels keep their pixel size.
func composeFrameSheet(cells []sheetCell, maxSide int) ([]byte, error) {
	if len(cells) == 0 {
		return nil, errors.New("no frames to compose")
	}
	if maxSide < 1 {
		return nil, fmt.Errorf("max side %d is not a size", maxSide)
	}
	first := cells[0].img.Bounds()
	cw, ch := first.Dx(), first.Dy()
	if cw < 1 || ch < 1 {
		return nil, errors.New("frame has no pixels")
	}
	cols, rows := sheetLayout(len(cells), ch > cw)
	sepW := (cols - 1) * browserSheetSep
	sepH := (rows - 1) * browserSheetSep
	cellW, cellH := cw, ch
	if longest := max(cols*cw+sepW, rows*ch+sepH); longest > maxSide {
		ratio := float64(maxSide) / float64(longest)
		cellW = max(1, int(float64(cw)*ratio))
		cellH = max(1, int(float64(ch)*ratio))
	}
	// The separators are fixed pixels, so they count against maxSide too.
	for cols*cellW+sepW > maxSide || rows*cellH+sepH > maxSide {
		switch {
		case cellW > 1 && (cols*cellW+sepW >= rows*cellH+sepH || cellH == 1):
			cellW--
		case cellH > 1:
			cellH--
		default:
			return nil, fmt.Errorf("max side %d is too small for %d frames", maxSide, len(cells))
		}
	}
	canvas := image.NewRGBA(image.Rect(0, 0, cols*cellW+sepW, rows*cellH+sepH))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(sheetSeparator), image.Point{}, draw.Src)
	for i, c := range cells {
		x := (i % cols) * (cellW + browserSheetSep)
		y := (i / cols) * (cellH + browserSheetSep)
		rect := image.Rect(x, y, x+cellW, y+cellH)
		draw.ApproxBiLinear.Scale(canvas, rect, c.img, c.img.Bounds(), draw.Src, nil)
		if c.label != "" {
			drawSheetLabel(canvas, rect, c.label)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, canvas); err != nil {
		return nil, fmt.Errorf("encode contact sheet: %w", err)
	}
	return buf.Bytes(), nil
}

// drawSheetLabel draws label in a solid black box in the top-left corner of cell,
// at the font's own size, since the sheet is already at its final resolution. A
// label that does not fit the cell is left out rather than drawn over a neighbour.
func drawSheetLabel(dst *image.RGBA, cell image.Rectangle, label string) {
	face := basicfont.Face7x13
	const padX, padY = 3, 1
	m := face.Metrics()
	textW := font.MeasureString(face, label).Ceil()
	boxW, boxH := textW+2*padX, m.Height.Ceil()+2*padY
	if cell.Dx() < boxW || cell.Dy() < boxH {
		return
	}
	box := image.Rect(cell.Min.X, cell.Min.Y, cell.Min.X+boxW, cell.Min.Y+boxH)
	draw.Draw(dst, box, image.NewUniform(color.Black), image.Point{}, draw.Src)
	d := &font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(color.White),
		Face: face,
		Dot:  fixed.P(cell.Min.X+padX, cell.Min.Y+padY+m.Ascent.Ceil()),
	}
	d.DrawString(label)
}

// frameBurst is what a burst of screenshots produced: the frames taken, in
// order, and the offsets that were not taken after the first failed capture.
type frameBurst struct {
	cells   []sheetCell
	missing []int
	err     error // the first capture failure; nil when every offset was taken
}

// captureFrames takes one screenshot at each offset, in milliseconds after base.
// The label is the time the capture was requested, so a late frame says so. The
// first failed capture stops the burst and marks that offset and every later one
// missing. A context that ends mid-burst returns its error. The held screenshot
// is dropped first: a frame does not replace b.shot, so a coordinate click still
// needs a normal screenshot.
func (b *browserTool) captureFrames(ctx context.Context, tabID string, surf browserSurface, offsets []int, base time.Time) (frameBurst, error) {
	b.dropShot()
	var burst frameBurst
	for i, off := range offsets {
		if d := time.Until(base.Add(time.Duration(off) * time.Millisecond)); d > 0 {
			timer := time.NewTimer(d)
			select {
			case <-ctx.Done():
				timer.Stop()
				return frameBurst{}, ctx.Err()
			case <-timer.C:
			}
		}
		requested := time.Since(base).Milliseconds()
		img, err := captureFrame(ctx, surf)
		if err != nil {
			if cerr := ctx.Err(); cerr != nil {
				return frameBurst{}, cerr
			}
			log.Printf("browser: frame at +%d ms of tab %s: %v", off, tabID, err)
			burst.missing = slices.Clone(offsets[i:])
			burst.err = err
			return burst, nil
		}
		burst.cells = append(burst.cells, sheetCell{img: img, label: fmt.Sprintf("+%d ms", requested)})
	}
	return burst, nil
}

// captureFrame takes one screenshot and decodes it.
func captureFrame(ctx context.Context, surf browserSurface) (image.Image, error) {
	raw, cssW, cssH, err := surf.Screenshot(ctx)
	if err != nil {
		return nil, err
	}
	if cssW <= 0 || cssH <= 0 {
		return nil, errors.New("tab has no visible viewport")
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode frame: %w", err)
	}
	return img, nil
}

// finishFrames builds the result text for a burst after actionLine. The frames
// taken are composed into the view-only sheet held in b.sheet. A burst with no
// frame leaves b.sheet nil, so the call attaches no image.
func (b *browserTool) finishFrames(actionLine string, burst frameBurst) string {
	// No composed sheet means no image will be attached even though the call is
	// advertised image-producing (ProducesImage). The "cannot view screenshots"
	// caveat is TRUE only when the sheet exists — saying it when the capture
	// merely failed makes the model switch to action=text or retry endlessly.
	if len(burst.cells) == 0 {
		return actionLine + "\n" + fmt.Sprintf("No frame could be captured: %v. Vision is not the problem; the capture failed.", burst.err)
	}
	sheet, err := composeFrameSheet(burst.cells, b.maxSide)
	if err != nil {
		log.Printf("browser: contact sheet: %v", err)
		return actionLine + "\n" + fmt.Sprintf("No frame could be captured: %v. Vision is not the problem; the capture failed.", err)
	}
	b.mu.Lock()
	b.sheet = sheet
	b.mu.Unlock()
	labels := make([]string, len(burst.cells))
	for i, c := range burst.cells {
		labels[i] = c.label
	}
	out := fmt.Sprintf("%s\nFrames after the action, in reading order: %s. This image is a view-only contact sheet: take a screenshot before a coordinate click. If no image is shown to you, this model cannot view screenshots: use action=text instead.", actionLine, strings.Join(labels, ", "))
	if len(burst.missing) > 0 {
		offs := make([]string, len(burst.missing))
		for i, off := range burst.missing {
			offs[i] = fmt.Sprintf("+%d ms", off)
		}
		noun := "Frame"
		if len(offs) > 1 {
			noun = "Frames"
		}
		out += fmt.Sprintf("\n%s at %s could not be captured: %v.", noun, strings.Join(offs, ", "), burst.err)
	}
	return out
}
