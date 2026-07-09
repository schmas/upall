package plain

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/schmas/upall/internal/engine"
)

func newTestSink(color bool) (*Sink, *bytes.Buffer) {
	var out bytes.Buffer
	s := New([]engine.Step{{Key: "a", Label: "A"}}, &out, color, "", true)
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

// TestRenderSummaryWritesManifest proves the shared exit path leaves a valid
// manifest.json so both plain and TUI runs record history.
func TestRenderSummaryWritesManifest(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "20260709-090000")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	steps := []engine.Step{{Key: "a", Label: "A"}}
	states := []engine.State{engine.StateOK}
	durs := []engine.Result{{Dur: 2 * time.Second}}

	var out bytes.Buffer
	RenderSummary(&out, "upall", steps, states, durs, dir, false, false)

	m, err := engine.ReadManifest(dir)
	if err != nil {
		t.Fatalf("manifest not written by RenderSummary: %v", err)
	}
	if len(m.Steps) != 1 || m.Steps[0].State != "ok" || m.Steps[0].DurMs != 2000 {
		t.Errorf("manifest steps = %+v", m.Steps)
	}
}

// TestRenderSummaryNoManifestWhenNoRunDir proves a runDir of "" writes nothing
// and does not error (logging disabled).
func TestRenderSummaryNoManifestWhenNoRunDir(t *testing.T) {
	var out bytes.Buffer
	RenderSummary(&out, "upall",
		[]engine.Step{{Key: "a", Label: "A"}},
		[]engine.State{engine.StateOK},
		[]engine.Result{{}}, "", false, false)
	// Reaching here without a panic/error is the assertion; there is no file.
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
