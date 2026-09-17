package remote

import "testing"

func TestLimitedBufferDropsBeyondMax(t *testing.T) {
	b := &LimitedBuffer{Max: 5}
	for _, chunk := range []string{"abc", "def", "ghi"} {
		n, err := b.Write([]byte(chunk))
		if err != nil || n != len(chunk) {
			t.Fatalf("Write(%q) = %d, %v; want full length and nil error", chunk, n, err)
		}
	}
	if got := b.String(); got != "abcde" {
		t.Fatalf("retained %q, want %q", got, "abcde")
	}
	if !b.Truncated {
		t.Fatal("expected Truncated after overflow")
	}
}

func TestLimitedBufferKeepsWithinMax(t *testing.T) {
	b := &LimitedBuffer{Max: 10}
	if _, err := b.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if b.Truncated || b.String() != "hello" {
		t.Fatalf("unexpected truncation: %+v", b)
	}
}
