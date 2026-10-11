package dbconnect

import "testing"

func TestJSONSafeIntBoundaries(t *testing.T) {
	cases := []struct {
		name string
		in   int64
		want any
	}{
		{"zero", 0, int64(0)},
		{"small negative", -42, int64(-42)},
		{"max safe", maxSafeJSONInt, int64(maxSafeJSONInt)},
		{"min safe", -maxSafeJSONInt, int64(-maxSafeJSONInt)},
		{"first unsafe positive", maxSafeJSONInt + 1, "9007199254740992"},
		{"first unsafe negative", -maxSafeJSONInt - 1, "-9007199254740992"},
		{"snowflake-style id", 1234567890123456789, "1234567890123456789"},
		{"int64 max", 1<<63 - 1, "9223372036854775807"},
		{"int64 min", -1 << 63, "-9223372036854775808"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := jsonSafeInt(c.in); got != c.want {
				t.Fatalf("jsonSafeInt(%d) = %#v, want %#v", c.in, got, c.want)
			}
		})
	}
}
