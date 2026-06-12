package redact

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strconv"
)

// TokenPattern matches OCSEC tokens: [[OCSEC:<6-hex-nonce>:<index>]]
var TokenPattern = regexp.MustCompile(`\[\[OCSEC:[0-9a-f]{6}:\d+\]\]`)

// NewNonce generates a 6-character lowercase hex nonce using crypto/rand.
func NewNonce() string {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		panic("redact: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// FormatToken creates an OCSEC token with the given nonce and index.
func FormatToken(nonce string, idx int) string {
	return "[[OCSEC:" + nonce + ":" + strconv.Itoa(idx) + "]]"
}

// TokensForNonce extracts all tokens from text that match the given nonce.
// It returns a slice of token strings and their corresponding indexes.
func TokensForNonce(text, nonce string) ([]string, []int) {
	var tokens []string
	var indexes []int
	// Build a regex that matches tokens with the specific nonce
	pattern := regexp.MustCompile(`\[\[OCSEC:` + regexp.QuoteMeta(nonce) + `:(\d+)\]\]`)
	matches := pattern.FindAllStringSubmatch(text, -1)
	for _, m := range matches {
		if len(m) == 2 {
			tokens = append(tokens, m[0])
			// Parse index from capture group
			var idx int
			for _, c := range m[1] {
				idx = idx*10 + int(c-'0')
			}
			indexes = append(indexes, idx)
		}
	}
	return tokens, indexes
}