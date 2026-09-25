package server

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// Phase 05, status half: the operation-state probe over the transport.
//
// The remote path must not grow a second implementation of "what does a
// rebase look like". gitOperationStateFor (phase 01) is pure and already
// transport-neutral, so this file's whole job is to turn the remote host's
// state directory into the same []gitStateEntry the local path builds from
// disk, and hand it over.

// remoteGitOperationStateProbe builds the fixed shell block that reads the
// remote repository's operation state.
//
// SECURITY: this is a FIXED LITERAL. The only caller-supplied value is dir,
// and it reaches the script solely through remoteGitCommand's shell quoting —
// no request text, no path, and no probe name is ever concatenated here. The
// probe names come from the constant gitOperationStateNames list, and a test
// pins that every name is a safe literal (no quotes, spaces or shell
// metacharacters), so this block cannot be broken by a future name edit.
//
// The output is one line per state entry:
//
//	<name><TAB>D                     -> a directory
//	<name><TAB>F<TAB><base64>        -> a file, content base64-encoded
//
// File content is base64-encoded because a state file's bytes are
// attacker-influenced in the general case: MERGE_MSG holds a commit message,
// which may contain tabs, newlines and NULs. Encoding keeps every entry on
// exactly one line, so a multi-line message cannot forge extra entries or
// shift fields. The encoding is also the established transport convention
// here (remoteReadFile uses the same decoder), and the 4 KiB read bound is
// carried over from the local path so behavior matches.
func remoteGitOperationStateProbe(dir string) string {
	var b strings.Builder
	b.WriteString("g=$(")
	// --absolute-git-dir resolves the per-worktree git directory, so a
	// linked worktree is probed correctly rather than the main repo's.
	b.WriteString(remoteGitCommand(dir, "rev-parse", "--absolute-git-dir"))
	b.WriteString(") || exit 0\n")
	for _, name := range gitOperationStateNames {
		// The name is a constant from gitOperationStateNames (pinned safe by
		// TestRemoteGitOperationStateNamesAreSafeProbeLiterals); "$g/$n" is
		// quoted, so even a hypothetical metacharacter could not escape.
		b.WriteString(fmt.Sprintf(
			"n=%s; if [ -d \"$g/$n\" ]; then printf '%%s\\tD\\n' \"$n\"; "+
				"elif [ -f \"$g/$n\" ]; then printf '%%s\\tF\\t' \"$n\"; "+
				"head -c %d \"$g/$n\" | base64 | tr -d '\\n'; printf '\\n'; fi\n",
			shellQuotePathPOSIX(name), gitStateFileReadLimit))
	}
	// The block is appended to a batched script whose exit status belongs to
	// the is-inside-work-tree probe at the end, so this block must never
	// fail the script on its own.
	b.WriteString("true\n")
	return b.String()
}

// remoteGitConflictsProbe emits the unmerged records, base64-encoded.
//
// `status --porcelain=v2 -z` is the same probe the local path uses, and its
// output is NUL-separated raw paths. That raw form cannot go into the
// batched script directly: a filename may contain any byte except NUL and
// slash, including the 0x1e section separator, so one such filename would
// split the batched output and corrupt every field after it. Encoding the
// whole payload makes the transport collision-proof, and the decode is the
// same base64 convention the state probe and remoteReadFile already use.
//
// `base64` wraps at 76 columns on GNU, so the newlines are stripped; the
// trailing `|| true` keeps a non-repo from failing the batched script (the
// script's exit status belongs to the is-inside-work-tree probe at the end,
// which is what distinguishes a repository from a plain directory).
func remoteGitConflictsProbe(dir string) string {
	return remoteGitCommand(dir, "status", "--porcelain=v2", "-z") +
		" | base64 | tr -d '\\n' || true"
}

// decodeRemoteProbe reverses remoteGitConflictsProbe. An empty or malformed
// payload yields "", which every consumer treats as "nothing found" — the
// correct answer for a repository with no conflicts, and the safe one for a
// transport hiccup (status degrades to no conflicts rather than erroring).
func decodeRemoteProbe(out string) string {
	trimmed := strings.Join(strings.Fields(out), "")
	if trimmed == "" {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return ""
	}
	return string(decoded)
}

// parseRemoteStateEntries turns the probe's output back into the phase-01
// entry shape. It is pure: no I/O, and an unparseable line is skipped rather
// than aborting the whole status.
func parseRemoteStateEntries(out string) []gitStateEntry {
	entries := make([]gitStateEntry, 0, len(gitOperationStateNames))
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		name, rest, ok := strings.Cut(line, "\t")
		if !ok || name == "" {
			continue
		}
		if rest == "D" {
			entries = append(entries, gitStateEntry{Name: name, IsDir: true})
			continue
		}
		kind, encoded, ok := strings.Cut(rest, "\t")
		if !ok || kind != "F" {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			// A decode failure means the transport was corrupted for this
			// one file. The entry is still emitted with an empty value: the
			// local path does the same, because PRESENCE identifies a state
			// (only the progress fields need content). Dropping it entirely
			// would make a rebase look idle.
			entries = append(entries, gitStateEntry{Name: name})
			continue
		}
		entries = append(entries, gitStateEntry{Name: name, Value: string(decoded)})
	}
	return entries
}

// withoutPaths returns paths with every key of drop removed, preserving order.
func withoutPaths(paths []string, drop map[string]bool) []string {
	if len(drop) == 0 {
		return paths
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if !drop[p] {
			out = append(out, p)
		}
	}
	return out
}
