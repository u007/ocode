package server

import "testing"

func TestParseDBTablePage(t *testing.T) {
	cases := []struct {
		name       string
		limit      string
		offset     string
		wantLimit  int
		wantOffset int
		wantOK     bool
	}{
		{"defaults", "", "", dbTablesDefaultLimit, 0, true},
		{"explicit", "25", "50", 25, 50, true},
		{"max limit", "500", "", 500, 0, true},
		{"limit too large", "501", "", 0, 0, false},
		{"limit zero", "0", "", 0, 0, false},
		{"limit negative", "-1", "", 0, 0, false},
		{"limit not a number", "ten", "", 0, 0, false},
		{"offset negative", "", "-5", 0, 0, false},
		{"offset not a number", "", "x", 0, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			limit, offset, ok := parseDBTablePage(c.limit, c.offset)
			if ok != c.wantOK || limit != c.wantLimit || offset != c.wantOffset {
				t.Fatalf("parseDBTablePage(%q,%q) = (%d,%d,%v), want (%d,%d,%v)",
					c.limit, c.offset, limit, offset, ok, c.wantLimit, c.wantOffset, c.wantOK)
			}
		})
	}
}
