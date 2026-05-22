package skill

import (
	"os"
	"strings"
	"testing"
)

func TestParseSkillMetadataFromFrontmatter(t *testing.T) {
	content := `---
name: "Deploy Helper"
description: Guides deployment verification and rollback checks.
when_to_use: Use when shipping infra or app changes.
---

# Ignored title
`

	skill := parseSkillMetadata(content)
	if skill.Name != "Deploy Helper" {
		t.Fatalf("expected name from frontmatter, got %q", skill.Name)
	}
	if skill.Description != "Guides deployment verification and rollback checks." {
		t.Fatalf("unexpected description %q", skill.Description)
	}
	if skill.WhenToUse != "Use when shipping infra or app changes." {
		t.Fatalf("unexpected when-to-use %q", skill.WhenToUse)
	}
}

func TestParseSkillMetadataFallsBackToHeadingsAndBody(t *testing.T) {
	content := `# Incident Response

Purpose: Coordinate triage, mitigation, and follow-up for production incidents.
When to use: During outages, severe degradations, or time-sensitive customer impact.

## Steps
- assess
`

	skill := parseSkillMetadata(content)
	if skill.Name != "Incident Response" {
		t.Fatalf("expected heading-derived name, got %q", skill.Name)
	}
	if skill.Description != "Coordinate triage, mitigation, and follow-up for production incidents." {
		t.Fatalf("unexpected description %q", skill.Description)
	}
	if skill.WhenToUse != "During outages, severe degradations, or time-sensitive customer impact." {
		t.Fatalf("unexpected when-to-use %q", skill.WhenToUse)
	}
}

func TestBuildCatalogIncludesCompactEntries(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	project := t.TempDir()
	t.Chdir(project)

	skillDir := project + "/skills/release-checks"
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := `---
name: Release Checks
description: Verify readiness before deployment.
when_to_use: Before cutting a release or promoting a build.
---

# Release Checks
Full content here.
`
	if err := os.WriteFile(skillDir+"/SKILL.md", []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	catalog := BuildCatalog()
	for _, want := range []string{
		"--- Skill Catalog ---",
		"Release Checks: Verify readiness before deployment.",
		"When to use: Before cutting a release or promoting a build.",
		"load full SKILL.md contents on demand",
	} {
		if !strings.Contains(catalog, want) {
			t.Fatalf("expected %q in catalog, got:\n%s", want, catalog)
		}
	}
	if strings.Contains(catalog, "Full content here") {
		t.Fatalf("catalog should not include full skill content, got:\n%s", catalog)
	}
}
