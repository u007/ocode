package tui

import (
	"charm.land/lipgloss/v2"
)

const (
	tabChat  = 0
	tabFiles = 1
	tabGit   = 2
	tabLog   = 3
	tabCount = 4
)

func renderTabBar(active int, unread bool) string {
	labels := []string{"1:chat", "2:files", "3:git", "4:log"}
	if unread && active != tabChat {
		labels[0] = "1:chat\u25cf"
	}
	out := ""
	for i, label := range labels {
		if i == active {
			out += lipgloss.NewStyle().Bold(true).Reverse(true).Padding(0, 1).Render(label)
		} else {
			out += hintStyle.Padding(0, 1).Render(label)
		}
	}
	return out
}
