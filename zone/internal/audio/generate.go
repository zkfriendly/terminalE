package audio

import (
	"math"
	"time"

	"github.com/gopxl/beep/v2"
)

// silentLoop returns an endless silent streamer, used as a harmless fallback when
// an ambient layer can't be built (e.g. an empty playlist folder).
func silentLoop() beep.Streamer {
	return beep.StreamerFunc(func(samples [][2]float64) (int, bool) {
		for i := range samples {
			samples[i] = [2]float64{}
		}
		return len(samples), true
	})
}

// binaural returns an endless stereo streamer playing carrier Hz in the left ear
// and carrier+beat Hz in the right, producing a perceived "beat" at beat Hz. With
// a low carrier this is a gentle hum used for focus entrainment (best with
// headphones). amp is 0..1.
func binaural(carrier, beat, amp float64) beep.Streamer {
	var phaseL, phaseR float64
	stepL := 2 * math.Pi * carrier / float64(sampleRate)
	stepR := 2 * math.Pi * (carrier + beat) / float64(sampleRate)
	return beep.StreamerFunc(func(samples [][2]float64) (int, bool) {
		for i := range samples {
			samples[i][0] = math.Sin(phaseL) * amp
			samples[i][1] = math.Sin(phaseR) * amp
			phaseL += stepL
			phaseR += stepR
			if phaseL > 2*math.Pi {
				phaseL -= 2 * math.Pi
			}
			if phaseR > 2*math.Pi {
				phaseR -= 2 * math.Pi
			}
		}
		return len(samples), true
	})
}

// tone returns a finite sine streamer with a short fade in/out envelope.
func tone(freq float64, dur time.Duration, amp float64) beep.Streamer {
	total := sampleRate.N(dur)
	i := 0
	return beep.StreamerFunc(func(samples [][2]float64) (int, bool) {
		if i >= total {
			return 0, false
		}
		n := 0
		for n < len(samples) && i < total {
			t := float64(i) / float64(sampleRate)
			v := math.Sin(2*math.Pi*freq*t) * amp * envelope(i, total)
			samples[n][0] = v
			samples[n][1] = v
			i++
			n++
		}
		return n, true
	})
}

// silence returns a finite stretch of quiet, used to space chime notes.
func silence(dur time.Duration) beep.Streamer {
	total := sampleRate.N(dur)
	i := 0
	return beep.StreamerFunc(func(samples [][2]float64) (int, bool) {
		if i >= total {
			return 0, false
		}
		n := 0
		for n < len(samples) && i < total {
			samples[n] = [2]float64{}
			i++
			n++
		}
		return n, true
	})
}

// chime builds the note sequence for a transition.
func chime(c Chime) beep.Streamer {
	const amp = 0.28
	note := 150 * time.Millisecond
	gap := 40 * time.Millisecond

	// Frequencies (A4, C#5, E5, A5) — a major-ish arpeggio.
	a4, cs5, e5, a5 := 440.0, 554.37, 659.25, 880.0

	switch c {
	case ChimeBreak: // descending: wind down
		return beep.Seq(
			tone(e5, note, amp), silence(gap),
			tone(cs5, note, amp), silence(gap),
			tone(a4, note, amp),
		)
	case ChimeDone: // triumphant, longer
		long := 260 * time.Millisecond
		return beep.Seq(
			tone(a4, note, amp), silence(gap),
			tone(e5, note, amp), silence(gap),
			tone(a5, long, amp),
		)
	default: // ChimeWork — ascending: spin up
		return beep.Seq(
			tone(a4, note, amp), silence(gap),
			tone(cs5, note, amp), silence(gap),
			tone(e5, note, amp),
		)
	}
}

func envelope(i, total int) float64 {
	fade := total / 5
	if fade < 1 {
		fade = 1
	}
	switch {
	case i < fade:
		return float64(i) / float64(fade)
	case i > total-fade:
		return float64(total-i) / float64(fade)
	default:
		return 1
	}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
