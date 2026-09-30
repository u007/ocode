package tts

import (
	"errors"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"
)

var ErrEmptyText = errors.New("speech text is empty")

type Playback struct {
	Generation          uint64   `json:"generation"`
	SelectionGeneration uint64   `json:"selection_generation,omitempty"`
	Engine              EngineID `json:"engine"`
	Status              string   `json:"status"`
	Text                string   `json:"text,omitempty"`
	Error               string   `json:"error,omitempty"`
	// AudioID names the server-rendered audio for local engines; the client
	// fetches /api/tts/audio/{AudioID} once Status is "ready".
	AudioID string `json:"audio_id,omitempty"`
}

// Playback statuses. Browser Native never leaves "playing"/"stopped" on the
// server; local engines move playing -> synthesizing -> ready | error.
const (
	PlaybackStatusPlaying      = "playing"
	PlaybackStatusSynthesizing = "synthesizing"
	PlaybackStatusReady        = "ready"
	PlaybackStatusError        = "error"
	PlaybackStatusStopped      = "stopped"
)

type PlaybackManager struct {
	mu         sync.Mutex
	generation atomic.Uint64
	active     Playback
}

func (p *PlaybackManager) Replace(engine EngineID, text string) (Playback, error) {
	return p.ReplaceForSelection(engine, 0, text)
}

func (p *PlaybackManager) ReplaceForSelection(engine EngineID, selectionGeneration uint64, text string) (Playback, error) {
	text = NormalizeText(text)
	if text == "" {
		return Playback{}, ErrEmptyText
	}
	p.mu.Lock()
	generation := p.generation.Add(1)
	next := Playback{
		Generation:          generation,
		SelectionGeneration: selectionGeneration,
		Engine:              engine,
		Status:              "playing",
		Text:                text,
	}
	p.active = next
	p.mu.Unlock()
	return next, nil
}

func (p *PlaybackManager) Stop() Playback {
	return p.StopForSelection(0)
}

func (p *PlaybackManager) StopForSelection(selectionGeneration uint64) Playback {
	p.mu.Lock()
	generation := p.generation.Add(1)
	p.active = Playback{
		Generation:          generation,
		SelectionGeneration: selectionGeneration,
		Status:              "stopped",
	}
	active := p.active
	p.mu.Unlock()
	return active
}

// Transition updates the active playback only if it still has the given
// generation, so a finished synthesis job for a replaced or stopped request
// can never overwrite the newer state. It reports whether the update applied.
func (p *PlaybackManager) Transition(generation uint64, fn func(*Playback)) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.active.Generation != generation {
		return false
	}
	fn(&p.active)
	return true
}

func (p *PlaybackManager) Status() Playback {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.active
}

// stripMarkdown removes markdown emphasis markers, code fences, heading
// hashes, and link syntax from text before speech synthesis. It is a
// best-effort cleanup for the local TTS engines (kokoro/piper); the
// Browser Native engine receives already-clean text from the frontend.
// The server must not trust the client — a raw `assistant.content` fallback
// or a future caller could otherwise send "**bold**" verbatim.
//
// Compiled once at package init: stripMarkdown runs on every playback chunk,
// so recompiling these patterns per call was pure overhead.
var (
	mdFenceLang  = regexp.MustCompile("(?s)```[\\w-]*\\n(.*?)```")
	mdFence      = regexp.MustCompile("(?s)```(.*?)```")
	mdInlineCode = regexp.MustCompile("`([^`]+)`")
	mdRule       = regexp.MustCompile(`(?m)^[ \t]*(-{3,}|\*{3,}|_{3,})[ \t]*$`)
	mdBoldStar   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	mdBoldUnder  = regexp.MustCompile(`__([^_]+)__`)
	// Italic needs a non-word (or string edge) on both sides, so an unspaced
	// product like 2*3*4 is preserved while *emphasis* is stripped.
	mdItalicStar = regexp.MustCompile(`(^|[^*\w])\*(\S(?:[^*]*\S)?)\*([^*\w]|$)`)
	mdStrike     = regexp.MustCompile(`~~([^~]+)~~`)
	mdImage      = regexp.MustCompile(`!\[([^\]]*)\]\([^)]+\)`)
	mdLink       = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	mdHeading    = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	mdQuote      = regexp.MustCompile(`(?m)^>\s?`)
	mdListBullet = regexp.MustCompile(`(?m)^[ \t]*[-*+][ \t]+`)
	mdListNumber = regexp.MustCompile(`(?m)^[ \t]*\d+\.[ \t]+`)
	mdHTMLTag    = regexp.MustCompile(`</?(?:a|b|i|u|s|p|br|hr|em|strong|code|pre|span|div|img|sub|sup|kbd|mark|del|ins|details|summary|ul|ol|li|table|thead|tbody|tr|td|th|h[1-6]|blockquote)\b[^<>]*>`)
)

func stripMarkdown(text string) string {
	// Fenced code blocks: ```lang\ncode``` → code
	text = mdFenceLang.ReplaceAllString(text, "$1")
	text = mdFence.ReplaceAllString(text, "$1")
	// Inline code: `code` → code
	text = mdInlineCode.ReplaceAllString(text, "$1")
	// Horizontal rules: ---, ***, ___. Must run BEFORE italic so `***` is
	// consumed as a rule, not partially matched as italic (which would leave
	// a stray `*`). Use [ \t]* not \s* so the trailing newline is preserved.
	text = mdRule.ReplaceAllString(text, "")
	// Bold: **text** or __text__ → text
	text = mdBoldStar.ReplaceAllString(text, "$1")
	text = mdBoldUnder.ReplaceAllString(text, "$1")
	// Italic: *text* → text (non-space content, non-word flanks). Single
	// underscore italic is skipped: RE2 has no lookbehind, and
	// `snake_case_name` is far more common in code than _italic_.
	text = mdItalicStar.ReplaceAllString(text, "${1}${2}${3}")
	text = mdItalicStar.ReplaceAllString(text, "${1}${2}${3}")
	// Strikethrough: ~~text~~ → text
	text = mdStrike.ReplaceAllString(text, "$1")
	// Images: ![alt](url) → alt. Must run BEFORE links so the `!` prefix
	// is consumed as part of the image syntax, not left dangling.
	text = mdImage.ReplaceAllString(text, "$1")
	// Links: [text](url) → text
	text = mdLink.ReplaceAllString(text, "$1")
	// ATX headings: # text → text
	text = mdHeading.ReplaceAllString(text, "")
	// Blockquotes: > text → text
	text = mdQuote.ReplaceAllString(text, "")
	// List markers at line starts: "- ", "* ", "+ ", "1. ". Use [ \t]+ not
	// \s+ so a lone "*" left by italic stripping isn't matched as a list item
	// (the newline would otherwise be consumed as the required trailing whitespace).
	text = mdListBullet.ReplaceAllString(text, "")
	text = mdListNumber.ReplaceAllString(text, "")
	// HTML tags: only known element names, so prose comparisons
	// ("a < b and c > d") and generics ("Vec<T>") keep their words.
	text = mdHTMLTag.ReplaceAllString(text, "")
	return text
}

func NormalizeText(text string) string {
	text = stripMarkdown(text)
	var b strings.Builder
	for _, r := range text {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func ChunkText(text string, maxRunes int) []string {
	text = NormalizeText(text)
	if text == "" || maxRunes <= 0 {
		return nil
	}
	var chunks []string
	for len([]rune(text)) > maxRunes {
		runes := []rune(text)
		cut := maxRunes
		for i := maxRunes; i > maxRunes/2; i-- {
			if unicode.IsSpace(runes[i-1]) {
				cut = i
				break
			}
		}
		chunks = append(chunks, strings.TrimSpace(string(runes[:cut])))
		text = strings.TrimSpace(string(runes[cut:]))
	}
	if text != "" {
		chunks = append(chunks, text)
	}
	return chunks
}
