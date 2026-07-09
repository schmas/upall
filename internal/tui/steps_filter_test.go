package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/schmas/upall/internal/engine"
)

func threeSteps() []engine.Step {
	return []engine.Step{{Key: "a", Label: "A"}, {Key: "b", Label: "B"}, {Key: "c", Label: "C"}}
}

// TestStepFilterIsViewOnly proves the filter hides/shows rows without changing
// the underlying step set.
func TestStepFilterIsViewOnly(t *testing.T) {
	m, _, _ := testModel(threeSteps())
	sizeUp(m)
	startRunning(m)
	m.states[0] = engine.StateOK      // done
	m.states[1] = engine.StateRunning // not finished
	m.states[2] = engine.StatePending // not finished

	m.filter = FilterAll
	if got := m.visibleStepIndices(); len(got) != 3 {
		t.Errorf("All shows %v, want all 3", got)
	}
	m.filter = FilterPending
	if got := m.visibleStepIndices(); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("Pending visible = %v, want [1 2]", got)
	}
	m.filter = FilterDone
	if got := m.visibleStepIndices(); len(got) != 1 || got[0] != 0 {
		t.Errorf("Done visible = %v, want [0]", got)
	}
	if len(m.steps) != 3 {
		t.Error("filtering must not change the run set")
	}
}

// TestFilterCycleWraps proves cycling wraps in both directions and the ←/→ keys
// cycle when Steps is focused.
func TestFilterCycleWraps(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	m.filter = FilterAll

	for _, want := range []stepFilter{FilterPending, FilterDone, FilterAll} {
		m.cycleFilter(+1)
		if m.filter != want {
			t.Fatalf("cycle+ = %v, want %v", m.filter, want)
		}
	}
	m.cycleFilter(-1)
	if m.filter != FilterDone {
		t.Errorf("cycle- from All should wrap to Done, got %v", m.filter)
	}

	// → key cycles the filter when Steps is focused.
	m.filter = FilterAll
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if m.filter != FilterPending {
		t.Errorf("→ should advance filter to Pending, got %v", m.filter)
	}
}

// TestPreRunToggleExcludesStep proves Space toggles a step's include flag, the
// header count follows the included set, and begin() flags the excluded step
// Skip so the runner reports it skipped.
func TestPreRunToggleExcludesStep(t *testing.T) {
	m, launched, _ := testModel(demoSteps())
	sizeUp(m)

	// Select step 1 and toggle it off (idle + Steps focus).
	m.out = outSel{kind: outLiveStep, step: 1}
	m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if m.included[1] {
		t.Error("space should exclude the selected step")
	}
	if !m.included[0] {
		t.Error("other steps stay included")
	}
	if m.includedCount() != 1 {
		t.Errorf("included count = %d, want 1", m.includedCount())
	}

	// Start: the excluded step is flagged Skip before launch; the included one is not.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if *launched != 1 {
		t.Fatalf("launch count = %d, want 1", *launched)
	}
	if !m.rc.steps[1].Skip || m.rc.steps[1].SkipReason != "excluded" {
		t.Errorf("excluded step should be Skip=excluded, got Skip=%v reason=%q",
			m.rc.steps[1].Skip, m.rc.steps[1].SkipReason)
	}
	if m.rc.steps[0].Skip {
		t.Error("included step must not be skipped")
	}

	// The runner reports the excluded step via Skip → StateSkipped.
	m.Update(skipMsg{i: 1, reason: "excluded"})
	if m.states[1] != engine.StateSkipped {
		t.Errorf("excluded step should end StateSkipped, got %v", m.states[1])
	}
}

// TestToggleBlockedAfterStart proves inclusion is a pre-run choice: Space is a
// no-op once the run has started.
func TestToggleBlockedAfterStart(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	startRunning(m)
	m.out = outSel{kind: outLiveStep, step: 0}
	m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if !m.included[0] {
		t.Error("toggle must be a no-op after the run has started")
	}
}

// TestFilterCursorClampsWhenRowHidden proves the selection snaps to the nearest
// still-visible step when the active filter hides the current row.
func TestFilterCursorClampsWhenRowHidden(t *testing.T) {
	m, _, _ := testModel(threeSteps())
	sizeUp(m)
	startRunning(m)
	m.states[0] = engine.StateOK
	m.states[1] = engine.StateOK
	m.states[2] = engine.StatePending

	m.out = outSel{kind: outLiveStep, step: 2} // pending, hidden under Done
	m.filter = FilterDone
	m.clampStepCursor()
	if !m.isLiveStep() || m.out.step != 1 {
		t.Errorf("clamp should snap to nearest visible step 1, got %+v", m.out)
	}
}

// TestSelectionUsesCanonicalIndexUnderFilter proves the cursor maps to the
// canonical step index (not the visible row ordinal), so retry acts on the
// right step even when earlier rows are filtered out.
func TestSelectionUsesCanonicalIndexUnderFilter(t *testing.T) {
	m, launched, _ := testModel(threeSteps())
	sizeUp(m)
	startRunning(m)
	m.states[0] = engine.StatePending // hidden under Done
	m.states[1] = engine.StateFailed
	m.states[2] = engine.StateFailed

	m.filter = FilterDone
	m.setStepCursor(1) // first visible row → canonical step 1, not row 0
	if !m.isLiveStep() || m.out.step != 1 {
		t.Fatalf("cursor 1 under Done should select canonical step 1, got %+v", m.out)
	}

	m.running = false
	m.retry()
	if *launched != 1 || m.states[1] != engine.StatePending {
		t.Errorf("retry should fire on canonical step 1: launched=%d state1=%v",
			*launched, m.states[1])
	}
}
