package server

import (
	"testing"

	"github.com/u007/ocode/internal/config"
)

func TestLocalHTRPortDisabledExposesNothing(t *testing.T) {
	if port, ok := localHTRPort(config.BrowserConfig{HTREnabled: false, HTRPort: 3845}); ok || port != 0 {
		t.Fatalf("disabled HTR must not be exposed, got port=%d ok=%v", port, ok)
	}
}
