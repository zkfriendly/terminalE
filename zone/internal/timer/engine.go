// Package timer implements the pomodoro focus engine as a pure, I/O-free state
// machine. A session is a number of work/break cycles derived from the session
// length; callers tick it once per second and react to the returned transitions.
package timer

// Phase is the kind of the current block.
type Phase int

const (
	// Work is a focus block.
	Work Phase = iota
	// Break is a rest block.
	Break
	// Prepare is a one-off settle-in block at the very start of a session.
	Prepare
)

func (p Phase) String() string {
	switch p {
	case Break:
		return "break"
	case Prepare:
		return "prepare"
	default:
		return "work"
	}
}

// Block describes one work or break interval.
type Block struct {
	Phase   Phase
	Index   int // cycle index, 0-based
	Planned int // planned duration in seconds
}

// Transition is returned when a block ends.
type Transition struct {
	Ended    Block // the block that just finished
	Elapsed  int   // seconds actually spent in the ended block
	Next     Block // the block now starting (zero value if Finished)
	Finished bool  // true when the whole session is complete
}

// Engine is a pomodoro state machine. Construct with New and drive with Tick.
type Engine struct {
	workSec    int
	breakSec   int
	prepareSec int
	cycles     int

	cycleIdx  int
	phase     Phase
	remaining int
	running   bool
	done      bool
}

// New builds an engine for a session of totalSec made of workSec/breakSec cycles,
// preceded by an optional one-off prepareSec settle-in block. The number of full
// cycles is totalSec/(workSec+breakSec), minimum one.
func New(workSec, breakSec, totalSec, prepareSec int) *Engine {
	if workSec <= 0 {
		workSec = 1
	}
	if breakSec < 0 {
		breakSec = 0
	}
	if prepareSec < 0 {
		prepareSec = 0
	}
	cycle := workSec + breakSec
	cycles := 1
	if cycle > 0 {
		cycles = totalSec / cycle
	}
	if cycles < 1 {
		cycles = 1
	}
	e := &Engine{
		workSec:    workSec,
		breakSec:   breakSec,
		prepareSec: prepareSec,
		cycles:     cycles,
	}
	if prepareSec > 0 {
		e.phase = Prepare
		e.remaining = prepareSec
	} else {
		e.phase = Work
		e.remaining = workSec
	}
	return e
}

// PhaseFromString parses a phase name into a Phase.
func PhaseFromString(s string) Phase {
	switch s {
	case "break":
		return Break
	case "prepare":
		return Prepare
	default:
		return Work
	}
}

// Restore rebuilds an engine mid-session from persisted state. If remaining is
// non-positive the engine is left at the start of a fresh session instead.
func Restore(workSec, breakSec, totalSec, prepareSec int, phase Phase, remaining, cycleIdx int, running bool) *Engine {
	e := New(workSec, breakSec, totalSec, prepareSec)
	if remaining <= 0 {
		return e
	}
	e.phase = phase
	e.remaining = remaining
	if cycleIdx >= 0 && cycleIdx < e.cycles {
		e.cycleIdx = cycleIdx
	}
	e.running = running
	return e
}

// Start (re)starts the clock.
func (e *Engine) Start() {
	if !e.done {
		e.running = true
	}
}

// Pause halts the clock without losing position.
func (e *Engine) Pause() { e.running = false }

// Toggle flips between running and paused.
func (e *Engine) Toggle() {
	if e.done {
		return
	}
	e.running = !e.running
}

// Running reports whether the clock is advancing.
func (e *Engine) Running() bool { return e.running }

// Done reports whether the session has finished.
func (e *Engine) Done() bool { return e.done }

// Phase returns the current block phase.
func (e *Engine) Phase() Phase { return e.phase }

// Remaining returns seconds left in the current block.
func (e *Engine) Remaining() int { return e.remaining }

// Planned returns the planned duration of the current block.
func (e *Engine) Planned() int {
	switch e.phase {
	case Break:
		return e.breakSec
	case Prepare:
		return e.prepareSec
	default:
		return e.workSec
	}
}

// Elapsed returns seconds spent so far in the current block.
func (e *Engine) Elapsed() int { return e.Planned() - e.remaining }

// AdjustRemaining moves the playhead by delta seconds. Positive delta rewinds
// (adds time back); negative delta skips ahead. Remaining is clamped to
// [0, Planned]. Returns false when the session is already done.
func (e *Engine) AdjustRemaining(delta int) bool {
	if e.done || delta == 0 {
		return false
	}
	e.remaining += delta
	planned := e.Planned()
	if e.remaining > planned {
		e.remaining = planned
	}
	if e.remaining < 0 {
		e.remaining = 0
	}
	return true
}

// CycleIndex returns the current 0-based cycle index.
func (e *Engine) CycleIndex() int { return e.cycleIdx }

// Cycles returns the total number of cycles in the session.
func (e *Engine) Cycles() int { return e.cycles }

// Tick advances the clock by one second. It returns a Transition and true when a
// block boundary is crossed; otherwise the zero Transition and false.
func (e *Engine) Tick() (Transition, bool) {
	if !e.running || e.done {
		return Transition{}, false
	}
	if e.remaining > 0 {
		e.remaining--
	}
	if e.remaining > 0 {
		return Transition{}, false
	}
	return e.advance(e.Planned()), true
}

// Skip ends the current block immediately, returning the resulting transition.
func (e *Engine) Skip() (Transition, bool) {
	if e.done {
		return Transition{}, false
	}
	return e.advance(e.Elapsed()), true
}

// advance closes the current block (recording elapsed) and moves to the next.
func (e *Engine) advance(elapsed int) Transition {
	ended := Block{Phase: e.phase, Index: e.cycleIdx, Planned: e.Planned()}
	tr := Transition{Ended: ended, Elapsed: elapsed}

	// Prepare is a one-off pre-roll: it flows into the first work block.
	if e.phase == Prepare {
		e.phase = Work
		e.remaining = e.workSec
		tr.Next = Block{Phase: Work, Index: e.cycleIdx, Planned: e.workSec}
		return tr
	}

	if e.phase == Work && e.breakSec > 0 {
		e.phase = Break
		e.remaining = e.breakSec
		tr.Next = Block{Phase: Break, Index: e.cycleIdx, Planned: e.breakSec}
		return tr
	}

	// End of a cycle (either after a break, or after work when there are no breaks).
	e.cycleIdx++
	if e.cycleIdx >= e.cycles {
		e.done = true
		e.running = false
		e.remaining = 0
		tr.Finished = true
		return tr
	}
	e.phase = Work
	e.remaining = e.workSec
	tr.Next = Block{Phase: Work, Index: e.cycleIdx, Planned: e.workSec}
	return tr
}
