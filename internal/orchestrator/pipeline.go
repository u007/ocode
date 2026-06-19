// internal/orchestrator/pipeline.go
package orchestrator

import (
	"context"
	"fmt"
	"math/rand"
	"os/exec"
	"strings"
	"time"

	"github.com/u007/ocode/internal/agent"
)

// PipelineOptions configures a Pipeline run. All fields are optional.
type PipelineOptions struct {
	VerifyMode    string                    // overrides Plan.VerifyMode if non-empty
	MaxIterations int                       // overrides Plan.MaxIterations if > 0
	UseWorktree   bool                      // default true — set false for --no-worktree mode
	WorkDir       string                    // repo root; defaults to "."
	Backoff       BackoffPolicy             // used only when UseWorktree=false
	StatusFunc    func(s state, msg string) // called on each state transition (may be nil)
}

// Pipeline is the orchestration engine. Create with New(), run with Run().
type Pipeline struct {
	parent     *agent.Agent
	opts       PipelineOptions
	dispatchFn func(agentName, prompt string) (string, error) // swappable for tests
	doc        *ContextDoc
	iterCount  int
	runID      string
}

// New creates a Pipeline ready to run. parent must have its task tool configured.
func New(parent *agent.Agent, opts PipelineOptions) *Pipeline {
	if opts.WorkDir == "" {
		opts.WorkDir = "."
	}
	if opts.Backoff.MaxAttempts == 0 {
		opts.Backoff = DefaultBackoff
	}
	p := &Pipeline{parent: parent, opts: opts}
	p.dispatchFn = parent.DispatchSubagent
	p.runID = fmt.Sprintf("%d", time.Now().UnixNano())
	return p
}

// Run executes the pipeline for the given goal and returns a StructuredReport.
// The context controls cancellation.
func (p *Pipeline) Run(ctx context.Context, goal string) (*StructuredReport, error) {
	p.doc = &ContextDoc{}
	p.iterCount = 0

	// Planning
	p.emit(statePlanning, "analysing goal")
	planRaw, err := p.dispatchFn("orchestrator-planner", goal)
	if err != nil {
		return nil, fmt.Errorf("planning failed: %w", err)
	}
	plan, err := ExtractPlanJSON(planRaw)
	if err != nil {
		return nil, fmt.Errorf("planning failed: %w", err)
	}
	// Apply option overrides
	if p.opts.VerifyMode != "" {
		plan.VerifyMode = p.opts.VerifyMode
	}
	if p.opts.MaxIterations > 0 {
		plan.MaxIterations = p.opts.MaxIterations
	}
	p.doc.Plan = *plan

	// Worktree setup
	var wm *WorktreeManager
	if p.opts.UseWorktree {
		wm = NewWorktreeManager(p.opts.WorkDir)
		if _, err := wm.Setup(p.runID); err != nil {
			return nil, fmt.Errorf("worktree setup failed: %w", err)
		}
	}

	// Exploring (iteration 0)
	p.emit(stateExploring, "gathering codebase context")
	explorePrompt := fmt.Sprintf("Goal: %s\n\nGather codebase context for a developer who will implement this.", goal)
	if len(p.doc.ReExploreHints) > 0 {
		explorePrompt += "\n\nRe-explore hints (files missing from prior snapshot):\n- " + strings.Join(p.doc.ReExploreHints, "\n- ")
	}
	snapshot, err := p.dispatchFn("orchestrator-explorer", explorePrompt)
	if err != nil {
		return nil, fmt.Errorf("exploring failed: %w", err)
	}
	p.doc.ExploreSnapshot = snapshot

	// Main loop
	advisorConsulted := false
	postAdvisorAttempt := false
	var lastValidatorReport string
	var lastFilesChanged []FileDiff

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Circuit breaker
		if p.iterCount >= plan.MaxIterations {
			if postAdvisorAttempt {
				// Already tried post-advisor — halt
				break
			}
			// Escalate to advisor
			p.emit(stateAdvising, fmt.Sprintf("escalating after %d iterations", p.iterCount))
			advisorNote, err := p.escalateToAdvisor(ctx, lastValidatorReport)
			if err != nil {
				advisorNote = fmt.Sprintf("advisor unavailable: %v", err)
			}
			advisorConsulted = true
			postAdvisorAttempt = true
			// Add advisor note to next iteration context
			if p.iterCount > 0 {
				p.doc.Iterations[len(p.doc.Iterations)-1].AdvisorNote = advisorNote
			}
		}

		// Developing
		p.iterCount++
		p.emit(stateDeveloping, fmt.Sprintf("iteration %d", p.iterCount))
		brief := p.buildDeveloperBrief(lastValidatorReport)
		devPrompt := p.doc.Render(brief)
		devOut, err := p.dispatchFn("orchestrator-developer", devPrompt)
		if err != nil {
			return nil, fmt.Errorf("developer dispatch failed: %w", err)
		}
		devReport := ExtractDeveloperReport(devOut)

		// Build current iteration record
		iter := Iteration{
			Number:         p.iterCount,
			DeveloperBrief: brief,
		}

		// Compiling
		compileOut, compileErr := p.runCompile(ctx, plan.VerifyMode, wm)
		iter.CompilerOutput = compileOut
		if compileErr != nil {
			// Compile failed — add iteration and loop back to developer
			p.doc.AddIteration(iter)
			lastValidatorReport = ""
			lastFilesChanged = nil
			continue
		}

		// Validating
		p.emit(stateValidating, fmt.Sprintf("iteration %d", p.iterCount))
		valPrompt := p.buildValidatorPrompt(devReport)
		valOut, err := p.dispatchFn("orchestrator-validator", valPrompt)
		// Re-ask on malformed output (once)
		if err == nil {
			verdict, body := ExtractVerdict(valOut)
			if verdict == VerdictMalformed {
				valOut, err = p.dispatchFn("orchestrator-validator",
					"Your previous response did not contain a verdict. Output only VALIDATION_PASSED or a Validation Failure Report.")
				if err == nil {
					verdict, body = ExtractVerdict(valOut)
					if verdict == VerdictMalformed {
						body = "### Validation Failure Report\n- **Issue:** validator produced unreadable output — human review required"
						verdict = VerdictFailed
					}
				}
			}
			iter.ValidatorReport = body

			// Check for context gap hints
			if verdict == VerdictFailed {
				if gap := extractContextGap(body); gap != "" {
					p.doc.ReExploreHints = append(p.doc.ReExploreHints, gap)
					// Re-explore before next developer dispatch
					snapshot, _ := p.dispatchFn("orchestrator-explorer", fmt.Sprintf(
						"Re-explore these specific files missing from prior snapshot:\n- %s",
						strings.Join(p.doc.ReExploreHints, "\n- ")))
					if snapshot != "" {
						p.doc.ExploreSnapshot += "\n\n## Re-explored\n" + snapshot
						p.doc.ReExploreHints = nil
					}
				}
			}

			p.doc.AddIteration(iter)
			lastValidatorReport = body
			lastFilesChanged = fileChangesFromReport(devReport)

			if verdict == VerdictPassed {
				p.emit(stateDone, "validation passed")
				if wm != nil {
					_ = wm.Teardown(false)
				}
				return &StructuredReport{
					Goal:             goal,
					TotalIterations:  p.iterCount,
					AdvisorConsulted: advisorConsulted,
					Passed:           true,
				}, nil
			}
		} else {
			p.doc.AddIteration(iter)
		}
	}

	// HALTED
	p.emit(stateDone, "halted")
	if wm != nil {
		_ = wm.Teardown(true) // preserve worktree on halt
	}
	return &StructuredReport{
		Goal:                 goal,
		TotalIterations:      p.iterCount,
		AdvisorConsulted:     advisorConsulted,
		Passed:               false,
		FinalValidatorReport: lastValidatorReport,
		FilesChanged:         lastFilesChanged,
	}, nil
}

func (p *Pipeline) emit(s state, msg string) {
	if p.opts.StatusFunc != nil {
		p.opts.StatusFunc(s, msg)
	}
}

func (p *Pipeline) buildDeveloperBrief(lastValidatorReport string) string {
	if lastValidatorReport == "" {
		return "Implement the goal described above. Use the codebase context provided."
	}
	return fmt.Sprintf("The previous attempt was rejected. Fix the following issues:\n\n%s", lastValidatorReport)
}

func (p *Pipeline) buildValidatorPrompt(dr *DeveloperReport) string {
	var b strings.Builder
	b.WriteString(p.doc.Render(""))
	if dr.SuggestedValidatorFocus != "" {
		b.WriteString("\n\n[DEVELOPER FOCUS HINT]\n")
		b.WriteString(dr.SuggestedValidatorFocus)
	}
	return b.String()
}

func (p *Pipeline) runCompile(ctx context.Context, verifyMode string, wm *WorktreeManager) (string, error) {
	workDir := p.opts.WorkDir
	if wm != nil && wm.Path() != "" {
		workDir = wm.Path()
	}

	cmds := [][]string{{"go", "build", "./..."}, {"go", "vet", "./..."}}
	if verifyMode == "build_test_llm" {
		cmds = append(cmds, []string{"go", "test", "./..."})
	}

	if p.opts.UseWorktree || wm != nil {
		// Worktree mode: deterministic, no backoff
		return runCommands(ctx, workDir, cmds)
	}

	// No-worktree mode: retry with backoff. Clamp MaxAttempts to at least 1
	// so a single attempt is made even when the caller didn't configure backoff.
	maxAttempts := p.opts.Backoff.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	for attempt := 0; attempt < maxAttempts; attempt++ {
		out, err := runCommands(ctx, workDir, cmds)
		if err == nil {
			return out, nil
		}
		if attempt+1 < maxAttempts {
			p.emit(stateCompiling, fmt.Sprintf("compile failed (attempt %d/%d), retrying after backoff", attempt+1, p.opts.Backoff.MaxAttempts))
			delay := p.opts.Backoff.Delay(attempt, rand.Float64())
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return out, ctx.Err()
			}
		} else {
			return out, err
		}
	}
	return "", fmt.Errorf("unreachable")
}

func runCommands(ctx context.Context, dir string, cmds [][]string) (string, error) {
	var out strings.Builder
	for _, args := range cmds {
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Dir = dir
		b, err := cmd.CombinedOutput()
		out.Write(b)
		if err != nil {
			return out.String(), fmt.Errorf("%s failed: %w", args[0], err)
		}
	}
	return out.String(), nil
}

func extractContextGap(validatorReport string) string {
	for _, line := range strings.Split(validatorReport, "\n") {
		if v, ok := extractField(strings.TrimSpace(line), "**Context Gap:**"); ok && v != "" {
			return v
		}
	}
	return ""
}

func fileChangesFromReport(dr *DeveloperReport) []FileDiff {
	diffs := make([]FileDiff, len(dr.FilesChanged))
	for i, f := range dr.FilesChanged {
		diffs[i] = FileDiff{Path: f, Summary: dr.WhatWasDone}
	}
	return diffs
}
