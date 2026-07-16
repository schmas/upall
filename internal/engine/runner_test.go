package engine

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// recSink records everything the runner reports, guarded for -race. Output now
// arrives as raw pty chunks, so the sink reassembles the stream per step and
// splits it into logical lines on demand (mirroring what a real sink does).
type recSink struct {
	mu    sync.Mutex
	start []int
	skips map[int]string
	raw   map[int][]byte
	done  map[int]Result
}

func newRecSink() *recSink {
	return &recSink{skips: map[int]string{}, raw: map[int][]byte{}, done: map[int]Result{}}
}

func (s *recSink) Skip(i int, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.skips[i] = reason
}
func (s *recSink) StepStart(i int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.start = append(s.start, i)
}
func (s *recSink) Output(i int, p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.raw[i] = append(s.raw[i], p...) // copy: io.Copy reuses its buffer
}
func (s *recSink) StepDone(i int, res Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.done[i] = res
}

// linesOf splits a step's captured raw output into trimmed logical lines, so the
// existing assertions on line content keep working over the raw-chunk contract.
func (s *recSink) linesOf(i int) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw := strings.TrimRight(string(s.raw[i]), "\n")
	if raw == "" {
		return nil
	}
	lines := strings.Split(raw, "\n")
	for j, ln := range lines {
		lines[j] = strings.TrimRight(ln, "\r")
	}
	return lines
}

func TestRunAllStreamsLinesInOrder(t *testing.T) {
	sink := newRecSink()
	steps := []Step{{Key: "a", Run: []string{`printf 'l1\nl2\nl3\n'`}}}
	NewRunner("", sink).RunAll(context.Background(), steps)

	got := sink.linesOf(0)
	want := []string{"l1", "l2", "l3"}
	if len(got) != len(want) {
		t.Fatalf("lines = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
	if res := sink.done[0]; res.State != StateOK || res.RC != 0 {
		t.Fatalf("done = %+v, want OK rc0", res)
	}
}

func TestExitCodePropagates(t *testing.T) {
	sink := newRecSink()
	steps := []Step{{Key: "x", Run: []string{"exit 3"}}}
	NewRunner("", sink).RunAll(context.Background(), steps)
	if res := sink.done[0]; res.State != StateFailed || res.RC != 3 {
		t.Fatalf("done = %+v, want Failed rc3", res)
	}
}

func TestMultiCommandFailIfAnyRunsAll(t *testing.T) {
	sink := newRecSink()
	// v2 semantics: every command attempted, step fails if any command fails.
	steps := []Step{{Key: "m", Run: []string{"echo one", "false", "echo three"}}}
	NewRunner("", sink).RunAll(context.Background(), steps)

	joined := strings.Join(sink.linesOf(0), " ")
	if !strings.Contains(joined, "one") || !strings.Contains(joined, "three") {
		t.Fatalf("expected both commands to run, got %q", joined)
	}
	if res := sink.done[0]; res.State != StateFailed {
		t.Fatalf("done = %+v, want Failed (middle command failed)", res)
	}
}

func TestTimeoutKillsStep(t *testing.T) {
	sink := newRecSink()
	steps := []Step{{Key: "slow", Run: []string{"sleep 5"}, Timeout: 200 * time.Millisecond}}
	start := time.Now()
	NewRunner("", sink).RunAll(context.Background(), steps)
	elapsed := time.Since(start)

	if elapsed > 4*time.Second {
		t.Fatalf("timeout did not kill promptly: %v", elapsed)
	}
	res := sink.done[0]
	if res.State != StateFailed || res.Reason != "timed out" {
		t.Fatalf("done = %+v, want Failed 'timed out'", res)
	}
}

func TestStdinGetsEOFNoHang(t *testing.T) {
	sink := newRecSink()
	// If stdin were the pty (not /dev/null) this would block forever; the
	// generous timeout only catches a regression, it must not fire.
	steps := []Step{{Key: "rd", Shell: "bash", Run: []string{"read x; echo done"}, Timeout: 5 * time.Second}}
	start := time.Now()
	NewRunner("", sink).RunAll(context.Background(), steps)
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Fatalf("step reading stdin hung: %v", elapsed)
	}
	if res := sink.done[0]; res.State != StateOK {
		t.Fatalf("done = %+v, want OK (stdin EOF, echo ran)", res)
	}
	if joined := strings.Join(sink.linesOf(0), " "); !strings.Contains(joined, "done") {
		t.Fatalf("expected 'done', got %q", joined)
	}
}

func TestANSIColorSurvivesPTY(t *testing.T) {
	sink := newRecSink()
	steps := []Step{{Key: "c", Run: []string{`printf '\033[31mred\033[0m\n'`}}}
	NewRunner("", sink).RunAll(context.Background(), steps)

	sink.mu.Lock()
	sawESC := bytes.Contains(sink.raw[0], []byte{0x1b})
	sink.mu.Unlock()
	if !sawESC {
		t.Fatalf("expected ESC byte to survive pty capture, lines=%v", sink.linesOf(0))
	}
}

func TestRunOneReRunsSingleStep(t *testing.T) {
	sink := newRecSink()
	steps := []Step{
		{Key: "a", Run: []string{"echo a"}},
		{Key: "b", Run: []string{"echo b"}},
	}
	NewRunner("", sink).RunOne(context.Background(), steps, 1)

	if _, ran := sink.done[0]; ran {
		t.Fatal("RunOne ran step 0")
	}
	if res, ran := sink.done[1]; !ran || res.State != StateOK {
		t.Fatalf("RunOne step 1 = %+v ran=%v", res, ran)
	}
}

func TestRunFromSkipsCompletedSteps(t *testing.T) {
	sink := newRecSink()
	steps := []Step{
		{Key: "a", Run: []string{"echo a"}},
		{Key: "b", Run: []string{"echo b"}},
		{Key: "c", Run: []string{"echo c"}},
	}
	NewRunner("", sink).RunFrom(context.Background(), steps, 1)

	if _, ran := sink.done[0]; ran {
		t.Fatal("RunFrom ran step 0, which is before start")
	}
	for _, i := range []int{1, 2} {
		if res, ran := sink.done[i]; !ran || res.State != StateOK {
			t.Fatalf("RunFrom step %d = %+v ran=%v, want OK", i, res, ran)
		}
	}
}

func TestRunFromOutOfRangeIsNoop(t *testing.T) {
	sink := newRecSink()
	steps := []Step{{Key: "a", Run: []string{"echo a"}}}

	NewRunner("", sink).RunFrom(context.Background(), steps, len(steps))
	if len(sink.done) != 0 {
		t.Fatalf("RunFrom with start==len(steps) ran steps: %+v", sink.done)
	}

	NewRunner("", sink).RunFrom(context.Background(), steps, -1)
	if len(sink.done) != 0 {
		t.Fatalf("RunFrom with negative start ran steps: %+v", sink.done)
	}
}

func TestQuitCancelAbortsStep(t *testing.T) {
	sink := newRecSink()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()

	start := time.Now()
	NewRunner("", sink).RunAll(ctx, []Step{{Key: "s", Run: []string{"sleep 5"}}})
	elapsed := time.Since(start)

	if elapsed > 4*time.Second {
		t.Fatalf("quit did not cancel promptly: %v", elapsed)
	}
	if res := sink.done[0]; res.State != StateAborted {
		t.Fatalf("done = %+v, want Aborted", res)
	}
}

func TestSkippedStepReported(t *testing.T) {
	sink := newRecSink()
	steps := []Step{{Key: "s", Skip: true, SkipReason: "not applicable"}}
	NewRunner("", sink).RunAll(context.Background(), steps)
	if sink.skips[0] != "not applicable" {
		t.Fatalf("skip reason = %q", sink.skips[0])
	}
	if _, ran := sink.done[0]; ran {
		t.Fatal("skipped step should not run")
	}
}

// TestBackgroundSlaveHolderDoesNotHang is the C1 regression: the shell exits
// immediately but `sleep 3 &` inherits the pty slave and holds it open, so the
// master never gets EOF. Closing the master cannot unblock the read on darwin,
// so the runner must cancel the drain (cancelreader) and finish promptly.
func TestBackgroundSlaveHolderDoesNotHang(t *testing.T) {
	sink := newRecSink()
	steps := []Step{{Key: "bg", Shell: "bash", Run: []string{"echo started; sleep 3 &"}}}
	start := time.Now()
	NewRunner("", sink).RunAll(context.Background(), steps)
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Fatalf("runner hung on a backgrounded slave-holder: %v", elapsed)
	}
	if res := sink.done[0]; res.State != StateOK {
		t.Fatalf("done = %+v, want OK", res)
	}
	if joined := strings.Join(sink.linesOf(0), " "); !strings.Contains(joined, "started") {
		t.Errorf("expected 'started' output, got %q", joined)
	}
}

func TestTeesToLogfile(t *testing.T) {
	dir := t.TempDir()
	sink := newRecSink()
	steps := []Step{{Key: "log", Run: []string{"echo hello-log"}}}
	NewRunner(dir, sink).RunAll(context.Background(), steps)

	data, err := os.ReadFile(LogPath(dir, 1, "log"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "hello-log") {
		t.Fatalf("log missing output: %q", data)
	}
}
