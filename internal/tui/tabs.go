package tui

const (
	tabChat  = 0
	tabFiles = 1
	tabGit   = 2
	tabLog   = 3
	tabCount = 4
)

func renderTabBar(active int, unread bool) string {
	labels := []string{"chat", "files", "git", "log"}
	if unread && active != tabChat {
		labels[0] = "chat●"
	}
	out := ""
	for i, label := range labels {
		if i == active {
			out += selectedStyle.Padding(0, 1).Render(label)
		} else {
			out += hintStyle.Padding(0, 1).Render(label)
		}
	}
	return out
}
