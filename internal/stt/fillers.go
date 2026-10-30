package stt

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Filler-word cleanup runs on every finished transcript, after the engine has
// returned its text. Dictation is spoken, so a raw decode carries the
// hesitation sounds a speaker makes ("um", "uh", "hmm", ...) into the chat
// composer, where they read as noise rather than meaning.
//
// The rewrite is deterministic and deliberately conservative. It only removes
// whole-word hesitation sounds from a fixed list, and it never touches
// discourse markers whose removal could change the meaning of a sentence
// ("like", "you know", "I mean") or the affirmations "uh-huh"/"mm-hmm". It
// keeps the surrounding punctuation sensible: a comma pair that bracketed the
// filler is dropped ("we should, um, go" -> "we should go") and a filler at the
// start of a sentence leaves a clean capital ("Um, hello" -> "Hello").
//
// Nothing here calls a model. A second network round-trip per utterance would
// cost more than the cleanup is worth, and an LLM rewrite could silently
// change the user's words.

// fillerWords are the hesitation sounds removed from a transcript. Entries
// are matched case-insensitively against whole words, so "umbrella" and
// "humming" are untouched.
//
// The list is intentionally short. Only sounds that are hesitation in every
// context are included. "Ah" and "eh" are not, because they carry meaning
// ("ah, I see", "eh?"), and neither are "like", "you know", "I mean" or "oh".
var fillerWords = map[string]struct{}{
	"erm":  {},
	"hm":   {},
	"hmm":  {},
	"hmmm": {},
	"mm":   {},
	"mmm":  {},
	"mmmm": {},
	"uh":   {},
	"uhh":  {},
	"uhhh": {},
	"uhhm": {},
	"uhm":  {},
	"um":   {},
	"umh":  {},
	"umm":  {},
	"ummm": {},
}

// protectedHesitations are word pairs that must survive cleanup. "uh huh" and
// "mm hmm" are how a speaker says yes; removing either half would turn an
// affirmation into a nonsense word.
var protectedHesitations = map[string]string{
	"uh": "huh",
	"mm": "hmm",
}

// transcriptWordRE splits a transcript into words. Apostrophes and hyphens stay
// inside a word so "uh-huh" and "mm-hmm" are single tokens, never a filler plus
// a leftover.
var transcriptWordRE = regexp.MustCompile(`[\p{L}\p{N}'’\-]+`)

var (
	transcriptRunsOfSpaces = regexp.MustCompile(`[ \t]{2,}`)
	transcriptSpaceBefore  = regexp.MustCompile(`[ \t]+([,.;:!?])`)
)

// RemoveFillerWords strips hesitation sounds from a finished transcript.
//
// The text is returned unchanged when it contains no fillers, so a caller can
// apply this unconditionally without paying a formatting cost. A transcript
// that was nothing but hesitation sounds becomes "", which the callers already
// render as "nothing was heard".
func RemoveFillerWords(text string) string {
	index := transcriptWordRE.FindAllStringIndex(text, -1)
	if len(index) == 0 {
		return strings.TrimSpace(text)
	}
	words := make([]string, len(index))
	for i, loc := range index {
		words[i] = text[loc[0]:loc[1]]
	}
	hasFiller := false
	for i := range words {
		if isFillerWordAt(words, i) {
			hasFiller = true
			break
		}
	}
	if !hasFiller {
		// Nothing to remove: leave the engine's text byte-for-byte alone,
		// including its own spacing and punctuation choices.
		return text
	}

	var out strings.Builder
	kept := 0
	removed := false
	// capitalizeNext is set when a removed filler opened a sentence, so the
	// word that replaces it still starts with a capital.
	capitalizeNext := false
	// carry is the separator that ran into a removed word. It is merged with
	// the separator after the word so a comma pair cannot leave ", ," behind.
	carry := ""
	hasCarry := false

	for i, loc := range index {
		sep := text[:loc[0]]
		if i > 0 {
			sep = text[index[i-1][1]:loc[0]]
		}
		if isFillerWordAt(words, i) {
			if i == 0 || endsSentence(sep) {
				capitalizeNext = true
			}
			removed = true
			if hasCarry {
				carry = mergeFillerSeparators(carry, sep)
			} else {
				carry, hasCarry = sep, true
			}
			continue
		}
		if hasCarry {
			sep = mergeFillerSeparators(carry, sep)
			hasCarry = false
			carry = ""
		}
		word := words[i]
		if capitalizeNext {
			word = capitalizeWord(word)
			capitalizeNext = false
		}
		out.WriteString(sep)
		out.WriteString(word)
		kept++
	}

	// A transcript of pure hesitation is "nothing was heard", not the leftover
	// punctuation that followed the last filler ("Um." must not become ".").
	if kept == 0 {
		if removed {
			return ""
		}
		return strings.TrimSpace(text)
	}
	// Keep whatever trailed the last word (usually its full stop).
	out.WriteString(text[index[len(index)-1][1]:])
	return tidyTranscript(out.String())
}

// isFillerWordAt reports whether the word at i is a removable hesitation
// sound. A word that is half of a protected affirmation is kept.
func isFillerWordAt(words []string, i int) bool {
	word := strings.ToLower(words[i])
	if _, ok := fillerWords[word]; !ok {
		return false
	}
	if i+1 < len(words) && protectedHesitations[word] == strings.ToLower(words[i+1]) {
		return false
	}
	for first, second := range protectedHesitations {
		if i > 0 && strings.ToLower(words[i-1]) == first && word == second {
			return false
		}
	}
	return true
}

// mergeFillerSeparators joins the text before a removed filler with the text
// after it. The comma handling is what keeps the sentence readable:
//
//	"we should, um, go" -> "we should go"  (the comma pair bracketed "um")
//	"um, hello"         -> " hello"        (the comma belonged to "um")
//	"um hello"          -> " hello"        (plain space collapses later)
func mergeFillerSeparators(before, after string) string {
	bPunct := strings.TrimRight(before, " \t")
	bTail := before[len(bPunct):]
	aPunct := strings.TrimLeft(after, " \t")

	bComma := strings.HasSuffix(bPunct, ",")
	aComma := strings.HasPrefix(aPunct, ",")
	switch {
	case bComma && aComma:
		return joinFragments(strings.TrimSuffix(bPunct, ",")+bTail, strings.TrimPrefix(aPunct, ","))
	case aComma:
		return joinFragments(bPunct+bTail, strings.TrimPrefix(aPunct, ","))
	case bComma:
		return joinFragments(before, aPunct)
	default:
		return joinFragments(before, after)
	}
}

// joinFragments re-joins the two halves of a separator without inventing a
// space when the left half already ends in whitespace (a newline, say) and
// without keeping the redundant space of the right half.
func joinFragments(left, right string) string {
	right = strings.TrimLeft(right, " \t")
	if right == "" {
		return left
	}
	if left == "" {
		return " " + right
	}
	switch left[len(left)-1] {
	case ' ', '\t', '\n', '\r':
		return left + right
	}
	return left + " " + right
}

// tidyTranscript collapses the double spaces the merge can produce and drops a
// space that would sit before punctuation ("hello ." -> "hello."). It never
// reflows newlines: the transcript is prose, not a document.
func tidyTranscript(s string) string {
	s = transcriptRunsOfSpaces.ReplaceAllString(s, " ")
	s = transcriptSpaceBefore.ReplaceAllString(s, "$1")
	return strings.TrimSpace(s)
}

// endsSentence reports whether sep closes a sentence. A filler removed after
// a full stop must not leave the next word lowercase ("Hello. um, we go" ->
// "Hello. We go").
func endsSentence(sep string) bool {
	s := strings.TrimRight(sep, " \t\r\n")
	if s == "" {
		return false
	}
	switch s[len(s)-1] {
	case '.', '!', '?':
		return true
	}
	return false
}

// capitalizeWord upper-cases the first letter of one word, leaving an already
// capital (or non-letter) first rune alone.
func capitalizeWord(word string) string {
	for i, r := range word {
		if !unicode.IsLetter(r) {
			continue
		}
		if !unicode.IsLower(r) {
			return word
		}
		return word[:i] + string(unicode.ToUpper(r)) + word[i+utf8.RuneLen(r):]
	}
	return word
}
