package orchestrator

import (
	"fmt"
	"strings"
)

const recentIterationsCount = 2

// FileDiff records what a developer changed in one file during one iteration.
type FileDiff struct {
	Path    string
	Summary string // one-line description of what changed
	Diff    string // full unified diff text (may be empty)
}

// Iteration records one full developer round.
type Iteration struct {
	Number          int
	DeveloperBrief  string
	FilesChanged    []FileDiff
	CompilerOutput  string
	ValidatorReport string // empty if validator passed
	AdvisorNote     string // empty unless advisor was consulted
}

// ContextDoc is the pipeline's memory. It grows across iterations.
// Render() produces a curated prompt for each agent dispatch.
type ContextDoc struct {
	Plan            Plan
	ExploreSnapshot string
	Iterations      []Iteration
	ReExploreHints  []string
}

// AddIteration appends a completed iteration record.
func (c *ContextDoc) AddIteration(iter Iteration) {
	c.Iterations = append(c.Iterations, iter)
}

// Render produces the prompt string for the next agent dispatch.
// brief is the "[YOUR TASK THIS ROUND]" instruction — caller provides it
// so the prompt is specific to the current state (first attempt vs retry vs post-advisor).
func (c *ContextDoc) Render(brief string) string {
	var b strings.Builder

	// Goal
	b.WriteString("[GOAL]\n")
	b.WriteString(c.Plan.Goal)
	b.WriteString("\n\n")

	// Success criteria
	b.WriteString("[SUCCESS CRITERIA]\n")
	for _, sc := range c.Plan.SuccessCriteria {
		b.WriteString("- ")
		b.WriteString(sc)
		b.WriteString("\n")
	}
	b.WriteString("\n")

	// Codebase context
	b.WriteString("[CODEBASE CONTEXT]\n")
	b.WriteString(c.ExploreSnapshot)
	b.WriteString("\n\n")

	if len(c.Iterations) == 0 {
		b.WriteString("[YOUR TASK THIS ROUND]\n")
		b.WriteString(brief)
		return b.String()
	}

	// Split iterations: older (summarised) vs recent (verbatim)
	cutoff := len(c.Iterations) - recentIterationsCount
	if cutoff < 0 {
		cutoff = 0
	}

	if cutoff > 0 {
		b.WriteString("[PRIOR ATTEMPTS SUMMARY]\n")
		for _, iter := range c.Iterations[:cutoff] {
			files := make([]string, len(iter.FilesChanged))
			for i, f := range iter.FilesChanged {
				files[i] = f.Path
			}
			issue := "(no validator report)"
			if iter.ValidatorReport != "" {
				// Extract first Issue line for the summary
				for _, line := range strings.Split(iter.ValidatorReport, "\n") {
					if strings.HasPrefix(line, "- **Issue:**") {
						issue = strings.TrimPrefix(line, "- **Issue:** ")
						break
					}
				}
			}
			b.WriteString(fmt.Sprintf("Attempt %d: changed [%s] — validator rejected: %s\n",
				iter.Number, strings.Join(files, ", "), issue))
		}
		b.WriteString("\n")
	}

	// Recent iterations verbatim
	b.WriteString("[RECENT ATTEMPTS]\n")
	for _, iter := range c.Iterations[cutoff:] {
		b.WriteString(fmt.Sprintf("Attempt %d:\n", iter.Number))
		if len(iter.FilesChanged) > 0 {
			b.WriteString("  Changed:\n")
			for _, f := range iter.FilesChanged {
				b.WriteString(fmt.Sprintf("    %s — %s\n", f.Path, f.Summary))
			}
		}
		if iter.CompilerOutput != "" {
			b.WriteString("  Compiler:\n")
			b.WriteString(indent(iter.CompilerOutput, "    "))
			b.WriteString("\n")
		}
		if iter.ValidatorReport != "" {
			b.WriteString("  Validator rejection:\n")
			b.WriteString(indent(iter.ValidatorReport, "    "))
			b.WriteString("\n")
		}
		if iter.AdvisorNote != "" {
			b.WriteString("  Advisor note:\n")
			b.WriteString(indent(iter.AdvisorNote, "    "))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("[YOUR TASK THIS ROUND]\n")
	b.WriteString(brief)
	return b.String()
}

func indent(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}
