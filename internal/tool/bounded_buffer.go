package tool

// boundedBuffer accumulates output up to a fixed cap, then discards the rest
// while still reporting every byte as consumed.
//
// It exists because the bash pump (see ExecuteStreamCtx) copies a child's
// stdout/stderr into an in-memory sink for the lifetime of the command, and
// that lifetime is bounded only by a wall-clock timeout of up to 600s. An
// uncapped sink lets a single runaway or high-volume command — a verbose
// build, an accidental infinite echo loop, `find / | xargs cat` — grow the
// heap without limit for ten minutes. The post-hoc cap in truncateOutput runs
// only after the command exits, so it never bounds the peak.
//
// Retention is head-first, not a tail ring: truncateOutput surfaces the FIRST
// 30000 chars of a result, so keeping the head means the user sees the genuine
// start of the output. A tail ring would surface bytes from deep inside a
// runaway stream while still labelling them as the command's output. The
// per-Process ring in process.go keeps the tail on purpose and serves a
// different consumer (bash_output / the timeout Dump path).
type boundedBuffer struct {
	buf     []byte
	cap     int
	dropped int
}

// newBoundedBuffer returns a buffer that retains at most capBytes.
func newBoundedBuffer(capBytes int) *boundedBuffer {
	return &boundedBuffer{cap: capBytes}
}

// Write retains what still fits under the cap and counts the remainder as
// dropped.
//
// It ALWAYS reports len(p) consumed with a nil error. io.Copy treats a short
// write as io.ErrShortWrite and aborts the copy, so reporting only the retained
// count would tear the pump down the moment the cap was hit — stopping live
// streaming and the process output ring along with it. Discarding bytes is the
// intended behaviour here; failing the copy is not.
func (b *boundedBuffer) Write(p []byte) (int, error) {
	room := b.cap - len(b.buf)
	if room > 0 {
		if len(p) <= room {
			b.buf = append(b.buf, p...)
			return len(p), nil
		}
		b.buf = append(b.buf, p[:room]...)
		b.dropped += len(p) - room
		return len(p), nil
	}
	b.dropped += len(p)
	return len(p), nil
}

// String returns the retained prefix.
func (b *boundedBuffer) String() string { return string(b.buf) }

// Len returns the number of retained bytes.
func (b *boundedBuffer) Len() int { return len(b.buf) }

// Dropped returns how many bytes were discarded past the cap. Non-zero means
// the command outran the runaway guard and the result is incomplete.
func (b *boundedBuffer) Dropped() int { return b.dropped }
