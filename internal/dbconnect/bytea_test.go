package dbconnect

import "testing"

func TestByteaTextUsesPostgresHexForm(t *testing.T) {
	cases := []struct {
		in   []byte
		want string
	}{
		{[]byte{}, `\x`},
		{[]byte{0xde, 0xad, 0xbe, 0xef}, `\xdeadbeef`},
		{[]byte{0x00, 0xff}, `\x00ff`},
	}
	for _, c := range cases {
		if got := byteaText(c.in); got != c.want {
			t.Fatalf("byteaText(%x) = %q, want %q", c.in, got, c.want)
		}
	}
}
