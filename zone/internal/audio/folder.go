package audio

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/flac"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/vorbis"
	"github.com/gopxl/beep/v2/wav"
)

// playlist is an endless streamer that loops audio files in a folder, decoding
// and resampling each to the speaker sample rate on the fly.
type playlist struct {
	mu    sync.Mutex
	files []string
	idx   int

	cur    beep.StreamSeekCloser
	stream beep.Streamer // possibly resampled view of cur
	open   *os.File
}

// newPlaylist scans dir for supported audio files. Returns nil if none are found.
func newPlaylist(dir string) *playlist {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".mp3", ".wav", ".flac", ".ogg":
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	if len(files) == 0 {
		return nil
	}
	sort.Strings(files)
	return &playlist{files: files}
}

// Stream implements beep.Streamer, filling samples and looping forever.
func (p *playlist) Stream(samples [][2]float64) (int, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	filled := 0
	attempts := 0
	for filled < len(samples) {
		if p.stream == nil {
			if attempts > len(p.files) {
				// Nothing decodable; emit silence to keep the stream alive.
				for i := filled; i < len(samples); i++ {
					samples[i] = [2]float64{}
				}
				return len(samples), true
			}
			attempts++
			p.openCurrent()
			continue
		}
		n, ok := p.stream.Stream(samples[filled:])
		filled += n
		if !ok || n == 0 {
			p.closeCurrent()
			p.idx = (p.idx + 1) % len(p.files)
		} else {
			attempts = 0
		}
	}
	return filled, true
}

// Err implements beep.Streamer.
func (p *playlist) Err() error { return nil }

func (p *playlist) openCurrent() {
	path := p.files[p.idx]
	f, err := os.Open(path)
	if err != nil {
		p.idx = (p.idx + 1) % len(p.files)
		return
	}

	var (
		dec    beep.StreamSeekCloser
		format beep.Format
		decErr error
	)
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		dec, format, decErr = mp3.Decode(f)
	case ".wav":
		dec, format, decErr = wav.Decode(f)
	case ".flac":
		dec, format, decErr = flac.Decode(f)
	case ".ogg":
		dec, format, decErr = vorbis.Decode(f)
	}
	if decErr != nil {
		f.Close()
		p.idx = (p.idx + 1) % len(p.files)
		return
	}

	p.open = f
	p.cur = dec
	if format.SampleRate != sampleRate {
		p.stream = beep.Resample(4, format.SampleRate, sampleRate, dec)
	} else {
		p.stream = dec
	}
}

func (p *playlist) closeCurrent() {
	if p.cur != nil {
		p.cur.Close()
		p.cur = nil
	}
	if p.open != nil {
		p.open.Close()
		p.open = nil
	}
	p.stream = nil
}
