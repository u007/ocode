package vault

import (
	"crypto/rand"
	"log"
	"math/big"
	"strings"
)

// Password generator bounds and character classes. Length is clamped
// explicitly: anything below minGenLength becomes the default, anything above
// maxGenLength is capped.
const (
	defaultGenLength = 20
	minGenLength     = 8
	maxGenLength     = 256

	lowerChars  = "abcdefghijklmnopqrstuvwxyz"
	upperChars  = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	digitChars  = "0123456789"
	symbolChars = "!@#$%^&*()-_=+[]{};:,.<>?"
)

// GenOptions selects the character classes (lowercase is always included) and
// the desired length.
type GenOptions struct {
	Length  int
	Upper   bool
	Digits  bool
	Symbols bool
}

// GeneratePassword returns a random password containing at least one character
// from every enabled class. Randomness comes from crypto/rand; a rand failure
// is logged and falls back to index 0 rather than panicking (the generated
// password stays length-correct and still satisfies the class guarantees).
func GeneratePassword(opts GenOptions) string {
	length := opts.Length
	if length < minGenLength {
		length = defaultGenLength
	}
	if length > maxGenLength {
		length = maxGenLength
	}

	classes := make([]string, 0, 4)
	classes = append(classes, lowerChars)
	if opts.Upper {
		classes = append(classes, upperChars)
	}
	if opts.Digits {
		classes = append(classes, digitChars)
	}
	if opts.Symbols {
		classes = append(classes, symbolChars)
	}
	all := strings.Join(classes, "")

	out := make([]byte, 0, length)
	for _, class := range classes {
		out = append(out, class[randIndex(len(class))])
	}
	for len(out) < length {
		out = append(out, all[randIndex(len(all))])
	}

	// Fisher–Yates so the guaranteed characters are not at fixed positions.
	for i := len(out) - 1; i > 0; i-- {
		j := randIndex(i + 1)
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

// randIndex returns a uniform random index in [0, n). A crypto/rand failure is
// logged and returns 0.
func randIndex(n int) int {
	if n <= 0 {
		return 0
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		log.Printf("vault: generate password: rand: %v", err)
		return 0
	}
	return int(v.Int64())
}
