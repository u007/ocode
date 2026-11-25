package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/u007/ocode/internal/crashguard"
	"github.com/u007/ocode/internal/shell/sandbox"
	"github.com/u007/ocode/internal/tool"
)

// inspectShellDeadline bounds one inspect_shell process end to end. A variable
// so a test can shorten it and prove the kill path fires.
var inspectShellDeadline = 10 * time.Second

// inspectShellReapGrace bounds how long a terminated process gets to exit
// before its leader is killed outright.
var inspectShellReapGrace = time.Second

// inspectShellSandbox returns the confinement backend. A variable so tests can
// substitute an unavailable backend.
var inspectShellSandbox = sandbox.New

// inspectShellPrograms is the complete program allowlist. Anything else,
// including every absolute path and every project-relative name, is refused.
var inspectShellPrograms = map[string]bool{
	"pwd": true, "ls": true, "stat": true, "file": true, "readlink": true,
	"realpath": true, "wc": true, "head": true, "tail": true,
	"md5": true, "md5sum": true, "shasum": true, "sha1sum": true,
	"sha256sum": true, "sha512sum": true, "find": true, "go": true, "git": true,
}

// inspectShellEmptyTree is the SHA-1 empty tree. Pointing attribute lookup at it
// stops Git from reading the worktree's .gitattributes, so no filter or textconv
// driver a hostile repository names is ever selected. The flag is an argv option
// rather than an environment variable so an older Git refuses it (fail closed)
// instead of silently ignoring it. SHA-256 repositories are refused by Git
// itself, which also fails closed.
const inspectShellEmptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// inspectShellGitPrefix is the only Git prefix the runner ever executes. Model
// tokens are validated after it and never copied into it. core.attributesFile
// is pointed at /dev/null because a repository's own config can name an
// attributes file outside the tree, and --attr-source does not cover it.
var inspectShellGitPrefix = []string{
	"--no-pager", "-c", "core.fsmonitor=false", "-c", "core.pager=cat",
	"-c", "diff.external=", "-c", "core.hooksPath=/dev/null",
	"-c", "core.attributesFile=/dev/null",
	"--attr-source=" + inspectShellEmptyTree,
}

var (
	inspectRevisionRE   = regexp.MustCompile(`^[A-Za-z0-9._/@{}~^+][A-Za-z0-9._/@{}~^+-]*$`)
	inspectGoEnvNameRE  = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	inspectDigitsRE     = regexp.MustCompile(`^[0-9]{1,9}$`)
	inspectHeadTailFlag = regexp.MustCompile(`^-[nc][0-9]{1,9}$`)
)

// validatedCommand is one admitted inspection: the bare program name, the
// argv that follows it (Git prefix included), and the canonical project paths
// of its operands.
type validatedCommand struct {
	Program      string
	Args         []string
	ProjectPaths []string
}

// inspectErr is the one error shape for refusals. Messages name operand
// positions, never operand text, so a secret passed as an operand is not echoed.
func inspectErr(format string, args ...any) error {
	return fmt.Errorf("inspect_shell: "+format, args...)
}

// inspectShellBinaryDirs returns the fixed trusted directories, in order. The
// executed program is always the absolute candidate found in one of these; a
// project-local executable can never substitute. Windows has none, so the tool
// is disabled there.
func inspectShellBinaryDirs() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin",
			"/opt/homebrew/sbin", "/usr/local/sbin", "/usr/sbin", "/sbin", "/usr/local/go/bin"}
	case "linux":
		return []string{"/usr/local/bin", "/usr/bin", "/bin", "/usr/local/sbin",
			"/usr/sbin", "/sbin", "/usr/local/go/bin", "/usr/lib/go/bin", "/opt/go/bin"}
	default:
		return nil
	}
}

// inspectShellEnv is the complete environment of an inspection process. It is
// built from scratch: no provider credential or routing variable is inherited.
// HOME is passed only so `go env` resolves its defaults.
func inspectShellEnv() []string {
	env := []string{
		"PATH=" + strings.Join(inspectShellBinaryDirs(), string(os.PathListSeparator)),
		"LC_ALL=C",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_TERMINAL_PROMPT=0",
		"GOTOOLCHAIN=local",
	}
	if home := os.Getenv("HOME"); home != "" {
		env = append(env, "HOME="+home)
	}
	return env
}

// resolveInspectShellProgram returns the absolute path of name in the first
// trusted directory that holds an executable regular file of that name.
func resolveInspectShellProgram(dirs []string, name string) (string, error) {
	for _, dir := range dirs {
		p := filepath.Join(dir, name)
		info, err := os.Stat(p)
		if err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", inspectErr("program %s is not installed in the trusted directories", name)
}

// inspectTokenOK rejects empty tokens, control characters, and every character
// that a shell would parse. No shell runs, but such tokens are never valid.
func inspectTokenOK(tok string) bool {
	if tok == "" {
		return false
	}
	for _, r := range tok {
		if r < 0x20 || r == 0x7f {
			return false
		}
		if strings.ContainsRune(";|&$`<>()\\", r) {
			return false
		}
	}
	return true
}

// inspectOperand validates one path operand against the project. It returns the
// canonical absolute path and the project-relative form. Escapes, symlinks out
// of the project, sensitive names, and special files are refused.
func inspectOperand(workDir, tok string) (canon, rel string, err error) {
	root, err := canonicalProjectRoot(workDir)
	if err != nil {
		return "", "", err
	}
	canon, err = validateProjectPath(workDir, tok)
	if err != nil {
		return "", "", inspectErr("a path operand is outside the project or cannot be resolved")
	}
	if sensitiveNamePath(workDir, tok) || sensitiveNamePath(workDir, canon) {
		return "", "", inspectErr("a path operand names a sensitive path")
	}
	if specialFilePath(canon) {
		return "", "", inspectErr("a path operand is not a regular file or directory")
	}
	rel, err = filepath.Rel(root, canon)
	if err != nil {
		return "", "", inspectErr("a path operand could not be made project-relative")
	}
	return canon, rel, nil
}

// inspectPathArg is how a validated operand is passed to a program: relative to
// the project root and always "./"-prefixed so it can never read as a flag.
func inspectPathArg(rel string) string {
	if rel == "." {
		return "."
	}
	return "./" + rel
}

// splitInspectFlags separates leading flags from operands for a program with
// no sub-commands. A flag outside allowed, or any flag after the first operand,
// is refused.
func splitInspectFlags(prog string, args []string, allowed map[string]bool) (flags, operands []string, err error) {
	for i, tok := range args {
		if !strings.HasPrefix(tok, "-") {
			operands = append(operands, tok)
			continue
		}
		if len(operands) > 0 || !allowed[tok] {
			return nil, nil, inspectErr("%s: argument %d is a flag that is not allowed here", prog, i+1)
		}
		flags = append(flags, tok)
	}
	return flags, operands, nil
}

// inspectOperands validates every operand as a project path and returns the
// argv forms and canonical paths.
func inspectOperands(workDir string, ops []string) (args, canons []string, err error) {
	for _, op := range ops {
		canon, rel, err := inspectOperand(workDir, op)
		if err != nil {
			return nil, nil, err
		}
		args = append(args, inspectPathArg(rel))
		canons = append(canons, canon)
	}
	return args, canons, nil
}

// inspectRegularOperands is inspectOperands for programs that read file
// contents: each operand must be an existing regular file.
func inspectRegularOperands(prog, workDir string, ops []string) (args, canons []string, err error) {
	if len(ops) == 0 {
		return nil, nil, inspectErr("%s needs at least one file operand", prog)
	}
	args, canons, err = inspectOperands(workDir, ops)
	if err != nil {
		return nil, nil, err
	}
	for i, canon := range canons {
		info, serr := os.Stat(canon)
		if serr != nil {
			return nil, nil, inspectErr("%s: operand %d cannot be read", prog, i+1)
		}
		if !info.Mode().IsRegular() {
			return nil, nil, inspectErr("%s: operand %d is not a regular file", prog, i+1)
		}
	}
	return args, canons, nil
}

// validateInspectShellArgv admits one argv or refuses it. The result is the
// exact argv to execute, built here, with no shell and no model-supplied Git
// configuration.
func validateInspectShellArgv(workDir string, argv []string) (validatedCommand, error) {
	if len(argv) == 0 {
		return validatedCommand{}, inspectErr("argv is empty")
	}
	for i, tok := range argv {
		if !inspectTokenOK(tok) {
			return validatedCommand{}, inspectErr("argv element %d is empty or contains a disallowed character", i)
		}
	}
	prog, args := argv[0], argv[1:]
	if !inspectShellPrograms[prog] {
		return validatedCommand{}, inspectErr("argv element 0 is not an allowed program")
	}
	switch prog {
	case "pwd":
		if len(args) != 0 {
			return validatedCommand{}, inspectErr("pwd takes no arguments")
		}
		return validatedCommand{Program: prog}, nil
	case "ls":
		return inspectLs(workDir, args)
	case "stat":
		return inspectSimple(prog, workDir, nil, args, false)
	case "file":
		return inspectSimple(prog, workDir, map[string]bool{"-b": true}, args, false)
	case "readlink":
		return inspectSimple(prog, workDir, map[string]bool{"-f": true}, args, false)
	case "realpath":
		return inspectSimple(prog, workDir, nil, args, false)
	case "wc":
		return inspectSimple(prog, workDir, map[string]bool{"-l": true, "-w": true, "-c": true, "-m": true}, args, true)
	case "head", "tail":
		return inspectHeadTail(prog, workDir, args)
	case "md5", "md5sum", "shasum", "sha1sum", "sha256sum", "sha512sum":
		return inspectSimple(prog, workDir, nil, args, true)
	case "find":
		return inspectFind(workDir, args)
	case "go":
		return inspectGo(args)
	case "git":
		return inspectGit(workDir, args)
	}
	return validatedCommand{}, inspectErr("argv element 0 is not an allowed program")
}

// inspectSimple admits one program with a fixed flag set and path operands.
// regular requires every operand to be a regular file.
func inspectSimple(prog, workDir string, allowed map[string]bool, args []string, regular bool) (validatedCommand, error) {
	flags, ops, err := splitInspectFlags(prog, args, allowed)
	if err != nil {
		return validatedCommand{}, err
	}
	var pathArgs, canons []string
	if regular {
		pathArgs, canons, err = inspectRegularOperands(prog, workDir, ops)
	} else {
		if len(ops) == 0 {
			return validatedCommand{}, inspectErr("%s needs at least one operand", prog)
		}
		pathArgs, canons, err = inspectOperands(workDir, ops)
	}
	if err != nil {
		return validatedCommand{}, err
	}
	return validatedCommand{Program: prog, Args: append(flags, pathArgs...), ProjectPaths: canons}, nil
}

// inspectLs admits ls with read-only formatting flags. The runner always adds
// -1 so output is one name per line and can be filtered by name.
func inspectLs(workDir string, args []string) (validatedCommand, error) {
	allowed := map[string]bool{"-a": true, "-A": true, "-d": true, "-F": true, "-p": true, "-r": true, "-t": true, "-S": true}
	flags, ops, err := splitInspectFlags("ls", args, allowed)
	if err != nil {
		return validatedCommand{}, err
	}
	pathArgs, canons, err := inspectOperands(workDir, ops)
	if err != nil {
		return validatedCommand{}, err
	}
	argv := append(flags, "-1")
	argv = append(argv, pathArgs...)
	return validatedCommand{Program: "ls", Args: argv, ProjectPaths: canons}, nil
}

// inspectHeadTail admits head or tail with a byte or line count and regular
// file operands. -f and -F are not in the flag set, so they are refused.
func inspectHeadTail(prog, workDir string, args []string) (validatedCommand, error) {
	var flags, ops []string
	for i := 0; i < len(args); i++ {
		tok := args[i]
		if !strings.HasPrefix(tok, "-") {
			ops = append(ops, tok)
			continue
		}
		if len(ops) > 0 {
			return validatedCommand{}, inspectErr("%s: flags must precede operands", prog)
		}
		switch {
		case (tok == "-n" || tok == "-c") && i+1 < len(args) && inspectDigitsRE.MatchString(args[i+1]):
			flags = append(flags, tok, args[i+1])
			i++
		case inspectHeadTailFlag.MatchString(tok):
			flags = append(flags, tok)
		default:
			return validatedCommand{}, inspectErr("%s: argument %d is a flag that is not allowed", prog, i+1)
		}
	}
	pathArgs, canons, err := inspectRegularOperands(prog, workDir, ops)
	if err != nil {
		return validatedCommand{}, err
	}
	return validatedCommand{Program: prog, Args: append(flags, pathArgs...), ProjectPaths: canons}, nil
}

// inspectFind admits print-only traversal. Operands come first; then only the
// listed tests, with an explicit -print. -exec, -delete, -fprint, -L and every
// other primary are refused.
func inspectFind(workDir string, args []string) (validatedCommand, error) {
	i := 0
	var ops []string
	for i < len(args) && !strings.HasPrefix(args[i], "-") {
		ops = append(ops, args[i])
		i++
	}
	if len(ops) == 0 {
		ops = []string{"."}
	}
	pathArgs, canons, err := inspectOperands(workDir, ops)
	if err != nil {
		return validatedCommand{}, err
	}
	argv := append([]string{}, pathArgs...)
	printSeen := false
	for i < len(args) {
		tok := args[i]
		switch tok {
		case "-print":
			printSeen = true
			argv = append(argv, tok)
			i++
		case "-name", "-iname", "-path":
			if i+1 >= len(args) || args[i+1] == "" {
				return validatedCommand{}, inspectErr("find: expression %d needs a non-empty pattern", i+1)
			}
			argv = append(argv, tok, args[i+1])
			i += 2
		case "-type":
			if i+1 >= len(args) || len(args[i+1]) != 1 || !strings.Contains("bcdflps", args[i+1]) {
				return validatedCommand{}, inspectErr("find: expression %d needs a single type letter", i+1)
			}
			argv = append(argv, tok, args[i+1])
			i += 2
		case "-maxdepth", "-mindepth":
			if i+1 >= len(args) || !inspectDigitsRE.MatchString(args[i+1]) {
				return validatedCommand{}, inspectErr("find: expression %d needs a non-negative integer", i+1)
			}
			argv = append(argv, tok, args[i+1])
			i += 2
		default:
			return validatedCommand{}, inspectErr("find: expression %d is not an allowed test", i+1)
		}
	}
	if !printSeen {
		return validatedCommand{}, inspectErr("find requires an explicit -print")
	}
	return validatedCommand{Program: "find", Args: argv, ProjectPaths: canons}, nil
}

// inspectGo admits `go version` and `go env` with optional -json and variable
// names. Every other subcommand and every -w or -u is refused.
func inspectGo(args []string) (validatedCommand, error) {
	if len(args) == 0 {
		return validatedCommand{}, inspectErr("go needs a subcommand")
	}
	switch args[0] {
	case "version":
		if len(args) != 1 {
			return validatedCommand{}, inspectErr("go version takes no arguments")
		}
		return validatedCommand{Program: "go", Args: []string{"version"}}, nil
	case "env":
		argv := []string{"env"}
		rest := args[1:]
		if len(rest) > 0 && rest[0] == "-json" {
			argv = append(argv, "-json")
			rest = rest[1:]
		}
		for i, name := range rest {
			if !inspectGoEnvNameRE.MatchString(name) {
				return validatedCommand{}, inspectErr("go env: argument %d is not a variable name", i+1)
			}
			argv = append(argv, name)
		}
		return validatedCommand{Program: "go", Args: argv}, nil
	}
	return validatedCommand{}, inspectErr("go: subcommand is not allowed")
}

// splitInspectGitArgs separates Git flags from the non-flag tokens before "--"
// (pre) and the tokens after it (post). Unknown flags are refused.
func splitInspectGitArgs(sub string, rest []string, allowed map[string]bool) (flags, pre, post []string, err error) {
	seenSep := false
	for i, tok := range rest {
		switch {
		case seenSep:
			post = append(post, tok)
		case tok == "--":
			seenSep = true
		case strings.HasPrefix(tok, "-"):
			if !allowed[tok] {
				return nil, nil, nil, inspectErr("git %s: argument %d is a flag that is not allowed", sub, i+1)
			}
			flags = append(flags, tok)
		default:
			pre = append(pre, tok)
		}
	}
	return flags, pre, post, nil
}

// inspectPathspecs validates Git pathspecs as project paths. Pathspec magic
// (a leading ":") is refused. The argv form is project-relative because Git runs
// from the project root.
func inspectPathspecs(workDir string, toks []string) (args, canons []string, err error) {
	for _, tok := range toks {
		if strings.HasPrefix(tok, ":") {
			return nil, nil, inspectErr("git: pathspec magic is not allowed")
		}
		canon, rel, err := inspectOperand(workDir, tok)
		if err != nil {
			return nil, nil, err
		}
		args = append(args, rel)
		canons = append(canons, canon)
	}
	return args, canons, nil
}

// inspectGit admits the four metadata subcommands under the fixed hardening
// prefix. Patch, history, object and content forms are not in any allowlist.
func inspectGit(workDir string, args []string) (validatedCommand, error) {
	if len(args) == 0 {
		return validatedCommand{}, inspectErr("git needs a subcommand")
	}
	var body, canons []string
	var err error
	switch args[0] {
	case "status":
		body, canons, err = inspectGitStatus(workDir, args[1:])
	case "diff":
		body, canons, err = inspectGitDiff(workDir, args[1:])
	case "rev-parse":
		body, err = inspectGitRevParse(args[1:])
	case "ls-files":
		body, canons, err = inspectGitLsFiles(workDir, args[1:])
	default:
		return validatedCommand{}, inspectErr("git: subcommand is not allowed")
	}
	if err != nil {
		return validatedCommand{}, err
	}
	argv := append(append([]string{}, inspectShellGitPrefix...), body...)
	return validatedCommand{Program: "git", Args: argv, ProjectPaths: canons}, nil
}

// inspectGitStatus always requests NUL-delimited porcelain v1, so the output
// can be parsed as records before any sensitive filtering.
func inspectGitStatus(workDir string, rest []string) ([]string, []string, error) {
	allowed := map[string]bool{
		"--branch": true, "-b": true, "--ignored": true, "--untracked-files": true,
		"--untracked-files=no": true, "--untracked-files=normal": true, "--untracked-files=all": true,
		"--short": true, "-s": true, "--porcelain": true, "-z": true,
	}
	flags, pre, post, err := splitInspectGitArgs("status", rest, allowed)
	if err != nil {
		return nil, nil, err
	}
	body := []string{"status", "--porcelain=v1", "-z"}
	for _, f := range flags {
		switch f {
		case "--short", "-s", "--porcelain", "-z":
			// The output format is fixed above; these are accepted and ignored.
		default:
			body = append(body, f)
		}
	}
	pathArgs, canons, err := inspectPathspecs(workDir, append(pre, post...))
	if err != nil {
		return nil, nil, err
	}
	body = append(body, "--")
	return append(body, pathArgs...), canons, nil
}

// inspectGitDiff admits exactly one metadata format. Patch and content flags
// are not in the allowlist. --no-ext-diff and --no-textconv are always forced.
func inspectGitDiff(workDir string, rest []string) ([]string, []string, error) {
	allowed := map[string]bool{
		"--name-only": true, "--name-status": true, "--stat": true, "--numstat": true,
		"--cached": true, "--staged": true, "-z": true,
	}
	flags, revs, paths, err := splitInspectGitArgs("diff", rest, allowed)
	if err != nil {
		return nil, nil, err
	}
	var formats, cached []string
	for _, f := range flags {
		switch f {
		case "--name-only", "--name-status", "--stat", "--numstat":
			formats = append(formats, f)
		case "--cached", "--staged":
			cached = append(cached, f)
		}
	}
	if len(formats) != 1 {
		return nil, nil, inspectErr("git diff needs exactly one of --name-only, --name-status, --stat or --numstat")
	}
	if len(revs) > 2 {
		return nil, nil, inspectErr("git diff accepts at most two revisions")
	}
	for i, rev := range revs {
		if !inspectRevisionRE.MatchString(rev) {
			return nil, nil, inspectErr("git diff: revision %d is not allowed", i+1)
		}
	}
	pathArgs, canons, err := inspectPathspecs(workDir, paths)
	if err != nil {
		return nil, nil, err
	}
	body := []string{"diff", "--no-ext-diff", "--no-textconv"}
	body = append(body, cached...)
	body = append(body, formats[0])
	if formats[0] != "--stat" {
		body = append(body, "-z")
	}
	body = append(body, revs...)
	body = append(body, "--")
	return append(body, pathArgs...), canons, nil
}

// inspectGitRevParse admits the exact rev-parse forms the explainer needs.
func inspectGitRevParse(rest []string) ([]string, error) {
	metadata := map[string]bool{
		"--show-toplevel": true, "--show-prefix": true, "--is-inside-work-tree": true,
		"--is-shallow-repository": true, "--show-superproject-working-tree": true,
	}
	switch {
	case len(rest) == 1 && metadata[rest[0]]:
		return []string{"rev-parse", rest[0]}, nil
	case len(rest) == 2 && rest[0] == "--verify" && inspectRevisionRE.MatchString(rest[1]):
		return []string{"rev-parse", "--verify", rest[1]}, nil
	case len(rest) == 1 && inspectRevisionRE.MatchString(rest[0]):
		return []string{"rev-parse", rest[0]}, nil
	}
	return nil, inspectErr("git rev-parse: only the documented metadata forms are allowed")
}

// inspectGitLsFiles admits path listing with the long read-only selection flags.
func inspectGitLsFiles(workDir string, rest []string) ([]string, []string, error) {
	allowed := map[string]bool{
		"--cached": true, "--others": true, "--exclude-standard": true, "--modified": true,
		"--deleted": true, "--unmerged": true, "--ignored": true, "-z": true,
	}
	flags, pre, post, err := splitInspectGitArgs("ls-files", rest, allowed)
	if err != nil {
		return nil, nil, err
	}
	body := []string{"ls-files"}
	for _, f := range flags {
		if f != "-z" {
			body = append(body, f)
		}
	}
	body = append(body, "-z", "--")
	pathArgs, canons, err := inspectPathspecs(workDir, append(pre, post...))
	if err != nil {
		return nil, nil, err
	}
	return append(body, pathArgs...), canons, nil
}

// inspectExitCode maps a Wait error to an exit status. A signal-ended process
// reports -1 through ExitCode.
func inspectExitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return 1
}

// filterInspectShellOutput removes sensitive entries from output that names
// project paths, before the text is masked and capped. Filtering runs on the
// raw text because names are only meaningful before masking.
func filterInspectShellOutput(workDir string, v validatedCommand, out string) string {
	switch v.Program {
	case "ls":
		return filterInspectLs(workDir, v.Args, out)
	case "find":
		return filterInspectLines(out, func(line string) bool {
			return sensitivePathFilter(workDir, line)
		})
	case "git":
		return filterInspectGit(workDir, v.Args, out)
	}
	return out
}

// filterInspectLines keeps lines the predicate does not drop.
func filterInspectLines(out string, drop func(string) bool) string {
	lines := strings.Split(out, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if line != "" && drop(line) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// filterInspectLs drops sensitive names from ls -1 output. A header line
// ("./dir:") from a multi-operand listing switches the base directory.
func filterInspectLs(workDir string, args []string, out string) string {
	var operands []string
	for _, a := range args {
		if a == "." || strings.HasPrefix(a, "./") {
			operands = append(operands, a)
		}
	}
	base := ""
	if len(operands) == 1 {
		base = inspectOperandRel(operands[0])
	}
	lines := strings.Split(out, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if len(operands) > 1 && strings.HasSuffix(line, ":") {
			for _, op := range operands {
				if strings.TrimSuffix(line, ":") == op {
					base = inspectOperandRel(op)
					break
				}
			}
			kept = append(kept, line)
			continue
		}
		if line == "" {
			kept = append(kept, line)
			continue
		}
		name := line
		if strings.ContainsAny(line[len(line)-1:], "/*@|=") {
			name = line[:len(line)-1]
		}
		if sensitivePathFilter(workDir, filepath.Join(base, name)) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// inspectOperandRel maps an operand argv form ("." or "./x") back to the
// project-relative path it names.
func inspectOperandRel(arg string) string {
	if arg == "." {
		return ""
	}
	return strings.TrimPrefix(arg, "./")
}

// filterInspectGit parses Git output as records for the subcommand that
// produced it. The predicate is sensitivePathFilter, which is at least as strict
// as the name predicates alone: it also drops any path that does not resolve
// inside the project. Git reports repository-top-relative paths, so a
// project root below the top can make that resolution fail; the result is a
// dropped entry, never a leaked one.
func filterInspectGit(workDir string, args []string, out string) string {
	n := len(inspectShellGitPrefix)
	if len(args) <= n {
		return out
	}
	sensitive := func(p string) bool { return sensitivePathFilter(workDir, p) }
	switch args[n] {
	case "status":
		return filterGitStatusRecords(out, sensitive)
	case "ls-files":
		return filterGitRecords(out, sensitive)
	case "diff":
		format := ""
		for _, a := range args[n+1:] {
			if a == "--" {
				break
			}
			switch a {
			case "--name-only", "--name-status", "--stat", "--numstat":
				format = a
			}
		}
		switch format {
		case "--name-only":
			return filterGitRecords(out, sensitive)
		case "--name-status":
			return filterGitNameStatusRecords(out, sensitive)
		case "--numstat":
			return filterGitNumstatRecords(out, sensitive)
		case "--stat":
			return filterGitStatLines(out, sensitive)
		}
	}
	return out
}

// splitInspectNUL splits -z output into records, dropping the empty tail after
// the final NUL.
func splitInspectNUL(out string) []string {
	recs := strings.Split(out, "\x00")
	if len(recs) > 0 && recs[len(recs)-1] == "" {
		recs = recs[:len(recs)-1]
	}
	return recs
}

// joinInspectNUL rebuilds NUL-terminated records.
func joinInspectNUL(recs []string) string {
	var b strings.Builder
	for _, r := range recs {
		b.WriteString(r)
		b.WriteByte(0)
	}
	return b.String()
}

// filterGitRecords handles one-path-per-record output (name-only, ls-files).
func filterGitRecords(out string, sensitive func(string) bool) string {
	var kept []string
	for _, rec := range splitInspectNUL(out) {
		if !sensitive(rec) {
			kept = append(kept, rec)
		}
	}
	return joinInspectNUL(kept)
}

// filterGitStatusRecords handles porcelain v1 -z: "XY path", followed by the
// origin path when X is R or C.
func filterGitStatusRecords(out string, sensitive func(string) bool) string {
	recs := splitInspectNUL(out)
	var kept []string
	for i := 0; i < len(recs); i++ {
		rec := recs[i]
		if len(rec) < 4 {
			continue
		}
		entry := []string{rec}
		paths := []string{rec[3:]}
		if (rec[0] == 'R' || rec[0] == 'C') && i+1 < len(recs) {
			i++
			entry = append(entry, recs[i])
			paths = append(paths, recs[i])
		}
		if anyInspectSensitive(paths, sensitive) {
			continue
		}
		kept = append(kept, entry...)
	}
	return joinInspectNUL(kept)
}

// filterGitNameStatusRecords handles --name-status -z: a status token, then one
// path, or two for R and C.
func filterGitNameStatusRecords(out string, sensitive func(string) bool) string {
	recs := splitInspectNUL(out)
	var kept []string
	for i := 0; i < len(recs); i++ {
		n := 1
		if recs[i] != "" && (recs[i][0] == 'R' || recs[i][0] == 'C') {
			n = 2
		}
		if i+n >= len(recs) {
			break
		}
		group := recs[i : i+n+1]
		if !anyInspectSensitive(group[1:], sensitive) {
			kept = append(kept, group...)
		}
		i += n
	}
	return joinInspectNUL(kept)
}

// filterGitNumstatRecords handles --numstat -z: "added\tdeleted\tpath", or
// "added\tdeleted\t" followed by the old and new paths for a rename.
func filterGitNumstatRecords(out string, sensitive func(string) bool) string {
	recs := splitInspectNUL(out)
	var kept []string
	for i := 0; i < len(recs); i++ {
		parts := strings.SplitN(recs[i], "\t", 3)
		if len(parts) != 3 {
			continue
		}
		if parts[2] != "" {
			if !sensitive(parts[2]) {
				kept = append(kept, recs[i])
			}
			continue
		}
		if i+2 >= len(recs) {
			break
		}
		if !anyInspectSensitive(recs[i+1:i+3], sensitive) {
			kept = append(kept, recs[i], recs[i+1], recs[i+2])
		}
		i += 2
	}
	return joinInspectNUL(kept)
}

// filterGitStatLines handles --stat, which has no -z form. Quoted names are
// unquoted first. Truncated names ("...") are dropped whole because their
// visible tail cannot be classified. Rename rows are checked on both sides.
func filterGitStatLines(out string, sensitive func(string) bool) string {
	lines := strings.Split(out, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		idx := strings.Index(line, " | ")
		if idx < 0 {
			kept = append(kept, line)
			continue
		}
		name := strings.TrimSpace(line[:idx])
		if strings.HasPrefix(name, "...") {
			continue
		}
		if strings.HasPrefix(name, `"`) {
			u, err := strconv.Unquote(name)
			if err != nil {
				continue
			}
			name = u
		}
		flat := strings.NewReplacer("{", "", "}", "").Replace(name)
		drop := false
		for _, part := range strings.Split(flat, " => ") {
			if part != "" && sensitive(strings.TrimSpace(part)) {
				drop = true
				break
			}
		}
		if !drop {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

func anyInspectSensitive(paths []string, sensitive func(string) bool) bool {
	for _, p := range paths {
		if sensitive(p) {
			return true
		}
	}
	return false
}

// inspectShellCollector receives combined stdout and stderr. It keeps the first
// bounded window of raw bytes and drains the rest, so a chatty process cannot
// block on a full pipe. When the window fills, limit closes once.
type inspectShellCollector struct {
	mu       sync.Mutex
	buf      []byte
	overflow bool
	limit    chan struct{}
	once     sync.Once
}

func newInspectShellCollector() *inspectShellCollector {
	return &inspectShellCollector{limit: make(chan struct{})}
}

func (c *inspectShellCollector) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.overflow {
		return len(p), nil
	}
	room := toolDigestEvidenceScanBytes - len(c.buf)
	if len(p) <= room {
		c.buf = append(c.buf, p...)
		return len(p), nil
	}
	c.buf = append(c.buf, p[:room]...)
	c.overflow = true
	c.once.Do(func() { close(c.limit) })
	return len(p), nil
}

// snapshot returns the raw window. When the window overflowed, the trailing
// partial record is dropped so a cut name is never classified or returned.
func (c *inspectShellCollector) snapshot() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	raw := string(c.buf)
	if c.overflow {
		if i := strings.LastIndexAny(raw, "\n\x00"); i >= 0 {
			raw = raw[:i+1]
		} else {
			raw = ""
		}
	}
	return raw, c.overflow
}

// inspectShellTool runs one validated inspection under the supervisor and the
// mandatory sandbox. It is never registered on the main agent.
type inspectShellTool struct {
	workDir string
	sup     *tool.ProcessSupervisor
	dirs    []string
	wrapper sandbox.Wrapper
	seq     atomic.Uint64
}

// newInspectShellTool returns the tool when the mandatory sandbox works for
// this host and at least one allowlisted program is installed. Otherwise it
// returns false and the caller registers nothing.
func newInspectShellTool(snap toolDigestSnapshot, sup *tool.ProcessSupervisor) (tool.Tool, bool) {
	dirs := inspectShellBinaryDirs()
	if len(dirs) == 0 {
		return nil, false
	}
	w := inspectShellSandbox()
	if w == nil || !w.Available() {
		return nil, false
	}
	var probeProgram string
	for name := range inspectShellPrograms {
		if p, err := resolveInspectShellProgram(dirs, name); err == nil {
			probeProgram = p
			break
		}
	}
	if probeProgram == "" {
		return nil, false
	}
	// Probe the wrap once: some Linux backends accept only `<shell> -c <command>`
	// argv shapes. A host whose backend refuses argv-form commands cannot confine
	// inspections, so the tool is off there rather than failing on every call.
	probe := exec.Command(probeProgram)
	if _, err := w.Wrap(probe, sandbox.RootSet{WritableRoots: nil, NetworkEgress: false}); err != nil {
		return nil, false
	}
	return &inspectShellTool{workDir: snap.WorkDir, sup: sup, dirs: dirs, wrapper: w}, true
}

// registerInspectShellTool creates the run's supervisor, registers its shutdown
// as a toolbox cleanup, and adds the tool when it is available. An unavailable
// tool adds nothing and is not an error.
func registerInspectShellTool(box *toolDigestToolbox, snap toolDigestSnapshot) error {
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	box.addCleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := sup.Shutdown(ctx); err != nil {
			emitDebug("inspect_shell", fmt.Sprintf("supervisor shutdown: %v", err))
		}
	})
	t, ok := newInspectShellTool(snap, sup)
	if !ok {
		return nil
	}
	box.add(t)
	return nil
}

func (t *inspectShellTool) Name() string { return "inspect_shell" }

func (t *inspectShellTool) Description() string {
	return "Run one read-only inspection command given as an argv array (never a shell string). " +
		"Allowed: pwd; ls; stat; file; readlink; realpath; wc; head and tail (regular files); md5 and sha hashes; " +
		"find (print-only, explicit -print); go version and go env; git status, git diff (metadata-only: " +
		"--name-only, --name-status, --stat, --numstat), git rev-parse and git ls-files. Output is limited to 8 KiB " +
		"and sensitive paths are removed."
}

func (t *inspectShellTool) Parallel() bool { return false }

func (t *inspectShellTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        t.Name(),
		"description": t.Description(),
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"argv": map[string]interface{}{
					"type":        "array",
					"minItems":    1,
					"items":       map[string]interface{}{"type": "string"},
					"description": "The program name followed by its arguments. Never a shell command line.",
				},
			},
			"required":             []string{"argv"},
			"additionalProperties": false,
		},
	}
}

func (t *inspectShellTool) Execute(args json.RawMessage) (string, error) {
	return t.ExecuteCtx(context.Background(), args)
}

// ExecuteCtx validates the argv, resolves the fixed program, and runs it.
func (t *inspectShellTool) ExecuteCtx(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Argv []string `json:"argv"`
	}
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return "", inspectErr("arguments must be an object with one argv array")
	}
	v, err := validateInspectShellArgv(t.workDir, in.Argv)
	if err != nil {
		return "", err
	}
	program, err := resolveInspectShellProgram(t.dirs, v.Program)
	if err != nil {
		return "", err
	}
	if v.Program == "git" {
		local, err := inspectRepoLocalAttributesPresent(t.workDir)
		if err != nil {
			return "", inspectErr("could not check the repository's attributes files: %v", err)
		}
		if local {
			return "", inspectErr("git: this repository has a repository-local attributes file, which Git reads even when attributes come from a tree; inspection refused")
		}
	}
	return t.run(ctx, program, v)
}

// inspectRepoLocalAttributesPresent reports whether the repository that owns
// workDir has an info/attributes file in its git directory, or in the common
// directory of a linked worktree. Git reads that file even with --attr-source,
// so a filter it names would still run. The walk mirrors Git's discovery: the
// first .git entry wins, as a directory or a "gitdir:" file.
func inspectRepoLocalAttributesPresent(workDir string) (bool, error) {
	dir, err := canonicalProjectRoot(workDir)
	if err != nil {
		return false, err
	}
	for {
		dotGit := filepath.Join(dir, ".git")
		info, lerr := os.Lstat(dotGit)
		switch {
		case lerr == nil:
			gitDir, err := inspectGitDirOf(dotGit, info)
			if err != nil {
				return false, err
			}
			return inspectGitDirHasAttributes(gitDir)
		case errors.Is(lerr, fs.ErrNotExist):
		default:
			return false, lerr
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false, nil
		}
		dir = parent
	}
}

// inspectGitDirOf returns the git directory named by a .git entry: the entry
// itself for a directory, or the target of its "gitdir:" line for a file.
func inspectGitDirOf(dotGit string, info os.FileInfo) (string, error) {
	if info.IsDir() {
		return dotGit, nil
	}
	raw, err := os.ReadFile(dotGit)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(raw))
	const prefix = "gitdir:"
	if !strings.HasPrefix(line, prefix) {
		return "", errors.New("the .git file has no gitdir line")
	}
	target := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(dotGit), target)
	}
	return target, nil
}

// inspectGitDirHasAttributes checks gitDir and, when present, its common
// directory for an info/attributes file.
func inspectGitDirHasAttributes(gitDir string) (bool, error) {
	dirs := []string{gitDir}
	if raw, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
		common := strings.TrimSpace(string(raw))
		if !filepath.IsAbs(common) {
			common = filepath.Join(gitDir, common)
		}
		dirs = append(dirs, common)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	for _, d := range dirs {
		_, err := os.Lstat(filepath.Join(d, "info", "attributes"))
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, fs.ErrNotExist):
		default:
			return false, err
		}
	}
	return false, nil
}

// run starts the command under the supervisor and the sandbox, collects its
// output, and tears the whole process group down on timeout, cancel or overflow.
func (t *inspectShellTool) run(ctx context.Context, program string, v validatedCommand) (string, error) {
	if !t.wrapper.Available() {
		return "", inspectErr("sandbox confinement is unavailable; the command was not run")
	}
	root, err := canonicalProjectRoot(t.workDir)
	if err != nil {
		return "", err
	}
	out := newInspectShellCollector()
	cmd := exec.Command(program, v.Args...)
	cmd.Dir = root
	cmd.Env = inspectShellEnv()
	cmd.Stdout = out
	cmd.Stderr = out
	wrapped, err := t.wrapper.Wrap(cmd, sandbox.RootSet{WritableRoots: nil, NetworkEgress: false})
	if err != nil {
		return "", inspectErr("sandbox confinement could not be established; the command was not run: %v", err)
	}

	id := fmt.Sprintf("inspect_shell-%d", t.seq.Add(1))
	runCtx, cancel := context.WithTimeout(ctx, inspectShellDeadline)
	defer cancel()
	reg := tool.ProcessRegistration{
		ID:      id,
		Name:    "inspect_shell",
		Command: v.Program,
		Kind:    tool.ProcessKindBackgroundBash,
	}
	if _, err := tool.StartSupervised(t.sup, wrapped, reg); err != nil {
		return "", inspectErr("could not start %s: %v", v.Program, err)
	}

	waitCh := make(chan error, 1)
	crashguard.Go(func() { waitCh <- wrapped.Wait() })

	select {
	case err := <-waitCh:
		code := inspectExitCode(err)
		t.sup.MarkExited(id, code)
		return t.render(v, out, code, ""), nil
	case <-out.limit:
		code := t.stop(id, wrapped, waitCh)
		return t.render(v, out, code, "[output limit reached; the process was terminated]"), nil
	case <-runCtx.Done():
		t.stop(id, wrapped, waitCh)
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return "", inspectErr("%s timed out after %s and was terminated", v.Program, inspectShellDeadline)
		}
		return "", inspectErr("%s was cancelled and terminated", v.Program)
	}
}

// stop terminates the process group, then the leader if the group did not
// exit within the grace period, and records the process as killed. It returns
// the exit status.
func (t *inspectShellTool) stop(id string, cmd *exec.Cmd, waitCh <-chan error) int {
	killCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := t.sup.TerminateAll(killCtx); err != nil {
		emitDebug("inspect_shell", fmt.Sprintf("terminate %s: %v", id, err))
	}
	var err error
	select {
	case err = <-waitCh:
	case <-time.After(inspectShellReapGrace):
		if cmd.Process != nil {
			if kerr := tool.KillProcessGroup(cmd.Process); kerr != nil && !errors.Is(kerr, os.ErrProcessDone) {
				emitDebug("inspect_shell", fmt.Sprintf("kill group %s: %v", id, kerr))
			}
		}
		select {
		case err = <-waitCh:
		case <-time.After(inspectShellReapGrace):
			emitDebug("inspect_shell", fmt.Sprintf("%s did not exit after kill", id))
		}
	}
	code := inspectExitCode(err)
	t.sup.MarkKilled(id, code)
	return code
}

// render filters, masks and caps the collected output. The exit status and any
// note are appended after masking, since they carry no user data.
func (t *inspectShellTool) render(v validatedCommand, out *inspectShellCollector, code int, note string) string {
	raw, _ := out.snapshot()
	text := maskSecretsForDigest(filterInspectShellOutput(t.workDir, v, raw))
	if note != "" {
		text += "\n" + note
	}
	if code != 0 {
		text += fmt.Sprintf("\n[exit status %d]", code)
	}
	if strings.TrimSpace(text) == "" {
		text = "(no output)"
	}
	if len(text) > toolDigestEvidenceResultBytes {
		text = cutToolDigestText(text, toolDigestEvidenceResultBytes-len(toolDigestEvidenceMarker)) + toolDigestEvidenceMarker
	}
	return text
}
