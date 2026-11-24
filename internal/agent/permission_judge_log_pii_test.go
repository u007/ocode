package agent

import (
	"strings"
	"testing"

	"github.com/u007/ocode/internal/redact"
)

// TestRedactJudgeRecordStaysSecretsOnly pins the durable judge log to the
// secrets-only contract: PII in a command is kept, because explaining a verdict
// is the log's whole purpose, while a real credential is withheld. The pin
// exists because switching the backstop to redact.Detect with PII categories
// still compiles and leaves the named judge test set green — no other test
// feeds a PII-bearing command through this path.
func TestRedactJudgeRecordStaysSecretsOnly(t *testing.T) {
	const email = "jane@example.com"
	const awsKey = "AKIAIOSFODNN7EXAMPLE"

	cases := []struct {
		name         string
		agent        *Agent
		command      string
		wantWithheld bool
		wantCommand  string
	}{
		{
			// The user-approved carve-out: /mask is on, PII is enabled for the
			// conversation, and the command holds an email but no secret.
			name: "pii command is kept",
			agent: &Agent{
				redactionEnabled:  true,
				redactionRegistry: redact.NewRegistry("judgepii"),
				redactionPII:      []redact.PIICategory{redact.PIIContact},
			},
			command:     "run --owner " + email,
			wantCommand: "run --owner " + email,
		},
		{
			// Control: the backstop still fires for a known-format secret when
			// /mask is off (no registry to mask it with).
			name:         "secret command is withheld",
			agent:        &Agent{},
			command:      `curl -H "Authorization: Bearer ` + awsKey + `"`,
			wantWithheld: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &permissionJudgeRecord{Tool: "bash", Command: tc.command}
			tc.agent.redactJudgeRecord(rec)

			if tc.wantWithheld {
				if rec.CommandWithheld == "" {
					t.Fatalf("command was not withheld: %q", rec.Command)
				}
				if rec.Command != "" {
					t.Errorf("Command = %q, want empty when withheld", rec.Command)
				}
				return
			}

			if rec.CommandWithheld != "" {
				t.Errorf("CommandWithheld = %q, want empty (PII must not be withheld)", rec.CommandWithheld)
			}
			if rec.Command != tc.wantCommand {
				t.Errorf("Command = %q, want %q (PII must survive unchanged)", rec.Command, tc.wantCommand)
			}
			if !strings.Contains(rec.Command, email) {
				t.Errorf("email vanished from the record: %q", rec.Command)
			}
		})
	}
}
