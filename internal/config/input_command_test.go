package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadInputCommand(t *testing.T) {
	configDirectory := t.TempDir()
	t.Setenv(herdrPluginConfigDirEnv, configDirectory)
	path := filepath.Join(configDirectory, "config.toml")

	if got, err := LoadInputCommand(); err != nil || got != "" {
		t.Fatalf("LoadInputCommand() without a file = %q, %v; want empty command", got, err)
	}

	if err := os.WriteFile(path, []byte("input_command = \"agent prompt\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadInputCommand()
	if err != nil {
		t.Fatalf("LoadInputCommand() error = %v", err)
	}
	if got != "agent prompt" {
		t.Errorf("LoadInputCommand() = %q, want %q", got, "agent prompt")
	}
}

func TestLoadInputCommandRejectsInvalidTOML(t *testing.T) {
	configDirectory := t.TempDir()
	t.Setenv(herdrPluginConfigDirEnv, configDirectory)
	if err := os.WriteFile(filepath.Join(configDirectory, "config.toml"), []byte("input_command = [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadInputCommand(); err == nil {
		t.Fatal("LoadInputCommand() error = nil, want TOML parse error")
	}
}
