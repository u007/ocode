package remote

import (
	"strings"
	"testing"
)

func TestParseShellProbeParsesProbe(t *testing.T) {
	info := ParseShellProbe(
		"shell=/bin/bash\n" +
			"available=/bin/bash\n" +
			"available=/bin/bash\n" + // duplicate must collapse
			"available=/usr/bin/zsh\n",
	)

	if info.Default != "/bin/bash" {
		t.Fatalf("Default = %q, want /bin/bash", info.Default)
	}
	if len(info.Available) != 2 || info.Available[0] != "/bin/bash" || info.Available[1] != "/usr/bin/zsh" {
		t.Fatalf("Available = %v, want [/bin/bash /usr/bin/zsh]", info.Available)
	}
}

// A remote $SHELL naming a shell the host does not have is the failure this
// probe exists to prevent, so the probe must never report it as the default.
func TestParseShellProbeIgnoresUnusableDefault(t *testing.T) {
	// The remote's own shell loop skipped /bin/zsh (it is not executable
	// there), so it reports /bin/bash — the probe trusts the remote's answer.
	info := ParseShellProbe("shell=/bin/bash\navailable=/bin/bash\navailable=/bin/sh\n")
	if info.Default != "/bin/bash" {
		t.Fatalf("Default = %q, want /bin/bash", info.Default)
	}
	for _, s := range info.Available {
		if s == "/bin/zsh" {
			t.Fatalf("unusable /bin/zsh must not be reported: %v", info.Available)
		}
	}
}

func TestParseShellProbeDegradesToSh(t *testing.T) {
	if info := ParseShellProbe(""); info.Default != "/bin/sh" {
		t.Fatalf("empty probe: Default = %q, want /bin/sh", info.Default)
	}

	// MOTD/banner noise ahead of the probe output must not corrupt the result.
	if info := ParseShellProbe("Welcome to Ubuntu 24.04\nshell=/bin/bash\n"); info.Default != "/bin/bash" {
		t.Fatalf("banner noise: Default = %q, want /bin/bash", info.Default)
	}

	// A probe that prints only noise still yields the /bin/sh baseline.
	info := ParseShellProbe("Last login: Tue\n")
	if info.Default != "/bin/sh" || len(info.Available) != 0 {
		t.Fatalf("all-noise probe = %+v, want /bin/sh with no shells", info)
	}
}

func TestParseShellProbePreservesFallbackOnEmptyDefault(t *testing.T) {
	info := parseShellProbe("shell=\navailable=/bin/bash\n", RemoteShellInfo{Default: "/bin/sh"})
	if info.Default != "/bin/sh" {
		t.Fatalf("empty shell= must keep the fallback, got %q", info.Default)
	}
	if len(info.Available) != 1 || info.Available[0] != "/bin/bash" {
		t.Fatalf("Available = %v, want [/bin/bash]", info.Available)
	}
}

// The login script must not rely on `${SHELL:-...}`: that only guards an unset
// variable, so a *stale* (set but nonexistent) remote $SHELL would still fail.
func TestLoginShellScriptFallsThroughStaleSHELL(t *testing.T) {
	body := loginShellBody("echo hi")

	for _, want := range []string{`"$SHELL"`, "/bin/bash", "/usr/bin/zsh", "/bin/sh", `[ -x "$c" ]`, `exec "$c" -l -c 'echo hi'`} {
		if !strings.Contains(body, want) {
			t.Fatalf("script missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "${SHELL:-") {
		t.Fatalf("script still uses unset-only expansion:\n%s", body)
	}
	// The command must be single-quoted so shell metacharacters cannot break out.
	if !strings.Contains(body, `'echo hi'`) {
		t.Fatalf("command not quoted:\n%s", body)
	}
}

// Every remote script is a POSIX program, but ssh hands the command string to
// the remote *login* shell for parsing. The scripts must therefore be wrapped
// in `sh -c '…'` so a tcsh/csh/fish login shell never parses the for/case
// loops (unwrapped, tcsh loops forever printing "Missing '}'").
func TestRemoteScriptsAreIsolatedFromLoginShell(t *testing.T) {
	cases := map[string]string{
		"LoginShellScript":     LoginShellScript("echo hi"),
		"CdedLoginShellScript": CdedLoginShellScript("/srv/app", "echo hi"),
		"ShellProbeCommand":    ShellProbeCommand(),
		"launchScriptWithCd":   launchScriptWithCd("/srv/app"),
	}
	for name, script := range cases {
		if !strings.HasPrefix(script, "sh -c '") || !strings.HasSuffix(script, "'") {
			t.Fatalf("%s is not wrapped in sh -c '…':\n%s", name, script)
		}
	}
	// The wrapper must be the only thing the login shell sees: the inner body
	// is one single-quoted word, so `for`/`case`/`&&` never reach it unquoted.
	if LoginShellScript("echo hi") != "sh -c "+shellQuote(loginShellBody("echo hi")) {
		t.Fatalf("LoginShellScript does not quote its body as one word:\n%s", LoginShellScript("echo hi"))
	}
	// A cd prefix belongs inside the wrapper so /bin/sh, not the login shell,
	// expands "$HOME".
	if got := CdedLoginShellScript("~/app", "pwd"); !strings.HasPrefix(got, `sh -c 'cd "$HOME/app" && `) {
		t.Fatalf("cd prefix escaped the sh -c wrapper:\n%s", got)
	}
}

func TestLoginShellScriptQuotesHostileCommand(t *testing.T) {
	body := loginShellBody(`echo '; rm -rf /`)
	if !strings.Contains(body, `'echo '\''; rm -rf /'`) {
		t.Fatalf("command not safely quoted:\n%s", body)
	}
	// The outer wrapper re-quotes the whole body, so the hostile quote can
	// break out of neither layer.
	if got := LoginShellScript(`echo '; rm -rf /`); got != "sh -c "+shellQuote(body) {
		t.Fatalf("outer wrapper did not re-quote the hostile body:\n%s", got)
	}
}

// Non-interactive execution must skip a non-POSIX remote login shell (fish),
// because ocode hands it POSIX/bash syntax.
func TestLoginShellScriptSkipsNonPOSIXShell(t *testing.T) {
	body := loginShellBody("echo hi")
	if !strings.Contains(body, `case "${c##*/}" in bash|zsh|sh|dash|ksh)`) {
		t.Fatalf("missing POSIX-family filter:\n%s", body)
	}
}

// The interactive launcher deliberately does NOT filter by family: ocode hands
// it no command string, so the user's real login shell is correct there.
func TestShellLaunchScriptDoesNotFilterByFamily(t *testing.T) {
	script := shellLaunchScript()
	if strings.Contains(script, "case ") {
		t.Fatalf("interactive launcher must not filter shell families:\n%s", script)
	}
	if !strings.Contains(script, `exec "$c" -l`) || !strings.Contains(script, "exec /bin/sh -l") {
		t.Fatalf("missing exec fallback chain:\n%s", script)
	}
}

func TestCdedLoginShellScriptQuotesPath(t *testing.T) {
	script := CdedLoginShellScript("/srv/it's", "pwd")
	if want := shellQuote(`cd '/srv/it'\''s' && ` + loginShellBody("pwd")); !strings.Contains(script, want) {
		t.Fatalf("path not safely quoted:\n%s", script)
	}
}

func TestCdedLoginShellScriptOmitsEmptyPath(t *testing.T) {
	script := CdedLoginShellScript("", "pwd")
	if strings.Contains(script, "cd ") {
		t.Fatalf("empty path must not emit a cd:\n%s", script)
	}
}

func TestSortShellsPrefersBashThenZshThenSh(t *testing.T) {
	got := SortShells([]string{"/bin/sh", "/usr/bin/zsh", "/bin/bash", "/bin/dash"})
	want := []string{"/bin/bash", "/usr/bin/zsh", "/bin/dash", "/bin/sh"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SortShells = %v, want %v", got, want)
		}
	}
}

// The probe command itself must test executability, so the local side never
// receives (or caches) a path the remote cannot exec.
func TestDetectShellCmdTestsExecutability(t *testing.T) {
	cmd := ShellProbeCommand()
	for _, want := range []string{`[ -x "$c" ]`, `"$SHELL"`, "/etc/shells", `shell=`, `available=`} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("probe command missing %q:\n%s", want, cmd)
		}
	}
}
