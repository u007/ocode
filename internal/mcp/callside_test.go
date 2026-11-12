package mcp

import (
	"fmt"
	"testing"
)

func TestBoundedCallMapEvictsOldestBeyondCap(t *testing.T) {
	var m boundedCallMap[int]
	total := maxCallSideEntries + 6
	for i := 0; i < total; i++ {
		m.put(fmt.Sprintf("c%d", i), i)
	}
	if len(m.items) != maxCallSideEntries || len(m.order) != maxCallSideEntries {
		t.Fatalf("items=%d order=%d, want both %d", len(m.items), len(m.order), maxCallSideEntries)
	}
	for i := 0; i < 6; i++ {
		if _, ok := m.take(fmt.Sprintf("c%d", i)); ok {
			t.Fatalf("c%d survived eviction", i)
		}
	}
	if v, ok := m.take("c6"); !ok || v != 6 {
		t.Fatalf("c6 = (%d, %v), want (6, true)", v, ok)
	}
}

func TestBoundedCallMapTakeRemovesEntryAndOrder(t *testing.T) {
	var m boundedCallMap[string]
	m.put("a", "1")
	m.put("b", "2")
	if v, ok := m.take("a"); !ok || v != "1" {
		t.Fatalf("take a = (%q, %v)", v, ok)
	}
	if _, ok := m.take("a"); ok {
		t.Fatal("a taken twice")
	}
	if len(m.order) != 1 || m.order[0] != "b" {
		t.Fatalf("order = %v, want [b]", m.order)
	}
}

func TestBoundedCallMapReplacingAnIDKeepsOneEntry(t *testing.T) {
	var m boundedCallMap[string]
	m.put("a", "1")
	m.put("a", "2")
	if len(m.items) != 1 || len(m.order) != 1 {
		t.Fatalf("items=%d order=%v, want one entry", len(m.items), m.order)
	}
	if v, _ := m.take("a"); v != "2" {
		t.Fatalf("a = %q, want the replacement", v)
	}
}
