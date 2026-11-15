package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/snapshot"
)

// advisorGateLiveScenario is one session situation put to the real judge.
type advisorGateLiveScenario struct {
	name   string
	moment string
	// userRequest is the turn's human message.
	userRequest string
	// changed are the session's changed files (relative to the temp project).
	changed []string
	// reviews are earlier advisor reviews, oldest first. Each covers the named files.
	reviews []advisorReviewEntry
	// writesSinceReview are files written after the last review.
	writesSinceReview []string
	// turn activity
	toolCalls    map[string]int
	filesWritten []string
	bash         []string
	todoPlan     string
	// subject is the final report (end_of_turn) or the commit command (before_commit).
	subject string
	// want lists the acceptable choices.
	want []string
}

// TestAdvisorGateLiveScenarios puts realistic session states to the real judge
// and logs its choice and probability for each. Gated on TYPESAFE_API_KEY.
func TestAdvisorGateLiveScenarios(t *testing.T) {
	key := os.Getenv("TYPESAFE_API_KEY")
	if key == "" {
		t.Skip("TYPESAFE_API_KEY not set; skipping live advisor gate scenarios")
	}
	client := newTypesafeClient(key, "jev-latest", "https://api.typesafe.ai/v1")

	const (
		tabs  = "internal/tabs/tabs.go"
		tabsT = "internal/tabs/tabs_test.go"
		store = "web/src/stores/tabStore.ts"
	)
	firstReview := advisorReviewEntry{
		Moment:  advisorMomentEndOfTurn,
		Files:   []string{tabs, tabsT},
		Verdict: "Gaps: the dedupe ignores remote-host tabs, and no test covers two projects sharing a tab id. Fix both before reporting done.",
	}
	cleanReview := advisorReviewEntry{
		Moment:  advisorMomentEndOfTurn,
		Files:   []string{tabs, tabsT},
		Verdict: "complete. The dedupe handles local and remote tabs and the new test covers the shared-id case. Tests were run and pass.",
	}

	scenarios := []advisorGateLiveScenario{
		{
			name:         "1 edits, end of turn, never reviewed",
			moment:       advisorMomentEndOfTurn,
			userRequest:  "fix the session list showing duplicate tabs",
			changed:      []string{tabs, tabsT},
			toolCalls:    map[string]int{"read": 4, "grep": 2, "edit": 3, "bash": 1},
			filesWritten: []string{tabs, tabsT},
			bash:         []string{"go test ./internal/tabs/ -count=1"},
			subject:      "Fixed the duplicate tabs: the merge now dedupes by project and tab id. Added a test. go test ./internal/tabs passes.",
			want:         []string{advisorGateChoiceReview},
		},
		{
			name:              "2 reviewed, then fixes made, end of turn",
			moment:            advisorMomentEndOfTurn,
			userRequest:       "fix the session list showing duplicate tabs",
			changed:           []string{tabs, tabsT, store},
			reviews:           []advisorReviewEntry{firstReview},
			writesSinceReview: []string{tabs, tabsT, store},
			toolCalls:         map[string]int{"read": 3, "edit": 4, "bash": 2},
			filesWritten:      []string{tabs, tabsT, store},
			bash:              []string{"go test ./internal/tabs/ -count=1", "cd web && bun check ."},
			subject:           "Addressed the review: remote-host tabs are now deduped too, the web store no longer merges two projects, and a shared-id test was added. Tests pass.",
			want:              []string{advisorGateChoiceReview},
		},
		{
			name:        "3 reviewed clean, nothing changed since, end of turn",
			moment:      advisorMomentEndOfTurn,
			userRequest: "ok thanks, anything else left on this?",
			changed:     []string{tabs, tabsT},
			reviews:     []advisorReviewEntry{cleanReview},
			toolCalls:   map[string]int{"read": 1},
			subject:     "Nothing else is left: the fix and its test are in place and were reviewed.",
			want:        []string{advisorGateChoiceSkipReviewed, advisorGateChoiceSkipNotSubst},
		},
		{
			name:        "4 user asks for a plain commit, unreviewed changes",
			moment:      advisorMomentBeforeCommit,
			userRequest: "just commit",
			changed:     []string{tabs, tabsT},
			toolCalls:   map[string]int{"bash": 3},
			bash:        []string{"git status --short", "git diff --stat"},
			subject:     `git add -A && git commit -m "fix: dedupe tabs across projects"`,
			want:        []string{advisorGateChoiceSkipDeclined},
		},
		{
			name:         "5 fix and commit, commit about to run, never reviewed",
			moment:       advisorMomentBeforeCommit,
			userRequest:  "fix the session list showing duplicate tabs and commit it",
			changed:      []string{tabs, tabsT},
			toolCalls:    map[string]int{"read": 4, "edit": 3, "bash": 2},
			filesWritten: []string{tabs, tabsT},
			bash:         []string{"go test ./internal/tabs/ -count=1", "git status --short"},
			subject:      `git add -A && git commit -m "fix: dedupe tabs across projects"`,
			want:         []string{advisorGateChoiceReview},
		},
		{
			name:        "6 commit about to run, already reviewed, nothing changed since",
			moment:      advisorMomentBeforeCommit,
			userRequest: "commit and push",
			changed:     []string{tabs, tabsT},
			reviews:     []advisorReviewEntry{cleanReview},
			toolCalls:   map[string]int{"bash": 2},
			bash:        []string{"git status --short", "git diff --stat"},
			subject:     `git add -A && git commit -m "fix: dedupe tabs across projects"`,
			want:        []string{advisorGateChoiceSkipReviewed, advisorGateChoiceSkipNotSubst, advisorGateChoiceSkipDeclined},
		},
		{
			name:        "7 earlier unreviewed changes, this turn only answered a question",
			moment:      advisorMomentEndOfTurn,
			userRequest: "how does the tab store decide which project a tab belongs to?",
			changed:     []string{tabs},
			toolCalls:   map[string]int{"read": 5, "grep": 3},
			subject:     "A tab belongs to the project whose root it was opened under; the store keys tabs by an opaque project key and merges per project on PUT.",
			want:        []string{advisorGateChoiceSkipNotSubst},
		},
		{
			name:         "8 edits made, todo items still open, end of turn",
			moment:       advisorMomentEndOfTurn,
			userRequest:  "implement the tab dedupe plan",
			changed:      []string{tabs},
			toolCalls:    map[string]int{"read": 3, "edit": 2},
			filesWritten: []string{tabs},
			todoPlan:     "- [x] t1 dedupe local tabs in tabs.go\n- [ ] t2 dedupe remote-host tabs\n- [ ] t3 fix the web store merge\n- [ ] t4 add tests and run them",
			subject:      "Step 1 is done: local tabs are deduped. Next I will handle remote-host tabs, then the web store, then tests.",
			want:         []string{advisorGateChoiceSkipUnfinished},
		},
		{
			name:              "9 reviewed clean, then only a comment typo fixed, end of turn",
			moment:            advisorMomentEndOfTurn,
			userRequest:       "fix the typo in the comment above dedupeTabs",
			changed:           []string{tabs, tabsT},
			reviews:           []advisorReviewEntry{cleanReview},
			writesSinceReview: []string{tabs},
			toolCalls:         map[string]int{"read": 1, "edit": 1},
			filesWritten:      []string{tabs},
			subject:           "Fixed the typo: \"dedupliate\" is now \"deduplicate\" in the comment above dedupeTabs. No code changed.",
			want:              []string{advisorGateChoiceSkipNotSubst, advisorGateChoiceSkipReviewed},
		},
		{
			name:         "10 user says no need to validate, on a fix turn",
			moment:       advisorMomentEndOfTurn,
			userRequest:  "fix the duplicate tabs. no need to validate or review, i will check it myself",
			changed:      []string{tabs, tabsT},
			toolCalls:    map[string]int{"read": 4, "edit": 3},
			filesWritten: []string{tabs, tabsT},
			subject:      "Fixed the duplicate tabs: the merge now dedupes by project and tab id.",
			want:         []string{advisorGateChoiceSkipDeclined},
		},
		{
			name:              "11 reviewed, fixes made, commit about to run",
			moment:            advisorMomentBeforeCommit,
			userRequest:       "address the review and commit",
			changed:           []string{tabs, tabsT, store},
			reviews:           []advisorReviewEntry{firstReview},
			writesSinceReview: []string{tabs, tabsT, store},
			toolCalls:         map[string]int{"edit": 4, "bash": 2},
			filesWritten:      []string{tabs, tabsT, store},
			bash:              []string{"go test ./internal/tabs/ -count=1"},
			subject:           `git add -A && git commit -m "fix: dedupe remote tabs, stop cross-project merge"`,
			want:              []string{advisorGateChoiceReview},
		},
		{
			name:        "12 push only after a reviewed commit, end of turn",
			moment:      advisorMomentEndOfTurn,
			userRequest: "push it",
			changed:     []string{tabs, tabsT},
			reviews:     []advisorReviewEntry{cleanReview},
			toolCalls:   map[string]int{"bash": 5},
			bash:        []string{"git status --short", "git log --oneline -3", "git push origin main", "git status -sb", "git log origin/main --oneline -1"},
			subject:     "Pushed main to origin; the branch is up to date.",
			want:        []string{advisorGateChoiceSkipReviewed, advisorGateChoiceSkipNotSubst},
		},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			a, _ := gateTestAgent(t, nil)
			td := t.TempDir()
			snaps := snapshot.NewStore("gate-live", filepath.Join(td, "snaps"))
			a.snapshotStore = snaps
			abs := func(rel string) string { return filepath.Join(td, rel) }
			for i, f := range sc.changed {
				if err := snaps.Backup(abs(f), "tc"+string(rune('a'+i))); err != nil {
					t.Fatal(err)
				}
				snaps.RegisterWrite(abs(f), "tc"+string(rune('a'+i)))
			}
			for _, r := range sc.reviews {
				files := make([]string, len(r.Files))
				for i, f := range r.Files {
					files[i] = abs(f)
				}
				a.advisorReviews = append(a.advisorReviews, advisorReviewEntry{Moment: r.Moment, Files: files, Verdict: r.Verdict, Seq: snaps.LastWriteSeq()})
			}
			for i, f := range sc.writesSinceReview {
				if err := snaps.Backup(abs(f), "tw"+string(rune('a'+i))); err != nil {
					t.Fatal(err)
				}
				snaps.RegisterWrite(abs(f), "tw"+string(rune('a'+i)))
			}
			written := make([]string, len(sc.filesWritten))
			for i, f := range sc.filesWritten {
				written[i] = abs(f)
			}
			st := &advisorCheckpointState{
				userGoal:        sc.userRequest,
				toolCallsByName: sc.toolCalls,
				filesWritten:    written,
				bashCommands:    sc.bash,
			}
			state := a.advisorGateState(st, sc.moment, sc.subject)
			if sc.todoPlan != "" {
				state["todo_plan"] = sc.todoPlan
			}
			questions := advisorGateQuestions(sc.moment)

			resp, err := client.Decide(state, questions)
			if err != nil {
				t.Fatalf("Decide: %v", err)
			}
			ans, ok := resp.Answers[advisorGateKey]
			if !ok {
				t.Fatalf("no answer for %q", advisorGateKey)
			}
			if err := validateAnswer(questions[advisorGateKey], ans); err != nil {
				t.Fatalf("answer rejected: %v", err)
			}
			prob := ans.Probabilities[ans.Choice]
			acted := prob >= advisorGateChoiceFloor
			t.Logf("GATE %-62s -> %-22s p=%.2f confidence=%.2f acted=%t in_tokens=%d",
				sc.name, ans.Choice, prob, ans.Confidence, acted, resp.Usage.InputTokens)
			wanted := false
			for _, w := range sc.want {
				if ans.Choice == w {
					wanted = true
				}
			}
			if !wanted {
				t.Errorf("choice = %s (p=%.2f), want one of %s", ans.Choice, prob, strings.Join(sc.want, ", "))
			}
		})
	}
}
