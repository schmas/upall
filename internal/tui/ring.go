package tui

import "bytes"

// ring is a fixed-capacity buffer of the last N output lines for one step. It
// bounds TUI memory no matter how much a step prints; the complete, untruncated
// output still lives in the step's logfile on disk.
type ring struct {
	lines [][]byte
	cap   int
	start int
	size  int
}

func newRing(capacity int) *ring {
	if capacity < 1 {
		capacity = 1
	}
	return &ring{lines: make([][]byte, capacity), cap: capacity}
}

// append adds a line, evicting the oldest when full.
func (r *ring) append(b []byte) {
	if r.size < r.cap {
		r.lines[(r.start+r.size)%r.cap] = b
		r.size++
		return
	}
	r.lines[r.start] = b
	r.start = (r.start + 1) % r.cap
}

func (r *ring) reset() {
	r.start = 0
	r.size = 0
}

// bytes renders the buffered lines joined by newlines.
func (r *ring) bytes() []byte {
	var b bytes.Buffer
	for i := 0; i < r.size; i++ {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.Write(r.lines[(r.start+i)%r.cap])
	}
	return b.Bytes()
}

func (r *ring) String() string { return string(r.bytes()) }
