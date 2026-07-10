package tui

import (
	"fmt"
	"strings"
	"testing"

	textarea "charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/u007/ocode/internal/tui/fastviewport"
)

// TestURLDetectOnClick reproduces the "click URL -> no popup" report. It
// renders a message containing a raw URL, then scans every column of the URL's
// line to see whether transcriptUrlLinkAt ever returns a hit. If it only hits
// for X values offset from the true screen column, the coordinate mapping
// (missing left-chrome adjustment) is the bug.
func TestURLDetectOnClick(t *testing.T) {
	cases := []struct {
		name    string
		msg     message
		sidebar bool
	}{
		{"user raw url", message{role: roleUser, text: "see https://example.com now"}, false},
		{"assistant raw url", message{role: roleAssistant, text: "docs at https://example.com/path?x=1 please read"}, false},
		{"assistant markdown link", message{role: roleAssistant, text: "see [the docs](https://example.com/docs) for details"}, false},
		{"user markdown link", message{role: roleUser, text: "open [docs](https://example.com/docs)"}, false},
		{"user raw url + sidebar", message{role: roleUser, text: "see https://example.com now"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runURLDetect(t, tc.msg, tc.sidebar)
		})
	}
}

func runURLDetect(t *testing.T, msg message, sidebar bool) {
	m := model{
		ready:       true,
		width:       100,
		height:      30,
		sessionID:   "s1",
		showSidebar: sidebar,
		input:       textarea.New(),
		viewport:    fastviewport.New(96, 20),
		styles:      ApplyThemeColors("tokyonight"),
		messages:    []message{msg},
	}
	m.layout()
	m.renderTranscript()
	m.viewport.GotoBottom()

	// Scan every (line, X) with transcriptUrlLinkAt (the real detector, which
	// now covers both raw URLs and markdown links) and assert the URL is
	// detected somewhere.
	topY := m.viewportContentTopY()
	hits := map[int]bool{}
	var hitURL string
	for li := 0; li < len(m.rawTranscriptLines); li++ {
		y := topY + (li - m.viewport.YOffset())
		for x := 0; x < m.width; x++ {
			if r, ok := m.transcriptUrlLinkAt(tea.Mouse{X: x, Y: y}); ok {
				hits[x] = true
				hitURL = r.url
				t.Logf("HIT at line=%d x=%d url=%q", li, x, r.url)
			}
		}
	}
	if len(hits) == 0 {
		t.Fatalf("transcriptUrlLinkAt never detected the URL across the transcript — detection is broken. rawTranscriptLines=%#v", m.rawTranscriptLines)
	}
	// Report where hits land.
	minHit, maxHit := 1<<30, -1
	for x := range hits {
		if x < minHit {
			minHit = x
		}
		if x > maxHit {
			maxHit = x
		}
	}
	fmt.Printf("detected url=%q; hits span screen X=[%d,%d]\n", hitURL, minHit, maxHit)
}

// TestURLClickOpensDialog drives the real click handler (press + release)
// through handleMouseAction at a column where transcriptUrlLinkAt reports a
// hit, and asserts the confirmation dialog actually opens. This exercises the
// full path the user hits, not just the detector.
func TestURLClickOpensDialog(t *testing.T) {
	cases := []struct {
		name    string
		msg     message
		sidebar bool
	}{
		{"user raw url", message{role: roleUser, text: "see https://example.com now"}, false},
		{"assistant raw url", message{role: roleAssistant, text: "docs at https://example.com/path?x=1 please read"}, false},
		{"user raw url + sidebar", message{role: roleUser, text: "see https://example.com now"}, true},
		{"user markdown link", message{role: roleUser, text: "open [the docs](https://example.com/docs) now"}, false},
		{"assistant markdown link", message{role: roleAssistant, text: "see [the docs](https://example.com/docs) for details"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := model{
				ready:       true,
				width:       100,
				height:      30,
				sessionID:   "s1",
				showSidebar: tc.sidebar,
				input:       textarea.New(),
				viewport:    fastviewport.New(96, 20),
				styles:      ApplyThemeColors("tokyonight"),
				messages:    []message{tc.msg},
			}
			m.layout()
			m.renderTranscript()
			m.viewport.GotoBottom()

			// Find a (line, X) where transcriptUrlLinkAt reports a hit by scanning.
			topY := m.viewportContentTopY()
			var clickY, hitX int
			found := false
			for li := 0; li < len(m.rawTranscriptLines); li++ {
				y := topY + (li - m.viewport.YOffset())
				for x := 0; x < m.width; x++ {
					if _, ok := m.transcriptUrlLinkAt(tea.Mouse{X: x, Y: y}); ok {
						clickY, hitX, found = y, x, true
						break
					}
				}
				if found {
					break
				}
			}
			if !found {
				t.Fatalf("no clickable URL found in transcript for msg %q", tc.msg.text)
			}

			// Press then release on the URL.
			out1, _, _ := m.handleMouseAction(tea.Mouse{X: hitX, Y: clickY}, true)
			m1 := out1.(model)
			out2, _, handled := m1.handleMouseAction(tea.Mouse{X: hitX, Y: clickY}, false)
			m2 := out2.(model)
			if !handled {
				t.Errorf("release on URL should be handled")
			}
			if !m2.showURLDialog {
				t.Errorf("clicking a URL should open the confirmation dialog (showURLDialog=false)")
			}
			if !strings.HasPrefix(m2.pendingURL, "https://") {
				t.Errorf("pendingURL = %q", m2.pendingURL)
			}
		})
	}
}

// TestURLDuplicateMarkdownLabelsMapsBothLinks verifies that repeated markdown
// labels in one message resolve to distinct visible spans instead of all
// mapping to the first matching substring.
func TestURLDuplicateMarkdownLabelsMapsBothLinks(t *testing.T) {
	msg := message{role: roleUser, text: "open [docs](https://a.example.com) and [docs](https://b.example.com) now"}
	m := model{
		ready:     true,
		width:     100,
		height:    30,
		sessionID: "s1",
		input:     textarea.New(),
		viewport:  fastviewport.New(96, 20),
		styles:    ApplyThemeColors("tokyonight"),
		messages:  []message{msg},
	}
	m.layout()
	m.renderTranscript()
	m.viewport.GotoBottom()

	topY := m.viewportContentTopY()
	urls := map[string]bool{}
	for li := 0; li < len(m.rawTranscriptLines); li++ {
		y := topY + (li - m.viewport.YOffset())
		for x := 0; x < m.width; x++ {
			if r, ok := m.transcriptUrlLinkAt(tea.Mouse{X: x, Y: y}); ok {
				urls[r.url] = true
			}
		}
	}
	if !urls["https://a.example.com"] || !urls["https://b.example.com"] {
		t.Fatalf("duplicate markdown labels should map both URLs, got %v", urls)
	}
}

// TestURLModClickOpensDirectly verifies that ctrl+click (and cmd+click) on a
// URL bypasses the confirmation dialog and returns a browser-open command
// immediately — the plain-click path keeps the confirm dialog.
func TestURLModClickOpensDirectly(t *testing.T) {
	for _, mod := range []tea.KeyMod{tea.ModCtrl, tea.ModSuper} {
		m := model{
			ready:     true,
			width:     100,
			height:    30,
			sessionID: "s1",
			input:     textarea.New(),
			viewport:  fastviewport.New(96, 20),
			styles:    ApplyThemeColors("tokyonight"),
			messages:  []message{{role: roleUser, text: "see https://example.com now"}},
		}
		m.layout()
		m.renderTranscript()
		m.viewport.GotoBottom()

		topY := m.viewportContentTopY()
		var clickY, hitX int
		found := false
		for li := 0; li < len(m.rawTranscriptLines); li++ {
			y := topY + (li - m.viewport.YOffset())
			for x := 0; x < m.width; x++ {
				if _, ok := m.transcriptUrlLinkAt(tea.Mouse{X: x, Y: y}); ok {
					clickY, hitX, found = y, x, true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			t.Fatal("no clickable URL found in transcript")
		}

		out1, _, _ := m.handleMouseAction(tea.Mouse{X: hitX, Y: clickY, Mod: mod}, true)
		m1 := out1.(model)
		out2, cmd, handled := m1.handleMouseAction(tea.Mouse{X: hitX, Y: clickY, Mod: mod}, false)
		m2 := out2.(model)
		if !handled {
			t.Errorf("mod=%v: release on URL should be handled", mod)
		}
		if m2.showURLDialog {
			t.Errorf("mod=%v: mod+click must NOT open the confirmation dialog", mod)
		}
		if cmd == nil {
			t.Errorf("mod=%v: mod+click should return a browser-open command", mod)
		}
	}
}
