package debuglog

import (
	"fmt"
	"os"
	"time"
)

// mirrorMaxBytes caps the mirror file size; when exceeded the file is rotated
// to <path>.1 (single generation) so the log stays bounded across sessions.
const mirrorMaxBytes = 2 << 20 // 2MB

// MirrorKindToFile makes Append also write entries of the given kind to an
// append-only file at path, one timestamped line per entry. The in-memory ring
// buffer holds only the last 500 entries, so rare-but-critical events (e.g.
// compaction decisions) need a durable sink to be diagnosable after the fact.
// Passing an empty path disables mirroring.
func (l *log) MirrorKindToFile(kind EntryKind, path string) {
	l.mu.Lock()
	l.mirrorKind = kind
	l.mirrorPath = path
	l.mu.Unlock()
}

// mirror appends e to the mirror file. Called from Append outside l.mu (file
// I/O must not block Snapshot/Append callers on the lock). Write failures are
// reported once to the in-memory log; ERROR entries are never mirrored, so
// this cannot recurse.
func (l *log) mirror(path string, e Entry) {
	if st, err := os.Stat(path); err == nil && st.Size() > mirrorMaxBytes {
		if err := os.Rename(path, path+".1"); err != nil {
			l.mirrorFailOnce.Do(func() {
				l.Append(Entry{Kind: KindError, Message: fmt.Sprintf("debuglog: rotate mirror %s: %v", path, err)})
			})
			return
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		l.mirrorFailOnce.Do(func() {
			l.Append(Entry{Kind: KindError, Message: fmt.Sprintf("debuglog: open mirror %s: %v", path, err)})
		})
		return
	}
	defer f.Close()
	line := fmt.Sprintf("%s [%s] %s\n", time.Now().Format(time.RFC3339), e.Kind, e.Message)
	if _, err := f.WriteString(line); err != nil {
		l.mirrorFailOnce.Do(func() {
			l.Append(Entry{Kind: KindError, Message: fmt.Sprintf("debuglog: write mirror %s: %v", path, err)})
		})
	}
}
