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

	thumbSize := visibleLines * height / totalLines
	if thumbSize < 1 {
		thumbSize = 1
	}
	maxOffset := totalLines - visibleLines
	if maxOffset < 1 {
		maxOffset = 1
	}
	thumbTop := int(float64(offsetLines) / float64(maxOffset) * float64(height-thumbSize))

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
