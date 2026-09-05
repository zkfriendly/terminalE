package llm

import (
	"strings"
	"sync/atomic"
	"time"
)

// Stats holds note AI operation counters for the current process.
type Stats struct {
	InFlight int64
	OK       int64
	Err      int64
	LastMS   int64
}

var (
	statsInFlight int64
	statsOK       int64
	statsErr      int64
	statsLastMS   int64
)

// Snapshot returns a copy of the current LLM stats.
func Snapshot() Stats {
	return Stats{
		InFlight: atomic.LoadInt64(&statsInFlight),
		OK:       atomic.LoadInt64(&statsOK),
		Err:      atomic.LoadInt64(&statsErr),
		LastMS:   atomic.LoadInt64(&statsLastMS),
	}
}

func recordBegin() {
	atomic.AddInt64(&statsInFlight, 1)
}

func recordSuccess(d time.Duration) {
	atomic.AddInt64(&statsInFlight, -1)
	atomic.AddInt64(&statsOK, 1)
	atomic.StoreInt64(&statsLastMS, d.Milliseconds())
}

func recordFailure(d time.Duration) {
	atomic.AddInt64(&statsInFlight, -1)
	atomic.AddInt64(&statsErr, 1)
	atomic.StoreInt64(&statsLastMS, d.Milliseconds())
}

// ResetStats clears counters (for tests).
func ResetStats() {
	atomic.StoreInt64(&statsInFlight, 0)
	atomic.StoreInt64(&statsOK, 0)
	atomic.StoreInt64(&statsErr, 0)
	atomic.StoreInt64(&statsLastMS, 0)
}

// ModelLabel returns a short model name for display.
func ModelLabel(configured string) string {
	if configured != "" {
		return truncateModel(configured)
	}
	resolvedModelMu.Lock()
	m := resolvedModel
	resolvedModelMu.Unlock()
	if m != "" {
		return truncateModel(m)
	}
	return "auto"
}

func truncateModel(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "auto"
	}
	if i := strings.LastIndexAny(name, "/\\"); i >= 0 && i < len(name)-1 {
		name = name[i+1:]
	}
	if len(name) > 18 {
		return name[:15] + "…"
	}
	return name
}
