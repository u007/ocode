package clitools

// Catalog returns the supported CLI tools with per-platform install recipes.
// Package names are keyed by PackageManager string; a missing entry means the
// manager cannot provide the tool directly (the installer then falls through
// to the next available manager, e.g. cargo/go).
func Catalog() []Tool {
	return []Tool{
		{
			Name:        "fd",
			Aliases:     []string{"fdfind"},
			Description: "Fast, user-friendly alternative to find (fd-find)",
			Project:     "https://github.com/sharkdp/fd",
			install: map[string][]string{
				"brew":   {"fd"},
				"apt":    {"fd-find"},
				"dnf":    {"fd-find"},
				"pacman": {"fd"},
				"apk":    {"fd"},
				"pkg":    {"fd"},
				"winget": {"sharkdp.fd"},
				"choco":  {"fd"},
				"scoop":  {"fd"},
				"cargo":  {"fd-find"},
			},
		},
		{
			Name:        "rg",
			Aliases:     []string{"ripgrep"},
			Description: "ripgrep — recursively search with regex, respects .gitignore",
			Project:     "https://github.com/BurntSushi/ripgrep",
			install: map[string][]string{
				"brew":   {"ripgrep"},
				"apt":    {"ripgrep"},
				"dnf":    {"ripgrep"},
				"pacman": {"ripgrep"},
				"apk":    {"ripgrep"},
				"pkg":    {"ripgrep"},
				"winget": {"BurntSushi.ripgrep.MSVC"},
				"choco":  {"ripgrep"},
				"scoop":  {"ripgrep"},
				"cargo":  {"ripgrep"},
			},
		},
		{
			Name:        "fzf",
			Aliases:     []string{"fzf-tmux"},
			Description: "Command-line fuzzy finder",
			Project:     "https://github.com/junegunn/fzf",
			install: map[string][]string{
				"brew":   {"fzf"},
				"apt":    {"fzf"},
				"dnf":    {"fzf"},
				"pacman": {"fzf"},
				"apk":    {"fzf"},
				"pkg":    {"fzf"},
				"winget": {"junegunn.fzf"},
				"choco":  {"fzf"},
				"scoop":  {"fzf"},
				"go":     {"github.com/junegunn/fzf@latest"},
			},
		},
		{
			Name:        "eza",
			Aliases:     []string{"exa"},
			Description: "Modern, maintained replacement for ls (successor of exa)",
			Project:     "https://eza.rocks",
			install: map[string][]string{
				"brew":   {"eza"},
				"apt":    {"eza"},
				"dnf":    {"eza"},
				"pacman": {"eza"},
				"apk":    {"eza"},
				"pkg":    {"eza"},
				"winget": {"eza-community.eza"},
				"scoop":  {"eza"},
				"cargo":  {"eza"},
			},
		},
		{
			Name:        "bat",
			Description: "cat clone with syntax highlighting and git integration",
			Project:     "https://github.com/sharkdp/bat",
			install: map[string][]string{
				"brew":   {"bat"},
				"apt":    {"bat"},
				"dnf":    {"bat"},
				"pacman": {"bat"},
				"apk":    {"bat"},
				"pkg":    {"bat"},
				"winget": {"sharkdp.bat"},
				"choco":  {"bat"},
				"scoop":  {"bat"},
				"cargo":  {"bat"},
			},
		},
		{
			Name:        "grep",
			Aliases:     []string{"ggrep"},
			Description: "GNU grep — pattern search (BSD grep on macOS is fine; ggrep adds GNU flags)",
			Project:     "https://www.gnu.org/software/grep/",
			install: map[string][]string{
				"brew":   {"grep"},
				"apt":    {"grep"},
				"dnf":    {"grep"},
				"pacman": {"grep"},
				"apk":    {"grep"},
				"pkg":    {"gnugrep"},
			},
			// grep ships with every OS; Windows has no native recipe here —
			// Git-for-Windows provides it on PATH. cargo/npm recipes omitted
			// deliberately: no canonical crate/package exists.
		},
	}
}
