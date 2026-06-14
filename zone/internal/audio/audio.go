// Package audio drives zone's ambient sound and transition chimes via beep. It
// degrades gracefully: if no audio device is available, every call is a no-op so
// the TUI keeps working.
//
// Ambient sound is a set of independently toggleable layers that mix together:
// low-frequency binaural beats (15 Hz / 45 Hz for focus) and optionally a loop of
// the user's own audio files. Chimes are generated tones.
package audio

import (
	"sync"
	"time"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/effects"
	"github.com/gopxl/beep/v2/speaker"
	"github.com/zkfriendly/zone/internal/config"
)

const sampleRate beep.SampleRate = 44100

// Chime identifies a transition sound.
type Chime int

const (
	// ChimeWork plays when a work block begins.
	ChimeWork Chime = iota
	// ChimeBreak plays when a break begins.
	ChimeBreak
	// ChimeDone plays when the whole session completes.
	ChimeDone
)

// Track IDs for the ambient layers.
const (
	TrackBeat15 = "beat15"
	TrackBeat45 = "beat45"
	TrackFolder = "folder"
)

// TrackInfo is a UI-facing view of an ambient layer.
type TrackInfo struct {
	ID      string
	Label   string
	Enabled bool
}

// track is one mixable ambient layer.
type track struct {
	id       string
	label    string
	selected bool // user wants this layer on
	build    func() beep.Streamer

	ctrl  *beep.Ctrl      // nil until built
	vol   *effects.Volume // nil until built
	built bool
}

// Manager owns the speaker and the ambient layers.
type Manager struct {
	mu sync.Mutex

	cfg    config.Config
	ready  bool // speaker initialised
	failed bool // speaker init failed; stay silent

	volume float64
	gate   bool // ambient currently allowed to play (work block, running)
	tracks []*track
}

// New returns a Manager for the given config. The speaker is initialised lazily.
func New(cfg config.Config) *Manager {
	m := &Manager{cfg: cfg, volume: cfg.Volume}

	sel := cfg.Layers
	on := func(id string, def bool) bool {
		if sel == nil {
			return def
		}
		if v, ok := sel[id]; ok {
			return v
		}
		return def
	}

	m.tracks = []*track{
		{id: TrackBeat15, label: "15 Hz beat", selected: on(TrackBeat15, false),
			build: func() beep.Streamer { return binaural(200, 15, 0.16) }},
		{id: TrackBeat45, label: "45 Hz beat", selected: on(TrackBeat45, false),
			build: func() beep.Streamer { return binaural(200, 45, 0.16) }},
	}
	if cfg.AmbientFolder != "" {
		folder := cfg.AmbientFolder
		m.tracks = append(m.tracks, &track{
			id: TrackFolder, label: "playlist", selected: on(TrackFolder, false),
			build: func() beep.Streamer {
				if pl := newPlaylist(folder); pl != nil {
					return pl
				}
				return silentLoop()
			},
		})
	}
	return m
}

// ensureSpeaker initialises the speaker once. Returns false if audio is unavailable.
func (m *Manager) ensureSpeaker() bool {
	if m.ready {
		return true
	}
	if m.failed {
		return false
	}
	if err := speaker.Init(sampleRate, sampleRate.N(time.Second/10)); err != nil {
		m.failed = true
		return false
	}
	m.ready = true
	return true
}

// ensureTrack builds and queues a track's stream (paused). Speaker must be ready.
func (m *Manager) ensureTrack(t *track) {
	if t.built {
		return
	}
	t.vol = &effects.Volume{
		Streamer: t.build(),
		Base:     2,
		Volume:   volumeToGain(m.volume),
		Silent:   m.volume <= 0,
	}
	t.ctrl = &beep.Ctrl{Streamer: t.vol, Paused: true}
	speaker.Play(t.ctrl)
	t.built = true
}

func (m *Manager) anySelected() bool {
	for _, t := range m.tracks {
		if t.selected {
			return true
		}
	}
	return false
}

// applyGateLocked pauses/unpauses each built track based on selection and gate.
func (m *Manager) applyGateLocked() {
	if !m.ready {
		return
	}
	speaker.Lock()
	for _, t := range m.tracks {
		if t.ctrl != nil {
			t.ctrl.Paused = !(t.selected && m.gate)
		}
	}
	speaker.Unlock()
}

// SetAmbientActive enables (work block) or suspends (break/pause) ambient sound.
func (m *Manager) SetAmbientActive(on bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gate = on
	if on {
		if !m.anySelected() || !m.ensureSpeaker() {
			return
		}
		for _, t := range m.tracks {
			if t.selected {
				m.ensureTrack(t)
			}
		}
	}
	m.applyGateLocked()
}

// StartAmbient resumes ambient sound for a work block.
func (m *Manager) StartAmbient() { m.SetAmbientActive(true) }

// StopAmbient suspends ambient sound (break, pause, or session end).
func (m *Manager) StopAmbient() { m.SetAmbientActive(false) }

// Tracks returns the ambient layers in display order.
func (m *Manager) Tracks() []TrackInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]TrackInfo, len(m.tracks))
	for i, t := range m.tracks {
		out[i] = TrackInfo{ID: t.id, Label: t.label, Enabled: t.selected}
	}
	return out
}

// ToggleTrack flips a layer on/off by display index.
func (m *Manager) ToggleTrack(index int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if index < 0 || index >= len(m.tracks) {
		return
	}
	t := m.tracks[index]
	t.selected = !t.selected
	if t.selected && m.gate && m.ensureSpeaker() {
		m.ensureTrack(t)
	}
	m.applyGateLocked()
}

// Chime plays a short transition tone.
func (m *Manager) Chime(c Chime) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.cfg.ChimesEnabled || !m.ensureSpeaker() {
		return
	}
	speaker.Play(&effects.Volume{
		Streamer: chime(c),
		Base:     2,
		Volume:   volumeToGain(m.volume),
		Silent:   m.volume <= 0,
	})
}

// SetVolume updates playback volume (0..1) live across all layers.
func (m *Manager) SetVolume(v float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v = clamp(v, 0, 1)
	m.volume = v
	if !m.ready {
		return
	}
	speaker.Lock()
	for _, t := range m.tracks {
		if t.vol != nil {
			t.vol.Volume = volumeToGain(v)
			t.vol.Silent = v <= 0
		}
	}
	speaker.Unlock()
}

// Volume returns the current volume (0..1).
func (m *Manager) Volume() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.volume
}

// Selection returns the current per-layer on/off state, suitable for persisting.
func (m *Manager) Selection() map[string]bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]bool, len(m.tracks))
	for _, t := range m.tracks {
		out[t.id] = t.selected
	}
	return out
}

// Available reports whether audio output is working.
func (m *Manager) Available() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ensureSpeaker()
}

// Close releases the speaker.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ready {
		speaker.Clear()
		speaker.Close()
		m.ready = false
	}
}

// volumeToGain maps a 0..1 linear volume to a base-2 exponent for effects.Volume.
// 1.0 -> 0 (unity gain); 0.5 -> -2 (quarter gain); approaches silence near 0.
func volumeToGain(v float64) float64 {
	return (v - 1) * 4
}
