package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

var (
	scrollbarTrackStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#3B4261"))
	scrollbarThumbStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7AA2F7"))
)

const (
	scrollbarTrack = "┊"
	scrollbarThumb = "█"
)

func renderScrollbar(height, totalLines, visibleLines, offsetLines int) string {
	if height <= 0 {
		return ""
	}
	lines := make([]string, height)

	if totalLines <= visibleLines || totalLines == 0 {
		track := scrollbarTrackStyle.Render(scrollbarTrack)
		for i := range lines {
			lines[i] = track
		}
		return strings.Join(lines, "\n")
	}

	thumbTop, thumbSize, _ := scrollbarThumbMetrics(height, totalLines, visibleLines, offsetLines)

	track := scrollbarTrackStyle.Render(scrollbarTrack)
	thumb := scrollbarThumbStyle.Render(scrollbarThumb)
	for i := range lines {
		if i >= thumbTop && i < thumbTop+thumbSize {
			lines[i] = thumb
		} else {
			lines[i] = track
		}
	}
	return strings.Join(lines, "\n")
}

func scrollbarThumbMetrics(height, totalLines, visibleLines, offsetLines int) (top, size int, ok bool) {
	if height <= 0 || totalLines <= visibleLines || totalLines == 0 {
		return 0, 0, false
	}

	thumbSize := visibleLines * height / totalLines
	if thumbSize < 1 {
		thumbSize = 1
	}
	maxOffset := totalLines - visibleLines
	if maxOffset < 1 {
		maxOffset = 1
	}
	thumbTop := int(float64(offsetLines) / float64(maxOffset) * float64(height-thumbSize))
	return thumbTop, thumbSize, true
}

func scrollbarThumbOffset(mouseY, trackTop, trackHeight, totalLines, visibleLines, offsetLines int) (int, bool) {
	if mouseY < trackTop || mouseY >= trackTop+trackHeight {
		return 0, false
	}
	thumbTop, thumbSize, ok := scrollbarThumbMetrics(trackHeight, totalLines, visibleLines, offsetLines)
	if !ok {
		return 0, false
	}
	relY := mouseY - trackTop
	if relY < thumbTop || relY >= thumbTop+thumbSize {
		return 0, false
	}
	return relY - thumbTop, true
}

func renderListScrollbar(height, totalItems, visibleStart, visibleCount int) string {
	if height <= 0 {
		return ""
	}
	lines := make([]string, height)

	if totalItems <= visibleCount || totalItems == 0 {
		track := scrollbarTrackStyle.Render(scrollbarTrack)
		for i := range lines {
			lines[i] = track
		}
		return strings.Join(lines, "\n")
	}

	thumbSize := visibleCount * height / totalItems
	if thumbSize < 1 {
		thumbSize = 1
	}
	maxStart := totalItems - visibleCount
	if maxStart < 1 {
		maxStart = 1
	}
	thumbTop := int(float64(visibleStart) / float64(maxStart) * float64(height-thumbSize))

	track := scrollbarTrackStyle.Render(scrollbarTrack)
	thumb := scrollbarThumbStyle.Render(scrollbarThumb)
	for i := range lines {
		if i >= thumbTop && i < thumbTop+thumbSize {
			lines[i] = thumb
		} else {
			lines[i] = track
		}
	}
	return strings.Join(lines, "\n")
}
