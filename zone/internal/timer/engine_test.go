package timer

import "testing"

func tickN(e *Engine, n int) []Transition {
	var trs []Transition
	for i := 0; i < n; i++ {
		if tr, ok := e.Tick(); ok {
			trs = append(trs, tr)
		}
	}
	return trs
}

func TestCycleCount(t *testing.T) {
	// 50/10 over 240 minutes => 4 cycles.
	e := New(50*60, 10*60, 240*60, 0)
	if e.Cycles() != 4 {
		t.Fatalf("expected 4 cycles, got %d", e.Cycles())
	}
}

func TestWorkBreakTransition(t *testing.T) {
	e := New(2, 1, 3, 0) // 1 cycle: work 2s, break 1s
	e.Start()

	trs := tickN(e, 2) // finish the work block
	if len(trs) != 1 {
		t.Fatalf("expected 1 transition after work, got %d", len(trs))
	}
	tr := trs[0]
	if tr.Ended.Phase != Work || tr.Elapsed != 2 {
		t.Fatalf("unexpected ended work block: %+v", tr.Ended)
	}
	if tr.Next.Phase != Break || tr.Finished {
		t.Fatalf("expected next=break, not finished: %+v", tr)
	}
	if e.Phase() != Break || e.Remaining() != 1 {
		t.Fatalf("engine not in break state: phase=%v rem=%d", e.Phase(), e.Remaining())
	}
}

func TestSessionFinishes(t *testing.T) {
	e := New(2, 1, 3, 0) // 1 cycle = 3s total
	e.Start()

	trs := tickN(e, 3) // work(2) + break(1)
	if len(trs) != 2 {
		t.Fatalf("expected 2 transitions, got %d", len(trs))
	}
	last := trs[len(trs)-1]
	if !last.Finished {
		t.Fatalf("expected final transition to be finished: %+v", last)
	}
	if !e.Done() || e.Running() {
		t.Fatalf("engine should be done and stopped")
	}
}

func TestPauseStopsTime(t *testing.T) {
	e := New(5, 5, 10, 0)
	e.Start()
	tickN(e, 2)
	rem := e.Remaining()
	e.Pause()
	tickN(e, 3) // should not advance while paused
	if e.Remaining() != rem {
		t.Fatalf("paused engine advanced: before=%d after=%d", rem, e.Remaining())
	}
}

func TestSkipRecordsPartialElapsed(t *testing.T) {
	e := New(10, 5, 15, 0)
	e.Start()
	tickN(e, 3) // 3s into work
	tr, ok := e.Skip()
	if !ok {
		t.Fatal("skip should report a transition")
	}
	if tr.Ended.Phase != Work || tr.Elapsed != 3 {
		t.Fatalf("expected partial work elapsed=3, got %+v", tr)
	}
	if e.Phase() != Break {
		t.Fatalf("expected break after skip, got %v", e.Phase())
	}
}

func TestPreparePrecedesWork(t *testing.T) {
	e := New(2, 1, 3, 3) // 3s prepare, then 1 cycle: work 2s, break 1s
	if e.Phase() != Prepare || e.Remaining() != 3 {
		t.Fatalf("expected to start in prepare(3), got phase=%v rem=%d", e.Phase(), e.Remaining())
	}
	if e.Cycles() != 1 {
		t.Fatalf("prepare should not add a cycle, got %d", e.Cycles())
	}
	e.Start()

	trs := tickN(e, 3) // finish prepare
	if len(trs) != 1 {
		t.Fatalf("expected 1 transition after prepare, got %d", len(trs))
	}
	if trs[0].Ended.Phase != Prepare || trs[0].Next.Phase != Work || trs[0].Finished {
		t.Fatalf("prepare should flow into work: %+v", trs[0])
	}
	if e.Phase() != Work || e.Remaining() != 2 {
		t.Fatalf("expected work(2) after prepare, got phase=%v rem=%d", e.Phase(), e.Remaining())
	}

	// The rest of the session still completes in exactly one cycle.
	trs = tickN(e, 3) // work(2) + break(1)
	if !trs[len(trs)-1].Finished || !e.Done() {
		t.Fatal("session should finish after one cycle following prepare")
	}
}

func TestSkipPrepareGoesToWork(t *testing.T) {
	e := New(10, 5, 15, 180)
	e.Start()
	tr, ok := e.Skip()
	if !ok || tr.Ended.Phase != Prepare || tr.Next.Phase != Work {
		t.Fatalf("skipping prepare should jump to work: %+v", tr)
	}
	if e.Phase() != Work {
		t.Fatalf("expected work after skipping prepare, got %v", e.Phase())
	}
}

func TestNoBreakSessions(t *testing.T) {
	e := New(2, 0, 6, 0) // breakless => 3 cycles of pure work
	if e.Cycles() != 3 {
		t.Fatalf("expected 3 cycles, got %d", e.Cycles())
	}
	e.Start()
	trs := tickN(e, 6)
	if len(trs) != 3 {
		t.Fatalf("expected 3 work transitions, got %d", len(trs))
	}
	for i, tr := range trs {
		if tr.Ended.Phase != Work {
			t.Fatalf("transition %d not work: %+v", i, tr)
		}
	}
	if !trs[len(trs)-1].Finished || !e.Done() {
		t.Fatal("session should have finished")
	}
}

func TestAdjustRemaining(t *testing.T) {
	e := New(100, 50, 150, 0)
	e.Start()
	tickN(e, 20)
	if e.Remaining() != 80 || e.Elapsed() != 20 {
		t.Fatalf("setup: rem=%d elapsed=%d", e.Remaining(), e.Elapsed())
	}
	if !e.AdjustRemaining(15) {
		t.Fatal("rewind should succeed")
	}
	if e.Remaining() != 95 || e.Elapsed() != 5 {
		t.Fatalf("rewind: rem=%d elapsed=%d", e.Remaining(), e.Elapsed())
	}
	if !e.AdjustRemaining(-30) {
		t.Fatal("forward should succeed")
	}
	if e.Remaining() != 65 || e.Elapsed() != 35 {
		t.Fatalf("forward: rem=%d elapsed=%d", e.Remaining(), e.Elapsed())
	}
	e.AdjustRemaining(1000)
	if e.Remaining() != 100 {
		t.Fatalf("expected clamp to planned, got %d", e.Remaining())
	}
	e.AdjustRemaining(-1000)
	if e.Remaining() != 0 {
		t.Fatalf("expected clamp to 0, got %d", e.Remaining())
	}
}
