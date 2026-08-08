package server

import (
	"fmt"
	"testing"

	"github.com/u007/ocode/internal/agent"
)

func TestDbgStep(t *testing.T) {
	ag := agent.NewAgent(&fakeClientAskBash{}, nil, nil, nil)
	ag.Permissions().SetMode(agent.PermissionModeNormal)
	msgs := []agent.Message{{Role: "user", Content: "list files"}}
	resp, err := ag.Step(msgs)
	if err != nil {
		t.Fatalf("step err: %v", err)
	}
	for i, m := range resp {
		fmt.Printf("resp[%d] role=%s toolID=%q content=%q toolcalls=%d\n", i, m.Role, m.ToolID, trunc(m.Content, 60), len(m.ToolCalls))
	}
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
