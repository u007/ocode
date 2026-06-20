package debuglog

import "testing"

func TestKindDiscoveryExists(t *testing.T) {
	Log.Clear()
	Log.Append(Entry{Kind: KindDiscovery, Message: "rank: 3/12 attached"})
	snap := Log.Snapshot()
	found := false
	for _, e := range snap {
		if e.Kind == KindDiscovery && e.Message == "rank: 3/12 attached" {
			found = true
		}
	}
	if !found {
		t.Fatal("KindDiscovery entry must round-trip through the log")
	}
	if KindDiscovery != "DISCOVERY" {
		t.Fatalf("KindDiscovery value = %q", KindDiscovery)
	}
}
