package tool

import (
	"strings"
	"testing"
	"unicode/utf8"
)

const fetchTruncatedMarker = "\n\n... [content truncated]"

func TestTruncateFetched(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "ascii under limit unchanged",
			in:   "hello world",
			want: "hello world",
		},
		{
			name: "ascii at limit unchanged",
			in:   strings.Repeat("a", maxFetchedBytes),
			want: strings.Repeat("a", maxFetchedBytes),
		},
		{
			name: "ascii over limit cut at exactly the limit",
			in:   strings.Repeat("a", maxFetchedBytes+1),
			want: strings.Repeat("a", maxFetchedBytes) + fetchTruncatedMarker,
		},
		{
			name: "three-byte runes cut back to a rune boundary",
			// 50000 is not a multiple of 3, so the cut walks back to byte 49998.
			in:   strings.Repeat("中", 20000),
			want: strings.Repeat("中", 16666) + fetchTruncatedMarker,
		},
		{
			name: "two-byte runes cut on an exact boundary",
			in:   strings.Repeat("é", 30000),
			want: strings.Repeat("é", 25000) + fetchTruncatedMarker,
		},
		{
			name: "invalid byte under limit is dropped",
			in:   "ok\xffdone",
			want: "okdone",
		},
		{
			name: "invalid byte over limit is dropped before cutting",
			in:   strings.Repeat("a", 49999) + "\xffbbb",
			want: strings.Repeat("a", 49999) + "b" + fetchTruncatedMarker,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateFetched(tt.in)
			if got != tt.want {
				t.Fatalf("truncateFetched() len=%d, want len=%d", len(got), len(tt.want))
			}
		})
	}
}

func TestTruncateFetchedOutputIsAlwaysValidUTF8(t *testing.T) {
	inputs := map[string]string{
		"three-byte runes": strings.Repeat("中文", 15000),
		"invalid bytes":    strings.Repeat("a\xfe", 40000),
		"mixed invalid":    strings.Repeat("é\xc3", 20000),
		"short valid":      "naïve",
	}

	for name, in := range inputs {
		t.Run(name, func(t *testing.T) {
			out := truncateFetched(in)
			if !utf8.ValidString(out) {
				t.Fatalf("truncateFetched() output is not valid UTF-8")
			}
			if len(out) > maxFetchedBytes+len(fetchTruncatedMarker) {
				t.Fatalf("truncateFetched() len=%d exceeds %d", len(out), maxFetchedBytes+len(fetchTruncatedMarker))
			}
		})
	}
}
