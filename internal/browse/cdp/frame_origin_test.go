package cdp

import (
	"context"
	"strings"
	"testing"
)

// fakeFrameObjectID and fakeFrameID identify the one <iframe> the fake page
// resolves (see resolveFrameID), so a test can drive the positive frame path.
const (
	fakeFrameObjectID = "o-frame"
	fakeFrameID       = "frame-f"
	fakeFrameContext  = int64(42)
)

// framePage returns a fake page with one custom combobox and a recorded frame
// realm, so both Pick and ClickElement run inside the frame. quads is what
// DOM.getContentQuads reports for the <iframe> owner (its content box in
// top-viewport coordinates); box is the element's box.
func framePage(t *testing.T, quads, box []float64) (*fakePage, *Target) {
	t.Helper()
	page := comboPage([][]string{{"Malaysia"}})
	page.els[pathCombo].comboText = "Malaysia"
	page.contentQuads = [][]float64{quads}
	page.boxContent = box
	page.hit = true
	tgt := newFakePage(t, page)
	tgt.frameCtx.record(fakeFrameID, fakeFrameContext)
	return page, tgt
}

// TestClickElementInPlainFrameKeepsExpectedCoordinates pins the click path for
// an untransformed iframe, so unifying the frame-origin helper cannot move it:
// the iframe's content box starts at (10,10), the element's box centre is
// (20,15), and the frame-local hit point is (10,5).
func TestClickElementInPlainFrameKeepsExpectedCoordinates(t *testing.T) {
	page, tgt := framePage(t, defaultBox, defaultBox)
	if _, err := tgt.ClickElement(context.Background(), "#frame", "#combo", nil); err != nil {
		t.Fatalf("ClickElement in a plain frame: %v", err)
	}
	if got := strings.Join(page.clickLog(), "|"); got != "mouseMoved 20,15|mousePressed 20,15|mouseReleased 20,15" {
		t.Errorf("click events = %q, want the element centre", got)
	}
	if got := strings.Join(page.hitPoints, "|"); got != "10,5" {
		t.Errorf("hit test point = %q, want the frame-local (10,5)", got)
	}
}

// TestPickAndClickUseTheSameFrameOriginForATransformedFrame: a moved/scaled
// iframe reports its real content quad through DOM.getContentQuads, which a
// getBoundingClientRect + clientLeft + padding mapping cannot see. Pick's
// frameToTop and ClickElement's frameOrigin must resolve that same origin:
// pick adds it to the option point, click subtracts it from the element point.
func TestPickAndClickUseTheSameFrameOriginForATransformedFrame(t *testing.T) {
	// The iframe's content box: a 160x175 quad whose origin is (100,200).
	iframe := []float64{100, 200, 260, 200, 260, 375, 100, 375}
	// The element inside it: a 20x10 box whose top-viewport centre is (120,215).
	element := []float64{110, 210, 130, 210, 130, 220, 110, 220}
	page, tgt := framePage(t, iframe, element)
	ctx := context.Background()

	ox, oy, err := tgt.frameOrigin(ctx, "#frame")
	if err != nil || ox != 100 || oy != 200 {
		t.Fatalf("frameOrigin = (%.1f, %.1f, %v), want (100,200)", ox, oy, err)
	}
	tx, ty, err := tgt.frameToTop(ctx, "#frame", "Malaysia", 10, 20)
	if err != nil || tx != 110 || ty != 220 {
		t.Fatalf("frameToTop(10,20) = (%.1f, %.1f, %v), want (110,220)", tx, ty, err)
	}

	// Click subtracts the origin: (120,215) - (100,200) = the frame-local (20,15).
	if _, err := tgt.ClickElement(ctx, "#frame", "#combo", nil); err != nil {
		t.Fatalf("ClickElement in a transformed frame: %v", err)
	}
	if got := strings.Join(page.clickLog(), "|"); got != "mouseMoved 120,215|mousePressed 120,215|mouseReleased 120,215" {
		t.Errorf("click events = %q, want the element centre (120,215)", got)
	}
	if got := strings.Join(page.hitPoints, "|"); got != "20,15" {
		t.Errorf("hit test point = %q, want the frame-local (20,15)", got)
	}

	// Pick adds the same origin to the option point: (10,20) + (100,200).
	page.clicks = nil
	if _, err := tgt.Pick(ctx, "#frame", "#combo", "Malaysia", nil); err != nil {
		t.Fatalf("Pick in a transformed frame: %v", err)
	}
	if got := strings.Join(page.clickLog(), "|"); got != "mouseMoved 110,220|mousePressed 110,220|mouseReleased 110,220" {
		t.Errorf("pick click events = %q, want the option point plus the frame origin", got)
	}
}
