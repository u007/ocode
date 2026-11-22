package auth

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

// A malformed wrapper, one whose values MIX provider objects with non-objects, is
// a parse error. Reading it as flat would make one bogus provider named
// "credentials" and a later Set would drop the object providers, so writers must
// refuse and leave the file exactly as it is.
func TestMixedLegacyWrapperIsRefusedAndUntouched(t *testing.T) {
	resetCrossProcessStores(t)
	wrapper := []byte(`{"credentials":{"openai":{"type":"api","key":"sk-kept"},"x":null}}`)
	if err := writeFileAtomic(mustAuthPath(t), wrapper); err != nil {
		t.Fatalf("write malformed wrapper: %v", err)
	}

	if _, ok := Get("openai"); ok {
		t.Error("a malformed wrapper was read as a store")
	}
	if err := Set("gamma", apiKeyCred("sk-gamma")); err == nil {
		t.Fatal("Set over a malformed wrapper succeeded; writers must refuse")
	}
	if got := rawAuthBytes(t); !bytes.Equal(got, wrapper) {
		t.Error("a refused write changed auth.json")
	}

	_, err := parseAuthData(mustAuthPath(t), wrapper)
	if err == nil {
		t.Fatal("parseAuthData accepted a mixed wrapper")
	}
	if strings.Contains(err.Error(), "sk-kept") {
		t.Error("the parse error quotes a credential")
	}
}

// An empty wrapper is an empty store, not a provider named "credentials".
func TestEmptyLegacyWrapperIsEmptyStore(t *testing.T) {
	creds, err := parseAuthData("auth.json", []byte(`{"credentials":{}}`))
	if err != nil {
		t.Fatalf("parseAuthData(empty wrapper): %v", err)
	}
	if len(creds) != 0 {
		t.Fatalf("empty wrapper parsed to %d providers, want 0", len(creds))
	}

	resetCrossProcessStores(t)
	if err := writeFileAtomic(mustAuthPath(t), []byte(`{"credentials":{}}`)); err != nil {
		t.Fatalf("write empty wrapper: %v", err)
	}
	if got := List(); len(got) != 0 {
		t.Fatalf("empty wrapper loaded %d providers, want 0", len(got))
	}
	if _, ok := Get("credentials"); ok {
		t.Error("empty wrapper produced a provider named credentials")
	}
	mustSet(t, "openai", "sk-new")
	if keys := rawTopLevelKeys(t); !slices.Equal(keys, []string{"openai"}) {
		t.Errorf("after Set the top-level keys are %v, want [openai]", keys)
	}
}
