package remote

import "bytes"

// MaxExecOutput caps the stdout/stderr a non-interactive remote exec may
// accumulate in memory. Remote command output is captured into a buffer with
// no natural bound, so a runaway remote (a login shell looping on a syntax
// error, `yes`, a chatty MOTD hook) would otherwise grow the local process
// without limit for as long as the ssh session lives. 4 MiB is far above any
// probe, sync payload, or state read this package issues.
const MaxExecOutput = 4 << 20

// LimitedBuffer is a bytes.Buffer that silently discards writes beyond Max
// bytes (never an error — the producing process must keep running to its
// exit, and io.Copy-style pumps abort on a write error). Truncated reports
// whether anything was dropped.
type LimitedBuffer struct {
	Max       int
	buf       bytes.Buffer
	Truncated bool
}

// Write implements io.Writer.
func (b *LimitedBuffer) Write(p []byte) (int, error) {
	room := b.Max - b.buf.Len()
	if room <= 0 {
		b.Truncated = true
		return len(p), nil
	}
	if len(p) > room {
		b.Truncated = true
		b.buf.Write(p[:room])
		return len(p), nil
	}
	return b.buf.Write(p)
}

// String returns the retained bytes.
func (b *LimitedBuffer) String() string { return b.buf.String() }
