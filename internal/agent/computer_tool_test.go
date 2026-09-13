package agent

import (
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

func TestAgentHidesComputerToolUntilSupervisorAttached(t *testing.T) {
	cfg := &config.Config{Ocode: config.OcodeConfig{
		ComputerUse: config.ComputerUseConfig{Enabled: true},
	}}
	ag := NewAgent(nil, []tool.Tool{&tool.ComputerTool{Config: cfg}}, cfg, nil)
	defer ag.Shutdown()

	if ag.isToolAllowed("computer") {
		t.Fatal("computer tool must not be advertised before a process supervisor is attached")
	}
	if _, ok := ag.GetTool("computer"); !ok {
		t.Fatal("computer tool must be kept so a later SetSupervisor can attach the driver")
	}
	ag.SetSupervisor(tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{}))
	ct, ok := ag.GetTool("computer")
	if !ok {
		t.Fatal("computer tool missing after SetSupervisor")
	}
	if ct.(*tool.ComputerTool).Driver == nil {
		t.Fatal("driver not attached after SetSupervisor")
	}
	if !ag.isToolAllowed("computer") {
		t.Fatal("computer tool must be advertised once the driver is attached")
	}
}
