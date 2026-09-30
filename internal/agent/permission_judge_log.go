package agent

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/debuglog"
	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/redact"
)

// Why this file exists: the auto-permission verdict, its confidence, the concern
// the judge named, and the allowed_roots it was reasoning over used to exist only
// in an emitDebug line that reaches the TUI or stderr. Once the turn ended the
// evidence was gone, so a real below-floor deferral could not be diagnosed at all
// — which is how an `allow` at 0.21 went unexplained for an entire investigation.
// These records are the durable answer to "why was this call approved, deferred,
// or refused".
//
// Registration is deliberately lazy and lives here rather than in tui/model.go or
// server/handler.go. The judge is the only caller that needs the sink, so wiring
// it here gives identical behaviour in TUI, server and headless modes, instead of
// only in whichever mode remembered to register a mirror at startup.

// judgeLogOnce guards mirror registration. Registration mutates process-global
// state on the shared debuglog sink, so it must happen exactly once.
var judgeLogOnce sync.Once

// ensurePermissionJudgeLog registers the durable sink for PERMJUDGE entries.
// Failures are reported once on the ERROR channel and never affect a permission
// decision: this is diagnostics, and whether a call is approved must never depend
// on whether a log file could be opened. paths.LogsDir already creates its
// directory, so no separate MkdirAll is needed — the mirror opens the file with
// O_CREATE and would fail on a missing parent otherwise.
func ensurePermissionJudgeLog() {
	judgeLogOnce.Do(func() {
		dir, err := paths.LogsDir()
		if err != nil {
			emitDebug("ERROR", fmt.Sprintf("permission judge log disabled: resolve logs dir: %v", err))
			return
		}
		debuglog.Log.MirrorKindToFile(debuglog.KindPermissionJudge,
			filepath.Join(dir, "permission-judge.log"))
	})
}

// judgeOutcome classifies what the permission layer actually did with a verdict.
// It is the field that makes a record actionable: the same verdict can lead to
// several different outcomes, and only this says which one happened.
type judgeOutcome string

const (
	outcomeGranted        judgeOutcome = "granted"
	outcomeBelowFloor     judgeOutcome = "deferred_below_floor"
	outcomeGuardRefused   judgeOutcome = "refused_deterministic_guard"
	outcomeJudgeDenied    judgeOutcome = "denied_by_judge"
	outcomeRelaxedAllow   judgeOutcome = "granted_relaxed_concern"
	outcomeTransportError judgeOutcome = "transport_error"
	outcomeNoVerdict      judgeOutcome = "no_verdict"
	outcomeUnknownChoice  judgeOutcome = "unknown_choice"
)

// permissionJudgeRecord is the durable shape of one judge decision. Every field
// that explains a verdict is present, so the record is sufficient to answer a
// below-floor deferral after the fact without re-running anything.
type permissionJudgeRecord struct {
	Time             string             `json:"time"`
	Session          string             `json:"session,omitempty"`
	Tool             string             `json:"tool"`
	Model            string             `json:"model"`
	Rule             string             `json:"rule,omitempty"`
	Scope            string             `json:"scope,omitempty"`
	Command          string             `json:"command,omitempty"`
	CommandWithheld  string             `json:"command_withheld,omitempty"`
	WorkDir          string             `json:"working_directory,omitempty"`
	ResolvedCD       string             `json:"resolved_cd,omitempty"`
	AllowedRoots     []string           `json:"allowed_roots,omitempty"`
	AllowedRootsAll  int                `json:"allowed_roots_total,omitempty"`
	RootsOmitted     int                `json:"allowed_roots_omitted,omitempty"`
	AllowDestructive bool               `json:"allow_destructive"`
	Choice           string             `json:"choice,omitempty"`
	Confidence       float64            `json:"confidence"`
	Probabilities    map[string]float64 `json:"probabilities,omitempty"`
	Concern          string             `json:"concern,omitempty"`
	ConcernConf      float64            `json:"concern_confidence"`
	Floor            float64            `json:"floor"`
	Outcome          judgeOutcome       `json:"outcome"`
	Reason           string             `json:"reason,omitempty"`
	Error            string             `json:"error,omitempty"`
}

// judgeMaxLoggedRoots caps how many roots a record names. The full list runs to
// ~100 roots on a typical machine, which made each record ~100KB and left only
// ~20 records inside the shared 2MB cap — a log that cannot hold a session is
// worse than useless, because it looks like it is recording something.
const judgeMaxLoggedRoots = 12

// relevantAllowedRoots returns the allowed roots that actually bear on this call:
// the working directory, the cd target when one was folded, and any root that
// contains a path the command mentions. The total count is always reported,
// because "the target was in none of the N roots" is precisely the diagnosis the
// 0.21 investigation needed, and it must stay answerable when nothing matched.
// normalizeForCompare resolves a path so it can be compared with a root from
// AllowedRoots, which are symlink-resolved by construction. It delegates to
// resolveForScopeCheck — the permission layer's own resolver — rather than
// EvalSymlinks directly, because EvalSymlinks fails outright on a path that does
// not exist yet and the fallback would leave it unresolved. That matters twice
// over: on macOS /var/folders/... is a link to /private/var/folders/..., and a
// command routinely names a target that has not been created.
func normalizeForCompare(p string) string {
	if p == "" {
		return ""
	}
	if r, ok := resolveForScopeCheck(p); ok {
		return r
	}
	return filepath.Clean(p)
}

func relevantAllowedRoots(all []string, workDir string, commands ...string) (relevant []string, omitted int) {
	// Candidates come from BOTH the original and the folded command: folding
	// removes the `cd`, and the cd target is precisely the path whose containment
	// decides whether the judge was right to hesitate.
	candidates := map[string]bool{}
	if workDir != "" {
		candidates[normalizeForCompare(workDir)] = true
	}
	for _, command := range commands {
		for _, f := range splitShellFields(command) {
			if strings.HasPrefix(f, "/") {
				candidates[normalizeForCompare(f)] = true
			}
		}
	}
	keep := make([]string, 0, judgeMaxLoggedRoots)
	for _, root := range all {
		if root == "" {
			continue
		}
		nRoot := normalizeForCompare(root)
		// The workdir is already in candidates, so membership is the only test.
		match := false
		for c := range candidates {
			if c == nRoot || strings.HasPrefix(c, nRoot+string(filepath.Separator)) {
				match = true
				break
			}
		}
		if !match {
			continue
		}
		if len(keep) == judgeMaxLoggedRoots {
			omitted++
			continue
		}
		keep = append(keep, root)
	}
	return keep, omitted
}

// newJudgeRecord fills the context every record shares — tool, model, rule,
// scope, command, working directory, resolved cd, and the allowed roots the judge
// actually saw — and then merges the caller-supplied parts in order, later
// non-empty fields winning. Variadic so a branch with no verdict yet (transport
// failure, missing answer) can pass only its outcome.
func (a *Agent) newJudgeRecord(toolName, model string, args json.RawMessage,
	req *PermissionRequest, parts ...permissionJudgeRecord) permissionJudgeRecord {
	rec := permissionJudgeRecord{Tool: toolName, Model: model}
	if req != nil {
		rec.Rule, rec.Scope = req.Rule, string(req.Scope)
	}
	if a != nil {
		rec.WorkDir = a.effectiveWorkDir()
		rec.AllowDestructive = a.autoPermissionAllowsDestructive()
		if a.permissions != nil {
			rec.AllowedRootsAll = len(a.permissions.AllowedRoots())
		}
	}
	var command string
	var p struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(args, &p); err == nil {
		command = p.Command
	}
	rec.Command = command
	// Recompute the fold for the log. It is a pure function on the same input the
	// state builder used, and re-deriving it here is cheaper and less coupled
	// than threading the result out of buildTypesafePermissionState — and it
	// records the fact that a fold happened, which is the single most useful
	// thing to know when a verdict looks wrong.
	if folded, cwd, ok := foldTopLevelCds(command, rec.WorkDir, func(path string) bool {
		return a != nil && a.permissions != nil && isWithinAllowedScope(a.permissions, path)
	}); ok {
		rec.Command, rec.ResolvedCD = folded, cwd
	}
	if a != nil && a.permissions != nil {
		rec.AllowedRoots, rec.RootsOmitted = relevantAllowedRoots(
			a.permissions.AllowedRoots(), rec.WorkDir, rec.Command, command)
	}
	for _, part := range parts {
		if part.Choice != "" {
			rec.Choice = part.Choice
		}
		if part.Confidence != 0 {
			rec.Confidence = part.Confidence
		}
		if part.Probabilities != nil {
			rec.Probabilities = part.Probabilities
		}
		if part.Concern != "" {
			rec.Concern = part.Concern
		}
		if part.ConcernConf != 0 {
			rec.ConcernConf = part.ConcernConf
		}
		if part.Floor != 0 {
			rec.Floor = part.Floor
		}
		if part.Outcome != "" {
			rec.Outcome = part.Outcome
		}
		if part.Reason != "" {
			rec.Reason = part.Reason
		}
		if part.Error != "" {
			rec.Error = part.Error
		}
	}
	return rec
}

// logPermissionJudge writes one judge decision to the durable sink. It never
// returns an error and must never fail a permission call: a diagnostics problem
// cannot be allowed to change whether a tool runs. The record is emitted through
// emitDebug("PERMISSION", …) so it also reaches the TUI Log panel and the
// server's /api/logs, and is mirrored to <logsDir>/permission-judge.log with the
// shared 2MB rotation already implemented in internal/debuglog.
func (a *Agent) logPermissionJudge(rec permissionJudgeRecord) {
	ensurePermissionJudgeLog()
	if rec.Time == "" {
		rec.Time = time.Now().Format(time.RFC3339)
	}
	if rec.Session == "" && a != nil {
		rec.Session = a.sessionIDValue()
	}
	a.redactJudgeRecord(&rec)

	line, err := json.Marshal(rec)
	if err != nil {
		// Reported on ERROR, not PERMISSION: marshalling the record we are
		// writing to the sink just failed, and re-entering the sink to report that
		// would risk recursing through the same failure.
		emitDebug("ERROR", fmt.Sprintf("permission judge log: marshal record: %v", err))
		return
	}
	// Appended to the process sink DIRECTLY, not only through emitDebug: the
	// durable mirror is registered on debuglog.Log, and it only fires for entries
	// that reach that sink. Routing solely through the DebugAppend hook would make
	// the file depend on a startup-time wiring decision — and would silently
	// produce nothing in tests, which replace DebugAppend.
	debuglog.Log.Append(debuglog.Entry{Kind: debuglog.KindPermissionJudge, Message: string(line)})
	// Also surfaced to the TUI Log panel / server /api/logs. In server mode
	// DebugAppend points at this same sink, so the line appears twice in the
	// in-memory ring; it is rare, bounded, and the cost of missing the panel
	// outright would be higher.
	emitDebug("PERMISSION", string(line))
}

// redactJudgeRecord strips secret material from a record before it is written.
//
// Two layers, because they cover different gaps:
//
//  1. The session masking registry, which is authoritative when /mask is on —
//     the same substitution the judge request itself uses, so the log can never
//     show more than the model saw.
//  2. An unconditional pattern backstop, because /mask is off by default and a
//     bash command can carry a literal token (`curl -H "Authorization: $T"`,
//     `psql postgres://user:pw@host/db`). This file is durable and likely to be
//     attached to a bug report, so when the command trips the secret patterns it
//     is withheld rather than recorded. Withholding is acceptable because
//     diagnosing a low-confidence verdict needs the roots, working directory,
//     floor, confidence and concern — not the literal command text.
func (a *Agent) redactJudgeRecord(rec *permissionJudgeRecord) {
	scrub := func(s string) string {
		if a == nil {
			return s
		}
		if reg := a.judgeMaskRegistry(); reg != nil {
			return reg.Substitute(redactText(s, reg))
		}
		return s
	}
	rec.Command = scrub(rec.Command)
	rec.Reason = scrub(rec.Reason)
	rec.Error = scrub(rec.Error)

	// Backstop for when /mask is off. redact.Detect is used rather than
	// QuickSecretPatterns because the latter only matches a fixed list of vendor
	// formats; it misses, for example, a `curl -H "Authorization: Bearer <opaque>"`
	// whose token shape it does not know. Detect additionally covers
	// url_credentials and the keyword-adjacent entropy family (authorization,
	// bearer, token, api_key, password) in chat mode, which is the right mode
	// for a shell command line.
	//
	// The command is withheld rather than span-substituted: the log's purpose is
	// to explain a verdict, and that needs the roots, working directory, floor,
	// confidence and concern — not the literal text. Withholding is therefore a
	// small diagnostic loss in exchange for never persisting a credential in a
	// file that outlives the session and is likely to be pasted into a report.
	if rec.Command != "" {
		if spans := redact.Detect(rec.Command, nil, redact.DetectOpts{}); len(spans) > 0 {
			kinds := make([]string, 0, len(spans))
			for _, sp := range spans {
				kinds = append(kinds, sp.Kind)
			}
			rec.CommandWithheld = "command withheld: matched secret detectors " + strings.Join(kinds, ",")
			rec.Command = ""
		}
	}
}
