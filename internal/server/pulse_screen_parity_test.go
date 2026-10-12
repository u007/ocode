package server

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// pulseScreenParityCase is one row of testdata/pulse_screen_parity.json. The same
// file drives the dashboard card's test (web/src/components/Pulse), so both
// emulators are held to the same expectations.
type pulseScreenParityCase struct {
	Name     string   `json:"name"`
	Cols     int      `json:"cols"`
	Rows     int      `json:"rows"`
	Raw      string   `json:"raw"`
	Expect   []string `json:"expect"`
	KnownGap string   `json:"known_gap"`
}

// TestPulseScreenParity: renderPulseScreen gives the expected visible screen for
// every fixture. A case with known_gap is an x/vt limit the tool cannot fix, and the
// card, which runs xterm, is checked against the same expectation instead.
func TestPulseScreenParity(t *testing.T) {
	raw, err := os.ReadFile("testdata/pulse_screen_parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []pulseScreenParityCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if c.KnownGap != "" {
			// Known x/vt gap, documented in known_gap; the web side still asserts it.
			continue
		}
		got, err := renderPulseScreen(c.Raw, c.Cols, c.Rows)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		if !reflect.DeepEqual(got, c.Expect) {
			t.Errorf("%s: renderPulseScreen = %q, want %q", c.Name, got, c.Expect)
		}
	}
}
