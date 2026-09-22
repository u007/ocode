package session

import (
	"strings"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tool"
)

// transcriptTailVerdict classifies the last row of a transcript for the
// "interrupted turn" notice: a chat whose turn was cut off must not look like
// one that simply finished.
//
// One pure rule is shared by the resident agent's in-memory transcript and the
// decoded stored rows, so the verdict can never drift between the two sources.
type transcriptTailVerdict int

const (
	// tailComplete: the last row is a landed reply — an assistant row with
	// content or a notice. The turn is not interrupted. An empty transcript is
	// complete too: there is nothing to interrupt.
	tailComplete transcriptTailVerdict = iota
	// tailWaiting: the last tool round still holds an unanswered
	// question/permission sentinel. The dialog owns this state, and the
	// session is paused rather than interrupted.
	tailWaiting
	// tailUnfinished: no reply landed and nothing is waiting — a user row, an
	// answered-ask tool row, a tool_calls-only assistant, a content-less
	// assistant, or any other tool row.
	tailUnfinished
)

// transcriptTailVerdictFor classifies the LAST row of msgs.
//
// Only the last row matters: an assistant row carrying tool_calls has no tool
// results after it by construction, and a tool tail means no assistant reply
// ever followed. A trailing tool RUN may pause on more than one ask (parallel
// dispatch runs several calls before the pause check), so an unanswered
// sentinel anywhere in that round reads as waiting.
func transcriptTailVerdictFor(msgs []agent.Message) transcriptTailVerdict {
	if len(msgs) == 0 {
		return tailComplete
	}
	last := msgs[len(msgs)-1]
	if last.Role == "assistant" &&
		(strings.TrimSpace(last.Content) != "" || strings.TrimSpace(last.Notice) != "") {
		return tailComplete
	}
	if last.Role == "tool" {
		for i := trailingToolRunStart(msgs); i < len(msgs); i++ {
			if unansweredAsk(msgs[i].Content) {
				return tailWaiting
			}
		}
	}
	return tailUnfinished
}

// TranscriptTailUnfinished reports whether msgs has settled on a turn that is
// still owed a reply. This is the tail half of the server's interrupted-turn
// rule (see Handler.sessionInterrupted): it is true for a user row, an
// answered-ask tool row, a tool_calls-only assistant, a content-less
// assistant, and any other tool row — but false for a landed reply, an
// unanswered ask (the dialog owns it), and an empty transcript.
func TranscriptTailUnfinished(msgs []agent.Message) bool {
	return transcriptTailVerdictFor(msgs) == tailUnfinished
}

// unansweredAsk reports whether a tool-result row still holds an unresolved
// permission or question sentinel. A resolved ask has its sentinel content
// replaced in place, so it stops matching here.
func unansweredAsk(content string) bool {
	if strings.HasPrefix(content, tool.SentinelPermissionAsk) {
		return true
	}
	return strings.HasPrefix(content, tool.SentinelQuestionPrompt) &&
		strings.Contains(content, tool.SentinelWaitingForUser)
}

// trailingToolRunStart returns the index of the first message in the run of
// consecutive tool-role messages at the end of msgs — the results of the most
// recent assistant tool-call round.
func trailingToolRunStart(msgs []agent.Message) int {
	i := len(msgs)
	for i > 0 && msgs[i-1].Role == "tool" {
		i--
	}
	return i
}
