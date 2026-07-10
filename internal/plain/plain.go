// Package plain renders a run as v2-style streaming text: a per-step banner, the
// step's live output at full width, and a closing summary. It is the fallback
// sink for non-TTY, --plain, and NO_COLOR; the TUI sink lives in internal/tui.
package plain

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"

	"github.com/schmas/upall/internal/engine"
)

// ansiRE matches CSI (colors, cursor) and OSC (window title) escape sequences so
// plain/non-TTY output carries no ANSI, even though the pty capture contains it.
var ansiRE = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]|\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)")

// Sink implements engine.Sink for plain streaming output. It also owns the
// lifecycle banners (Begin/End) since those are not per-step events.
type Sink struct {
	steps  []engine.Step
	states []engine.State
	durs   []engine.Result
	out    io.Writer
	color  bool
	runDir string
	notify bool
	width  int

	// mu guards buf. Steps run serially, and runCmd joins its copy goroutine
	// before StepDone, so on the common path there is no contention. It only
	// matters on the rare abandon path (a wedged pty slave-holder that outlives
	// the drain grace): a late Output from the abandoned copy goroutine must not
	// race the next step's flush over the shared partial-line buffer.
	mu  sync.Mutex
	buf []byte // partial line carried between Output chunks
}

// New builds a plain sink over steps, writing to out. color enables ANSI
// passthrough; when false, ANSI is stripped from step output. notify enables the
// desktop notification on a failed run.
func New(steps []engine.Step, out io.Writer, color bool, runDir string, notify bool) *Sink {
	return &Sink{
		steps:  steps,
		states: make([]engine.State, len(steps)),
		durs:   make([]engine.Result, len(steps)),
		out:    out,
		color:  color,
		runDir: runDir,
		notify: notify,
		width:  72,
	}
}

func (s *Sink) c(code string) string {
	if s.color {
		return code
	}
	return ""
}

func (s *Sink) rule() {
	fmt.Fprintf(s.out, "%s%s%s\n", s.c(dim), strings.Repeat("─", s.width), s.c(reset))
}

// progress renders the compact "✓✓▶·····" strip of all steps' current states.
func (s *Sink) progress() string {
	var b strings.Builder
	for _, st := range s.states {
		b.WriteString(engine.Glyph(st))
	}
	return b.String()
}

// Begin prints the run header.
func (s *Sink) Begin(title string) {
	fmt.Fprintf(s.out, "%s▶ %s%s\n", s.c(bold), title, s.c(reset))
}

// Skip marks a step skipped and prints its one-line skipped banner.
func (s *Sink) Skip(i int, reason string) {
	s.states[i] = engine.StateSkipped
	fmt.Fprintf(s.out, " %s[%d/%d]%s %s %s%s%s %s(skipped)%s\n",
		s.c(dim), i+1, len(s.steps), s.c(reset), engine.Glyph(engine.StateSkipped),
		s.c(dim), s.steps[i].Label, s.c(reset), s.c(dim), s.c(reset))
}

// StepStart marks a step running and prints its full banner.
func (s *Sink) StepStart(i int) {
	s.states[i] = engine.StateRunning
	fmt.Fprintln(s.out)
	s.rule()
	fmt.Fprintf(s.out, " %s[%d/%d]%s %s %s%s%s   %s\n",
		s.c(bold), i+1, len(s.steps), s.c(reset), engine.Glyph(engine.StateRunning),
		s.c(bold), s.steps[i].Label, s.c(reset), s.progress())
	s.rule()
}

// Output buffers a raw chunk and prints every complete line it now holds. Line
// splitting lives here (the engine hands over raw pty bytes): each full line is
// stripped of ANSI when color is off, has a trailing carriage return removed
// (CRLF / bare-CR redraws), and is written with a newline. A trailing partial
// line is held until the next chunk or a StepDone flush.
func (s *Sink) Output(_ int, p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = append(s.buf, p...)
	for {
		idx := bytes.IndexByte(s.buf, '\n')
		if idx < 0 {
			break
		}
		s.emitLine(s.buf[:idx])
		s.buf = s.buf[idx+1:]
	}
}

// flushLine prints any buffered partial line (a step whose last line had no
// trailing newline) so no output is dropped at the step boundary.
func (s *Sink) flushLine() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.buf) > 0 {
		s.emitLine(s.buf)
		s.buf = s.buf[:0]
	}
}

func (s *Sink) emitLine(b []byte) {
	if !s.color {
		b = ansiRE.ReplaceAll(b, nil)
	}
	b = bytes.TrimRight(b, "\r")
	s.out.Write(b)
	s.out.Write([]byte{'\n'})
}

// StepDone records the outcome and prints the closing per-step line. Any
// buffered partial output line is flushed first so nothing is lost at the
// boundary.
func (s *Sink) StepDone(i int, res engine.Result) {
	s.flushLine()
	s.states[i] = res.State
	s.durs[i] = res
	label := s.steps[i].Label
	switch res.State {
	case engine.StateOK:
		fmt.Fprintf(s.out, " %s %s%s%s %s(%s)%s\n", engine.Glyph(engine.StateOK),
			s.c(green), label, s.c(reset), s.c(dim), engine.Hms(res.Dur), s.c(reset))
	case engine.StateAborted:
		fmt.Fprintf(s.out, " %s %s%s%s %s(%s, aborted)%s\n", engine.Glyph(engine.StateAborted),
			s.c(red), label, s.c(reset), s.c(dim), engine.Hms(res.Dur), s.c(reset))
	default: // failed
		detail := fmt.Sprintf("exit %d", res.RC)
		if res.Reason != "" {
			detail = res.Reason
		}
		fmt.Fprintf(s.out, " %s %s%s%s %s(%s, %s)%s\n", engine.Glyph(engine.StateFailed),
			s.c(red), label, s.c(reset), s.c(dim), engine.Hms(res.Dur), detail, s.c(reset))
	}
}
