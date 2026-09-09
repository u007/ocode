package server

import (
	"testing"

	"github.com/u007/ocode/internal/browse/cdp"
)

func TestLoadBrowseOptionsUsesCanonicalHTRDefaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	options := LoadBrowseOptions(nil)
	if !options.HTREnabled {
		t.Fatal("HTR must remain enabled when config loading falls back to defaults")
	}
	if options.HTRPort != cdp.DefaultHTRPort {
		t.Fatalf("HTR port = %d, want %d", options.HTRPort, cdp.DefaultHTRPort)
	}
	if options.HTRNativeHostName != cdp.DefaultHTRNativeHostName {
		t.Fatalf("HTR host = %q, want %q", options.HTRNativeHostName, cdp.DefaultHTRNativeHostName)
	}
}
