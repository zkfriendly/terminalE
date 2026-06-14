// Package config holds user-tunable settings for zone: the pomodoro session
// shape and the ambient audio behaviour. Settings persist as JSON next to the
// database in the user's config directory.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
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
}

// Default returns a 3-min prepare + classic 50/10 over 4h session.
func Default() Config {
	return Config{
		PrepareMinutes: 3,
		WorkMinutes:    50,
		BreakMinutes:   10,
		TotalMinutes:   240,
		AmbientMode:    AmbientNoise,
		AmbientFolder:  "",
		Volume:         0.6,
		ChimesEnabled:  true,
		Layers: map[string]bool{
			"beat15": false,
			"beat45": false,
		},
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
func (c Config) Save() error {
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
		c.AmbientMode = AmbientNoise
	}
	if c.Layers == nil {
		c.Layers = map[string]bool{"beat15": false, "beat45": false}
	}
}

// PrepareSec, WorkSec, BreakSec, TotalSec expose the session shape in seconds.
func (c Config) PrepareSec() int { return c.PrepareMinutes * 60 }
func (c Config) WorkSec() int    { return c.WorkMinutes * 60 }
func (c Config) BreakSec() int   { return c.BreakMinutes * 60 }
func (c Config) TotalSec() int   { return c.TotalMinutes * 60 }
