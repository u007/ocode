package tts

import (
	"errors"
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
}

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

func (p *PlaybackManager) Status() Playback {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.active
}

func NormalizeText(text string) string {
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
