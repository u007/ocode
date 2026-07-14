package tui

const (
	tabChat   = 0
	tabAgents = 1
	tabFiles  = 2
	tabGit    = 3
	tabLog    = 4
	tabCount  = 5
)

func renderTabBar(active int, unread bool) string {
	labels := []string{"chat", "agents", "files", "git", "log"}
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
