package tui

import (
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/schmas/upall/internal/engine"
)

const flushInterval = 40 * time.Millisecond

// Sink implements engine.Sink for the TUI by forwarding runner events into the
// Bubble Tea program as messages. Raw output chunks are coalesced and flushed on
// a tick so a chatty step cannot flood the update loop with one message per read.
// The flush holds the lock across the send, giving natural backpressure (and
// preserving order) if the program's queue fills.
type Sink struct {
	p       *tea.Program
	mu      sync.Mutex
	pending map[int][]byte
	stop    chan struct{}
}

// NewSink builds a sink bound to a running program.
func NewSink(p *tea.Program) *Sink {
	return &Sink{p: p, pending: map[int][]byte{}, stop: make(chan struct{})}
}

// Start begins the periodic flush loop; Stop ends it (flushing once more).
func (s *Sink) Start() { go s.loop() }
func (s *Sink) Stop()  { close(s.stop) }

func (s *Sink) loop() {
	t := time.NewTicker(flushInterval)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			s.flush()
			return
		case <-t.C:
			s.flush()
		}
	}
}

func (s *Sink) flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pending) == 0 {
		return
	}
	batch := s.pending
	s.pending = map[int][]byte{}
	s.p.Send(bytesMsg(batch))
}

// Output buffers a raw output chunk for the next flush. The engine reuses its
// read buffer between calls, so the bytes are copied (via append) on retain.
func (s *Sink) Output(i int, p []byte) {
	s.mu.Lock()
	s.pending[i] = append(s.pending[i], p...)
	s.mu.Unlock()
}

// StepStart, StepDone and Skip flush any buffered output first so a step's output
// is always delivered before its terminal event.
func (s *Sink) StepStart(i int) {
	s.flush()
	s.p.Send(startMsg{i})
}

func (s *Sink) StepDone(i int, res engine.Result) {
	s.flush()
	s.p.Send(doneMsg{i: i, res: res})
}

func (s *Sink) Skip(i int, reason string) {
	s.flush()
	s.p.Send(skipMsg{i: i, reason: reason})
}
