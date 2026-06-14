package audio

import (
	"testing"

	"github.com/zkfriendly/zone/internal/config"
)

// These tests exercise selection state only; the speaker is never initialised
// because tracks are toggled while the ambient gate is off.
func TestTrackSelectionDefaults(t *testing.T) {
	m := New(config.Config{
		Volume: 0,
		Layers: map[string]bool{"beat15": false, "beat45": false},
	})
	tracks := m.Tracks()
	if len(tracks) != 2 {
		t.Fatalf("expected 2 tracks, got %d", len(tracks))
	}
	if tracks[0].ID != TrackBeat15 || tracks[1].ID != TrackBeat45 {
		t.Fatalf("unexpected track order: %+v", tracks)
	}
	if tracks[0].Enabled || tracks[1].Enabled {
		t.Fatal("beat layers should start off")
	}
}

func TestToggleTrackOverlap(t *testing.T) {
	m := New(config.Config{
		Volume: 0,
		Layers: map[string]bool{"beat15": false, "beat45": false},
	})
	// Enable both beat layers (overlap allowed).
	m.ToggleTrack(0)
	m.ToggleTrack(1)
	sel := m.Selection()
	if !sel[TrackBeat15] || !sel[TrackBeat45] {
		t.Fatalf("expected both beat layers enabled, got %+v", sel)
	}
	// Toggle the first off; the other stays on independently.
	m.ToggleTrack(0)
	sel = m.Selection()
	if sel[TrackBeat15] || !sel[TrackBeat45] {
		t.Fatalf("expected only 45 Hz enabled, got %+v", sel)
	}
}

func TestFolderTrackAddedWhenConfigured(t *testing.T) {
	m := New(config.Config{AmbientFolder: "/tmp/some-music-dir"})
	tracks := m.Tracks()
	if len(tracks) != 3 || tracks[2].ID != TrackFolder {
		t.Fatalf("expected a folder track, got %+v", tracks)
	}
}
