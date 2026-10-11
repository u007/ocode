package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// fileBackupFacts are facts about project files a bash command replaces,
// derived deterministically for the permission judge. The judge does no path
// tracking of its own: measured live, a rubric rule that allowed "overwrite a
// file the command first saved to a temp root" equally allowed a command that
// saved a DIFFERENT file, so which file was saved is established here instead.
type fileBackupFacts struct {
	// SavedThenReplaced: files the command copied or moved to a temp root and
	// replaced afterwards. Nothing is lost.
	SavedThenReplaced []fileBackup `json:"saved_then_replaced,omitempty"`
	// MovedToTemp: files the command moved aside into a temp root.
	MovedToTemp []fileBackup `json:"moved_to_temp,omitempty"`
	// ReplacedWithoutBackup: existing files the command overwrites with
	// `git show … > file` although it saved no copy of them first.
	ReplacedWithoutBackup []string `json:"replaced_without_backup,omitempty"`
	// Unaccounted is set when the command contains a write the analysis cannot
	// vouch for: an unresolvable redirect target, a write-capable command it
	// does not model, an overwrite of an existing file that was not saved, or
	// a backup that is itself touched later. The facts then prove nothing and
	// must not be presented to the judge as "all saved".
	Unaccounted bool `json:"-"`
}

// allSavedFirst reports whether every write the command makes to a project file
// is one the analysis verified as saved to a temp root first. Fails closed.
func (f fileBackupFacts) allSavedFirst() bool {
	return len(f.SavedThenReplaced) > 0 && len(f.ReplacedWithoutBackup) == 0 && !f.Unaccounted
}

// unmodelledWriters are command heads that can write or delete files in ways
// analyzeFileBackups does not follow. Their presence withholds allSavedFirst
// unless every path operand is under a temp root.
var unmodelledWriters = map[string]bool{
	"tee": true, "dd": true, "sed": true, "perl": true, "awk": true, "truncate": true,
	"rm": true, "rmdir": true, "unlink": true, "install": true, "rsync": true, "ln": true,
	"patch": true, "touch": true, "chmod": true, "chown": true, "tar": true, "unzip": true,
	"xargs": true, "find": true, "eval": true, "source": true, ".": true,
}

// readOnlyGitSubcommands never write the working tree or the index.
var readOnlyGitSubcommands = map[string]bool{
	"show": true, "status": true, "diff": true, "log": true, "rev-parse": true,
	"ls-files": true, "cat-file": true, "blame": true, "describe": true,
}

type fileBackup struct {
	File    string `json:"file"`
	SavedTo string `json:"saved_to"`
}

func (f fileBackupFacts) empty() bool {
	return len(f.SavedThenReplaced) == 0 && len(f.MovedToTemp) == 0 && len(f.ReplacedWithoutBackup) == 0
}

// analyzeFileBackups walks command in order, tracking literal `cd` targets
// from cwd. A fragment it cannot read as one of the shapes below contributes
// nothing; a parse failure yields no facts at all.
func analyzeFileBackups(command, cwd string) fileBackupFacts {
	var facts fileBackupFacts
	parseTarget := command
	if header, docs := extractHeredocs(command); len(docs) > 0 {
		parseTarget = header
	}
	parsed, err := parseShellCommandLine(parseTarget)
	if err != nil {
		return facts
	}
	backupPaths := map[string]bool{}
	// touchesBackup: p is a saved backup, or a directory holding one.
	touchesBackup := func(p string) bool {
		for b := range backupPaths {
			if b == p || strings.HasPrefix(b, p+string(filepath.Separator)) {
				return true
			}
		}
		return false
	}
	resolve := func(p string) (string, bool) {
		if p == "" || strings.ContainsAny(p, "$`*?~") {
			return "", false
		}
		if !filepath.IsAbs(p) {
			if cwd == "" {
				return "", false
			}
			p = filepath.Join(cwd, p)
		}
		return filepath.Clean(p), true
	}
	saved := map[string]string{}
	replaced := map[string]bool{}
	markReplaced := func(target string, fromGitShow bool) {
		if touchesBackup(target) {
			facts.Unaccounted = true
			return
		}
		if isTempDir(target) || strings.HasPrefix(target, "/dev/") || replaced[target] {
			return
		}
		if tmp, ok := saved[target]; ok {
			replaced[target] = true
			facts.SavedThenReplaced = append(facts.SavedThenReplaced, fileBackup{File: target, SavedTo: tmp})
			return
		}
		info, err := os.Stat(target)
		if err != nil || !info.Mode().IsRegular() {
			return // a new file: nothing to lose
		}
		if !fromGitShow {
			facts.Unaccounted = true
			return
		}
		replaced[target] = true
		facts.ReplacedWithoutBackup = append(facts.ReplacedWithoutBackup, target)
	}
	for _, c := range parsed {
		words := c.cmdWords
		if len(words) == 0 {
			continue
		}
		head := filepath.Base(words[0])
		var operands []string
		for _, w := range words[1:] {
			if !strings.HasPrefix(w, "-") {
				operands = append(operands, w)
			}
		}
		switch {
		case head == "cd" && len(operands) == 1:
			if dir, ok := resolve(operands[0]); ok {
				cwd = dir
			} else {
				cwd = ""
				facts.Unaccounted = true
			}
		case head == "cd":
			facts.Unaccounted = true
		case head == "cp" || head == "mv":
			if len(operands) != 2 {
				facts.Unaccounted = true
				break
			}
			src, okSrc := resolve(operands[0])
			dst, okDst := resolve(operands[1])
			if !okSrc || !okDst {
				facts.Unaccounted = true
				break
			}
			if touchesBackup(dst) || (head == "mv" && touchesBackup(src)) {
				facts.Unaccounted = true
				break
			}
			switch {
			case !isTempDir(src) && isTempDir(dst):
				saved[src] = dst
				backupPaths[dst] = true
				if head == "mv" {
					facts.MovedToTemp = append(facts.MovedToTemp, fileBackup{File: src, SavedTo: dst})
				}
			case !isTempDir(dst):
				// temp -> project, or project -> project.
				markReplaced(dst, false)
			}
		case head == "git":
			// A global option (-C, -c, --git-dir) moves the subcommand; only a
			// plain `git <read-only subcommand>` is vouched for.
			if len(words) < 2 || !readOnlyGitSubcommands[words[1]] {
				facts.Unaccounted = true
			}
		case unmodelledWriters[head] || isOpaqueCommandHead(words):
			allTemp := len(operands) > 0 && !isOpaqueCommandHead(words)
			for _, op := range operands {
				p, ok := resolve(op)
				if !ok || !isTempDir(p) || touchesBackup(p) {
					allTemp = false
				}
			}
			if !allTemp {
				facts.Unaccounted = true
			}
		}
		fromGitShow := head == "git" && len(words) > 1 && words[1] == "show"
		stdin := map[string]bool{}
		for _, r := range c.stdinRedirections {
			stdin[r] = true
		}
		for _, r := range c.redirections {
			if stdin[r] {
				continue
			}
			if target, ok := resolve(r); ok {
				markReplaced(target, fromGitShow)
			} else {
				facts.Unaccounted = true
			}
		}
	}
	return facts
}
