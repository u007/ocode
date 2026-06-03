package plugins

import (
	"fmt"
	"path/filepath"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage/memory"
)

// CurrentCommitHash returns the short (7-char) hash of the current HEAD in the
// plugin's git repo. Returns empty string if the dir is not a git repo.
func CurrentCommitHash(pluginDir string) string {
	repo, err := gogit.PlainOpen(pluginDir)
	if err != nil {
		return ""
	}
	head, err := repo.Head()
	if err != nil {
		return ""
	}
	return head.Hash().String()[:7]
}

// SyncState represents the sync status of an installed plugin.
type SyncState string

const (
	SyncUnknown  SyncState = "unknown"
	SyncUpToDate SyncState = "up-to-date"
	SyncBehind   SyncState = "behind"
	SyncPinned   SyncState = "pinned"
	SyncDirty    SyncState = "dirty"
	SyncError    SyncState = "error"
)

// SyncStatusResult holds the result of a sync check.
type SyncStatusResult struct {
	Name       string
	LocalHash  string
	RemoteHash string
	State      SyncState
	Message    string
}

// CheckSync compares the local plugin's HEAD with the remote HEAD.
// It does a lightweight ls-remote via go-git without downloading objects.
func CheckSync(dir, source, ref string) SyncStatusResult {
	result := SyncStatusResult{}

	repo, err := gogit.PlainOpen(dir)
	if err != nil {
		result.State = SyncError
		result.Message = fmt.Sprintf("cannot open repo: %v", err)
		return result
	}

	// Get local HEAD hash.
	head, err := repo.Head()
	if err != nil {
		result.State = SyncError
		result.Message = fmt.Sprintf("cannot read HEAD: %v", err)
		return result
	}
	result.LocalHash = head.Hash().String()[:7]

	// If pinned to a commit SHA, it's pinned — no update possible.
	if ref != "" && looksLikeCommitSHA(ref) {
		result.State = SyncPinned
		result.Message = fmt.Sprintf("pinned to %s", ref[:7])
		return result
	}

	// Check for dirty worktree.
	wt, err := repo.Worktree()
	if err == nil {
		status, err := wt.Status()
		if err == nil && !status.IsClean() {
			result.State = SyncDirty
			result.Message = "local changes present"
			return result
		}
	}

	// ls-remote to get the remote hash for the target ref.
	remote, err := repo.Remote("origin")
	if err != nil {
		result.State = SyncError
		result.Message = "no origin remote found"
		return result
	}

	refs, err := remote.List(&gogit.ListOptions{PeelingOption: gogit.AppendPeeled})
	if err != nil {
		result.State = SyncError
		result.Message = fmt.Sprintf("ls-remote failed: %v", err)
		return result
	}

	// Resolve the remote hash for the target ref.
	// Try exact match, then as tag, then as branch, then fall back to HEAD.
	if ref != "" {
		// Try exact refs/ path.
		if strings.HasPrefix(ref, "refs/") {
			for _, r := range refs {
				if r.Name().String() == ref {
					result.RemoteHash = r.Hash().String()[:7]
					break
				}
			}
		}
		// Try as tag.
		if result.RemoteHash == "" {
			tagName := "refs/tags/" + ref
			peeledTagName := tagName + "^{}"
			for _, r := range refs {
				name := r.Name().String()
				if name == peeledTagName {
					result.RemoteHash = r.Hash().String()[:7]
					break
				}
				if name == tagName && result.RemoteHash == "" {
					result.RemoteHash = r.Hash().String()[:7]
				}
			}
		}
		// Try as branch.
		if result.RemoteHash == "" {
			branchName := "refs/heads/" + ref
			for _, r := range refs {
				if r.Name().String() == branchName {
					result.RemoteHash = r.Hash().String()[:7]
					break
				}
			}
		}
	} else {
		// No ref — compare against remote HEAD.
		for _, r := range refs {
			if r.Name() == plumbing.HEAD {
				result.RemoteHash = r.Hash().String()[:7]
				break
			}
		}
	}

	if result.RemoteHash == "" {
		result.State = SyncError
		result.Message = "could not resolve remote ref"
		return result
	}

	if result.LocalHash == result.RemoteHash {
		result.State = SyncUpToDate
		result.Message = fmt.Sprintf("at %s (latest)", result.LocalHash)
	} else {
		result.State = SyncBehind
		result.Message = fmt.Sprintf("local %s → remote %s", result.LocalHash, result.RemoteHash)
	}

	return result
}

// UpdateGit fetches and resets the plugin repo to the target ref.
// It rejects dirty worktrees (uncommitted changes) to avoid data loss.
// For pinned commit SHAs, it returns an error (no update possible).
func UpdateGit(dir, source, ref string) (Plugin, error) {
	repo, err := gogit.PlainOpen(dir)
	if err != nil {
		return Plugin{}, fmt.Errorf("open repo %s: %w", dir, err)
	}

	// Reject pinned commits.
	if ref != "" && looksLikeCommitSHA(ref) {
		return Plugin{}, fmt.Errorf("plugin is pinned to commit %s; remove and reinstall to update", ref[:7])
	}

	// Check for dirty worktree.
	wt, err := repo.Worktree()
	if err != nil {
		return Plugin{}, fmt.Errorf("access worktree: %w", err)
	}
	status, err := wt.Status()
	if err != nil {
		return Plugin{}, fmt.Errorf("check worktree status: %w", err)
	}
	if !status.IsClean() {
		return Plugin{}, fmt.Errorf("local changes detected — commit or discard changes before updating")
	}

	// Fetch all refs from origin.
	if err := repo.Fetch(&gogit.FetchOptions{
		RemoteName: "origin",
		Force:      true,
	}); err != nil && err != gogit.NoErrAlreadyUpToDate {
		return Plugin{}, fmt.Errorf("git fetch: %w", err)
	}

	// Determine what to checkout.
	var targetHash plumbing.Hash

	if ref != "" {
		// Try to resolve as a tag first.
		tagRef, err := repo.Reference(plumbing.NewTagReferenceName(ref), true)
		if err == nil {
			targetHash = tagRef.Hash()
		} else {
			// Try as a branch.
			branchRef, err := repo.Reference(plumbing.NewBranchReferenceName(ref), true)
			if err != nil {
				return Plugin{}, fmt.Errorf("ref %q not found as tag or branch", ref)
			}
			targetHash = branchRef.Hash()
		}
	} else {
		// No ref specified — resolve origin/HEAD.
		originHEAD, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", "HEAD"), true)
		if err != nil {
			// Fallback: ls-remote to resolve HEAD.
			remote, err2 := repo.Remote("origin")
			if err2 != nil {
				return Plugin{}, fmt.Errorf("resolve origin/HEAD: %w", err)
			}
			refs, err2 := remote.List(&gogit.ListOptions{})
			if err2 != nil {
				return Plugin{}, fmt.Errorf("ls-remote: %w", err2)
			}
			for _, r := range refs {
				if r.Name() == plumbing.HEAD {
					targetHash = r.Hash()
					break
				}
			}
			if targetHash == (plumbing.Hash{}) {
				return Plugin{}, fmt.Errorf("could not resolve remote HEAD")
			}
		} else {
			targetHash = originHEAD.Hash()
		}
	}

	// Checkout the target commit.
	if err := wt.Checkout(&gogit.CheckoutOptions{
		Hash:  targetHash,
		Force: true,
	}); err != nil {
		return Plugin{}, fmt.Errorf("checkout %s: %w", targetHash.String()[:7], err)
	}

	// Re-read the manifest after update.
	p, err := readManifest(dir)
	if err != nil {
		return Plugin{}, fmt.Errorf("read updated manifest: %w", err)
	}
	if p.Name == "" {
		p.Name = filepath.Base(dir)
	}
	return p, nil
}

// ListRemoteTags fetches and returns available tags from the remote.
func ListRemoteTags(source string) ([]string, error) {
	gitURL := normaliseGitURL(source)

	storer := memory.NewStorage()

	remote := gogit.NewRemote(storer, &gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{gitURL},
	})

	refs, err := remote.List(&gogit.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("ls-remote: %w", err)
	}

	var tags []string
	for _, ref := range refs {
		name := ref.Name().String()
		if strings.HasPrefix(name, "refs/tags/") {
			tag := strings.TrimPrefix(name, "refs/tags/")
			if strings.HasSuffix(tag, "^{}") {
				continue
			}
			tags = append(tags, tag)
		}
	}
	return tags, nil
}
