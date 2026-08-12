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

// TestUserFacingFlagRoundTrips ensures the UserFacing boolean survives a
// snapshot — the TUI's chat-transcript promoter depends on seeing the
// original flag, not a zeroed copy.
func TestUserFacingFlagRoundTrips(t *testing.T) {
	Log.Clear()
	Log.Append(Entry{Kind: KindDiscovery, Message: "downloading llama-server …", UserFacing: true})
	Log.Append(Entry{Kind: KindDiscovery, Message: "internal warm log", UserFacing: false})
	snap := Log.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(snap))
	}
	if !snap[0].UserFacing {
		t.Fatal("first entry should be user-facing")
	}
	if snap[1].UserFacing {
		t.Fatal("second entry should NOT be user-facing")
	}
}

// TestSnapshotSinceCursor verifies the cursor-based diff across normal
// appends, a clear (no re-emits, restart at 0), and ring-buffer drops
// (count-based diffing stalls at cap; the cursor must not).
func TestSnapshotSinceCursor(t *testing.T) {
	l := newLog()
	l.Append(Entry{Kind: KindAgent, Message: "a"})
	cursor := l.Cursor()
	l.Append(Entry{Kind: KindAgent, Message: "b"})
	l.Append(Entry{Kind: KindAgent, Message: "c"})

	fresh, cursor := l.SnapshotSince(cursor)
	if len(fresh) != 2 || fresh[0].Message != "b" || fresh[1].Message != "c" {
		t.Fatalf("expected [b c], got %v", fresh)
	}

	// Idle call returns nothing new.
	fresh, cursor = l.SnapshotSince(cursor)
	if len(fresh) != 0 {
		t.Fatalf("expected no fresh entries, got %v", fresh)
	}

	// Clear then burst: only post-clear entries, no duplicates.
	l.Clear()
	l.Append(Entry{Kind: KindAgent, Message: "d"})
	fresh, cursor = l.SnapshotSince(cursor)
	if len(fresh) != 1 || fresh[0].Message != "d" {
		t.Fatalf("expected [d] after clear, got %v", fresh)
	}

	// Fill past cap so the ring drops oldest entries; new appends must still
	// surface even though len(entries) no longer grows.
	for i := 0; i < cap+10; i++ {
		l.Append(Entry{Kind: KindAgent, Message: "fill"})
	}
	l.Append(Entry{Kind: KindAgent, Message: "tail"})
	fresh, _ = l.SnapshotSince(l.Cursor() - 1)
	if len(fresh) != 1 || fresh[0].Message != "tail" {
		t.Fatalf("expected [tail] at ring steady state, got %d entries", len(fresh))
	}
}
