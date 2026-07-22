package tui

const (
	tabChat    = 0
	tabAgents  = 1
	tabFiles   = 2
	tabChanges = 3
	tabGit     = 4
	tabLog     = 5
	tabCount   = 6
)

func renderTabBar(active int, unread bool) string {
	labels := []string{"chat", "agents", "files", "changes", "git", "log"}
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
