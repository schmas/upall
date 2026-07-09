package engine

// Sink receives step lifecycle events from the runner. Implementations decide
// how to present or store them: the plain sink prints directly; the TUI sink
// forwards each event through program.Send so all shared-state mutation happens
// on the Bubble Tea update loop (never on the runner goroutine).
//
// The int argument is the step's index in the slice passed to the runner, so a
// consumer can map events back to its own per-step state.
type Sink interface {
	// Skip reports that a step did not apply and was not executed.
	Skip(i int, reason string)
	// StepStart reports that a step has begun executing.
	StepStart(i int)
	// Output reports a raw chunk of a step's output exactly as read from the pty.
	// A chunk may contain carriage returns, escape sequences, and partial or
	// multiple lines; line boundaries are not aligned to chunk boundaries. The
	// slice is only valid for the duration of the call (io.Copy reuses its
	// buffer), so an implementation that retains bytes must copy them.
	Output(i int, p []byte)
	// StepDone reports the final outcome of a step.
	StepDone(i int, res Result)
}
