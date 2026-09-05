package tui

import (
	"os"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/zkfriendly/zone/internal/config"
)

func isolateSettingsConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)
	if base, err := os.UserConfigDir(); err != nil || base != dir {
		t.Skip("settings persistence tests require an isolated user config directory")
	}
}

func selectSetting(t *testing.T, v *settingsView, label string) {
	t.Helper()
	for i, field := range v.fields {
		if field.label == label {
			v.cursor = i
			return
		}
	}
	t.Fatalf("setting %q not found", label)
}

func TestProviderSettingPersists(t *testing.T) {
	isolateSettingsConfig(t)
	app, _ := newTestApp(t)
	app.Update(gotoSettingsMsg{})
	v := app.settings
	v.cfg.LLMEnabled = true
	v.cfg.CodexModel = "codex-choice"
	v.cfg.LMStudioModel = "local-choice"
	selectSetting(t, v, "AI provider")
	v.updateNormal(tea.KeyPressMsg{Code: tea.KeyEnter})
	if v.prompt.IsFocused() {
		t.Fatal("provider should cycle without text input")
	}
	if v.cfg.LLMProvider != config.ProviderLocal {
		t.Fatalf("provider: %s", v.cfg.LLMProvider)
	}
	loaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LLMProvider != config.ProviderLocal || loaded.CodexModel != "codex-choice" || loaded.LMStudioModel != "local-choice" {
		t.Fatalf("settings not preserved: %+v", loaded)
	}
	v.updateNormal(tea.KeyPressMsg{Code: tea.KeySpace})
	loaded, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LLMProvider != config.ProviderCodex {
		t.Fatalf("switch back failed: %s", loaded.LLMProvider)
	}
}

func TestModelSettingsCanReturnToAuto(t *testing.T) {
	isolateSettingsConfig(t)
	app, _ := newTestApp(t)
	app.Update(gotoSettingsMsg{})
	v := app.settings
	for _, label := range []string{"Codex model", "Local LLM model"} {
		selectSetting(t, v, label)
		if err := v.applyInput("custom"); err != nil {
			t.Fatal(err)
		}
		v.startInput()
		v.prompt.SetValue("")
		v.updateInput(tea.KeyPressMsg{Code: tea.KeyEnter})
		if got := v.fieldValue(*v.currentField()); got != "" {
			t.Fatalf("%s not cleared: %q", label, got)
		}
		loaded, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		if label == "Codex model" && loaded.CodexModel != "" || label == "Local LLM model" && loaded.LMStudioModel != "" {
			t.Fatalf("auto selection not saved: %+v", loaded)
		}
	}
}
