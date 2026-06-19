// internal/orchestrator/advisor.go
package orchestrator

import (
	"context"
	"fmt"
)

// escalateToAdvisor sends the full pipeline context to the advisor agent and
// returns its resolution note. The advisor is dispatched as the "general"
// subagent — its system prompt plus the specific advisor framing below is
// what the advisor sees. The pipeline passes the full context in the prompt,
// not as a system prompt override.
//
// In practice the advisor subagent will use the parent agent's advisor tool
// (if advisor is enabled). Using "general" as the agent name with the prompt
// below is the correct pattern.
func (p *Pipeline) escalateToAdvisor(ctx context.Context, lastReport string) (string, error) {
	prompt := fmt.Sprintf(`You are reviewing a failed multi-agent coding pipeline.
The developer has attempted %d times without satisfying the validator.

%s

The final validator report was:
%s

Diagnose WHY convergence failed. Is the goal underspecified? Is the validator
applying an unreasonable standard? Is there a missing dependency or architectural
constraint the developer was not told about? Provide one specific, actionable
resolution strategy the developer can act on in a single attempt.`,
		p.iterCount,
		p.doc.Render(""),
		lastReport,
	)

	note, err := p.dispatchFn("general", prompt)
	if err != nil {
		return "", fmt.Errorf("advisor dispatch failed: %w", err)
	}
	return note, nil
}
