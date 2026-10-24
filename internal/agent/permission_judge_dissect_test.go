//go:build integration

package agent

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// TestPermissionJudgeDissect finds WHICH part of a bash command drags the Jev
// auto-permission score down. It scores the whole line, every top-level
// segment alone, and the whole line with each segment left out, then prints a
// table. Skill: skills/ocode-jev-score-dissect.
//
// Live and billed: skipped unless OCODE_JEV_DISSECT_CMD is set.
//
//	OCODE_JEV_DISSECT_CMD   the command line to dissect (required)
//	OCODE_JEV_DISSECT_DIR   working directory (default: this repo)
//	OCODE_JEV_DISSECT_MODE  permission mode: normal|sandbox|yolo|locked (default: normal)
//	OCODE_JEV_DISSECT_RUNS  judge calls per variant (default 3)
//
// Run: OCODE_AGENT_TEST_HOME=1 OCODE_JEV_DISSECT_CMD='...' \
//
//	go test -tags integration ./internal/agent -run TestPermissionJudgeDissect -count=1 -v
func TestPermissionJudgeDissect(t *testing.T) {
	cmd := os.Getenv("OCODE_JEV_DISSECT_CMD")
	if cmd == "" {
		t.Skip("set OCODE_JEV_DISSECT_CMD to dissect a command")
	}
	runs := 3
	if v := os.Getenv("OCODE_JEV_DISSECT_RUNS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			t.Fatalf("OCODE_JEV_DISSECT_RUNS=%q: want a positive integer", v)
		}
		runs = n
	}
	wd := os.Getenv("OCODE_JEV_DISSECT_DIR")
	if wd == "" {
		wd, _ = os.Getwd()
	}
	mode := PermissionMode(os.Getenv("OCODE_JEV_DISSECT_MODE"))
	if mode == "" {
		mode = PermissionModeNormal
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	client, ok := newClientFn(cfg, "typesafe/jev-latest").(*TypesafeClient)
	if !ok || client == nil || client.APIKey == "" {
		t.Fatal("no keyed TypeSafe client")
	}
	tool.SetPersistentExtraAllowedPaths(cfg.Ocode.ExtraAllowedPaths)
	a := &Agent{config: cfg, permissions: NewPermissionManager(), tools: map[string]tool.Tool{}}
	a.permissions.LoadFromConfig(cfg.Permission)
	a.permissions.LoadFromOcode(cfg.Ocode.Permissions)
	a.SetWorkDir(wd)
	a.permissions.SetMode(mode)
	for _, p := range strings.Split(os.Getenv("OCODE_JEV_DISSECT_ALLOW"), ",") {
		if p = strings.TrimSpace(p); p != "" {
			a.permissions.SetBashPrefixRule(p, PermissionAllow)
		}
	}

	floor := autoJudgeMinConfidenceDefault
	segs := dissectSplitSegments(cmd)
	type variant struct{ label, line string }
	variants := []variant{{"WHOLE", cmd}}
	if len(segs) > 1 {
		for i, s := range segs {
			variants = append(variants, variant{"only #" + strconv.Itoa(i) + ": " + s, s})
		}
		for i, s := range segs {
			rest := append(append([]string{}, segs[:i]...), segs[i+1:]...)
			variants = append(variants, variant{"without #" + strconv.Itoa(i) + ": " + s, strings.Join(rest, " && ")})
		}
	}

	t.Logf("mode=%s dir=%s floor=%.2f runs=%d", mode, wd, floor, runs)
	for _, v := range variants {
		args, _ := json.Marshal(map[string]string{"command": v.line})
		d := a.permissions.Decide("bash", args)
		req := d.Request
		rule := "-"
		if req == nil {
			req = &PermissionRequest{Rule: "tool.bash", Scope: PermissionScopeTool}
		} else {
			rule = req.Rule
		}
		var confs []float64
		concerns := map[string]int{}
		choices := map[string]int{}
		for i := 0; i < runs; i++ {
			state := a.buildTypesafePermissionState("bash", args, req)
			ans := permissionJudgeEvalAsk(client, state, a.typesafePermissionQuestions())
			if ans.Err != "" {
				t.Errorf("%s: judge error: %s", v.label, ans.Err)
				continue
			}
			confs = append(confs, ans.Confidence)
			concerns[ans.Concern]++
			choices[ans.Choice]++
		}
		lo, hi := 2.0, -1.0
		for _, c := range confs {
			if c < lo {
				lo = c
			}
			if c > hi {
				hi = c
			}
		}
		okGrant, why := a.verifyAutoGrant("bash", args, req)
		t.Logf("%-5s static=%-5s rule=%s conf=%.2f-%.2f choice=%v concern=%v guard_ok=%v %s\n      %s",
			passMark(lo, floor), d.Level, rule, lo, hi, choices, concerns, okGrant, why, v.label)
	}
	t.Log("read: a segment whose `only` row is low, or whose `without` row jumps up, is the culprit; concern names the rubric category the judge cited")
}

func passMark(lowest, floor float64) string {
	if lowest < floor {
		return "LOW"
	}
	return "ok"
}

// dissectSplitSegments splits on top-level &&, || and ; outside quotes and
// $(...). A pipeline stays whole (a lone `tail -25` is not a command anyone
// runs), and a lone & (as in 2>&1) never splits.
func dissectSplitSegments(cmd string) []string {
	var segs []string
	var cur strings.Builder
	var quote byte
	depth := 0
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			segs = append(segs, s)
		}
		cur.Reset()
	}
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '$' && i+1 < len(cmd) && cmd[i+1] == '(':
			depth++
		case c == ')' && depth > 0:
			depth--
		case depth == 0 && c == ';':
			flush()
			continue
		case depth == 0 && c == '|' && i+1 < len(cmd) && cmd[i+1] == '|':
			i++
			flush()
			continue
		case depth == 0 && c == '&' && i+1 < len(cmd) && cmd[i+1] == '&':
			i++
			flush()
			continue
		}
		cur.WriteByte(c)
	}
	flush()
	return segs
}
