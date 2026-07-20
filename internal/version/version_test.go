package version

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionMatchesChangelog(t *testing.T) {
	// Find the project root by walking up from this file's directory.
	// The version package lives at internal/version/version.go,
	// so the project root is three levels up: internal/version/ → internal/ → .
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	// Walk up to find the project root (contains CHANGES.md).
	root := dir
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(root, "CHANGES.md")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatal("could not find project root (CHANGES.md not found)")
		}
		root = parent
	}

	changesData, err := os.ReadFile(filepath.Join(root, "CHANGES.md"))
	if err != nil {
		t.Fatalf("read CHANGES.md: %v", err)
	}

	// Look for the version bump line: "- **Version Bump** — X.Y.Z → W.X.Y"
	// in the [Unreleased] section.
	content := string(changesData)
	lines := strings.Split(content, "\n")
	inUnreleased := false
	found := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "## [Unreleased]" {
			inUnreleased = true
			continue
		}
		if strings.HasPrefix(trimmed, "## ") && inUnreleased {
			break // past the Unreleased section
		}
		if inUnreleased && strings.Contains(trimmed, "**Version Bump**") {
			// Found the version bump entry; check it contains the current version.
			if !strings.Contains(trimmed, Version) {
				t.Fatalf("CHANGES.md version bump entry does not reference current version %q:\n  %s",
					Version, trimmed)
			}
			found = true
			break
		}
	}
	if !found {
		t.Logf("CHANGES.md does not contain a Version Bump entry in [Unreleased]; checking file-level mention of %s", Version)
		// Fallback: check that Version appears somewhere in the Unreleased section
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "## [Unreleased]" {
				inUnreleased = true
				continue
			}
			if strings.HasPrefix(trimmed, "## ") && inUnreleased {
				break
			}
			if inUnreleased && strings.Contains(trimmed, Version) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("version %q not found in [Unreleased] section of CHANGES.md", Version)
		}
	}
}
