package agent

// command_behavior.go models a small internal capability catalog for shell
// commands. The permission engine uses it to derive OS-specific allowlists
// without exposing capability IDs to persisted permission rules.

type commandBehavior string

const (
	commandReadOnly        commandBehavior = "read_only"
	commandMutatingBounded commandBehavior = "mutating_bounded"
	commandAlwaysAllow     commandBehavior = "always_allow"
)

type commandCapability struct {
	ID             string
	Behavior       commandBehavior
	Aliases        []string
	UnixAliases    []string
	WindowsAliases []string
}

var commandCapabilities = []commandCapability{
	{
		ID:       "fs.readtext",
		Behavior: commandReadOnly,
		Aliases: []string{
			"awk", "sed", "tr", "cat", "tac", "rev", "head", "tail",
			"less", "more", "nl", "sort", "uniq", "cut", "paste", "join",
			"comm", "column", "expand", "unexpand", "fold", "wc", "grep",
			"rg", "ag",
		},
		WindowsAliases: []string{"type", "findstr"},
	},
	{
		ID:       "fs.inspect",
		Behavior: commandReadOnly,
		Aliases: []string{
			"ls", "tree", "file", "stat", "du", "basename", "dirname",
			"realpath", "readlink", "diff", "cmp",
		},
		WindowsAliases: []string{"dir", "tree"},
	},
	{
		ID:       "fs.hash",
		Behavior: commandReadOnly,
		Aliases:  []string{"md5sum", "sha1sum", "sha256sum", "sha512sum", "shasum", "cksum"},
	},
	{
		ID:       "fs.binaryinspect",
		Behavior: commandReadOnly,
		Aliases:  []string{"xxd", "hexdump", "od", "strings"},
	},
	{
		ID:       "fs.json",
		Behavior: commandReadOnly,
		Aliases:  []string{"jq", "yq"},
	},
	{
		ID:       "fs.search",
		Behavior: commandReadOnly,
		Aliases:  []string{"find", "fd"},
	},
	{
		ID:             "fs.change_dir",
		Behavior:       commandMutatingBounded,
		Aliases:        []string{"cd"},
		WindowsAliases: []string{"cd", "chdir"},
	},
	{
		ID:             "fs.mutate.mkdir",
		Behavior:       commandMutatingBounded,
		Aliases:        []string{"mkdir"},
		WindowsAliases: []string{"md"},
	},
	{
		ID:             "shell.introspection",
		Behavior:       commandAlwaysAllow,
		Aliases:        []string{"pwd", "whoami", "hostname", "uname", "id", "tty", "date", "true", "false", ":", "echo", "printf", "which", "locale", "tput", "groups", "users", "uptime", "arch"},
		UnixAliases:    []string{"type"},
		WindowsAliases: []string{"cls", "ver", "where"},
	},
}

func buildBashAutoAllowPrefixes(goos string) map[string]bool {
	m := make(map[string]bool)
	for _, cap := range commandCapabilities {
		if cap.Behavior == commandReadOnly || cap.Behavior == commandMutatingBounded {
			for _, alias := range cap.Aliases {
				m[alias] = true
			}
			if goos != "windows" {
				for _, alias := range cap.UnixAliases {
					m[alias] = true
				}
			}
			if goos == "windows" {
				for _, alias := range cap.WindowsAliases {
					m[alias] = true
				}
			}
		}
	}
	return m
}

func buildBashAutoAllowDefaultModes(goos string) map[string]string {
	m := make(map[string]string)
	for _, cap := range commandCapabilities {
		if cap.Behavior != commandReadOnly && cap.Behavior != commandMutatingBounded {
			continue
		}
		mode := bashPrefixModeReadOnly
		if cap.Behavior == commandMutatingBounded {
			mode = bashPrefixModeMutating
		}
		for _, alias := range cap.Aliases {
			m[alias] = mode
		}
		if goos != "windows" {
			for _, alias := range cap.UnixAliases {
				m[alias] = mode
			}
		}
		if goos == "windows" {
			for _, alias := range cap.WindowsAliases {
				m[alias] = mode
			}
		}
	}
	// Mutating-but-bounded commands remain auto-allowable in-root, but we do not
	// persist them as read-only project-scoped rules.
	m["sed"] = bashPrefixModeMutating
	m["mkdir"] = bashPrefixModeMutating
	if goos == "windows" {
		m["md"] = bashPrefixModeMutating
	}
	return m
}

func buildBashAlwaysAllow(goos string) map[string]bool {
	m := make(map[string]bool)
	for _, cap := range commandCapabilities {
		if cap.Behavior != commandAlwaysAllow {
			continue
		}
		for _, alias := range cap.Aliases {
			m[alias] = true
		}
		if goos != "windows" {
			for _, alias := range cap.UnixAliases {
				m[alias] = true
			}
		}
		if goos == "windows" {
			for _, alias := range cap.WindowsAliases {
				m[alias] = true
			}
		}
	}
	return m
}
