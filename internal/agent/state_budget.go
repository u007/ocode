package agent

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

// Shared pre-flight budget on the `state` sent to a decision backend.
//
// Sizing: TypeSafe documents Jev as "64k tokens per request; 32k tokens for
// `state` plus the longest question". This budget is set from the STRICTER of
// the two — the 32k `state` limit — and not from clef's flatter 65,536.
//
// The bytes-per-token factor is 3, not the usual 4. That is deliberate and the
// direction of the error matters: a chars/4 estimate UNDER-estimates the token
// count of source code and JSON, because punctuation and short identifiers
// tokenise poorly. A budget sized at 32_000 x 4 bytes would therefore admit
// states that are genuinely over the documented limit. Measured on the worst
// realistic payload in this repo (a 200 KB `write` body, which reaches the judge
// as `arguments.content`) the true ratio is about 3.6 bytes/token.
//
// HONEST LIMITATION: this is an estimate, not a tokenizer, and it still errs
// toward NOT asking — a state just under 96 KB may exceed 32k real tokens. It
// cannot be tightened without a real tokenizer, which this package does not
// carry. TestSharedStateBudget_WorstCaseProductionPayloads is therefore the
// load-bearing safety net: it measures each judge at its worst realistic size,
// so a future change that grows a payload past the ceiling fails there rather
// than degrading auto-mode silently in production.
const decisionStateBudgetBytes = 96_000

// statePreviewBytes is the per-field preview size projection aims for.
//
// Projection retries at progressively smaller previews (see prepareDecisionState)
// rather than committing to one number, so a state carrying many bulky fields
// still converges instead of being refused when a single fixed preview would
// have fitted.
const statePreviewBytes = 4096

// stateProjectedPreviews is the ladder of preview sizes tried, in order.
var stateProjectedPreviews = []int{statePreviewBytes, 1024, 256, 64}

// stateProjectionKey is the top-level state field that records that projection
// ran and which fields it clipped.
//
// It is STRUCTURED and TOP-LEVEL on purpose. The permission judge already has a
// truncation protocol — interpreter.source.truncated, executed_scripts[].truncated,
// "do not approve on a partial view", and the truncated_or_unknown concern that
// drops the confidence floor. Projection would otherwise be a second, UNSIGNALLED
// truncation: a marker buried inside a preview string, invisible to a judge that
// was explicitly taught to distrust partial views. Reusing the existing protocol
// means the auto-allow path cannot silently grade a clipped `write`.
const stateProjectionKey = "_projection"

// bulkyStateKeys are the field names whose VALUES are file contents, diff bodies
// or similar bulky payloads. Only these are projected.
//
// This is an allowlist on purpose. Projecting every oversized string would make
// refusal effectively unreachable — almost any state could be shrunk to fit —
// and an unreachable refusal is an untested branch. Keeping the list explicit
// means "this state is too big and we will not silently mangle it" stays a real,
// exercised outcome.
var bulkyStateKeys = map[string]bool{
	"arguments_raw": true,
	"body":          true,
	"code":          true,
	"content":       true,
	"diff":          true,
	"file_content":  true,
	"new_string":    true,
	"new_str":       true,
	"old_string":    true,
	"old_str":       true,
	"output":        true,
	"output_text":   true,
	"patch":         true,
	"result":        true,
	"source":        true,
	"source_code":   true,
	"stderr":        true,
	"stdout":        true,
	"text":          true,
}

// prepareDecisionState measures a judge state and, when it is over budget,
// projects its bulky fields down to a bounded preview.
//
// It returns (prepared, projected, err):
//
//   - projected == false: the state was within budget and is returned untouched.
//     The common case must stay byte-identical, or every judge's prompt changes
//     for no reason.
//   - projected == true: bulky fields were truncated. A truncation marker is
//     always embedded, so the judge can tell a preview from a whole file and
//     does not grade an invisible tail as if it were the real thing.
//   - err != nil: the state cannot be brought under budget and must NOT be sent.
//
// On refusal it returns nil and an error. It deliberately does NOT fall back to
// the other backend, and it does NOT silently send an over-budget state: a
// silent provider switch on the permission path would make "which model decided
// this?" unanswerable, and silently truncating the command a judge is being
// asked about is a safety problem rather than a UX one. Both backends surface the
// error to their caller, which already defers to the human.
func prepareDecisionState(state any) (any, bool, error) {
	size, err := marshalledDecisionStateSize(state)
	if err != nil {
		return nil, false, err
	}
	if size <= decisionStateBudgetBytes {
		return state, false, nil
	}

	fields, ok := state.(map[string]any)
	if ok {
		for _, preview := range stateProjectedPreviews {
			collector := &truncationRecorder{}
			projected := projectDecisionState(fields, preview, collector)
			psize, perr := marshalledDecisionStateSize(projected)
			if perr != nil {
				return nil, false, perr
			}
			if psize <= decisionStateBudgetBytes {
				projected[stateProjectionKey] = map[string]any{
					"applied": true,
					"fields":  collector.names(),
				}
				return projected, true, nil
			}
		}
	}
	return nil, false, fmt.Errorf("decision state is %d bytes, over the %d byte budget, and no projection of its bulky fields brings it under: refusing rather than sending a mangled or over-budget state", size, decisionStateBudgetBytes)
}

// marshalledDecisionStateSize measures the JSON the backend will actually
// receive, which is the payload PLUS framing. Sizing a test or a guard against
// the payload alone is the mistake this function exists to prevent: a 100-byte
// payload marshals to 116 bytes.
func marshalledDecisionStateSize(state any) (int, error) {
	raw, err := json.Marshal(state)
	if err != nil {
		return 0, fmt.Errorf("measure decision state: %w", err)
	}
	return len(raw), nil
}

// projectDecisionState returns a copy of fields with every bulky value replaced
// by a bounded preview. It never mutates the caller's map, because the same
// state may be reused by a second judge or re-read for debug output.
//
// It recurses into nested maps and slices, since the permission judge nests the
// offending payload as arguments.content rather than a top-level field.
func projectDecisionState(fields map[string]any, preview int, rec *truncationRecorder) map[string]any {
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		if bulkyStateKeys[k] {
			out[k] = projectDecisionValue(k, v, preview, rec)
			continue
		}
		out[k] = projectNested(k, v, preview, rec)
	}
	return out
}

// truncationRecorder collects the dotted paths projection actually clipped, so
// the state can tell the judge which content it is NOT seeing.
type truncationRecorder struct{ seen map[string]bool }

func (r *truncationRecorder) note(path string) {
	if r.seen == nil {
		r.seen = map[string]bool{}
	}
	r.seen[path] = true
}

func (r *truncationRecorder) names() []string {
	out := make([]string, 0, len(r.seen))
	for k := range r.seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// projectDecisionValue truncates a single bulky value, whatever shape it has.
func projectDecisionValue(path string, v any, preview int, rec *truncationRecorder) any {
	switch t := v.(type) {
	case string:
		return truncateStateValue(path, t, preview, rec)
	case map[string]any:
		return projectDecisionState(t, preview, rec)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = projectDecisionValue(fmt.Sprintf("%s[%d]", path, i), e, preview, rec)
		}
		return out
	default:
		return v
	}
}

// projectNested walks a non-bulky value looking for bulky keys further down.
func projectNested(path string, v any, preview int, rec *truncationRecorder) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			child := path + "." + k
			if bulkyStateKeys[k] {
				out[k] = projectDecisionValue(child, e, preview, rec)
			} else {
				out[k] = projectNested(child, e, preview, rec)
			}
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = projectNested(fmt.Sprintf("%s[%d]", path, i), e, preview, rec)
		}
		return out
	default:
		return v
	}
}

// truncateStateValue clips s to preview bytes and appends an explicit marker.
//
// The marker is not decoration. Without it the judge cannot distinguish a
// preview from a complete file and may treat the clipped tail as the whole
// input, which is precisely the invisible-truncation failure the projection
// exists to avoid.
func truncateStateValue(path, s string, preview int, rec *truncationRecorder) any {
	if len(s) <= preview {
		return s
	}
	rec.note(path)
	return s[:preview] + fmt.Sprintf("\n... [truncated for the judge: %d of %d bytes shown]", preview, len(s))
}

// probabilitySumTolerance is how far a returned probability set may miss 1.
//
// It suits values rounded to a few decimal places. A one-part-in-a-million
// tolerance would reject ordinary backend output and turn a real verdict into
// "no verdict". Erring TIGHT is the safe direction, because rejection is
// fail-open: an over-tight tolerance merely makes the judge less effective,
// while an over-loose one lets a malformed distribution gate on a decision.
const probabilitySumTolerance = 1e-3

// validateAnswer reports whether an answer may be trusted to gate on.
//
// A rejected answer must cause the caller to KEEP the candidate. This is the most
// important property in the file: the zero value of Noul is 0, which every
// relevance judge reads as "definitely irrelevant" and therefore vetoes. A
// fail-closed bug would otherwise be hiding inside a system whose contract is
// fail-open.
func validateAnswer(q TypesafeQuestion, ans TypesafeAnswer) error {
	if ans.Type != q.Type {
		return fmt.Errorf("answer type %q disagrees with question type %q", ans.Type, q.Type)
	}
	switch ans.Type {
	case "choice":
		if ans.Choice == "" {
			return fmt.Errorf("choice answer selected no option")
		}
		if len(q.Criteria) > 0 {
			if _, ok := q.Criteria[ans.Choice]; !ok {
				return fmt.Errorf("choice %q is not one of the %d offered options", ans.Choice, len(q.Criteria))
			}
		}
		if err := validateProbabilities(ans.Probabilities); err != nil {
			return err
		}
	case "noul":
		if err := validateUnitInterval("noul", ans.Noul); err != nil {
			return err
		}
	case "score":
		// Score has no documented range, so only finiteness is meaningful here.
		if math.IsNaN(ans.Score) || math.IsInf(ans.Score, 0) {
			return fmt.Errorf("score is not finite: %v", ans.Score)
		}
	default:
		return fmt.Errorf("unknown answer type %q", ans.Type)
	}
	if err := validateUnitInterval("confidence", ans.Confidence); err != nil {
		return err
	}
	return nil
}

// validateProbabilities checks each probability is a real probability and that
// the set is normalised.
func validateProbabilities(p map[string]float64) error {
	if len(p) == 0 {
		return nil
	}
	sum := 0.0
	for k, v := range p {
		if err := validateUnitInterval("probability "+k, v); err != nil {
			return err
		}
		sum += v
	}
	if math.Abs(sum-1) > probabilitySumTolerance {
		return fmt.Errorf("probabilities sum to %v, want 1 within %v", sum, probabilitySumTolerance)
	}
	return nil
}

// validateUnitInterval rejects anything outside [0,1], including NaN and the
// infinities. Go's comparison operators make every one of these false against
// NaN, so a naive `v < 0 || v > 1` would ACCEPT NaN — hence the explicit check.
func validateUnitInterval(name string, v float64) error {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return fmt.Errorf("%s is not finite: %v", name, v)
	}
	if v < 0 || v > 1 {
		return fmt.Errorf("%s is %v, outside the unit interval [0,1]", name, v)
	}
	return nil
}
