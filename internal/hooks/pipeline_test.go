package hooks

import (
	"encoding/json"
	"testing"
)

func TestToolBeforeHookModifiesArgs(t *testing.T) {
	p := New()
	p.RegisterToolBefore(func(name string, args json.RawMessage) json.RawMessage {
		if name == "read" {
			return json.RawMessage(`{"path":"/modified"}`)
		}
		return args
	})
	result := p.RunToolBefore("read", json.RawMessage(`{"path":"/original"}`))
	if string(result) != `{"path":"/modified"}` {
		t.Errorf("got %s, want modified args", string(result))
	}
}

func TestToolBeforeHookChaining(t *testing.T) {
	p := New()
	calls := 0
	p.RegisterToolBefore(func(_ string, args json.RawMessage) json.RawMessage { calls++; return args })
	p.RegisterToolBefore(func(_ string, args json.RawMessage) json.RawMessage { calls++; return args })
	p.RunToolBefore("any", json.RawMessage(`{}`))
	if calls != 2 {
		t.Errorf("expected 2 calls, got %d", calls)
	}
}

func TestToolAfterHookModifiesResult(t *testing.T) {
	p := New()
	p.RegisterToolAfter(func(name, result string) string { return "[modified] " + result })
	got := p.RunToolAfter("bash", "hello")
	if got != "[modified] hello" {
		t.Errorf("got %q", got)
	}
}

func TestChatParamsHookOverridesTemperature(t *testing.T) {
	p := New()
	p.RegisterChatParams(func(model string, cp ChatParams) ChatParams {
		v := 0.1
		cp.Temperature = &v
		return cp
	})
	got := p.RunChatParams("gpt-4", ChatParams{})
	if got.Temperature == nil || *got.Temperature != 0.1 {
		t.Errorf("expected temperature 0.1, got %v", got.Temperature)
	}
}

func TestShellEnvHookInjectsVars(t *testing.T) {
	p := New()
	p.RegisterShellEnv(func(cwd string) map[string]string {
		return map[string]string{"MY_VAR": "injected"}
	})
	env := p.RunShellEnv("/some/dir")
	if env["MY_VAR"] != "injected" {
		t.Errorf("expected MY_VAR=injected, got %q", env["MY_VAR"])
	}
}

func TestShellEnvHookMergesMultiple(t *testing.T) {
	p := New()
	p.RegisterShellEnv(func(_ string) map[string]string { return map[string]string{"A": "1"} })
	p.RegisterShellEnv(func(_ string) map[string]string { return map[string]string{"B": "2"} })
	env := p.RunShellEnv("/dir")
	if env["A"] != "1" || env["B"] != "2" {
		t.Errorf("expected merged env, got %v", env)
	}
}

func TestEmptyPipelineIsNoOp(t *testing.T) {
	p := New()
	args := json.RawMessage(`{"x":1}`)
	if got := p.RunToolBefore("any", args); string(got) != string(args) {
		t.Errorf("empty pipeline changed args: %s", got)
	}
	if got := p.RunToolAfter("any", "result"); got != "result" {
		t.Errorf("empty pipeline changed result: %s", got)
	}
	if got := p.RunChatParams("m", ChatParams{}); got.Temperature != nil {
		t.Errorf("empty pipeline set temperature")
	}
	if env := p.RunShellEnv("/"); len(env) != 0 {
		t.Errorf("empty pipeline returned env vars")
	}
}
