package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// pipboyArt is the Vault Boy thumbs-up ASCII art shown as a fixed background
// when the pipboy theme is active and the transcript is empty.
// It does not live inside the scrollable viewport content so it cannot be
// scrolled or selected.
const pipboyArt = `
   .  *  .    *   .    *  .   *
  *     .   *   .    *      .
    .      ╔══════════════╗     *
  *    .   ║ ♦ PIP-BOY   ║  .
    .   *  ║   3  0  0  0 ║     .
  *        ╚══════════════╝  *
    .                             .
         .     ┌───────┐
   *           │ ◉   ◉ │       *
       .       │   ⌣   │   .
         .     │╔═════╗│
   *           ││     ││  .
       .    *  │╚═════╝│         .
               └───────┘
               ╱       ╲    *
          .   ╱─────────╲
  ┌──┐   *   │  ╔═════╗ │    .
  │▲ │───────┤  ║ 3 0 ║ ├──
  │  │   .   │  ╚═════╝ │    .
  └──┘        ╲─────────╱  *
          .        │           .
   *            ───┴───           *
    .                         .
   *    V A U L T - T E C    *
      "War never changes."
  .    *      .     *    .   *
`

// pipboyArtLines is the art split into lines, computed once.
// Trim both leading and trailing newlines so the const's opening/closing
// backtick newlines do not produce empty phantom lines.
var pipboyArtLines = strings.Split(strings.Trim(pipboyArt, "\n"), "\n")

// renderPipboyBackground returns the Pip-Boy art centered inside a box of
// (width × height) cells. artStyle controls the foreground colour — pass
// m.styles.Hint so the art uses the current theme's per-instance hint colour
// instead of the package-level dimStyle (which is pinned to tokyonight).
func renderPipboyBackground(width, height int, artStyle lipgloss.Style) string {
	if width <= 0 || height <= 0 {
		return ""
	}

	artW := 0
	for _, l := range pipboyArtLines {
		if w := lipgloss.Width(l); w > artW {
			artW = w
		}
	}
	artH := len(pipboyArtLines)

	// Centre vertically — push down by the top padding rows.
	topPad := (height - artH) / 2
	if topPad < 0 {
		topPad = 0
	}

	var sb strings.Builder
	for i := 0; i < topPad; i++ {
		sb.WriteByte('\n')
	}

	leftPad := (width - artW) / 2
	if leftPad < 0 {
		leftPad = 0
	}
	pad := strings.Repeat(" ", leftPad)

	for _, line := range pipboyArtLines {
		sb.WriteString(pad)
		sb.WriteString(artStyle.Render(line))
		sb.WriteByte('\n')
	}

	return sb.String()
}
