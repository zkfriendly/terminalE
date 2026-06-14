// Package session implements zone's focus daemon: a background process that owns
// the running pomodoro engine and audio, so a focus session keeps running (and
// playing) even after the terminal that started it is closed. The TUI talks to
// the daemon over a Unix socket and renders snapshots of its state.
package session

import (
	"path/filepath"

	"github.com/zkfriendly/zone/internal/config"
)

// Operations the client can request from the daemon.
const (
	OpStatus  = "status"  // fetch current snapshot
	OpToggle  = "toggle"  // pause/resume
	OpSkip    = "skip"    // skip current block
	OpTrack   = "track"   // toggle an ambient layer (by Index)
	OpVolume  = "volume"  // set volume (Volume 0..1)
	OpPreview = "preview" // play the start + end chimes as a demo
	OpEnd     = "end"     // end the session and stop the daemon
)

// Command is a request sent from the TUI client to the daemon.
type Command struct {
	Op     string  `json:"op"`
	Index  int     `json:"index,omitempty"`
	Volume float64 `json:"volume,omitempty"`
}

// TrackInfo mirrors an ambient layer's state for display.
type TrackInfo struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Enabled bool   `json:"enabled"`
}

// Snapshot is the daemon's current state, returned for every command.
type Snapshot struct {
	SessionID   int64       `json:"session_id"`
	TaskTitle   string      `json:"task_title"`
	ProjectName string      `json:"project_name"`
	Phase       string      `json:"phase"` // "work" | "break"
	Running     bool        `json:"running"`
	Remaining   int         `json:"remaining"`
	Planned     int         `json:"planned"`
	CycleIndex  int         `json:"cycle_index"`
	Cycles      int         `json:"cycles"`
	Finished    bool        `json:"finished"`
	Accrued     int         `json:"accrued"`     // work seconds this session
	TodayTotal  int         `json:"today_total"` // total work seconds today
	Volume      float64     `json:"volume"`
	Tracks      []TrackInfo `json:"tracks"`
}

// SocketPath returns the daemon's Unix socket path.
func SocketPath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "zoned.sock"), nil
}

// LogPath returns the daemon's log file path.
func LogPath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "zoned.log"), nil
}
