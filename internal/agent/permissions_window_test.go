package agent

import (
	"encoding/json"
	"testing"
)

func TestPermissions_WindowListAllowedOthersAsk(t *testing.T) {
	pm := NewPermissionManager()
	if got := pm.Check("window"); got != PermissionAsk {
		t.Fatalf("Check(window) = %s; want ask", got)
	}
	if dec := pm.Decide("window", json.RawMessage(`{"action":"list"}`)); dec.Level != PermissionAllow {
		t.Fatalf("list: %s; want allow", dec.Level)
	}
	for _, action := range []string{"focus", "move_resize", "minimize", "restore", "maximize", "close"} {
		dec := pm.Decide("window", json.RawMessage(`{"action":"`+action+`","id":"7"}`))
		if dec.Level != PermissionAsk || dec.Request == nil || dec.Request.Rule != "tool.window" {
			t.Fatalf("%s: %+v; want ask with rule tool.window", action, dec)
		}
		if want := action + " 7"; dec.Request.Command != want {
			t.Errorf("%s: command %q want %q", action, dec.Request.Command, want)
		}
	}
}

func TestPermissions_WindowDenyRule(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetRule("window", PermissionDeny)
	if dec := pm.Decide("window", json.RawMessage(`{"action":"close","id":"1"}`)); dec.Level != PermissionDeny {
		t.Fatalf("got %s want deny", dec.Level)
	}
}

func TestPermissions_WindowListHonoursDeny(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetRule("window", PermissionDeny)
	if dec := pm.Decide("window", json.RawMessage(`{"action":"list"}`)); dec.Level != PermissionDeny {
		t.Fatalf("list under deny: got %s want deny", dec.Level)
	}
}
