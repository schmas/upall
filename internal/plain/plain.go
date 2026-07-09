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
	width  int
}

// New builds a plain sink over steps, writing to out. color enables ANSI
// passthrough; when false, ANSI is stripped from step output.
func New(steps []engine.Step, out io.Writer, color bool, runDir string) *Sink {
	return &Sink{
		steps:  steps,
		states: make([]engine.State, len(steps)),
		durs:   make([]engine.Result, len(steps)),
		out:    out,
		color:  color,
		runDir: runDir,
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

// Line streams one line of step output, stripping ANSI when color is off.
func (s *Sink) Line(_ int, b []byte) {
	if !s.color {
		b = ansiRE.ReplaceAll(b, nil)
	}
	b = bytes.TrimRight(b, "\r")
	s.out.Write(b)
	s.out.Write([]byte{'\n'})
}

// StepDone records the outcome and prints the closing per-step line.
func (s *Sink) StepDone(i int, res engine.Result) {
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
