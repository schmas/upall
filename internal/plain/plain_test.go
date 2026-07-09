package plain

import (
	"bytes"
	"strings"
	"testing"

	"github.com/schmas/upall/internal/engine"
)

func newTestSink(color bool) (*Sink, *bytes.Buffer) {
	var out bytes.Buffer
	s := New([]engine.Step{{Key: "a", Label: "A"}}, &out, color, "")
	return s, &out
}

// TestPlainBodyBytesColorOff pins the streamed body byte-for-byte with color off:
// raw chunks are split on \n, ANSI is stripped, a trailing \r is trimmed while an
// internal \r is preserved, and a final partial line (no newline) flushes on
// StepDone. This is the Phase 1 parity guard — the plain path must be unchanged
// by the switch from line events to raw-byte Output.
func TestPlainBodyBytesColorOff(t *testing.T) {
	s, out := newTestSink(false)
	// Split lines across chunk boundaries, include a \r redraw, an ANSI color
	// run, a bare trailing \r (CRLF), and a partial tail with no newline.
	s.Output(0, []byte("line1\nprog 1%\rprog 9"))
	s.Output(0, []byte("9%\n\x1b[31mred\x1b[0m\ncrlf\r\npartial-no-nl"))
	s.StepDone(0, engine.Result{State: engine.StateOK})

	wantBody := "line1\nprog 1%\rprog 99%\nred\ncrlf\npartial-no-nl\n"
	if !strings.HasPrefix(out.String(), wantBody) {
		t.Fatalf("plain body mismatch\n got: %q\nwant prefix: %q", out.String(), wantBody)
	}
}

// TestPlainBodyKeepsANSIColorOn: with color on, SGR sequences pass through.
func TestPlainBodyKeepsANSIColorOn(t *testing.T) {
	s, out := newTestSink(true)
	s.Output(0, []byte("\x1b[31mred\x1b[0m\n"))
	if !strings.HasPrefix(out.String(), "\x1b[31mred\x1b[0m\n") {
		t.Fatalf("color-on body should keep ANSI, got %q", out.String())
	}
}

// TestPlainNoTrailingNewlineStillPrints: a step whose last line has no trailing
// newline still emits that line (flushed on StepDone), so no output is dropped.
func TestPlainNoTrailingNewlineStillPrints(t *testing.T) {
	s, out := newTestSink(false)
	s.Output(0, []byte("only-line-no-newline"))
	// Before StepDone the partial is buffered, not yet written.
	if strings.Contains(out.String(), "only-line") {
		t.Fatalf("partial line should buffer until flush, got %q", out.String())
	}
	s.StepDone(0, engine.Result{State: engine.StateOK})
	if !strings.HasPrefix(out.String(), "only-line-no-newline\n") {
		t.Fatalf("partial line should flush on StepDone, got %q", out.String())
	}
}
