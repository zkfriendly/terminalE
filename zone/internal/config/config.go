// Package config holds user-tunable settings for zone: the pomodoro session
// shape and the ambient audio behaviour. Settings persist as JSON next to the
// database in the user's config directory.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// AmbientMode selects what plays during work blocks.
const (
	AmbientOff    = "off"    // silence
	AmbientNoise  = "noise"  // built-in generated layers (focus beats)
	AmbientFolder = "folder" // loop audio files from a user folder ("study with me")
)

// Config is the full set of user preferences.
type Config struct {
	PrepareMinutes int             `json:"prepare_minutes"` // settle-in block before the first work block
	WorkMinutes    int             `json:"work_minutes"`
	BreakMinutes   int             `json:"break_minutes"`
	TotalMinutes   int             `json:"total_minutes"`
	AmbientMode    string          `json:"ambient_mode"`
	AmbientFolder  string          `json:"ambient_folder"`
	Volume         float64         `json:"volume"` // 0.0 (silent) .. 1.0 (full)
	ChimesEnabled  bool            `json:"chimes_enabled"`
	Layers         map[string]bool `json:"layers"` // ambient layer on/off (e.g. beat15, beat45)

	// Local LLM settings for auto-labeling session notes.
	// JSON names stay lm_studio_* so existing config files keep working.
	LMStudioEnabled bool   `json:"lm_studio_enabled"`
	LMStudioURL     string `json:"lm_studio_url"`
	LMStudioModel   string `json:"lm_studio_model"` // empty = local-model

	// ResumeNotesSessionID remembers which session had the notes browser open when
	// the app quit, so re-attaching can restore that overlay.
	ResumeNotesSessionID int64 `json:"resume_notes_session_id,omitempty"`
}

// Default returns a 3-min prepare + classic 50/10 over 4h session.
func Default() Config {
	return Config{
		PrepareMinutes: 3,
		WorkMinutes:    50,
		BreakMinutes:   10,
		TotalMinutes:   240,
		AmbientMode:    AmbientOff,
		AmbientFolder:  "",
		Volume:         0.6,
		ChimesEnabled:  true,
		Layers: map[string]bool{
			"beat15": false,
			"beat45": false,
		},
		LMStudioEnabled: true,
		LMStudioURL:     "http://127.0.0.1:11434",
	}
}

// Dir returns the zone config/data directory, creating it if needed.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "zone")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Path returns the absolute path to config.json.
func Path() (string, error) {
	return path()
}

// Load reads the config file, writing (and returning) defaults if absent.
func Load() (Config, error) {
	p, err := path()
	if err != nil {
		return Default(), err
	}
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		cfg := Default()
		_ = cfg.Save()
		return cfg, nil
	}
	if err != nil {
		return Default(), err
	}
	cfg := Default()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default(), err
	}
	cfg.normalize()
	return cfg, nil
}

// Save writes the config as pretty JSON.
func (c *Config) Save() error {
	c.normalize()
	p, err := path()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func (c *Config) normalize() {
	if c.PrepareMinutes < 0 {
		c.PrepareMinutes = 0
	}
	if c.WorkMinutes <= 0 {
		c.WorkMinutes = 50
	}
	if c.BreakMinutes < 0 {
		c.BreakMinutes = 10
	}
	if c.TotalMinutes <= 0 {
		c.TotalMinutes = c.WorkMinutes + c.BreakMinutes
	}
	if c.Volume < 0 {
		c.Volume = 0
	}
	if c.Volume > 1 {
		c.Volume = 1
	}
	if c.AmbientMode == "" {
		c.AmbientMode = AmbientOff
	}
	if c.Layers == nil {
		c.Layers = map[string]bool{"beat15": false, "beat45": false}
	}
	if c.LMStudioURL == "" {
		c.LMStudioURL = "http://127.0.0.1:11434"
	} else {
		c.LMStudioURL = normalizeLocalLLMURL(c.LMStudioURL)
	}
}

func normalizeLocalLLMURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "://") {
		return raw
	}
	if isDigits(raw) {
		return "http://127.0.0.1:" + raw
	}
	return "http://" + raw
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// PrepareSec, WorkSec, BreakSec, TotalSec expose the session shape in seconds.
func (c Config) PrepareSec() int { return c.PrepareMinutes * 60 }
func (c Config) WorkSec() int    { return c.WorkMinutes * 60 }
func (c Config) BreakSec() int   { return c.BreakMinutes * 60 }
func (c Config) TotalSec() int   { return c.TotalMinutes * 60 }
